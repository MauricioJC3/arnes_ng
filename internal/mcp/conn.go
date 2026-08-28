package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// jsonRPC message shapes (2.0).

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message) }

// conn is a synchronous newline-delimited JSON-RPC channel to one server.
// Only one request is in flight at a time (the harness makes blocking calls).
type conn struct {
	w      io.Writer
	r      *bufio.Reader
	closer io.Closer // the process; nil in tests

	mu     sync.Mutex
	nextID int
}

func newConn(w io.Writer, r io.Reader, closer io.Closer) *conn {
	return &conn{w: w, r: bufio.NewReader(r), closer: closer, nextID: 1}
}

// spawn launches the configured server process and wires a conn to its pipes.
func spawn(ctx context.Context, cfg ServerConfig) (*conn, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = append(os.Environ(), envSlice(cfg.Env)...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return newConn(stdin, stdout, &procCloser{cmd: cmd, stdin: stdin}), nil
}

type procCloser struct {
	cmd   *exec.Cmd
	stdin io.Closer
}

func (p *procCloser) Close() error {
	_ = p.stdin.Close()
	_ = p.cmd.Process.Kill()
	return p.cmd.Wait()
}

func (c *conn) close() error {
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}

// call sends a request and returns the raw result, skipping notification and
// log lines until the response with the matching id arrives.
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++

	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	if err := c.writeJSON(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: raw}); err != nil {
		return nil, err
	}

	for {
		line, err := c.readLine(ctx)
		if err != nil {
			return nil, err
		}
		var resp rpcResponse
		if json.Unmarshal(line, &resp) != nil || resp.ID != id {
			continue // log output, notification, or a response we're not waiting on
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// notify sends a fire-and-forget notification (no id, no response expected).
func (c *conn) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	return c.writeJSON(rpcNotification{JSONRPC: "2.0", Method: method, Params: raw})
}

func (c *conn) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = c.w.Write(b)
	return err
}

// readLine reads one '\n'-terminated message, honoring ctx cancellation. The
// reader goroutine unblocks when the pipe closes (which close() forces).
func (c *conn) readLine(ctx context.Context) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := c.r.ReadBytes('\n')
		ch <- result{b, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.b, res.err
	}
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}
