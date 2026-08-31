package tui

import (
	"fmt"
	"strings"

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

// visibleTodos is how many checklist rows the panel will render. It is 0 when
// the panel is disabled, the list is empty, or every item is done -- a finished
// checklist retires itself instead of lingering on screen.
func (m Model) visibleTodos() int {
	n := len(m.todoItems)
	if m.todos == nil || n == 0 || allTodosDone(m.todoItems) {
		return 0
	}
	if n > maxTodoRows {
		return maxTodoRows
	}
	return n
}

// allTodosDone reports whether a non-empty checklist has every item completed.
func allTodosDone(items []todo.Item) bool {
	for _, it := range items {
		if it.Status != todo.Done {
			return false
		}
	}
	return len(items) > 0
}

// relayout recomputes component sizes for the current terminal size.
func (m *Model) relayout() {
	vpHeight := m.height - m.footerRows() - 1
	if vpHeight < 3 {
		vpHeight = 3
	}
	m.transcript.resize(m.width, vpHeight)
	m.ta.SetWidth(m.width - 2)
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
	switch m.state() {
	case stateConnectForm:
		foot = m.formBox(m.connect.view(m.styles, m.modelRows()), m.theme.Accent)
	case stateModelForm:
		foot = m.formBox(m.model.view(m.styles, m.modelRows()), m.theme.Accent)
	case stateSessionForm:
		foot = m.formBox(m.session.view(m.styles, m.sessionRows()), m.theme.Accent)
	case stateApproval:
		foot = m.approvalBox()
	case stateBusy:
		// The prompt stays visible so the user can type ahead; Enter queues it.
		// The "working / N queued" indicator lives in the status bar.
		foot = m.inputBox(m.ta.View())
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
	top, end := m.menu.visibleWindow()
	var b strings.Builder
	if top > 0 {
		b.WriteString("  " + m.styles.Muted.Render(fmt.Sprintf("↑ %d más", top)) + "\n")
	}
	for i := top; i < end; i++ {
		c := items[i]
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
	if end < len(items) {
		b.WriteString("  " + m.styles.Muted.Render(fmt.Sprintf("↓ %d más", len(items)-end)) + "\n")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.Border)).
		Width(m.boxWidth()).
		Render(strings.TrimRight(b.String(), "\n"))
}

// formBox wraps a multi-step form (e.g. /connect) in a bordered box.
// modelRows is how many model entries the picker shows at once, scaled to the
// terminal height with room left for the box border, title, notes and hints.
// The list scrolls inside this window instead of overflowing the screen.
func (m Model) modelRows() int {
	n := m.height - 12
	if n < 6 {
		return 6
	}
	if n > 16 {
		return 16
	}
	return n
}

// sessionRows is how many entries the /sessions picker shows at once. Each
// entry is two lines, so it gets roughly half the model picker's budget; the
// list scrolls inside that window instead of overflowing the screen.
func (m Model) sessionRows() int {
	n := (m.height - 12) / 2
	if n < 4 {
		return 4
	}
	if n > 10 {
		return 10
	}
	return n
}

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
	if m.mouseOn {
		seg = append(seg, "🖱 scroll (Ctrl+O: copiar)")
	}
	if m.busy {
		work := m.sp.View() + " trabajando"
		if n := len(m.queued); n > 0 {
			work += fmt.Sprintf(" · %d en cola", n)
		}
		seg = append(seg, work)
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
