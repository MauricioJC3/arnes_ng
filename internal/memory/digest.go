package memory

import (
	"fmt"
	"strings"
)

// digestNoteChars caps how much of a single note goes into the digest.
const digestNoteChars = 240

// Digest renders the store's most recent notes as a compact block to prepend to
// the system prompt, so a fresh session (or a model switch) starts with the
// project's accumulated context. It returns "" when there are no notes.
func Digest(store Store, maxNotes int) string {
	if store == nil || maxNotes <= 0 {
		return ""
	}
	notes, err := store.All()
	if err != nil || len(notes) == 0 {
		return ""
	}
	if len(notes) > maxNotes {
		notes = notes[:maxNotes]
	}

	var b strings.Builder
	b.WriteString("## Memoria del proyecto\n\n")
	b.WriteString("Datos guardados en sesiones anteriores (usá recall para buscar más):\n")
	for _, n := range notes {
		text := strings.ReplaceAll(strings.TrimSpace(n.Text), "\n", " ")
		if len(text) > digestNoteChars {
			text = text[:digestNoteChars-1] + "…"
		}
		fmt.Fprintf(&b, "- [%s] %s", n.CreatedAt.Format("2006-01-02"), text)
		if len(n.Tags) > 0 {
			fmt.Fprintf(&b, " (tags: %s)", strings.Join(n.Tags, ", "))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
