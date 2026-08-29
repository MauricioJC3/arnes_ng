package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/agent"
	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/checkpoint"
	"github.com/MauricioJC3/arnes_ng/internal/lsp"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/session"
	"github.com/MauricioJC3/arnes_ng/internal/skill"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

// These tests drive the WHOLE stack the way a real heavy session does: the real
// app kernel, the real streaming openai_compat provider, real tools, a real
// on-disk session store and checkpoints -- talking to a scripted OpenAI-style
// SSE server over real HTTP. No mock provider. This is the layer where the
// bugs that unit tests missed (loop budget, truncation, stream timeout, garbled
// body) actually live.

// ocTurn is one scripted assistant turn: the SSE data frames the server streams
// back, and an optional HTTP status to fail with instead (for retry tests).
type ocTurn struct {
	frames     []string
	failStatus int // if non-zero, respond with this status and no body
	delay      time.Duration
}

// ocServer is a scripted OpenAI-compatible /chat/completions SSE endpoint.
type ocServer struct {
	srv   *httptest.Server
	mu    sync.Mutex
	i     int
	reqs  int
	turns [][]ocTurn // turns[n] = the ordered attempts for the n-th model call
}

func newOCServer(t *testing.T, turns ...[]ocTurn) *ocServer {
	t.Helper()
	s := &ocServer{turns: turns}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs++
		if s.i >= len(s.turns) {
			s.mu.Unlock()
			http.Error(w, "no more scripted turns", http.StatusInternalServerError)
			return
		}
		attempts := s.turns[s.i]
		// pop the first attempt for this turn; advance the turn when none left
		attempt := attempts[0]
		if len(attempts) > 1 {
			s.turns[s.i] = attempts[1:]
		} else {
			s.i++
		}
		s.mu.Unlock()

		if attempt.delay > 0 {
			time.Sleep(attempt.delay)
		}
		if attempt.failStatus != 0 {
			w.WriteHeader(attempt.failStatus)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, f := range attempt.frames {
			fmt.Fprintf(w, "data: %s\n\n", f)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *ocServer) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqs
}

// textFrames streams `content` as one delta then finishes with `stop`.
func textFrames(content string) []string {
	return []string{
		`{"choices":[{"delta":{"content":` + jsonStr(content) + `},"finish_reason":null}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":8}}`,
		`[DONE]`,
	}
}

// toolCallFrames streams a text preamble then one complete tool call.
func toolCallFrames(preamble, id, name, argsJSON string) []string {
	return []string{
		`{"choices":[{"delta":{"content":` + jsonStr(preamble) + `},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":` + jsonStr(id) + `,"function":{"name":` + jsonStr(name) + `,"arguments":` + jsonStr(argsJSON) + `}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":30,"completion_tokens":12}}`,
		`[DONE]`,
	}
}

func jsonStr(s string) string {
	b := strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// newStreamingApp is newIntegrationApp with streaming on, a delta sink, and a
// real openai_compat provider pointed at baseURL.
func newStreamingApp(t *testing.T, baseURL string) (*App, *[]string, *sync.Mutex) {
	t.Helper()
	dir := t.TempDir()
	store, err := session.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mem, err := memory.NewFileStore(dir+"/mem.json", "test/proj")
	if err != nil {
		t.Fatal(err)
	}
	tools := BuildBaseTools(BaseToolDeps{
		Todos:  todo.NewStore(),
		LSPMgr: lsp.NewManager(lsp.Config{}, dir),
		Skills: skill.NewRegistry(),
		Mem:    mem,
	})
	prov := provider.NewOpenAICompat(provider.OpenAICompatConfig{
		BaseURL: baseURL, APIKey: "test-key", Model: "test-model",
	})

	deltas := make(chan string, 256)
	var mu sync.Mutex
	var got []string
	go func() {
		for d := range deltas {
			mu.Lock()
			got = append(got, d)
			mu.Unlock()
		}
	}()

	a := &App{
		providerName: "openai_compat",
		prov:         prov,
		cfgPath:      dir + "/config.json",
		store:        store,
		tools:        tools,
		baseApprover: approval.AllowAll{},
		mode:         ModeNormal,
		subagents:    subagent.NewRegistry(),
		checkpoints:  checkpoint.NewStore(),
		mem:          mem,
		streaming:    true,
		deltas:       deltas,
		maxSteps:     agent.DefaultMaxSteps,
		maxTokens:    agent.DefaultMaxTokens,
	}
	t.Cleanup(func() { close(deltas) })
	return a, &got, &mu
}

// TestE2EStreamingToolTurnPersistsAndResumes: a full two-round streaming turn
// (model streams a write_file call -> tool runs -> model streams the final
// answer), then a fresh app resumes the session and sees the whole history.
func TestE2EStreamingToolTurnPersistsAndResumes(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short: HTTP + disco reales")
	}
	target := filepath.Join(t.TempDir(), "hello.txt")
	args := `{"path":` + jsonStr(target) + `,"content":"hola mundo"}`

	oc := newOCServer(t,
		[]ocTurn{{frames: toolCallFrames("Voy a crear el archivo. ", "call_1", "write_file", args)}},
		[]ocTurn{{frames: textFrames("Listo, archivo creado.")}},
	)
	a, deltas, dmu := newStreamingApp(t, oc.srv.URL)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	sessID := a.sess.ID

	out, err := a.Run(context.Background(), "creá hello.txt con 'hola mundo'")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 1. final text
	if out != "Listo, archivo creado." {
		t.Fatalf("texto final = %q", out)
	}
	// 2. streaming deltas actually arrived (both rounds)
	dmu.Lock()
	joined := strings.Join(*deltas, "")
	dmu.Unlock()
	if !strings.Contains(joined, "Voy a crear el archivo.") || !strings.Contains(joined, "Listo, archivo creado.") {
		t.Fatalf("deltas incompletos: %q", joined)
	}
	// 3. the tool really ran
	if b, err := os.ReadFile(target); err != nil || string(b) != "hola mundo" {
		t.Fatalf("write_file no corrió: err=%v content=%q", err, b)
	}
	// 4. two model calls
	if oc.requests() != 2 {
		t.Fatalf("requests = %d, quiero 2", oc.requests())
	}
	// 5. usage accumulated from both rounds (prompt 30+20, completion 12+8)
	if in, outTok := a.SessionUsage(); in != 50 || outTok != 20 {
		t.Fatalf("SessionUsage = %d/%d, quiero 50/20", in, outTok)
	}
	// 6. persisted, and a fresh app resumes the full history
	a2, _, _ := newStreamingApp(t, oc.srv.URL)
	// share the same store dir as a
	a2.store = a.store
	if _, err := a2.ResumeSession(sessID); err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	h := a2.History()
	if len(h) < 4 {
		t.Fatalf("historial resumido incompleto: %d mensajes", len(h))
	}
	if in, outTok := a2.SessionUsage(); in != 50 || outTok != 20 {
		t.Fatalf("uso no sobrevivió al resume: %d/%d", in, outTok)
	}
}

// TestE2EStreamRetriesA500MidConversation: the second model call 500s once, the
// provider retries, and the turn completes -- no error surfaces to the user.
func TestE2EStreamRetriesA500MidConversation(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short")
	}
	target := filepath.Join(t.TempDir(), "x.txt")
	args := `{"path":` + jsonStr(target) + `,"content":"x"}`
	oc := newOCServer(t,
		[]ocTurn{{frames: toolCallFrames("ok ", "c1", "write_file", args)}},
		[]ocTurn{
			{failStatus: http.StatusBadGateway},
			{frames: textFrames("recuperado")},
		},
	)
	a, _, _ := newStreamingApp(t, oc.srv.URL)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	out, err := a.Run(context.Background(), "hacelo")
	if err != nil {
		t.Fatalf("un 500 transitorio no debería fallar el turno: %v", err)
	}
	if out != "recuperado" {
		t.Fatalf("out = %q", out)
	}
	if oc.requests() != 3 { // turn1 + turn2(500) + turn2(retry)
		t.Fatalf("requests = %d, quiero 3", oc.requests())
	}
}

// TestE2ECancelMidStreamKeepsPartial: cancelling the context mid-turn returns
// context.Canceled and does not corrupt the session.
func TestE2ECancelMidStreamKeepsPartial(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short")
	}
	oc := newOCServer(t,
		[]ocTurn{{frames: textFrames("no llego a terminar"), delay: 300 * time.Millisecond}},
	)
	a, _, _ := newStreamingApp(t, oc.srv.URL)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	_, err := a.Run(ctx, "dale")
	if err == nil {
		t.Fatal("esperaba error por cancelación")
	}
	if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("error inesperado: %v", err)
	}
	// la sesión sigue cargable
	if _, err := a.store.Load(a.sess.ID); err != nil {
		t.Fatalf("la sesión quedó corrupta tras cancelar: %v", err)
	}
}
