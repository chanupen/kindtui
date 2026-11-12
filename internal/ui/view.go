package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/chanupen/kindtui/internal/engine"
)

type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool {
	return r.w > 0 && r.h > 0 && x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// geom is the screen layout. View and the mouse handler share it.
type geom struct {
	header   int
	prompt   int
	footer   int
	body     rect
	clusters rect
	nodes    rect
	insights rect
	side     bool
}

func (m Model) fullHeader() bool {
	return m.height >= 24 && m.width >= 80
}

func (m Model) geom() geom {
	g := geom{header: 1, footer: 2}
	if m.fullHeader() {
		g.header = 6
		g.footer = 1
	}
	if m.cmdMode || m.filtering {
		g.prompt = 3
	}
	y0 := g.header + g.prompt
	bh := m.height - y0 - g.footer
	if bh < 3 {
		bh = 3
	}
	g.body = rect{0, y0, m.width, bh}
	ph := bh
	if bh >= 22 {
		g.insights = rect{0, y0 + bh - 6, m.width, 6}
		ph = bh - 6
	}
	if m.width >= 110 {
		g.side = true
		cw := m.width * 2 / 5
		cw = max(40, min(cw, 62))
		g.clusters = rect{0, y0, cw, ph}
		g.nodes = rect{cw, y0, m.width - cw, ph}
		return g
	}
	ch := len(m.visible()) + 3
	ch = max(5, min(ch, ph/2))
	g.clusters = rect{0, y0, m.width, ch}
	g.nodes = rect{0, y0 + ch, m.width, ph - ch}
	return g
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "kindtui"
	}
	if m.width < 60 || m.height < 16 {
		return fit(paint(amberC).Render("kindtui needs at least 60×16. Make the terminal larger."), m.width, m.height)
	}
	g := m.geom()
	lines := m.headerLines(g)
	if g.prompt > 0 {
		lines = append(lines, m.promptBox()...)
	}
	switch m.screen {
	case scrCreate:
		lines = append(lines, m.createView(g.body.w, g.body.h)...)
	case scrOp:
		lines = append(lines, m.opView(g.body.w, g.body.h)...)
	case scrInfo:
		lines = append(lines, m.infoView(g.body.w, g.body.h)...)
	default:
		lines = append(lines, m.dashView(g)...)
	}
	lines = append(lines, m.footerLines(g)...)
	lines = grow(lines, m.height, m.width)
	if m.help {
		lines = overlay(lines, m.helpBox(), m.width, m.height)
	} else if m.modal != modalNone {
		lines = overlay(lines, m.modalBox(), m.width, m.height)
	}
	return strings.Join(lines, "\n")
}

// ---------- header ----------

var logo = []string{
	` _    _         _ _        _ `,
	`| | _(_)_ _  __| | |_ _  _(_)`,
	`| |/ / | ' \/ _' |  _| || | |`,
	`|_|\_\_|_||_\__,_|\__|\_,_|_|`,
}

func (m Model) headerLines(g geom) []string {
	if g.header == 1 {
		return []string{m.compactHeader()}
	}
	info := []string{
		m.kv("Runtime", m.runtimeValue()),
		m.kv("Kind", m.toolValue(m.tools.KindVer, m.tools.Kind)),
		m.kv("Kubectl", m.toolValue(m.tools.KubectlVer, m.tools.Kubectl)),
		m.kv("Context", m.contextValue()),
		m.kv("Clusters", m.countLine()),
	}
	infoW := 40
	logoW := 0
	if m.width >= 130 {
		logoW = 31
	}
	hintW := m.width - infoW - logoW - 2
	grid := hintGrid(m.headerHints(), hintW, 5)
	out := make([]string, 0, 6)
	for i := 0; i < 5; i++ {
		line := " " + pad(info[i], infoW) + pad(grid[i], hintW)
		if logoW > 0 {
			l := ""
			if i < len(logo) {
				l = bold(orangeC).Render(logo[i])
			} else {
				l = paint(dimC).Render("  kind cluster manager")
			}
			line += " " + l
		}
		out = append(out, pad(line, m.width))
	}
	if m.showBanner() {
		out = append(out, pad(" "+bold(amberC).Render("⚠ ")+paint(amberC).Render(m.bannerText()), m.width))
	} else {
		out = append(out, "")
	}
	return out
}

