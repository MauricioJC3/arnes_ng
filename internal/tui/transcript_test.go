package tui

import (
	"strings"
	"testing"
)

func newTranscript() *transcript {
	t := &transcript{styles: DefaultTheme().Styles(), mdStyle: "dark"}
	t.resize(80, 10)
	return t
}

func TestTranscriptCommitLiveMovesStreamedText(t *testing.T) {
	tr := newTranscript()
	tr.appendDelta("hola ")
	tr.appendDelta("mundo")
	if tr.live != "hola mundo" {
		t.Fatalf("live = %q", tr.live)
	}

	tr.commitLive()
	if tr.live != "" {
		t.Fatalf("live debería quedar vacío tras commit, quedó %q", tr.live)
	}
	if n := len(tr.entries); n != 1 || tr.entries[0].kind != kindAssistant {
		t.Fatalf("commitLive no dejó una entry assistant: %+v", tr.entries)
	}
	if strings.Contains(tr.entries[0].text, "hola mundo") == false {
		t.Fatalf("la entry no tiene el texto en vuelo: %q", tr.entries[0].text)
	}

	tr.commitLive() // sin texto en vuelo: no-op
	if len(tr.entries) != 1 {
		t.Fatalf("commitLive sin live agregó una entry: %+v", tr.entries)
	}
}

func TestTranscriptAssistantEntryIsMarkdownRendered(t *testing.T) {
	tr := newTranscript()
	raw := "# Título\n\n- uno\n- dos\n"
	tr.add(kindAssistant, raw)

	got := tr.entries[len(tr.entries)-1]
	if got.rendered == "" || got.rendered == raw {
		t.Fatalf("glamour no transformó el markdown: %q", got.rendered)
	}

	tr.add(kindInfo, "# esto no es markdown")
	if info := tr.entries[len(tr.entries)-1]; info.rendered != "" {
		t.Fatalf("una entry info no debería pre-renderizarse: %q", info.rendered)
	}
}

func TestTranscriptMarkdownRendererCachesPerWidth(t *testing.T) {
	tr := newTranscript()
	tr.add(kindAssistant, "hola")
	first := tr.md
	if first == nil {
		t.Fatal("no se creó el renderer")
	}

	tr.add(kindAssistant, "otra vez")
	if tr.md != first {
		t.Fatal("el renderer se recreó sin cambiar el ancho")
	}

	tr.resize(120, 10)
	tr.add(kindAssistant, "más ancho")
	if tr.md == first {
		t.Fatal("cambió el ancho pero el renderer no se recreó")
	}
}

func TestTranscriptAddKeepsScrollWhenNotAtBottom(t *testing.T) {
	tr := newTranscript()
	for i := 0; i < 60; i++ {
		tr.add(kindInfo, "relleno")
	}
	if !tr.vp.AtBottom() {
		t.Fatal("debería arrancar al fondo")
	}

	tr.vp.GotoTop()
	tr.add(kindInfo, "llega algo nuevo mientras leo arriba")
	if tr.vp.AtBottom() {
		t.Fatal("una entry info no debería tirar el scroll al fondo mientras se lee historia")
	}

	tr.add(kindUser, "pero un mensaje del usuario sí")
	if !tr.vp.AtBottom() {
		t.Fatal("un mensaje del usuario debería saltar al fondo")
	}
}

func TestTranscriptReflowRerendersAssistantEntries(t *testing.T) {
	tr := newTranscript()
	tr.add(kindAssistant, "# hola\n\ntexto")
	before := tr.entries[0].rendered

	tr.resize(40, 10)
	tr.reflow()
	if tr.entries[0].rendered == before {
		t.Fatal("reflow no re-renderizó la entry al ancho nuevo")
	}
}
