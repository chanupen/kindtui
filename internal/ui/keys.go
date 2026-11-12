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
	}
	if m.toolsReady && !m.tools.DaemonOK && m.tools.StartWith != "" {
		return []hint{{"C", "start " + m.tools.StartWith}, {"?", "help"}, {"q", "quit"}}
	}
	if m.focus == paneNodes {
		return []hint{
			{"enter", "shell (login)"}, {"l", "node logs"}, {"b", "kubelet logs"}, {"d", "describe"}, {"p", "pods on node"},
			{"u", "start node"}, {"x", "stop node"}, {"r", "restart node"}, {"o", "k9s"}, {"esc", "clusters"},
			{":", "command"}, {"?", "help"}, {"q", "quit"},
		}
	}
	return []hint{
		{"enter", "nodes"}, {"n", "new"}, {"s", "start"}, {"x", "stop"}, {"r", "restart"},
		{"ctrl-d", "delete"}, {"o", "k9s"}, {"c", "use context"}, {"i", "load image"}, {"e", "export logs"},
		{"/", "filter"}, {":", "command"}, {"?", "help"}, {"q", "quit"},
	}
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
	secs := []sec{
		{"GENERAL", []hint{
			{"tab", "switch clusters ↔ nodes"}, {"↑↓ j k", "move"}, {"g / G", "first / last"},
			{"/", "filter clusters"}, {":", "command line"}, {"ctrl-r", "refresh now"},
			{"I", "tool paths"}, {"C", "start Colima/Podman"}, {"?", "this help"}, {"q", "quit"},
		}},
		{"CLUSTERS", []hint{
			{"enter", "open nodes"}, {"n", "create cluster"}, {"s", "start"}, {"x", "stop"},
			{"r", "restart"}, {"ctrl-d", "delete"}, {"o", "open in k9s"}, {"c", "set kubectl context"},
			{"i", "load local image"}, {"e", "export logs ~/kind-logs"},
		}},
		{"NODES", []hint{
			{"enter / s", "shell into node"}, {"l", "container log (follow)"}, {"b", "kubelet journal"},
			{"J", "containerd journal"}, {"d", "kubectl describe"}, {"p", "pods on node"},
			{"u", "start node"}, {"x", "stop node"}, {"r", "restart node"}, {"esc", "back"},
		}},
		{"COMMANDS  (:)", []hint{
			{"shell NODE", "log in to any node"}, {"logs NODE", "follow node log"},
			{"journal U", "journal of unit U"}, {"create", "new cluster"},
			{"start / stop", "selected cluster"}, {"delete", "selected cluster"},
			{"filter TEXT", "filter list"}, {"k9s", "open k9s"}, {"info", "tool paths"}, {"quit", ""},
		}},
	}
	colW := 40
	cols := 4
	if m.width < 4*colW+6 {
		cols = 2
	}
	if m.width < 2*colW+6 {
		cols = 1
	}
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
					k := s.rows[r].key
					c = bold(keyC).Render(pad(k, 12)) + " " + paint(dimC).Render(clip(s.rows[r].desc, colW-15))
				}
				line += pad(c, colW)
			}
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}
	lines = append(lines, paint(dimC).Render("Mouse: click to select, double-click a cluster to open nodes, double-click a node to log in.  esc closes."))
	w := min(m.width-4, cols*colW+4)
	body := make([]string, 0, len(lines)+2)
	body = append(body, "")
	for _, l := range lines {
		body = append(body, " "+l)
	}
	h := min(len(body)+2, m.height-2)
	return box("Help", body, w, h, true)
}