func (m Model) bannerText() string {
	h := m.tools.Hint
	if h == "" {
		h = "container runtime is not running"
	}
	if m.tools.StartWith != "" {
		h += "  — press C to start " + m.tools.StartWith
	}
	return h
}

func (m Model) compactHeader() string {
	parts := []string{
		bold(orangeC).Render("kindtui"),
		m.runtimeValue(),
		"kind " + m.toolValue(m.tools.KindVer, m.tools.Kind),
		"ctx " + m.contextValue(),
		m.countLine(),
	}
	sep := paint(lineC).Render(" │ ")
	line := " " + strings.Join(parts, sep)
	if m.showBanner() {
		line += sep + paint(amberC).Render("runtime down")
	}
	return pad(line, m.width)
}

func (m Model) kv(k, v string) string {
	return bold(amberC).Render(fmt.Sprintf("%-9s", k+":")) + " " + v
}

func (m Model) runtimeValue() string {
	if !m.toolsReady {
		return paint(dimC).Render("checking…")
	}
	name := m.tools.RuntimeName
	if name == "" {
		return paint(redC).Render("none found")
	}
	if m.tools.ClientVer != "" {
		name += " " + m.tools.ClientVer
	}
	if m.tools.DaemonOK {
		return name + " " + paint(greenC).Render("● up")
	}
	return name + " " + paint(redC).Render("● down")
}

func (m Model) contextValue() string {
	if !m.toolsReady {
		return paint(dimC).Render("…")
	}
	if m.tools.Context == "" {
		return paint(dimC).Render("none")
	}
	return m.tools.Context
}

func (m Model) toolValue(ver, path string) string {
	if !m.toolsReady {
		return paint(dimC).Render("…")
	}
	if ver == "" && path == "" {
		return paint(redC).Render("missing")
	}
	if ver == "" {
		return path
	}
	return ver
}

func (m Model) countLine() string {
	n := len(m.clusters)
	run := 0
	nodes := 0
	for _, c := range m.clusters {
		if c.Status == engine.StatusRunning {
			run++
		}
		nodes += len(c.Nodes)
	}
	return fmt.Sprintf("%d · %d running · %d nodes", n, run, nodes)
}

// ---------- dashboard ----------

func (m Model) dashView(g geom) []string {
	cl := m.clustersBox(g.clusters)
	nd := m.nodesBox(g.nodes)
	var out []string
	if g.side {
		for i := 0; i < g.clusters.h; i++ {
			out = append(out, cl[i]+nd[i])
		}
	} else {
		out = append(out, cl...)
		out = append(out, nd...)
	}
	if g.insights.h > 0 {
		out = append(out, m.insightsBox(g.insights)...)
	}
	return grow(out, g.body.h, m.width)
}

var clusterCols = []column{
	{title: "NAME", min: 10, max: 30, flex: true, prio: 0},
	{title: "STATUS", min: 10, prio: 0},
	{title: "NODES", min: 5, right: true, prio: 1},
	{title: "TOPOLOGY", min: 10, flex: true, prio: 3},
	{title: "AGE", min: 4, right: true, prio: 2},
}

func (m Model) clustersBox(r rect) []string {
	vis := m.visible()
	title := fmt.Sprintf("Clusters[%d]", len(vis))
	if q := strings.TrimSpace(m.filter.Value()); q != "" {
		title += " </" + q + ">"
	}
	inner := r.w - 2
	ih := r.h - 2
	focused := m.focus == paneClusters && m.screen == scrDash
	var body []string
	if len(vis) == 0 {
		body = m.emptyList()
	} else {
		rows := make([][]styledCell, len(vis))
		for i, c := range vis {
			age := ""
			if !c.Created.IsZero() {
				age = engine.Age(m.now.Sub(c.Created))
			}
			ready := 0
			for _, n := range c.Nodes {
				if strings.EqualFold(n.State, "running") {
					ready++
				}
			}
			nodes := strconv.Itoa(len(c.Nodes))
			if ready != len(c.Nodes) {
				nodes = fmt.Sprintf("%d/%d", ready, len(c.Nodes))
			}
			rows[i] = []styledCell{
				{c.Name, bold(textC)},
				{"● " + statusWord(c.Status), statusStyle(c.Status)},
				{nodes, paint(textC)},
				{shortTopology(c), paint(dimC)},
				{age, paint(dimC)},
			}
		}
		cols := append([]column(nil), clusterCols...)
		for _, c := range vis {
			cols[0].min = max(cols[0].min, min(len(c.Name), 30))
		}
		body, _ = table(cols, rows, m.cursor, inner, ih, focused)
	}
	return box(title, body, r.w, r.h, focused)
}

