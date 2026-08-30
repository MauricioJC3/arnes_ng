package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

// fastBackoff shrinks the retry wait for the duration of a test.
func fastBackoff(t *testing.T) {
	t.Helper()
	base, capD := providerRetryBase, providerRetryCap
	providerRetryBase, providerRetryCap = time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { providerRetryBase, providerRetryCap = base, capD })
}

func TestCallProviderRetries(t *testing.T) {
	ctx := context.Background()

	t.Run("reintenta un error transitorio y termina bien", func(t *testing.T) {
		fastBackoff(t)
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			if n < 3 {
				return provider.Response{}, errors.New("openai_compat: HTTP 503")
			}
			return provider.Response{Text: "ok", StopReason: provider.StopEndTurn}, nil
		}
		var warns int
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(5),
			WithWarnFn(func(error) { warns++ }))

		out, err := a.Run(ctx, "hola")
		if err != nil {
			t.Fatalf("debería recuperarse: %v", err)
		}
		if out != "ok" || n != 3 {
			t.Fatalf("out=%q n=%d, quiero ok/3", out, n)
		}
		if warns != 2 {
			t.Fatalf("avisos = %d, quiero 2 (uno por reintento)", warns)
		}
	})

	t.Run("un error no transitorio no se reintenta", func(t *testing.T) {
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			return provider.Response{}, errors.New("openai_compat: HTTP 400: bad request")
		}
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(5))

		_, err := a.Run(ctx, "hola")
		if err == nil || !strings.Contains(err.Error(), "400") {
			t.Fatalf("esperaba el 400 propagado, tengo: %v", err)
		}
		if n != 1 {
			t.Fatalf("llamadas = %d, quiero 1 (sin reintento)", n)
		}
	})

	t.Run("se rinde tras agotar los reintentos", func(t *testing.T) {
		fastBackoff(t)
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			return provider.Response{}, errors.New("openai_compat: sin respuesta tras 4 intentos: HTTP 503")
		}
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(5), WithProviderRetries(2))

		_, err := a.Run(ctx, "hola")
		if err == nil || !strings.Contains(err.Error(), "provider:") {
			t.Fatalf("esperaba el error del provider propagado, tengo: %v", err)
		}
		if n != 3 {
			t.Fatalf("llamadas = %d, quiero 3 (1 + 2 reintentos)", n)
		}
	})

	t.Run("WithProviderRetries(0) desactiva el reintento", func(t *testing.T) {
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			return provider.Response{}, errors.New("openai_compat: HTTP 503")
		}
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(5), WithProviderRetries(0))

		if _, err := a.Run(ctx, "hola"); err == nil {
			t.Fatal("esperaba error")
		}
		if n != 1 {
			t.Fatalf("llamadas = %d, quiero 1", n)
		}
	})

	t.Run("un contexto cancelado corta el reintento", func(t *testing.T) {
		cctx, cancel := context.WithCancel(context.Background())
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			cancel() // the user hits Ctrl+C while the first call is failing
			return provider.Response{}, errors.New("openai_compat: HTTP 503")
		}
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(5))

		_, err := a.Run(cctx, "hola")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("esperaba context.Canceled, tengo: %v", err)
		}
		if n != 1 {
			t.Fatalf("llamadas = %d, quiero 1 (no reintenta tras cancelar)", n)
		}
	})
}
