package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/MauricioJC3/arnes_ng/internal/command"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

// maxTodoRows caps how many checklist rows the panel shows; the rest collapse
// into a "+N más" line.
const maxTodoRows = 8

// footerRows is the height reserved for the input / approval / status area
// (plus the task panel when there is one).
func (m Model) footerRows() int {
	rows := m.ta.Height() + 2 /* border */ + 2 /* status */
	if n := m.visibleTodos(); n > 0 {
		rows += n + 1 /* header */ + 2 /* border */
	}
	return rows
}

// visibleTodos is how many checklist rows the panel will render (0 when the
// panel is disabled or empty).
func (m Model) visibleTodos() int {
	n := len(m.todoItems)
	if m.todos == nil || n == 0 {
		return 0
	}
	if n > maxTodoRows {
		return maxTodoRows
	}
	return n
}

// relayout recomputes component sizes for the current terminal size.
func (m *Model) relayout() {
	vpHeight := m.height - m.footerRows() - 1
	if vpHeight < 3 {
		vpHeight = 3
	}
	if !m.ready {
		m.vp = viewport.New(m.width, vpHeight)
		m.ready = true
	} else {
		m.vp.Width = m.width
		m.vp.Height = vpHeight
	}
	m.ta.SetWidth(m.width - 2)
	m.vp.SetContent(m.renderTranscript())
	m.vp.GotoBottom()
}

// renderTranscript styles every entry (plus any in-flight streamed text). Each
// entry is wrapped to the viewport width individually -- assistant entries are
// already wrapped by glamour, so they are left as-is.
func (m Model) renderTranscript() string {
	w := m.vp.Width
	if w <= 0 {
		w = m.width
	}
	if w <= 0 {
		w = 80
	}

	lines := make([]string, 0, len(m.entries)+1)
	for _, e := range m.entries {
		lines = append(lines, m.renderEntry(e, w))
	}
	if m.live != "" {
		lines = append(lines, wrapTo(m.styles.Assistant.Render(m.live), w))
	}
	return strings.Join(lines, "\n\n")
}

func (m Model) renderEntry(e entry, w int) string {
	switch e.kind {
	case kindUser:
		return wrapTo(m.styles.Accent.Render("▌")+" "+m.styles.User.Render(e.text), w)
	case kindAssistant:
		if e.rendered != "" {
			return e.rendered // glamour output, already wrapped
		}
		return wrapTo(m.styles.Assistant.Render(e.text), w)
	case kindError:
		return wrapTo(m.styles.Error.Render("✗ "+e.text), w)
	default: // kindInfo
		return wrapTo(m.styles.Muted.Render(e.text), w)
	}
}

func wrapTo(s string, w int) string {
	if w <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(w).Render(s)
}

// View satisfies tea.Model.
func (m Model) View() string {
	if m.quitting {
		return "chau.\n"
	}
	if !m.ready {
		return "iniciando…\n"
	}

	var foot string
	switch {
	case m.connect != nil:
		foot = m.formBox(m.connect.view(m.styles), m.theme.Accent)
	case m.model != nil:
		foot = m.formBox(m.model.view(m.styles), m.theme.Accent)
	case m.pending != nil:
		foot = m.approvalBox()
	case m.busy:
		foot = m.inputBox(m.styles.Muted.Render(m.sp.View() + " pensando…   " + m.styles.Muted.Render("Ctrl+C para cancelar")))
	default:
		box := m.inputBox(m.ta.View())
		if m.menu.open {
			box = m.menuView() + "\n" + box
		}
		foot = box
	}

	parts := []string{m.vp.View(), m.statusBar()}
	if p := m.todoPanel(); p != "" {
		parts = append(parts, p)
	}
	parts = append(parts, foot)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// todoPanel renders the current task checklist as a bordered box. It returns ""
// when there is nothing to show.
func (m Model) todoPanel() string {
	n := m.visibleTodos()
	if n == 0 {
		return ""
	}
	done := 0
	for _, it := range m.todoItems {
		if it.Status == todo.Done {
			done++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n",
		m.styles.Accent.Render("tareas"),
		m.styles.Muted.Render(fmt.Sprintf("%d/%d", done, len(m.todoItems))))

	truncated := len(m.todoItems) > n
	shown := n
	if truncated {
		shown = n - 1
	}
	for _, it := range m.todoItems[:shown] {
		b.WriteString(m.todoLine(it))
		b.WriteByte('\n')
	}
	if truncated {
		b.WriteString(m.styles.Muted.Render(fmt.Sprintf("  … +%d más", len(m.todoItems)-shown)))
		b.WriteByte('\n')
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.Border)).
		Width(m.boxWidth()).
		Render(strings.TrimRight(b.String(), "\n"))
}

func (m Model) todoLine(it todo.Item) string {
	switch it.Status {
	case todo.Done:
		return m.styles.Success.Render("  ✔ ") + m.styles.Muted.Render(it.Content)
	case todo.InProgress:
		return m.styles.Accent.Render("  ▶ ") + m.styles.User.Render(it.Content)
	default:
		return m.styles.Muted.Render("  ☐ " + it.Content)
	}
}

// menuView renders the "/…" autocomplete list.
func (m Model) menuView() string {
	items := m.menu.items
	if len(items) > maxMenuRows {
		items = items[:maxMenuRows]
	}
	var b strings.Builder
	for i, c := range items {
		label := c.Name
		if c.Args != "" {
			label += " " + c.Args
		}
		if i == m.menu.idx {
			b.WriteString(m.styles.Accent.Render("❯ ") + m.styles.User.Render(label) +
				"  " + m.styles.Muted.Render(c.Short))
		} else {
			b.WriteString("  " + m.styles.Muted.Render(label))
		}
		b.WriteByte('\n')
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.Border)).
		Width(m.boxWidth()).
		Render(strings.TrimRight(b.String(), "\n"))
}