func shortTopology(c engine.Cluster) string {
	cp, wk := 0, 0
	for _, n := range c.Nodes {
		if n.Role == "control-plane" {
			cp++
		} else {
			wk++
		}
	}
	return fmt.Sprintf("%dcp + %dw", cp, wk)
}

var nodeCols = []column{
	{title: "NAME", min: 14, max: 34, flex: true, prio: 0},
	{title: "ROLE", min: 13, prio: 1},
	{title: "STATE", min: 9, prio: 0},
	{title: "READY", min: 5, prio: 1},
	{title: "IP", min: 11, max: 16, flex: true, prio: 2},
	{title: "CPU", min: 6, right: true, prio: 3},
	{title: "MEM", min: 9, right: true, prio: 3},
	{title: "PODS", min: 4, right: true, prio: 4},
	{title: "VERSION", min: 8, prio: 5},
	{title: "UPTIME", min: 12, flex: true, max: 22, prio: 6},
	{title: "PORTS", min: 14, flex: true, prio: 7},
}

func (m Model) nodesBox(r rect) []string {
	c, ok := m.selected()
	focused := m.focus == paneNodes && m.screen == scrDash
	if !ok {
		return box("Nodes", []string{"", "  " + paint(dimC).Render("select a cluster")}, r.w, r.h, focused)
	}
	nodes := m.currentNodes()
	title := fmt.Sprintf("Nodes(%s)[%d]", c.Name, len(nodes))
	if m.detailBusy && m.detailInflight == c.Name {
		title += " " + spinFrames[m.frame%len(spinFrames)]
	}
	pods := m.podsPerNode()
	rows := make([][]styledCell, len(nodes))
	for i, n := range nodes {
		state := strings.ToLower(n.State)
		ready := n.Ready
		readySt := paint(dimC)
		switch ready {
		case "True":
			ready, readySt = "yes", paint(greenC)
		case "False":
			ready, readySt = "no", bold(redC)
		case "":
			ready = "—"
		}
		mem := n.Mem
		if i := strings.Index(mem, "/"); i > 0 {
			mem = strings.TrimSpace(mem[:i])
		}
		podN := ""
		if p, ok := pods[n.Name]; ok {
			podN = strconv.Itoa(p)
		}
		roleSt := paint(violetC)
		if n.Role == "control-plane" {
			roleSt = bold(orangeC)
		}
		rows[i] = []styledCell{
			{n.Name, bold(textC)},
			{n.Role, roleSt},
			{"● " + state, stateStyle(state)},
			{ready, readySt},
			{dash(n.IP), paint(textC)},
			{dash(n.CPU), paint(textC)},
			{dash(mem), paint(textC)},
			{dash(podN), paint(textC)},
			{dash(n.Kubelet), paint(dimC)},
			{n.Summary, paint(dimC)},
			{shortPorts(n.Ports), paint(dimC)},
		}
	}
	cols := append([]column(nil), nodeCols...)
	for _, n := range nodes {
		cols[0].min = max(cols[0].min, min(len(n.Name), 40))
	}
	body, _ := table(cols, rows, m.nodeCursor, r.w-2, r.h-2, focused)
	if len(nodes) == 0 {
		body = append(body, "  "+paint(dimC).Render("no node containers"))
	}
	if !focused && len(nodes) > 0 && len(body) < r.h-3 {
		body = append(body, "", "  "+paint(dimC).Render("enter or tab → select a node, then enter to log in"))
	}
	return box(title, body, r.w, r.h, focused)
}

