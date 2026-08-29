package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// These tests drive, on the full spine (App.Run -> agent loop -> tools ->
// session), the two failures a user hits on a demanding real project, and pin
// how arnes handles them now that the loop has a truncation nudge (A), a
// malformed-args guard (C) and a repeated-call breaker (E).
//
// The provider is a scripted mock: it stands in for "a model under load on a
// big task". The point is to watch what arnes itself does at that boundary.

// writeProjectFiles drops n small Go files in a fresh temp dir and returns their
// absolute paths -- a stand-in for "a project big enough that the model walks it
// file by file".
func writeProjectFiles(t *testing.T, n int) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("mod%02d.go", i))
		body := fmt.Sprintf("package proj\n\n// file %d\nfunc F%d() int { return %d }\n", i, i, i)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// TestStressModelNeverStopsAndHitsStepBudget: the model reads a different
// project file every turn (real work, never a repeat) and never returns a final
// answer. The turn must stop on the step-budget circuit breaker after exactly
// agent.DefaultMaxSteps provider calls, keep the partial text, and still persist
// the session so the user can say "seguí".
func TestStressModelNeverStopsAndHitsStepBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short: recorre 50 vueltas del loop de agente")
	}

	files := writeProjectFiles(t, 50)

	var calls int
	prov := &provider.MockProvider{
		Handler: func(req provider.Request) (provider.Response, error) {
			calls++
			in, _ := json.Marshal(map[string]string{"path": files[calls-1]})
			return provider.Response{
				Text: fmt.Sprintf("sigo revisando el proyecto (paso %d)...", calls),
				ToolCalls: []provider.ToolCall{
					{ID: fmt.Sprintf("c%d", calls), Name: "read_file", Input: in},
				},
				StopReason: provider.StopToolUse,
				Usage:      provider.Usage{InputTokens: 100, OutputTokens: 20},
			}, nil
		},
	}

	a := newIntegrationApp(t, prov)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	sessID := a.sess.ID

	out, err := a.Run(context.Background(), "revisá todo el proyecto y hacé un resumen")

	if err == nil {
		t.Fatal("esperaba el corte por tope de pasos, no hubo error")
	}
	t.Logf("error devuelto al usuario:\n  %v", err)
	if !strings.Contains(err.Error(), "me detuve tras 50 pasos") {
		t.Fatalf("el error no es el corte del circuit breaker: %v", err)
	}
	if calls != 50 {
		t.Fatalf("el loop no respetó el presupuesto: %d llamadas al provider (esperaba 50)", calls)
	}
	// La salida parcial se conserva (commit 614d310: "keep partial output").
	if !strings.Contains(out, "sigo revisando el proyecto") {
		t.Fatalf("se perdió el texto parcial del turno: %q", out)
	}
	// La sesión quedó guardada con el historial parcial: "seguí" puede continuar.
	saved, err := a.store.Load(sessID)
	if err != nil {
		t.Fatalf("la sesión no se persistió: %v", err)
	}
	if len(saved.Messages) < 100 {
		t.Fatalf("historial parcial no persistido (%d mensajes)", len(saved.Messages))
	}
}

// TestStressTruncatedToolCallArgsBreakOutOfTheLoop: every turn the model emits a
// write_file call whose JSON arguments are cut off (a provider that hit
// max_tokens mid-write). arnes must NOT dispatch it: the malformed-args guard
// feeds the model a clear "tu respuesta se cortó" error, and when the model
// keeps re-issuing the identical call the repeat breaker cuts the turn at the
// 3rd try instead of grinding through all 50 steps.
func TestStressTruncatedToolCallArgsBreakOutOfTheLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short")
	}

	target := filepath.Join(t.TempDir(), "big_generated.go")
	truncated := json.RawMessage(
		`{"path":"` + target + `","content":"package main\n\nfunc main() {\n\tprintln(\"`)

	var calls int
	prov := &provider.MockProvider{
		Handler: func(req provider.Request) (provider.Response, error) {
			calls++
			return provider.Response{
				ToolCalls: []provider.ToolCall{
					{ID: fmt.Sprintf("w%d", calls), Name: "write_file", Input: truncated},
				},
				StopReason: provider.StopToolUse,
				Usage:      provider.Usage{InputTokens: 100, OutputTokens: 4096},
			}, nil
		},
	}

	a := newIntegrationApp(t, prov)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	_, err := a.Run(context.Background(), "generá el archivo grande")

	if err == nil || !strings.Contains(err.Error(), `repitió la misma llamada a "write_file" 3 veces`) {
		t.Fatalf("esperaba el corte del guardia de repetición, err = %v", err)
	}
	t.Logf("error devuelto al usuario:\n  %v", err)
	// El turno se cortó temprano, no consumió el presupuesto entero.
	if calls > 4 {
		t.Fatalf("el guardia de repetición no cortó a tiempo: %d llamadas al provider", calls)
	}
	// El resultado realimentado al modelo es el aviso de args truncados, no un
	// "unexpected end of JSON input" críptico, y la tool nunca corrió.
	var sawClearError bool
	for _, c := range mock(prov).Calls {
		for _, m := range c.Messages {
			for _, tr := range m.ToolResults {
				if tr.IsError && strings.Contains(tr.Content, "mal formados") {
					sawClearError = true
					t.Logf("resultado que recibe el modelo:\n  %s", tr.Content)
				}
			}
		}
	}
	if !sawClearError {
		t.Fatal("no se realimentó el aviso claro de argumentos truncados")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("write_file no debería haber escrito nada, stat err = %v", statErr)
	}
}
