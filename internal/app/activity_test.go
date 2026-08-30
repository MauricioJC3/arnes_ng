package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/checkpoint"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func call(name string, args map[string]any) provider.ToolCall {
	raw, _ := json.Marshal(args)
	return provider.ToolCall{ID: "1", Name: name, Input: raw}
}

func TestActivityLineFormatsCommonTools(t *testing.T) {
	tests := []struct {
		name string
		call provider.ToolCall
		want string
	}{
		{"bash single line", call("bash", map[string]any{"command": "go test ./..."}), "$ go test ./..."},
		{"bash multi line is collapsed", call("bash", map[string]any{"command": "cd x\nmake build"}), "$ cd x …"},
		{"write_file shows path", call("write_file", map[string]any{"path": "internal/x/y.go", "content": "..."}), "escribió internal/x/y.go"},
		{"edit_file shows path", call("edit_file", map[string]any{"path": "main.go"}), "editó main.go"},
		{"read_file shows path", call("read_file", map[string]any{"path": "README.md"}), "leyó README.md"},
		{"grep shows pattern", call("grep", map[string]any{"pattern": "func New"}), "grep func New"},
		{"todo_write is generic", call("todo_write", map[string]any{"todos": []any{}}), "actualizó la lista de tareas"},
		{"unknown tool falls back to name", call("some_mcp_tool", nil), "some_mcp_tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := activityLine(tt.call); got != tt.want {
				t.Fatalf("activityLine = %q, quería %q", got, tt.want)
			}
		})
	}
}

func TestActivityLineTruncatesGiantInput(t *testing.T) {
	got := activityLine(call("bash", map[string]any{"command": strings.Repeat("x", 500)}))
	if n := len([]rune(got)); n > activityMaxLen {
		t.Fatalf("línea de %d runas, tope %d", n, activityMaxLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("una línea truncada debería terminar en elipsis: %q", got)
	}
}

func TestEmitActivityIsNonBlockingWhenChannelFull(t *testing.T) {
	a := &App{activity: make(chan string, 1)}
	a.activity <- "ya lleno" // occupy the only slot

	done := make(chan struct{})
	go func() {
		a.emitActivity(call("bash", map[string]any{"command": "ls"}))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emitActivity bloqueó con el canal lleno en vez de descartar la línea")
	}
}

func TestEmitActivityNilChannelIsNoop(t *testing.T) {
	a := &App{} // no activity channel
	a.emitActivity(call("bash", map[string]any{"command": "ls"}))
}

func TestToolObserverFansOutToCheckpointsAndActivity(t *testing.T) {
	act := make(chan string, 1)

	onlyActivity := &App{activity: act}
	if onlyActivity.toolObserver() == nil {
		t.Fatal("con activity configurado debería haber un observer")
	}
	onlyActivity.toolObserver()(call("write_file", map[string]any{"path": "a.go"}))
	select {
	case got := <-act:
		if got != "escribió a.go" {
			t.Fatalf("observer emitió %q", got)
		}
	default:
		t.Fatal("el observer solo-activity no emitió la línea")
	}

	both := &App{checkpoints: checkpoint.NewStore(), activity: make(chan string, 1)}
	if both.toolObserver() == nil {
		t.Fatal("con checkpoints+activity debería haber un observer combinado")
	}

	bare := &App{}
	if bare.toolObserver() != nil {
		t.Fatal("sin checkpoints ni activity no debería haber observer")
	}
}
