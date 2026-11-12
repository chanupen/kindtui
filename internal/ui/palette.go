package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------- matching ----------

// matchActions filters the registry by a query. Substring hits on the name
// rank above fuzzy (in-order letters) hits on the whole text.
func matchActions(q string, runnable bool) []*action {
	q = strings.ToLower(strings.TrimSpace(q))
	type hit struct {
		a    *action
		rank int
		i    int
	}
	var hits []hit
	for i, a := range actions {
		if runnable && a.run == nil {
			continue
		}
		if q == "" {
			hits = append(hits, hit{a, 0, i})
			continue
		}
		name := strings.ToLower(a.id + " " + a.cmd + " " + strings.Join(a.aliases, " "))
		blob := strings.ToLower(name + " " + a.desc + " " + a.hint + " " + a.scope.String() + " " + strings.Join(a.keys, " "))
		switch {
		case strings.HasPrefix(name, q):
			hits = append(hits, hit{a, 0, i})
		case strings.Contains(name, q):
			hits = append(hits, hit{a, 1, i})
		case strings.Contains(blob, q):
			hits = append(hits, hit{a, 2, i})
		case subsequence(blob, q):
			hits = append(hits, hit{a, 3, i})
		}
	}
	// Stable by rank, then registry order.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && (hits[j].rank < hits[j-1].rank || hits[j].rank == hits[j-1].rank && hits[j].i < hits[j-1].i); j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	out := make([]*action, len(hits))
	for i, h := range hits {
		out[i] = h.a
	}
	return out
}

func subsequence(s, q string) bool {
	j := 0
	for i := 0; i < len(s) && j < len(q); i++ {
		if s[i] == q[j] {
			j++
		}
	}
	return j == len(q)
}

// run executes an action the way its key would, from any screen.
func (m Model) runAction(a *action, arg string) (tea.Model, tea.Cmd) {
	m.palette = false
	m.paletteInput.Blur()
	if m.screen == scrRef {
		m.screen = scrDash
	}
	if a.scope == scopeNode && m.focus != paneNodes && arg == "" {
		// Node actions from the palette or reference use the shown node.
		if n, ok := m.firstNode(); ok {
			if _, sel := m.selectedNode(); !sel {
				arg = n.Name
			}
		}
	}
	return a.run(m, arg)
}

// ---------- clipboard ----------

type copiedMsg struct {
	text string
	err  error
}

// copyCmd puts text on the system clipboard when a helper (wl-copy, xclip,
// pbcopy) exists, and always sends OSC 52 so it also works over SSH.
func copyCmd(text string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.WriteAll(text)
		seq := osc52.New(text)
		switch {
		case os.Getenv("TMUX") != "":
			seq = seq.Tmux()
		case strings.HasPrefix(os.Getenv("TERM"), "screen"):
			seq = seq.Screen()
		}
		if _, werr := seq.WriteTo(os.Stderr); werr == nil {
			err = nil
		}
		return copiedMsg{text: text, err: err}
	}
}

// ---------- command palette (ctrl-p) ----------

func (m Model) openPalette() (Model, tea.Cmd) {
	m.palette = true
	m.paletteCursor = 0
	m.paletteInput.SetValue("")
	m = m.blur()
	return m, m.paletteInput.Focus()
}

func (m Model) onPalette(s string, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := matchActions(m.paletteInput.Value(), true)
	switch s {
	case "esc", "ctrl+p":
		m.palette = false
		m.paletteInput.Blur()
		return m, nil
	case "up", "ctrl+k", "shift+tab":
		m.paletteCursor = clamp(m.paletteCursor-1, 0, len(items)-1)
		return m, nil
	case "down", "ctrl+j", "tab", "ctrl+n":
		m.paletteCursor = clamp(m.paletteCursor+1, 0, len(items)-1)
		return m, nil
	case "ctrl+y":
		if m.paletteCursor < len(items) && items[m.paletteCursor].shell != nil {
			return m, copyCmd(items[m.paletteCursor].shell(m.target("")))
		}
		return m, nil
	case "enter":
		if m.paletteCursor >= len(items) {
			return m, nil
		}
		return m.runAction(items[m.paletteCursor], "")
	}
	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(k)
	m.paletteCursor = 0
	return m, cmd
}

