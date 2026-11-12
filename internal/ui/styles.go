package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Fixed mid-tone colors. They read on both light and dark backgrounds, so the
// program does not depend on background detection, which guesses wrong over
// SSH and in some multiplexers. Body text uses the terminal's own foreground.
var (
	textC   lipgloss.TerminalColor = lipgloss.NoColor{}
	accentC lipgloss.TerminalColor = lipgloss.Color("#00AFD7")
	greenC  lipgloss.TerminalColor = lipgloss.Color("#2EBD59")
	amberC  lipgloss.TerminalColor = lipgloss.Color("#E0A100")
	redC    lipgloss.TerminalColor = lipgloss.Color("#E5484D")
	violetC lipgloss.TerminalColor = lipgloss.Color("#A371F7")
	orangeC lipgloss.TerminalColor = lipgloss.Color("#FF8C1A")
	dimC    lipgloss.TerminalColor = lipgloss.Color("#8B949E")
	lineC   lipgloss.TerminalColor = lipgloss.Color("#56606B")
	headC   lipgloss.TerminalColor = lipgloss.Color("#C9D1D9")
	keyC                           = accentC
	cyanC                          = accentC

	selFocus = lipgloss.NewStyle().Background(lipgloss.Color("#00AFD7")).Foreground(lipgloss.Color("#0B0F14")).Bold(true)
	selBlur  = lipgloss.NewStyle().Background(lipgloss.Color("#3A4452")).Foreground(lipgloss.Color("#F0F3F6"))
)

func paint(c lipgloss.TerminalColor) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

func bold(c lipgloss.TerminalColor) lipgloss.Style {
	return paint(c).Bold(true)
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "")
	if gap := width - ansi.StringWidth(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// padRight right-aligns s inside width.
func padLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "")
	if gap := width - ansi.StringWidth(s); gap > 0 {
		s = strings.Repeat(" ", gap) + s
	}
	return s
}

func fit(s string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = pad(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

func cell(s string, width int) string {
	return pad(clip(s, width), width)
}

func gap(n int) string {
	if n < 1 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func ansiWidth(s string) int {
	return ansi.StringWidth(s)
}

// box draws a rounded frame of exactly w×h with the title set into the top edge.
func box(title string, body []string, w, h int, focused bool) []string {
	if w < 4 || h < 2 {
		return grow(nil, h, w)
	}
	bc := lineC
	tc := headC
	if focused {
		bc = accentC
		tc = accentC
	}
	b := paint(bc)
	inner := w - 2
	t := ""
	if title != "" {
		t = " " + clip(title, inner-4) + " "
	}
	tw := ansiWidth(t)
	top := b.Render("╭─") + bold(tc).Render(t) + b.Render(strings.Repeat("─", max(0, inner-1-tw))+"╮")
	out := []string{top}
	side := b.Render("│")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		out = append(out, side+pad(line, inner)+side)
	}
	out = append(out, b.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return out
}

// overlay places box lines centered over base lines.
func overlay(base []string, top []string, width, height int) []string {
	base = grow(base, height, width)
	bw := 0
	for _, l := range top {
		if w := ansiWidth(l); w > bw {
			bw = w
		}
	}
	x := (width - bw) / 2
	y := (height - len(top)) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	for i, l := range top {
		row := y + i
		if row >= len(base) {
			break
		}
		left := ansi.Truncate(base[row], x, "")
		right := ansi.TruncateLeft(base[row], x+bw, "")
		base[row] = left + "\x1b[0m" + pad(l, bw) + "\x1b[0m" + right
	}
	return base
}

func grow(lines []string, h, width int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for i := range lines {
		lines[i] = pad(lines[i], width)
	}
	return lines
}

// column is one table column. Lower prio is dropped last when space runs out.
type column struct {
	title string
	min   int
	max   int // 0 means it may grow without limit when flex
	flex  bool
	right bool
	prio  int
}

// colWidths returns the width of each column; 0 means hidden.
func colWidths(cols []column, width int) []int {
	ws := make([]int, len(cols))
	shown := make([]bool, len(cols))
	for i := range cols {
		shown[i] = true
	}
	total := func() int {
		t, n := 0, 0
		for i, c := range cols {
			if shown[i] {
				t += c.min
				n++
			}
		}
		if n > 1 {
			t += n - 1
		}
		return t
	}
	for total() > width {
		drop, worst := -1, -1
		for i, c := range cols {
			if shown[i] && c.prio > worst && i != 0 {
				drop, worst = i, c.prio
			}
		}
		if drop < 0 {
			break
		}
		shown[drop] = false
	}
	extra := width - total()
	for i, c := range cols {
		if shown[i] {
			ws[i] = c.min
		}
	}
	// Grow flex columns evenly up to their max.
	for extra > 0 {
		grew := false
		for i, c := range cols {
			if !shown[i] || !c.flex || (c.max > 0 && ws[i] >= c.max) || extra == 0 {
				continue
			}
			ws[i]++
			extra--
			grew = true
		}
		if !grew {
			break
		}
	}
	return ws
}

type styledCell struct {
	text  string
	style lipgloss.Style
}

func renderRow(cols []column, ws []int, cells []styledCell, plain bool) string {
	var b strings.Builder
	first := true
	for i, c := range cols {
		if ws[i] == 0 {
			continue
		}
		if !first {
			b.WriteByte(' ')
		}
		first = false
		txt := ""
		var st lipgloss.Style
		if i < len(cells) {
			txt = cells[i].text
			st = cells[i].style
		}
		var s string
		if c.right {
			s = padLeft(clip(txt, ws[i]), ws[i])
		} else {
			s = cell(txt, ws[i])
		}
		if !plain {
			s = st.Render(s)
		}
		b.WriteString(s)
	}
	return b.String()
}

// table renders a header plus a scrolled window of rows into h lines.
// It returns the lines and the index of the first row shown.
func table(cols []column, rows [][]styledCell, cursor, w, h int, focused bool) ([]string, int) {
	ws := colWidths(cols, w)
	hdr := make([]styledCell, len(cols))
	for i, c := range cols {
		hdr[i] = styledCell{text: c.title, style: bold(headC)}
	}
	lines := []string{renderRow(cols, ws, hdr, false)}
	room := h - 1
	off := tableOffset(cursor, len(rows), room)
	for i := off; i < len(rows) && len(lines) < h; i++ {
		if i == cursor {
			st := selBlur
			if focused {
				st = selFocus
			}
			lines = append(lines, st.Render(pad(renderRow(cols, ws, rows[i], true), w)))
			continue
		}
		lines = append(lines, renderRow(cols, ws, rows[i], false))
	}
	return lines, off
}

func tableOffset(cursor, n, room int) int {
	if room < 1 || n <= room {
		return 0
	}
	off := 0
	if cursor >= room {
		off = cursor - room + 1
	}
	if off > n-room {
		off = n - room
	}
	return off
}
