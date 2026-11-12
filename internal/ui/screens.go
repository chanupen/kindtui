package ui

import (
	"fmt"
	"strings"

	"github.com/chanupen/kindtui/internal/engine"
)

func (m Model) createView(w, h int) []string {
	var lines []string
	add := func(s string) { lines = append(lines, " "+s) }
	add("")
	add(m.formRow("Name", 0, m.inputs[0].View()))
	add(m.formRow("Shape", 1, m.shapeControl()))
	if m.shape == shapeCustom {
		add(m.formRow("Workers", 2, m.inputs[1].View()+paint(dimC).Render("  control plane is extra")))
	}
	add(m.formRow("Image", 3, m.inputs[2].View()))
	add(m.formRow("Wait", 4, m.inputs[3].View()+paint(dimC).Render("  seconds, 0 skips --wait")))
	add("")
	switch m.shape {
	case shapeSingle:
		add(paint(dimC).Render("One control-plane node, no workers."))
	case shapeRepo:
		if m.repoPath == "" {
			add(paint(amberC).Render("No kind config found from here or KIND_CONFIG."))
		} else {
			add(bold(amberC).Render("Config  ") + m.repo.Topology() + paint(dimC).Render("  "+m.repoPath))
		}
	default:
		add(paint(dimC).Render("A generated config is written for this create, then removed."))
	}
	add("")
	spec, err := m.spec()
	if err == nil {
		add(bold(amberC).Render("Command ") + engine.CreatePreview(spec))
		if m.shape != shapeRepo {
			add("")
			for _, l := range strings.Split(strings.TrimRight(engine.RenderConfig(spec.Name, spec.Workers), "\n"), "\n") {
				add("        " + paint(dimC).Render(l))
			}
		}
	} else if m.formErr == "" {
		add(paint(dimC).Render(err.Error()))
	}
	if m.formErr != "" {
		add("")
		add(bold(redC).Render("✗ " + m.formErr))
	}
	for i := range lines {
		lines[i] = clip(lines[i], w-2)
	}
	return box("Create cluster", lines, w, h, true)
}

func (m Model) formRow(label string, id int, value string) string {
	f := m.fields()
	on := m.formField < len(f) && f[m.formField] == id
	marker := "  "
	ls := paint(dimC)
	if on {
		marker = bold(accentC).Render("▸ ")
		ls = bold(accentC)
	}
	return marker + ls.Render(fmt.Sprintf("%-9s", label)) + " " + value
}

func (m Model) shapeControl() string {
	labels := []string{"single", "repo config", "custom"}
	parts := make([]string, len(labels))
	for i, label := range labels {
		if i == m.shape {
			parts[i] = selFocus.Render(" " + label + " ")
		} else {
			parts[i] = paint(dimC).Render(" " + label + " ")
		}
	}
	return strings.Join(parts, " ")
}

func (m Model) opStatusText() string {
	switch {
	case m.opRunning:
		return paint(accentC).Render(spinFrames[m.frame%len(spinFrames)] + " running")
	case m.opErr == "":
		return paint(greenC).Render("✓ done")
	case m.opErr == "cancelled" || m.opErr == "timed out":
		return paint(amberC).Render("■ " + m.opErr)
	default:
		return bold(redC).Render("✗ failed")
	}
}

// opBodyH is the number of log lines that fit on the operation screen.
func (m Model) opBodyH() int {
	g := m.geom()
	return max(1, g.body.h-3-m.opCmdLines())
}

// opCmdLines is 1 when the op view shows its equivalent shell command.
func (m Model) opCmdLines() int {
	if m.opCmd != "" {
		return 1
	}
	return 0
}

func (m Model) opStart() int {
	bh := m.opBodyH()
	n := len(m.opLines)
	if m.opFollow {
		return max(0, n-bh)
	}
	return max(0, min(m.opOff, n-bh))
}

