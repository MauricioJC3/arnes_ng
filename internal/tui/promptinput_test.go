package tui

import "testing"

func newPI() promptInput {
	p := newPromptInput(DefaultTheme().Styles())
	p.history = []string{"uno", "dos", "tres"}
	p.histAt = len(p.history) // not mid-recall
	return p
}

func TestPromptInputOnUpLadder(t *testing.T) {
	// at the bottom, empty input, with history -> starts a recall (newest first)
	p := newPI()
	if got := p.onUp(true); got != navConsumed {
		t.Fatalf("onUp al fondo con historial = %v, quería navConsumed", got)
	}
	if p.ta.Value() != "tres" {
		t.Fatalf("no recuperó el más nuevo: %q", p.ta.Value())
	}
	// mid-recall -> keeps walking back
	if got := p.onUp(true); got != navConsumed || p.ta.Value() != "dos" {
		t.Fatalf("segundo ↑ = %v / %q", got, p.ta.Value())
	}

	// non-empty input, not mid-recall -> textarea handles the key (cursor move)
	p = newPI()
	p.ta.SetValue("escribiendo algo")
	if got := p.onUp(true); got != navTextarea {
		t.Fatalf("onUp con texto en el input = %v, quería navTextarea", got)
	}

	// empty input but transcript scrolled up -> scroll it, don't touch history
	p = newPI()
	if got := p.onUp(false); got != navScrollUp {
		t.Fatalf("onUp leyendo scrollback = %v, quería navScrollUp", got)
	}
	if p.ta.Value() != "" {
		t.Fatalf("onUp leyendo scrollback tocó el input: %q", p.ta.Value())
	}

	// empty input, at the bottom, no history -> scroll
	p = newPromptInput(DefaultTheme().Styles())
	if got := p.onUp(true); got != navScrollUp {
		t.Fatalf("onUp sin historial = %v, quería navScrollUp", got)
	}
}

func TestPromptInputOnDownLadder(t *testing.T) {
	// mid-recall -> walk toward the newest, then restore the draft past the end
	p := newPI()
	p.onUp(true) // -> "tres"
	p.onUp(true) // -> "dos"
	if got := p.onDown(true); got != navConsumed || p.ta.Value() != "tres" {
		t.Fatalf("onDown mid-recall = %v / %q", got, p.ta.Value())
	}

	// at the bottom, empty input, not mid-recall -> defers to the textarea
	p = newPI()
	if got := p.onDown(true); got != navTextarea {
		t.Fatalf("onDown al fondo = %v, quería navTextarea", got)
	}

	// transcript scrolled up -> scroll down
	p = newPI()
	if got := p.onDown(false); got != navScrollDown {
		t.Fatalf("onDown leyendo scrollback = %v, quería navScrollDown", got)
	}
}

func TestPromptInputRememberSkipsConsecutiveDups(t *testing.T) {
	p := newPromptInput(DefaultTheme().Styles())
	p.remember("igual")
	p.remember("igual")
	p.remember("distinto")
	p.remember("igual")
	if len(p.history) != 3 {
		t.Fatalf("history = %v, esperaba 3 (sin duplicados consecutivos)", p.history)
	}
	if p.histAt != len(p.history) || p.draft != "" {
		t.Fatalf("remember no reseteó el cursor de recall: histAt=%d draft=%q", p.histAt, p.draft)
	}
}

func TestPromptInputDetachHistory(t *testing.T) {
	p := newPI()
	p.onUp(true) // entra en recall (histAt < len)
	if p.histAt == len(p.history) {
		t.Fatal("debería estar en recall")
	}
	p.detachHistory()
	if p.histAt != len(p.history) {
		t.Fatalf("detachHistory no salió del recall: histAt=%d", p.histAt)
	}
}
