package ui

import (
	"strings"
)

type hint struct {
	key  string
	desc string
}

// headerHints are the keys for whatever has focus. The header and the compact
// footer both render this list, so they cannot drift apart.
func (m Model) headerHints() []hint {
	if m.palette {
		return []hint{{"enter", "run"}, {"↑↓", "select"}, {"ctrl-y", "copy"}, {"esc", "close"}}
	}
	if m.cmdMode {
		return []hint{{"enter", "run"}, {"tab", "complete"}, {"esc", "cancel"}}
	}
	if m.filtering {
		return []hint{{"enter", "apply"}, {"esc", "clear"}}
	}
	switch m.screen {
	case scrCreate:
		return []hint{{"tab", "next field"}, {"←→", "shape"}, {"enter", "create"}, {"esc", "back"}}
	case scrOp:
		h := []hint{{"↑↓", "scroll"}, {"pgup/dn", "page"}, {"g/G", "top/end"}, {"f", "follow"}}
		if m.opRunning {
			h = append(h, hint{"esc", "stop & back"})
		} else {
			h = append(h, hint{"esc", "back"})
		}
		return append(h, hint{"?", "help"})
	case scrInfo:
		return []hint{{"esc", "back"}}
	case scrRef:
		return []hint{{"enter", "run"}, {"y", "copy command"}, {"/", "filter"}, {"ctrl-p", "palette"}, {"esc", "back"}}
	}
	if m.toolsReady && !m.tools.DaemonOK && m.tools.StartWith != "" {
		return []hint{{"C", "start " + m.tools.StartWith}, {"ctrl-p", "palette"}, {"?", "help"}, {"q", "quit"}}
	}
	return paneHints(m.focus)
}

// hintGrid lays hints out in columns of `rows` lines, k9s style.
func hintGrid(hints []hint, width, rows int) []string {
	out := make([]string, rows)
	if width < 12 {
		return out
	}
	col := 0
	for i := 0; i < len(hints); i += rows {
		end := min(i+rows, len(hints))
		chunk := hints[i:end]
		kw := 0
		cw := 0
		for _, h := range chunk {
			kw = max(kw, len([]rune(h.key))+2)
		}
		for _, h := range chunk {
			cw = max(cw, kw+1+len([]rune(h.desc)))
		}
		if col+cw > width {
			break
		}
		for r := 0; r < rows; r++ {
			s := ""
			if r < len(chunk) {
				k := "<" + chunk[r].key + ">"
				s = bold(keyC).Render(k) + gap(kw-len([]rune(k))+1) + paint(dimC).Render(chunk[r].desc)
			}
			out[r] += pad(s, cw+3)
		}
		col += cw + 3
	}
	return out
}

func inlineHints(hints []hint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = bold(keyC).Render("<"+h.key+">") + " " + paint(dimC).Render(h.desc)
	}
	return strings.Join(parts, "  ")
}

func (m Model) helpBox() []string {
	type sec struct {
		title string
		rows  []hint
	}
	keyed := func(sc scope) []hint {
		var out []hint
		for _, a := range actions {
			if a.scope != sc || len(a.keys) == 0 {
				continue
			}
			ks := make([]string, 0, 2)
			for _, k := range a.keys[:min(2, len(a.keys))] {
				ks = append(ks, prettyKey(k))
			}
			out = append(out, hint{strings.Join(ks, " "), a.desc})
		}
		return out
	}
	var cmds []hint
	for _, a := range actions {
		if a.cmd != "" && a.arg != "" {
			cmds = append(cmds, hint{a.cmdLabel(), a.desc})
		}
	}
	secs := []sec{
		{"NAVIGATION", navHelp},
		{"GLOBAL", keyed(scopeGlobal)},
		{"CLUSTERS PANE", keyed(scopeCluster)},
		{"NODES PANE", keyed(scopeNode)},
		{"COMMANDS WITH ARGUMENTS (:)", cmds},
	}
	colW := 56
	cols := max(1, min(3, (m.width-6)/colW))
	var lines []string
	for i := 0; i < len(secs); i += cols {
		group := secs[i:min(i+cols, len(secs))]
		maxRows := 0
		for _, s := range group {
			maxRows = max(maxRows, len(s.rows))
		}
		head := ""
		for _, s := range group {
			head += pad(bold(amberC).Render(s.title), colW)
		}
		lines = append(lines, head)
		for r := 0; r < maxRows; r++ {
			line := ""
			for _, s := range group {
				c := ""
				if r < len(s.rows) {
					kw := 15
					if strings.HasPrefix(s.rows[r].key, ":") {
						kw = 24
					}
					c = bold(keyC).Render(pad(s.rows[r].key, kw)) + " " + paint(dimC).Render(clip(s.rows[r].desc, colW-kw-3))
				}
				line += pad(c, colW)
			}
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}
	lines = append(lines, paint(dimC).Render("K: every action with its shell command   ctrl-p: search and run   esc: close"))
	w := min(m.width-4, cols*colW+4)
	body := []string{""}
	for _, l := range lines {
		body = append(body, " "+l)
	}
	h := min(len(body)+2, m.height-2)
	if len(body)+2 > h {
		// Too tall: keep the top and say where the rest is.
		body = body[:h-3]
		body = append(body, " "+paint(amberC).Render("… more: press K for the full command reference"))
	}
	return box("Help", body, w, h, true)
}
