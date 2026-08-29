package provider

import (
	"math"
	"testing"
)

func TestPrice(t *testing.T) {
	if in, out, ok := Price("claude-opus-5"); !ok || in != 5 || out != 25 {
		t.Fatalf("opus-5 = %v/%v ok=%v", in, out, ok)
	}
	if in, out, ok := Price("  DeepSeek-V4-Flash  "); !ok || in != 0.44 || out != 1.32 {
		t.Fatalf("deepseek-v4-flash (con espacios/mayúsculas) = %v/%v ok=%v", in, out, ok)
	}
	if _, _, ok := Price("modelo-fantasma"); ok {
		t.Fatal("un modelo desconocido no debería tener precio")
	}
}

func TestCost(t *testing.T) {
	// 1M in + 1M out en opus-5 = 5 + 25 = 30 USD
	usd, ok := Cost("claude-opus-5", 1_000_000, 1_000_000)
	if !ok || math.Abs(usd-30) > 1e-9 {
		t.Fatalf("usd=%v ok=%v", usd, ok)
	}
	if _, ok := Cost("fantasma", 100, 100); ok {
		t.Fatal("sin precio no hay costo")
	}
}

func TestEffectiveInputTokens(t *testing.T) {
	// sin caché: pasa derecho
	if got := (Usage{InputTokens: 500}).EffectiveInputTokens(); got != 500 {
		t.Fatalf("sin caché = %d, quiero 500", got)
	}
	// cache read a 0.1x, cache write (5 min) a 1.25x, sumados al input fresco
	u := Usage{InputTokens: 100, CacheReadInputTokens: 10_000, CacheCreationInputTokens: 400}
	// 100 + 10000/10 + 400*5/4 = 100 + 1000 + 500 = 1600
	if got := u.EffectiveInputTokens(); got != 1600 {
		t.Fatalf("ponderado = %d, quiero 1600", got)
	}
	// el costo sale exacto porque Cost es lineal y la caché se factura a tarifa de input
	usd, ok := Cost("claude-opus-5", u.EffectiveInputTokens(), 0)
	if !ok || math.Abs(usd-(1600.0/1e6*5)) > 1e-12 {
		t.Fatalf("usd=%v ok=%v", usd, ok)
	}
}
