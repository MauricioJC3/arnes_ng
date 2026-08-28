package todo

import (
	"sync"
	"testing"
)

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{Pending, InProgress, Done} {
		if !s.Valid() {
			t.Errorf("%q debería ser válido", s)
		}
	}
	if Status("nope").Valid() {
		t.Error("status desconocido no debería ser válido")
	}
}

func TestStoreSetGetCounts(t *testing.T) {
	s := NewStore()
	if got := s.Get(); len(got) != 0 {
		t.Fatalf("store nuevo no está vacío: %v", got)
	}

	s.Set([]Item{
		{Content: "a", Status: Done},
		{Content: "b", Status: InProgress},
		{Content: "c", Status: Pending},
	})
	done, total := s.Counts()
	if done != 1 || total != 3 {
		t.Fatalf("counts = %d/%d, quiero 1/3", done, total)
	}

	// Get devuelve una copia: mutarla no toca el store.
	got := s.Get()
	got[0].Content = "mutado"
	if s.Get()[0].Content != "a" {
		t.Fatal("Get no devolvió una copia")
	}
}

func TestStoreOnChange(t *testing.T) {
	s := NewStore()
	var mu sync.Mutex
	var last []Item
	s.OnChange(func(items []Item) {
		mu.Lock()
		last = items
		mu.Unlock()
	})

	s.Set([]Item{{Content: "x", Status: Pending}})
	mu.Lock()
	defer mu.Unlock()
	if len(last) != 1 || last[0].Content != "x" {
		t.Fatalf("callback recibió %v", last)
	}

	s.OnChange(nil)
	s.Set([]Item{{Content: "y", Status: Done}}) // no debe panicar sin callback
}
