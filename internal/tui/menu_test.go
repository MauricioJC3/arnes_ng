package tui

import (
	"strings"
	"testing"
)

func TestCommandMenu(t *testing.T) {
	var mn commandMenu

	t.Run("abre con '/' y lista todo", func(t *testing.T) {
		mn.update("/")
		if !mn.open || len(mn.items) < 5 {
			t.Fatalf("open=%v items=%d", mn.open, len(mn.items))
		}
	})

	t.Run("filtra por prefijo", func(t *testing.T) {
		mn.update("/co")
		if !mn.open {
			t.Fatal("debería seguir abierto")
		}
		for _, it := range mn.items {
			if !strings.HasPrefix(it.Name, "/co") {
				t.Fatalf("filtró mal: %q", it.Name)
			}
		}
	})

	t.Run("se cierra al escribir un espacio", func(t *testing.T) {
		mn.update("/connect ")
		if mn.open {
			t.Fatal("no debería estar abierto con argumentos")
		}
	})

	t.Run("se cierra sin coincidencias", func(t *testing.T) {
		mn.update("/zzz")
		if mn.open {
			t.Fatal("no debería abrir sin matches")
		}
	})

	t.Run("move rota y respeta límites", func(t *testing.T) {
		mn.update("/")
		n := len(mn.items)
		mn.move(-1)
		if mn.idx != n-1 {
			t.Fatalf("idx tras move(-1) = %d, quiero %d", mn.idx, n-1)
		}
		mn.move(1)
		if mn.idx != 0 {
			t.Fatalf("idx tras move(1) = %d, quiero 0", mn.idx)
		}
	})

	t.Run("selected devuelve el item resaltado", func(t *testing.T) {
		mn.update("/re")
		s, ok := mn.selected()
		if !ok || s.Name != "/resume" {
			t.Fatalf("selected = %+v %v", s, ok)
		}
	})
}
