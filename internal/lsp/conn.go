// Package lsp is a minimal Language Server Protocol client: it launches a
// language server per file type and exposes diagnostics, go-to-definition and
// hover to the agent as a single tool.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message) }

// message is a JSON-RPC 2.0 frame. Requests carry Method+ID, notifications
// Method only, responses ID only.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// conn is an async LSP JSON-RPC channel over a server's stdio, framed with
// Content-Length headers. A single read loop demultiplexes responses to the
// waiting caller and hands notifications to onNotify.
type conn struct {
	w      io.Writer
	closer io.Closer

	wmu sync.Mutex // serializes frame writes

	mu      sync.Mutex
	nextID  int
	pending map[int]chan message

	onNotify func(method string, params json.RawMessage)

	closeOnce sync.Once
	closed    chan struct{}
}

func newConn(w io.Writer, r io.Reader, closer io.Closer, onNotify func(string, json.RawMessage)) *conn {
	c := &conn{
		w:        w,
		closer:   closer,
		nextID:   1,
		pending:  map[int]chan message{},
		onNotify: onNotify,
		closed:   make(chan struct{}),
	}
	go c.readLoop(bufio.NewReader(r))
	return c
}

// call sends a request and blocks until the matching response, ctx expiry, or
// the connection closing.
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	ch := make(chan message, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.write(message{JSONRPC: "2.0", ID: json.RawMessage(strconv.Itoa(id)), Method: method, Params: mustRaw(params)}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, fmt.Errorf("conexión LSP cerrada")
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// notify sends a fire-and-forget notification.
func (c *conn) notify(method string, params any) error {
	return c.write(message{JSONRPC: "2.0", Method: method, Params: mustRaw(params)})
}

func (c *conn) write(m message) error {
	m.JSONRPC = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

func (c *conn) readLoop(r *bufio.Reader) {
	defer c.markClosed()
	for {
		body, err := readFrame(r)
		if err != nil {
			return
		}
		var m message
		if json.Unmarshal(body, &m) != nil {
			continue
		}

		switch {
		case m.Method != "" && len(m.ID) > 0:
			// Server-to-client request: answer so the server does not stall. We
			// implement none of them, so reply with a null result.
			_ = c.write(message{JSONRPC: "2.0", ID: m.ID, Result: json.RawMessage("null")})
		case m.Method != "":
			if c.onNotify != nil {
				c.onNotify(m.Method, m.Params)
			}
		case len(m.ID) > 0:
			id, convErr := strconv.Atoi(strings.Trim(string(m.ID), `"`))
			if convErr != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[id]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
}

// readFrame reads one Content-Length-framed message body.
func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("Content-Length inválido: %q", v)
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("frame LSP sin Content-Length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (c *conn) markClosed() {
	c.closeOnce.Do(func() { close(c.closed) })
}

func (c *conn) close() error {
	c.markClosed()
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}

func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