func (m Model) opView(w, h int) []string {
	bh := h - 3 - m.opCmdLines()
	start := m.opStart()
	end := min(len(m.opLines), start+bh)
	var lines []string
	var head []string
	if m.opCmd != "" {
		head = []string{" " + paint(greenC).Render(clip("$ "+m.opCmd, w-4))}
	}
	if len(m.opLines) == 0 {
		lines = append(lines, paint(dimC).Render(" waiting for output…"))
	}
	for _, ln := range m.opLines[start:end] {
		lines = append(lines, " "+colorLog(clip(strings.ReplaceAll(ln, "\t", "    "), w-3)))
	}
	lines = append(head, grow(lines, bh, w-2)...)
	pos := "all"
	if len(m.opLines) > bh {
		pct := 100
		if len(m.opLines)-bh > 0 {
			pct = start * 100 / (len(m.opLines) - bh)
		}
		pos = fmt.Sprintf("%d%%", pct)
	}
	mode := paint(dimC).Render("paused")
	if m.opFollow {
		mode = paint(greenC).Render("following")
	}
	foot := fmt.Sprintf(" %s  %s  %s", m.opStatusText(), paint(dimC).Render(fmt.Sprintf("%d lines · %s", len(m.opLines), pos)), mode)
	if m.opErr != "" && m.opErr != "cancelled" && m.opErr != "timed out" {
		foot += "  " + paint(redC).Render(clip(m.opErr, w/2))
	}
	lines = append(lines, foot)
	return box(m.opTitle, lines, w, h, true)
}

// colorLog tints obvious error and warning lines. It only looks at plain text.
func colorLog(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "error") || strings.Contains(low, "fail") || strings.Contains(s, "✗") || strings.HasPrefix(strings.TrimSpace(s), "E1") || strings.HasPrefix(strings.TrimSpace(s), "E0"):
		return paint(redC).Render(s)
	case strings.Contains(low, "warn") || strings.HasPrefix(strings.TrimSpace(s), "W1") || strings.HasPrefix(strings.TrimSpace(s), "W0"):
		return paint(amberC).Render(s)
	case strings.Contains(s, "✓"):
		return paint(greenC).Render(s)
	}
	return s
}

func (m Model) infoView(w, h int) []string {
	t := m.tools
	lab := func(s string) string { return bold(amberC).Render(fmt.Sprintf(" %-14s", s)) }
	lines := []string{
		"",
		lab("Runtime") + m.runtimeValue(),
		lab("Runtime path") + dash(t.Runtime),
		lab("Server") + dash(t.ServerVer),
		lab("kind") + m.toolValue(t.KindVer, t.Kind) + paint(dimC).Render("  "+t.Kind),
		lab("kubectl") + m.toolValue(t.KubectlVer, t.Kubectl) + paint(dimC).Render("  "+t.Kubectl),
		lab("Context") + m.contextValue(),
		lab("Colima") + dash(t.ColimaVer) + paint(dimC).Render("  "+t.Colima),
		lab("Repo config") + dash(m.repoPath),
		lab("Clusters") + m.countLine(),
	}
	if len(t.Broken) > 0 {
		lines = append(lines, lab("Broken links")+paint(amberC).Render(strings.Join(t.Broken, ", ")))
	}
	if t.Hint != "" {
		lines = append(lines, "", " "+paint(amberC).Render(t.Hint))
	}
	return box("Info", lines, w, h, true)
}

func (m Model) modalBox() []string {
	w := min(64, m.width-4)
	var body []string
	body = append(body, "")
	for _, l := range strings.Split(m.modalBody, "\n") {
		body = append(body, " "+l)
	}
	switch m.modal {
	case modalType, modalImage:
		body = append(body, "", " "+bold(accentC).Render("› ")+m.modalInput.View(), "",
			" "+paint(dimC).Render("enter confirm   esc cancel"))
	default:
		body = append(body, "", " "+bold(accentC).Render("<y>")+" confirm   "+bold(accentC).Render("<n>")+" cancel")
	}
	body = append(body, "")
	return box(m.modalTitle, body, w, len(body)+2, true)
}
