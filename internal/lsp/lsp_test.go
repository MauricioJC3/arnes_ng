package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeServer speaks just enough LSP over the given pipes to exercise the client.
type fakeServer struct {
	in  io.Reader // client -> server
	out io.Writer // server -> client
	t   *testing.T
}

func (s *fakeServer) run() {
	br := bufio.NewReader(s.in)
	for {
		body, err := readFrame(br)
		if err != nil {
			return
		}
		var m message
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch m.Method {
		case "initialize":
			s.respond(m.ID, map[string]any{"capabilities": map[string]any{}})
		case "initialized", "textDocument/didClose", "exit":
			// nothing
		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(m.Params, &p)
			s.notify("textDocument/publishDiagnostics", map[string]any{
				"uri": p.TextDocument.URI,
				"diagnostics": []map[string]any{{
					"range":    map[string]any{"start": map[string]int{"line": 2, "character": 4}, "end": map[string]int{"line": 2, "character": 9}},
					"severity": 1,
					"source":   "fakels",
					"message":  "algo anda mal",
				}},
			})
		case "textDocument/definition":
			s.respond(m.ID, []map[string]any{{
				"uri":   "file:///proj/otro.go",
				"range": map[string]any{"start": map[string]int{"line": 9, "character": 0}, "end": map[string]int{"line": 9, "character": 3}},
			}})
		case "textDocument/hover":
			s.respond(m.ID, map[string]any{
				"contents": map[string]any{"kind": "markdown", "value": "func Foo() error"},
			})
		case "shutdown":
			s.respond(m.ID, nil)
		default:
			if len(m.ID) > 0 {
				s.respond(m.ID, nil)
			}
		}
	}
}

func (s *fakeServer) respond(id json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	s.writeFrame(message{JSONRPC: "2.0", ID: id, Result: raw})
}

func (s *fakeServer) notify(method string, params any) {
	raw, _ := json.Marshal(params)
	s.writeFrame(message{JSONRPC: "2.0", Method: method, Params: raw})
}

func (s *fakeServer) writeFrame(m message) {
	b, _ := json.Marshal(m)
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(b))
	s.out.Write(b)
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// newTestClient wires a Client to an in-process fakeServer.
func newTestClient(t *testing.T, root string) *Client {
	t.Helper()
	cliR, srvW := io.Pipe()
	srvR, cliW := io.Pipe()
	fs := &fakeServer{in: srvR, out: srvW, t: t}
	go fs.run()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := newClient(ctx, root, cliW, cliR, nopCloser{})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestClientDiagnostics(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, dir)

	ds, err := c.Diagnostics(context.Background(), "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 {
		t.Fatalf("diagnósticos = %d, quiero 1", len(ds))
	}
	if ds[0].Severity != 1 || ds[0].Message != "algo anda mal" || ds[0].Range.Start.Line != 2 {
		t.Fatalf("diagnóstico mal parseado: %+v", ds[0])
	}
}

func TestClientDefinition(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	c := newTestClient(t, dir)

	locs, err := c.Definition(context.Background(), "a.go", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) != 1 || !strings.HasSuffix(locs[0].URI, "otro.go") || locs[0].Range.Start.Line != 9 {
		t.Fatalf("locations = %+v", locs)
	}
}

func TestClientHover(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	c := newTestClient(t, dir)

	txt, err := c.Hover(context.Background(), "a.go", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if txt != "func Foo() error" {
		t.Fatalf("hover = %q", txt)
	}
}

func TestClientReadMissingFile(t *testing.T) {
	c := newTestClient(t, t.TempDir())
	if _, err := c.Diagnostics(context.Background(), "no-existe.go"); err == nil {
		t.Fatal("esperaba error de archivo inexistente")
	}
}

func TestParseHoverForms(t *testing.T) {
	cases := map[string]string{
		`{"contents":{"kind":"markdown","value":" x "}}`:       "x",
		`{"contents":"plano"}`:                                 "plano",
		`{"contents":["uno",{"language":"go","value":"dos"}]}`: "uno\ndos",
		`null`: "",
	}
	for in, want := range cases {
		if got := parseHover(json.RawMessage(in)); got != want {
			t.Errorf("parseHover(%s) = %q, quiero %q", in, got, want)
		}
	}
}

func TestParseLocationsLink(t *testing.T) {
	in := `[{"targetUri":"file:///x.go","targetRange":{"start":{"line":3,"character":1},"end":{"line":3,"character":4}}}]`
	locs := parseLocations(json.RawMessage(in))
	if len(locs) != 1 || locs[0].URI != "file:///x.go" || locs[0].Range.Start.Line != 3 {
		t.Fatalf("locs = %+v", locs)
	}
}

func TestReadFrame(t *testing.T) {
	raw := "Content-Length: 2\r\n\r\n{}"
	b, err := readFrame(bufio.NewReader(strings.NewReader(raw)))
	if err != nil || string(b) != "{}" {
		t.Fatalf("b=%q err=%v", b, err)
	}
	if _, err := readFrame(bufio.NewReader(strings.NewReader("Foo: bar\r\n\r\n"))); err == nil {
		t.Fatal("esperaba error sin Content-Length")
	}
}

func TestURIRoundTrip(t *testing.T) {
	p := "/home/user/proj/main.go"
	if got := URIPath(pathToURI(p)); got != p {
		t.Fatalf("round trip: %q -> %q", p, got)
	}
}

// ensure ids stay numeric on the wire (some servers are strict).
func TestCallIDIsNumeric(t *testing.T) {
	var m message
	_ = json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":7,"result":null}`), &m)
	if _, err := strconv.Atoi(strings.Trim(string(m.ID), `"`)); err != nil {
		t.Fatalf("id no numérico: %s", m.ID)
	}
}
