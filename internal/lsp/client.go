package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// diagWindow is how long Diagnostics waits for the server to publish results
// after the file is opened, plus a short grace to catch a follow-up push.
const (
	diagWindow = 2500 * time.Millisecond
	diagGrace  = 300 * time.Millisecond
)

// Position is a zero-based line/character, as LSP uses on the wire.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a start/end pair.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Diagnostic is one problem the server reported for a file.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// Location is a file + range, for go-to-definition results.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Client is a running language server plus the client-side bookkeeping.
type Client struct {
	conn *conn
	root string

	mu      sync.Mutex
	version int
	diags   map[string][]Diagnostic // latest diagnostics per document URI
	waiters map[string][]chan struct{}
}

// Start launches cmd (command+args) and completes the LSP initialize handshake
// with root as the workspace root.
func Start(ctx context.Context, root string, command string, args []string) (*Client, error) {
	proc, w, r, err := spawn(ctx, command, args)
	if err != nil {
		return nil, err
	}
	return newClient(ctx, root, w, r, proc)
}

// newClient wires a Client to an already-open server stream and runs the
// initialize handshake. Split from Start so tests can drive it with pipes.
func newClient(ctx context.Context, root string, w io.Writer, r io.Reader, closer io.Closer) (*Client, error) {
	c := &Client{
		root:    root,
		diags:   map[string][]Diagnostic{},
		waiters: map[string][]chan struct{}{},
	}
	c.conn = newConn(w, r, closer, c.onNotify)

	initParams := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   pathToURI(root),
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{},
				"synchronization":    map[string]any{},
			},
		},
		"clientInfo": map[string]any{"name": "arnes"},
	}
	if _, err := c.conn.call(ctx, "initialize", initParams); err != nil {
		c.conn.close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if err := c.conn.notify("initialized", map[string]any{}); err != nil {
		c.conn.close()
		return nil, err
	}
	return c, nil
}

// Close shuts the server down.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = c.conn.call(ctx, "shutdown", nil)
	_ = c.conn.notify("exit", nil)
	return c.conn.close()
}

func (c *Client) onNotify(method string, params json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	c.mu.Lock()
	c.diags[p.URI] = p.Diagnostics
	waiters := c.waiters[p.URI]
	delete(c.waiters, p.URI)
	c.mu.Unlock()
	for _, w := range waiters {
		close(w)
	}
}

// Diagnostics opens path in the server, waits briefly for it to publish, and
// returns the problems it reported.
func (c *Client) Diagnostics(ctx context.Context, path string) ([]Diagnostic, error) {
	abs, uri, text, err := c.read(path)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	delete(c.diags, uri)
	wait := make(chan struct{})
	c.waiters[uri] = append(c.waiters[uri], wait)
	c.mu.Unlock()

	if err := c.didOpen(abs, uri, text); err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	select {
	case <-wait:
	case <-time.After(diagWindow):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	time.Sleep(diagGrace) // let a follow-up push land

	c.mu.Lock()
	out := append([]Diagnostic(nil), c.diags[uri]...)
	c.mu.Unlock()
	return out, nil
}

// Definition returns the locations that define the symbol at (line, character),
// both zero-based.
func (c *Client) Definition(ctx context.Context, path string, line, character int) ([]Location, error) {
	abs, uri, text, err := c.read(path)
	if err != nil {
		return nil, err
	}
	if err := c.didOpen(abs, uri, text); err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	res, err := c.conn.call(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     Position{Line: line, Character: character},
	})
	if err != nil {
		return nil, err
	}
	return parseLocations(res), nil
}

// Hover returns the server's hover text for (line, character), both zero-based.
func (c *Client) Hover(ctx context.Context, path string, line, character int) (string, error) {
	abs, uri, text, err := c.read(path)
	if err != nil {
		return "", err
	}
	if err := c.didOpen(abs, uri, text); err != nil {
		return "", err
	}
	defer c.didClose(uri)

	res, err := c.conn.call(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     Position{Line: line, Character: character},
	})
	if err != nil {
		return "", err
	}
	return parseHover(res), nil
}

// --- document sync -----------------------------------------------------------

func (c *Client) didOpen(abs, uri, text string) error {
	c.mu.Lock()
	c.version++
	v := c.version
	c.mu.Unlock()
	return c.conn.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": languageID(abs),
			"version":    v,
			"text":       text,
		},
	})
}

func (c *Client) didClose(uri string) {
	_ = c.conn.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

// read resolves path against the workspace root and returns its absolute path,
// file URI and contents.
func (c *Client) read(path string) (abs, uri, text string, err error) {
	abs = path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(c.root, path)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", "", "", fmt.Errorf("no se pudo leer %s: %w", path, err)
	}
	return abs, pathToURI(abs), string(data), nil
}

// --- result parsing --------------------------------------------------------

func parseLocations(res json.RawMessage) []Location {
	if len(res) == 0 || string(res) == "null" {
		return nil
	}
	// Location
	var one Location
	if json.Unmarshal(res, &one) == nil && one.URI != "" {
		return []Location{one}
	}
	// []Location
	var many []Location
	if json.Unmarshal(res, &many) == nil && len(many) > 0 && many[0].URI != "" {
		return many
	}
	// []LocationLink
	var links []struct {
		TargetURI   string `json:"targetUri"`
		TargetRange Range  `json:"targetRange"`
	}
	if json.Unmarshal(res, &links) == nil {
		out := make([]Location, 0, len(links))
		for _, l := range links {
			if l.TargetURI != "" {
				out = append(out, Location{URI: l.TargetURI, Range: l.TargetRange})
			}
		}
		return out
	}
	return nil
}

func parseHover(res json.RawMessage) string {
	if len(res) == 0 || string(res) == "null" {
		return ""
	}
	var h struct {
		Contents json.RawMessage `json:"contents"`
	}
	if json.Unmarshal(res, &h) != nil {
		return ""
	}
	// MarkupContent {kind, value}
	var mc struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(h.Contents, &mc) == nil && mc.Value != "" {
		return strings.TrimSpace(mc.Value)
	}
	// plain string
	var s string
	if json.Unmarshal(h.Contents, &s) == nil && s != "" {
		return strings.TrimSpace(s)
	}
	// []MarkedString ({language, value} or string)
	var arr []json.RawMessage
	if json.Unmarshal(h.Contents, &arr) == nil {
		var parts []string
		for _, item := range arr {
			var ms struct {
				Value string `json:"value"`
			}
			if json.Unmarshal(item, &ms) == nil && ms.Value != "" {
				parts = append(parts, ms.Value)
				continue
			}
			var str string
			if json.Unmarshal(item, &str) == nil && str != "" {
				parts = append(parts, str)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}