func shortPorts(p string) string {
	if p == "" {
		return ""
	}
	out := []string{}
	for _, f := range strings.Fields(p) {
		f = strings.TrimPrefix(f, "0.0.0.0:")
		f = strings.TrimPrefix(f, "127.0.0.1:")
		f = strings.TrimSuffix(f, "/tcp")
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func (m Model) podsPerNode() map[string]int {
	out := map[string]int{}
	c, ok := m.selected()
	if !ok || m.detail == nil || m.detailName != c.Name {
		return out
	}
	if m.detailErr != "" && len(m.detail.Pods) == 0 {
		return out
	}
	for _, p := range m.detail.Pods {
		if p.Node != "" {
			out[p.Node]++
		}
	}
	return out
}

func (m Model) insightsBox(r rect) []string {
	c, ok := m.selected()
	if !ok {
		return box("Overview", []string{"", " " + paint(dimC).Render("no cluster selected")}, r.w, r.h, false)
	}
	lab := func(s string) string { return bold(amberC).Render(fmt.Sprintf(" %-11s", s)) }
	var lines []string
	api := ""
	for _, n := range c.Nodes {
		for _, f := range strings.Fields(n.Ports) {
			if strings.HasSuffix(f, "->6443/tcp") {
				api = "https://" + strings.TrimSuffix(f, "->6443/tcp")
			}
		}
	}
	if m.detail != nil && m.detailName == c.Name {
		for _, n := range m.detail.Cluster.Nodes {
			for _, f := range strings.Fields(n.Ports) {
				if strings.HasSuffix(f, "->6443/tcp") {
					api = "https://" + strings.TrimSuffix(f, "->6443/tcp")
				}
			}
		}
	}
	ctx := "kind-" + c.Name
	if m.tools.Context == ctx {
		ctx += paint(greenC).Render(" (current)")
	} else {
		ctx += paint(dimC).Render("  press c to switch")
	}
	switch {
	case c.Status == engine.StatusStopped:
		lines = append(lines,
			lab("Status")+paint(dimC).Render("stopped — all node containers are down"),
			lab("Next")+"press "+bold(accentC).Render("s")+" to start it, or "+bold(accentC).Render("ctrl-d")+" to delete it",
		)
	case m.detail == nil || m.detailName != c.Name:
		msg := "loading…"
		if m.detailErr != "" && m.detailName == c.Name {
			msg = m.detailErr
		}
		lines = append(lines, lab("Kubernetes")+paint(dimC).Render(msg))
	default:
		d := m.detail
		ver := engine.KubeVersion(d.Cluster.Nodes)
		img, osName, rt := "", "", ""
		for _, n := range d.Cluster.Nodes {
			if img == "" {
				img = n.Image
			}
			if osName == "" {
				osName = n.OS
			}
			if rt == "" {
				rt = n.Runtime
			}
		}
		lines = append(lines, lab("Kubernetes")+dash(ver)+gap(3)+bold(amberC).Render("API ")+dash(api)+gap(3)+bold(amberC).Render("Context ")+ctx)
		if d.KubeErr != "" {
			lines = append(lines, lab("kubectl")+paint(redC).Render(clip(d.KubeErr, r.w-16)))
		} else {
			run, pend, fail, other := engine.PodCounts(d.Pods)
			restarts := 0
			for _, p := range d.Pods {
				restarts += p.Restarts
			}
			pod := fmt.Sprintf("%d total  ", len(d.Pods)) +
				paint(greenC).Render(fmt.Sprintf("%d running", run)) + "  " +
				colorIf(pend > 0, amberC).Render(fmt.Sprintf("%d pending", pend)) + "  " +
				colorIf(fail > 0, redC).Render(fmt.Sprintf("%d failed", fail)) + "  " +
				paint(dimC).Render(fmt.Sprintf("%d completed/other  %d restarts", other, restarts))
			lines = append(lines, lab("Pods")+pod)
		}
		lines = append(lines, lab("Node image")+dash(img)+paint(dimC).Render("  "+osName+"  "+rt))
		warn := engine.WarningEvents(d.Events, 1)
		if len(warn) == 0 {
			lines = append(lines, lab("Events")+paint(greenC).Render("no warnings"))
		} else {
			ev := warn[0]
			age := ""
			if !ev.Time.IsZero() {
				age = engine.Age(m.now.Sub(ev.Time)) + " ago  "
			}
			lines = append(lines, lab("Warning")+paint(amberC).Render(age+ev.Reason+"  "+ev.Object+"  ")+paint(dimC).Render(oneLine(ev.Message)))
		}
	}
	for i := range lines {
		lines[i] = clip(lines[i], r.w-2)
	}
	return box("Overview("+c.Name+")", lines, r.w, r.h, false)
}

func colorIf(on bool, c lipgloss.TerminalColor) lipgloss.Style {
	if on {
		return bold(c)
	}
	return paint(dimC)
}

func (m Model) emptyList() []string {
	if !m.toolsReady {
		return []string{"", "  " + paint(dimC).Render("looking for docker/podman, kind, kubectl…")}
	}
	if strings.TrimSpace(m.filter.Value()) != "" {
		return []string{"", "  " + paint(dimC).Render("no clusters match the filter (esc clears)")}
	}
	if !m.tools.DaemonOK {
		lines := []string{"", "  " + paint(amberC).Render("container runtime is down")}
		if m.tools.StartWith != "" {
			lines = append(lines, "  "+paint(dimC).Render("press C to start "+m.tools.StartWith))
		}
		return lines
	}
	if !m.listLoaded {
		return []string{"", "  " + paint(dimC).Render("loading clusters…")}
	}
	return []string{"", "  No kind clusters yet.", "  " + paint(dimC).Render("press ") + bold(accentC).Render("n") + paint(dimC).Render(" to create one")}
}

func statusWord(s engine.Status) string {
	switch s {
	case engine.StatusRunning:
		return "Running"
	case engine.StatusStopped:
		return "Stopped"
	case engine.StatusDegraded:
		return "Degraded"
	default:
		return "Broken"
	}
}

func statusStyle(s engine.Status) lipgloss.Style {
	switch s {
	case engine.StatusRunning:
		return paint(greenC)
	case engine.StatusDegraded:
		return bold(amberC)
	case engine.StatusStopped:
		return paint(dimC)
	default:
		return bold(redC)
	}
}

func stateStyle(state string) lipgloss.Style {
	switch state {
	case "running":
		return paint(greenC)
	case "exited", "stopped", "created":
		return paint(dimC)
	case "paused", "restarting":
		return paint(amberC)
	default:
		return paint(redC)
	}
}

// ---------- prompt & footer ----------

func (m Model) promptBox() []string {
	var label, view string
	if m.cmdMode {
		label, view = ":", m.cmdInput.View()
	} else {
		label, view = "/", m.filter.View()
	}
	line := " " + bold(amberC).Render(label) + " " + view
	return box("", []string{line}, m.width, 3, true)
}

func (m Model) footerLines(g geom) []string {
	var crumbs []string
	pill := func(s string, on bool) string {
		st := lipgloss.NewStyle().Padding(0, 1).Bold(true)
		if on {
			st = st.Background(lipgloss.Color("#E0A100")).Foreground(lipgloss.Color("#0B0F14"))
		} else {
			st = st.Background(lipgloss.Color("#3A4452")).Foreground(lipgloss.Color("#F0F3F6"))
		}
		return st.Render(s)
	}
	switch m.screen {
	case scrCreate:
		crumbs = []string{pill("clusters", false), pill("create", true)}
	case scrOp:
		crumbs = []string{pill("clusters", false), pill(m.opKind, true)}
	case scrInfo:
		crumbs = []string{pill("clusters", false), pill("info", true)}
	default:
		c, ok := m.selected()
		if m.focus == paneNodes && ok {
			crumbs = []string{pill("clusters", false), pill(c.Name, false), pill("nodes", true)}
		} else {
			crumbs = []string{pill("clusters", true)}
			if ok {
				crumbs = append(crumbs, pill(c.Name, false))
			}
		}
	}
	left := " " + strings.Join(crumbs, " ")
	right := ""
	if m.toast != "" {
		st := paint(greenC)
		if m.toastErr {
			st = bold(amberC)
		}
		right = st.Render(m.toast)
	} else if m.busy() {
		right = paint(accentC).Render(spinFrames[m.frame%len(spinFrames)]) + paint(dimC).Render(" working")
	} else {
		right = paint(dimC).Render(m.now.Format("15:04:05"))
	}
	room := m.width - ansiWidth(left) - 2
	right = clip(right, max(0, room))
	line := left + gap(m.width-ansiWidth(left)-ansiWidth(right)-1) + right
	out := []string{pad(line, m.width)}
	if g.footer == 2 {
		out = append([]string{pad(" "+inlineHints(m.headerHints()), m.width)}, out...)
	}
	return out
}
