package tui

import (
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/command"
)

// maxMenuRows caps how many suggestions the popup shows.
const maxMenuRows = 8

// commandMenu is the "/…" autocomplete popup shown above the input.
type commandMenu struct {
	open  bool
	items []command.Spec
	idx   int
}

// update recomputes the menu from the current input line. It opens only while
// the input is a single "/token" with no space yet.
func (mn *commandMenu) update(input string) {
	if !strings.HasPrefix(input, "/") || strings.ContainsAny(input, " \n\t") {
		*mn = commandMenu{}
		return
	}

	var matched []command.Spec
	for _, c := range command.Commands() {
		if strings.HasPrefix(c.Name, input) {
			matched = append(matched, c)
		}
	}
	mn.items = matched
	mn.open = len(matched) > 0
	if mn.idx >= len(matched) {
		mn.idx = 0
	}
}

func (mn *commandMenu) move(delta int) {
	if !mn.open || len(mn.items) == 0 {
		return
	}
	n := len(mn.items)
	mn.idx = (mn.idx + delta%n + n) % n
}

// visibleWindow is the [top, end) slice of matches to render so the highlighted
// row stays on screen when more than maxMenuRows commands match.
func (mn *commandMenu) visibleWindow() (top, end int) {
	return listWindow(mn.idx, len(mn.items), maxMenuRows)
}

func (mn *commandMenu) selected() (command.Spec, bool) {
	if !mn.open || mn.idx >= len(mn.items) {
		return command.Spec{}, false
	}
	return mn.items[mn.idx], true
}
