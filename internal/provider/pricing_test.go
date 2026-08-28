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