func (m Model) paletteBox() []string {
	w := min(96, m.width-4)
	items := matchActions(m.paletteInput.Value(), true)
	rows := min(12, max(3, m.height-12))
	t := m.target("")
	body := []string{" " + bold(amberC).Render("› ") + m.paletteInput.View(), ""}
	off := tableOffset(m.paletteCursor, len(items), rows)
	for i := off; i < len(items) && i < off+rows; i++ {
		a := items[i]
		key := pad(a.keyLabel(), 8)
		scope := pad(a.scope.String(), 8)
		line := " " + key + " " + scope + " " + a.desc
		if i == m.paletteCursor {
			body = append(body, selFocus.Render(pad(line, w-2)))
			continue
		}
		body = append(body, " "+bold(keyC).Render(key)+" "+paint(dimC).Render(scope)+" "+a.desc)
	}
	if len(items) == 0 {
		body = append(body, " "+paint(dimC).Render("no matching action"))
	}
	for len(body) < rows+2 {
		body = append(body, "")
	}
	preview := ""
	if m.paletteCursor < len(items) && items[m.paletteCursor].shell != nil {
		preview = "$ " + items[m.paletteCursor].shell(t)
	}
	body = append(body, "", " "+paint(greenC).Render(clip(preview, w-4)),
		" "+paint(dimC).Render("enter run   ↑↓ select   ctrl-y copy command   esc close"))
	return box("Command palette", body, w, len(body)+2, true)
}

// ---------- command reference screen (K, :cmds) ----------

func (m Model) openRef() (Model, tea.Cmd) {
	m.screen = scrRef
	m.refCursor = 0
	m.refFiltering = false
	m.refInput.SetValue("")
	m = m.blur()
	return m, nil
}

func (m Model) onRef(s string, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := matchActions(m.refInput.Value(), false)
	if m.refFiltering {
		switch s {
		case "esc":
			m.refInput.SetValue("")
			m.refFiltering = false
			m.refInput.Blur()
			m.refCursor = 0
			return m, nil
		case "enter", "down", "up":
			m.refFiltering = false
			m.refInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.refInput, cmd = m.refInput.Update(k)
		m.refCursor = 0
		return m, cmd
	}
	page := max(1, m.geom().body.h-5)
	switch s {
	case "esc", "q", "K":
		if m.refInput.Value() != "" && s == "esc" {
			m.refInput.SetValue("")
			m.refCursor = 0
			return m, nil
		}
		m.screen = scrDash
		return m, nil
	case "/":
		m.refFiltering = true
		return m, m.refInput.Focus()
	case "up", "k":
		m.refCursor = clamp(m.refCursor-1, 0, len(items)-1)
	case "down", "j":
		m.refCursor = clamp(m.refCursor+1, 0, len(items)-1)
	case "pgup":
		m.refCursor = clamp(m.refCursor-page, 0, len(items)-1)
	case "pgdown", " ":
		m.refCursor = clamp(m.refCursor+page, 0, len(items)-1)
	case "g", "home":
		m.refCursor = 0
	case "G", "end":
		m.refCursor = max(0, len(items)-1)
	case "y", "c":
		if m.refCursor < len(items) && items[m.refCursor].shell != nil {
			return m, copyCmd(items[m.refCursor].shell(m.target("")))
		}
		return m.note("this action has no shell equivalent"), nil
	case "enter":
		if m.refCursor < len(items) && items[m.refCursor].run != nil {
			return m.runAction(items[m.refCursor], "")
		}
	case "?":
		m.help = true
	case "ctrl+p":
		return m.openPalette()
	}
	return m, nil
}

var refCols = []column{
	{title: "KEY", min: 7, prio: 1},
	{title: "COMMAND", min: 16, prio: 2},
	{title: "SCOPE", min: 7, prio: 3},
	{title: "ACTION", min: 26, prio: 0},
	{title: "EQUIVALENT SHELL COMMAND", min: 24, flex: true, prio: 0},
}

func (m Model) refView(w, h int) []string {
	t := m.target("")
	items := matchActions(m.refInput.Value(), false)
	rows := make([][]styledCell, len(items))
	for i, a := range items {
		sh := ""
		if a.shell != nil {
			sh = a.shell(t)
		}
		rows[i] = []styledCell{
			{a.keyLabel(), bold(keyC)},
			{a.cmdLabel(), paint(amberC)},
			{a.scope.String(), paint(dimC)},
			{a.desc, paint(textC)},
			{sh, paint(greenC)},
		}
	}
	var body []string
	if m.refFiltering || m.refInput.Value() != "" {
		body = append(body, " "+bold(amberC).Render("/ ")+m.refInput.View())
	}
	th := h - 2 - len(body) - 3
	tbl, _ := table(refCols, rows, m.refCursor, w-2, th, true)
	body = append(body, tbl...)
	for len(body) < h-5 {
		body = append(body, "")
	}
	desc, full := "", ""
	if m.refCursor < len(items) {
		a := items[m.refCursor]
		desc = a.desc
		if a.cmdLabel() != "" {
			desc = a.cmdLabel() + " — " + desc
		}
		if a.shell != nil {
			full = "$ " + a.shell(t)
		}
	}
	body = append(body, " "+bold(textC).Render(clip(desc, w-4)), " "+paint(greenC).Render(clip(full, w-4)),
		" "+paint(dimC).Render("enter run   y copy command   / filter   ctrl-p palette   esc back"))
	where := t.cluster
	if t.node != "<node>" {
		where += " › " + t.node
	}
	return box(fmt.Sprintf("Commands(%s)[%d]", where, len(items)), body, w, h, true)
}
