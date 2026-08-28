package hook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFile(t *testing.T) {
	t.Run("archivo ausente = config vacía", func(t *testing.T) {
		c, err := LoadFile(filepath.Join(t.TempDir(), "nope.json"))
		if err != nil || !c.Empty() {
			t.Fatalf("c=%+v err=%v", c, err)
		}
	})

	t.Run("match inválido es error", func(t *testing.T) {
		_, err := LoadFile(write(t, `{"pre_tool":[{"match":"[","command":"true"}]}`))
		if err == nil || !strings.Contains(err.Error(), "match") {
			t.Fatalf("esperaba error de regex, tengo: %v", err)
		}
	})

	t.Run("command vacío es error", func(t *testing.T) {
		_, err := LoadFile(write(t, `{"post_tool":[{"match":"x"}]}`))
		if err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func call(name, input string) provider.ToolCall {
	return provider.ToolCall{ID: "c1", Name: name, Input: json.RawMessage(input)}
}

func TestPreTool(t *testing.T) {
	ctx := context.Background()

	t.Run("hook que sale != 0 y bloquea devuelve error", func(t *testing.T) {
		c, err := LoadFile(write(t, `{"pre_tool":[{"match":"bash","command":"echo nope >&2; exit 1"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		err = New(c, time.Second).PreTool(ctx, call("bash", `{}`))
		if err == nil || !strings.Contains(err.Error(), "nope") {
			t.Fatalf("esperaba bloqueo con la salida del hook, tengo: %v", err)
		}
	})

	t.Run("block:false solo advierte, no bloquea", func(t *testing.T) {
		c, err := LoadFile(write(t, `{"pre_tool":[{"match":"bash","command":"exit 1","block":false}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := New(c, time.Second).PreTool(ctx, call("bash", `{}`)); err != nil {
			t.Fatalf("no debería bloquear: %v", err)
		}
	})

	t.Run("el hook no matchea la tool: no corre", func(t *testing.T) {
		c, err := LoadFile(write(t, `{"pre_tool":[{"match":"^bash$","command":"exit 1"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := New(c, time.Second).PreTool(ctx, call("read_file", `{}`)); err != nil {
			t.Fatalf("no debería correr para read_file: %v", err)
		}
	})

	t.Run("recibe el JSON de la tool-call en stdin", func(t *testing.T) {
		c, err := LoadFile(write(t, `{"pre_tool":[{"command":"grep -q '\"tool\":\"bash\"' && exit 3"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		err = New(c, time.Second).PreTool(ctx, call("bash", `{"cmd":"ls"}`))
		if err == nil {
			t.Fatal("esperaba que el hook viera el payload y saliera != 0")
		}
	})
}

func TestPostTool(t *testing.T) {
	ctx := context.Background()

	c, err := LoadFile(write(t, `{"post_tool":[{"match":"edit_file","command":"echo formateado"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	note := New(c, time.Second).PostTool(ctx, call("edit_file", `{}`), "editado x", false)
	if !strings.Contains(note, "formateado") {
		t.Fatalf("nota = %q", note)
	}

	// Otra tool: sin nota.
	if n := New(c, time.Second).PostTool(ctx, call("bash", `{}`), "", false); n != "" {
		t.Fatalf("no debería haber nota para bash: %q", n)
	}
}

func TestRunTimeout(t *testing.T) {
	c, err := LoadFile(write(t, `{"pre_tool":[{"command":"sleep 10"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = New(c, 100*time.Millisecond).PreTool(context.Background(), call("bash", `{}`))
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("esperaba timeout, tengo: %v", err)
	}
	// The hook sleeps 10s; a working 100ms timeout plus the 2s WaitDelay returns
	// in ~2s. The bound is loose so a slow CI runner under -race doesn't flake,
	// but still well under the 10s sleep.
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("el timeout no cortó a tiempo (%s)", elapsed)
	}
}