// formBox wraps a multi-step form (e.g. /connect) in a bordered box.
func (m Model) formBox(body, borderColor string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(m.boxWidth()).
		Render(body)
}

func (m Model) inputBox(body string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.Border)).
		Width(m.boxWidth()).
		Render(body)
}

func (m Model) approvalBox() string {
	r := m.pending
	inner := fmt.Sprintf("%s  permiso para  %s\n%s\n\n%s   %s",
		m.styles.Accent.Render("⚠"),
		m.styles.User.Render(r.Call.Name),
		m.styles.Muted.Render(oneLine(string(r.Call.Input), m.boxWidth()-4)),
		m.styles.Success.Render("[y] permitir"),
		m.styles.Error.Render("[n] denegar"),
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.Accent)).
		Padding(0, 1).
		Width(m.boxWidth()).
		Render(inner)
}

func (m Model) statusBar() string {
	seg := []string{m.prov().Model()}

	if md, ok := m.conv.(command.Modes); ok {
		switch md.Mode() {
		case "auto":
			seg = append(seg, m.styles.Error.Render("⚡ auto"))
		case "plan":
			seg = append(seg, m.styles.Tool.Render("◔ plan"))
		}
	}
	if m.sessionID != nil {
		if id := m.sessionID(); id != "" {
			seg = append(seg, "◈ "+shortID(id))
		}
	}
	if m.stats != nil {
		seg = append(seg, "~"+humanTokens(m.stats())+" ctx")
	}
	if m.cost != nil {
		if c := m.cost(); c != "" {
			seg = append(seg, m.styles.Success.Render(c))
		}
	}
	if m.goalIter > 0 {
		seg = append(seg, m.styles.Tool.Render(fmt.Sprintf("⟳ objetivo %d/%d", m.goalIter, m.goalMax)))
	}
	if m.busy {
		seg = append(seg, m.sp.View()+" trabajando")
	} else if !m.vp.AtBottom() {
		seg = append(seg, "▼ ↑↓/PgUp para ver más")
	}
	if m.quitHint {
		seg = append(seg, m.styles.Error.Render("Esc de nuevo para salir"))
	}

	rule := m.styles.Border.Render(strings.Repeat("─", max(m.width, 1)))
	line := m.styles.Muted.Render(" "+strings.Join(seg, "  ·  ")) + m.styles.Muted.Render("   shift+tab: modo")
	return rule + "\n" + line
}

func (m Model) boxWidth() int {
	if m.width < 4 {
		return 2
	}
	return m.width - 2
}

// --- small helpers -------------------------------------------------------------

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	r := []rune(s)
	if max > 1 && len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func shortID(id string) string {
	if len(id) > 13 {
		return id[:13]
	}
	return id
}

func humanTokens(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%dt", n)
	case n < 100_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%dk", n/1000)
	}
}
