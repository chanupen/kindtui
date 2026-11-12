package ui

import (
	"context"
	"errors"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chanupen/kindtui/internal/engine"
)

type execMsg struct {
	what string
	cmd  *exec.Cmd
	err  error
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.resizeInputs()
		return m.syncList(), nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m.onKey(msg)
	case tea.MouseMsg:
		return m.onMouse(msg)
	case clockMsg:
		return m.onClock(time.Time(msg))
	case frameMsg:
		if m.busy() {
			m.frame++
			return m, frameCmd()
		}
		m.spinning = false
		return m, nil
	case toolsMsgSeq:
		if msg.seq != m.toolsSeq {
			return m, nil
		}
		m.tools = msg.tools
		m.toolsReady = true
		m.toolsBusy = false
		if msg.tools.DaemonOK {
			return m.refresh()
		}
		return m, nil
	case clustersMsg:
		if msg.seq != m.clusterSeq {
			return m, nil
		}
		m.listBusy = false
		if msg.err != nil {
			return m.warn(oneLine(msg.err.Error())), nil
		}
		first := !m.listLoaded
		m.listLoaded = true
		prev := m.detailName
		m = m.applyClusters(msg.clusters)
		if c, ok := m.selected(); ok && (first || c.Name != prev) {
			return m.requestDetail()
		}
		return m, nil
	case detailMsg:
		m.detailBusy = false
		if c, ok := m.selected(); ok && c.Name == msg.name {
			m.detailName = msg.name
			if msg.err != nil {
				m.detail = nil
				m.detailErr = oneLine(msg.err.Error())
			} else {
				d := msg.detail
				m.detail = &d
				m.detailErr = d.KubeErr
			}
		}
		if m.detailPending {
			m.detailPending = false
			return m.requestDetail()
		}
		return m, nil
	case configMsg:
		if msg.err == nil {
			m.repoPath = msg.path
			m.repo = msg.sum
			if !m.nameTouched && msg.sum.Name != "" && m.inputs[0].Value() == "kind" {
				m.inputs[0].SetValue(msg.sum.Name)
			}
			if !m.shapeTouched && msg.path != "" {
				m.shape = shapeRepo
			}
		} else if !m.shapeTouched {
			m.shape = shapeSingle
		}
		return m, nil
	case logLineMsg:
		if msg.id != m.opID {
			return m, nil
		}
		m.opLines = append(m.opLines, msg.lines...)
		if over := len(m.opLines) - maxOpLines; over > 0 {
			m.opLines = m.opLines[over:]
			if !m.opFollow {
				m.opOff = max(0, m.opOff-over)
			}
		}
		return m, listenLines(m.linesCh, msg.id)
	case linesClosedMsg:
		return m, nil
	case opDoneMsg:
		if msg.id != m.opID {
			return m, nil
		}
		m.opRunning = false
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.opErr = opStatus(msg.err)
		var cmds []tea.Cmd
		var cmd tea.Cmd
		if m.opKind == "runtime" {
			m, cmd = m.issueProbe()
			cmds = append(cmds, cmd)
		}
		if mutating(m.opKind) {
			m, cmd = m.refresh()
			cmds = append(cmds, cmd)
			m.detailName = ""
			if msg.err == nil {
				m = m.note(m.opTitle + " — done")
			} else if m.opErr != "cancelled" {
				m = m.warn(m.opTitle + " — " + m.opErr)
			}
		}
		return m, tea.Batch(cmds...)
	case copiedMsg:
		if msg.err != nil {
			return m.warn("copy failed: " + oneLine(msg.err.Error())), nil
		}
		return m.note("copied: " + msg.text), nil
	case toastMsg:
		if msg.err != nil {
			return m.warn(oneLine(msg.err.Error())), nil
		}
		m = m.note(msg.text)
		m, cmd := m.issueProbe()
		return m, cmd
	case execMsg:
		if msg.err != nil {
			return m.warn(oneLine(msg.err.Error())), nil
		}
		what := msg.what
		return m, tea.ExecProcess(msg.cmd, func(err error) tea.Msg {
			return shellDoneMsg{what: what, err: err}
		})
	case shellDoneMsg:
		var exitErr *exec.ExitError
		if msg.err != nil && !errors.As(msg.err, &exitErr) {
			m = m.warn(oneLine(msg.err.Error()))
		} else if msg.what != "" {
			m = m.note("back from " + msg.what)
		}
		m.detailName = ""
		return m.refresh()
	default:
		return m.forward(msg)
	}
}

// mutating ops change cluster state. Viewer ops (logs, describe) do not.
func mutating(kind string) bool {
	switch kind {
	case "logs", "kubelet", "journal", "describe", "pods", "export":
		return false
	}
	return true
}

func opStatus(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return oneLine(err.Error())
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

func (m Model) onClock(now time.Time) (tea.Model, tea.Cmd) {
	m.now = now
	m.ticks++
	if m.toast != "" && !m.toastUntil.IsZero() && now.After(m.toastUntil) {
		m.toast = ""
	}
	var cmds []tea.Cmd
	var cmd tea.Cmd
	idle := !m.opRunning || !mutating(m.opKind)
	if m.ticks%2 == 0 && idle && m.screen == scrDash {
		m, cmd = m.refresh()
		cmds = append(cmds, cmd)
	}
	if m.ticks%5 == 0 && idle && m.screen == scrDash {
		m, cmd = m.requestDetail()
		cmds = append(cmds, cmd)
	}
	if m.ticks%15 == 0 {
		m, cmd = m.issueProbe()
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, m.clockCmd())
	return m, tea.Batch(cmds...)
}

func (m Model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if m.help {
		switch s {
		case "esc", "?", "q", "enter":
			m.help = false
		}
		return m, nil
	}
	if m.palette {
		return m.onPalette(s, k)
	}
	if m.modal != modalNone {
		return m.onModal(s, k)
	}
	if m.cmdMode {
		return m.onCmd(s, k)
	}
	if m.filtering {
		return m.onFilter(s, k)
	}
	switch m.screen {
	case scrCreate:
		return m.onCreate(s, k)
	case scrOp:
		return m.onOp(s)
	case scrInfo:
		if s == "esc" || s == "q" || s == "enter" {
			m.screen = scrDash
		}
		return m, nil
	case scrRef:
		return m.onRef(s, k)
	default:
		return m.onDash(s)
	}
}

func (m Model) onDash(s string) (tea.Model, tea.Cmd) {
	// Navigation is not an action; it only moves the cursor.
	switch s {
	case "up", "k":
		return m.moveFocused(-1)
	case "down", "j":
		return m.moveFocused(1)
	case "pgup", "ctrl+u":
		return m.moveFocused(-10)
	case "pgdown", "ctrl+f":
		return m.moveFocused(10)
	case "home", "g":
		return m.moveFocused(-1 << 20)
	case "end", "G":
		return m.moveFocused(1 << 20)
	case "tab", "shift+tab":
		if m.focus == paneClusters {
			return m.enterNodes()
		}
		m.focus = paneClusters
		return m, nil
	case "/":
		return m.openFilter()
	case "esc":
		if m.focus == paneClusters && m.filter.Value() != "" {
			m.filter.SetValue("")
			return m.afterMove()
		}
	}
	if a := actionByKey(s, m.focus); a != nil && a.run != nil {
		return a.run(m, "")
	}
	return m, nil
}

func (m Model) openCmd() (Model, tea.Cmd) {
	m.cmdMode = true
	m.cmdInput.SetValue("")
	m = m.blur()
	return m, m.cmdInput.Focus()
}

func (m Model) openFilter() (Model, tea.Cmd) {
	m.focus = paneClusters
	m.filtering = true
	m = m.blur()
	return m, m.filter.Focus()
}

func (m Model) enterNodes() (tea.Model, tea.Cmd) {
	if _, ok := m.selected(); !ok {
		return m.note("no cluster selected"), nil
	}
	m.focus = paneNodes
	nodes := m.currentNodes()
	if m.nodeCursor >= len(nodes) {
		m.nodeCursor = 0
	}
	return m, nil
}

func (m Model) moveFocused(d int) (tea.Model, tea.Cmd) {
	if m.focus == paneNodes {
		n := len(m.currentNodes())
		m.nodeCursor = clamp(m.nodeCursor+d, 0, n-1)
		return m, nil
	}
	m.cursor += d
	return m.afterMove()
}

// afterMove clamps the cursor and loads detail for a newly selected cluster.
func (m Model) afterMove() (tea.Model, tea.Cmd) {
	prev := ""
	if m.detail != nil {
		prev = m.detailName
	}
	m = m.syncList()
	c, ok := m.selected()
	if !ok || c.Name == prev {
		return m, nil
	}
	m.nodeCursor = 0
	return m.requestDetail()
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------- command line ----------

func (m Model) onCmd(s string, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s {
	case "esc":
		m.cmdMode = false
		m.cmdInput.Blur()
		return m, nil
	case "enter":
		line := m.cmdInput.Value()
		m.cmdMode = false
		m.cmdInput.Blur()
		m.cmdInput.SetValue("")
		return m.runCommand(line)
	case "tab":
		m.cmdInput.SetValue(m.complete(m.cmdInput.Value()))
		m.cmdInput.CursorEnd()
		return m, nil
	}
	var cmd tea.Cmd
	m.cmdInput, cmd = m.cmdInput.Update(k)
	return m, cmd
}

func (m Model) allNodeNames() []string {
	var out []string
	for _, c := range m.clusters {
		for _, n := range c.Nodes {
			out = append(out, n.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (m Model) complete(line string) string {
	fields := strings.Fields(line)
	trailing := strings.HasSuffix(line, " ")
	pick := func(prefix string, pool []string) string {
		var hits []string
		for _, p := range pool {
			if strings.HasPrefix(p, prefix) {
				hits = append(hits, p)
			}
		}
		if len(hits) == 0 {
			return prefix
		}
		common := hits[0]
		for _, h := range hits[1:] {
			for !strings.HasPrefix(h, common) {
				common = common[:len(common)-1]
			}
		}
		if len(hits) == 1 {
			return common + " "
		}
		return common
	}
	switch {
	case len(fields) == 0:
		return line
	case len(fields) == 1 && !trailing:
		return pick(fields[0], commandNames())
	default:
		last := ""
		if !trailing {
			last = fields[len(fields)-1]
			fields = fields[:len(fields)-1]
		}
		pool := m.allNodeNames()
		if fields[0] == "filter" || fields[0] == "cluster" {
			pool = nil
			for _, c := range m.clusters {
				pool = append(pool, c.Name)
			}
		}
		return strings.Join(fields, " ") + " " + pick(last, pool)
	}
}

func (m Model) runCommand(line string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return m, nil
	}
	name := strings.ToLower(fields[0])
	arg := strings.Join(fields[1:], " ")
	if a := actionByCmd(name); a != nil && a.run != nil {
		return a.run(m, arg)
	}
	// A bare node name logs in to it.
	if _, ok := m.findNode(fields[0]); ok {
		return actionByID("shell").run(m, fields[0])
	}
	return m.warn("unknown command: " + name + "  (ctrl-p searches every action, K lists them)"), nil
}

func (m Model) selectCluster(name string) (Model, bool) {
	m.filter.SetValue("")
	for i, c := range m.visible() {
		if c.Name == name {
			m.cursor = i
			return m.syncList(), true
		}
	}
	return m, false
}

// findNode looks a node up by full container name, or by a role suffix of the
// selected cluster such as "worker2" or "control-plane".
func (m Model) findNode(name string) (engine.Node, bool) {
	for _, c := range m.clusters {
		for _, n := range c.Nodes {
			if n.Name == name {
				return n, true
			}
		}
	}
	if c, ok := m.selected(); ok {
		for _, n := range c.Nodes {
			if n.Name == c.Name+"-"+name {
				return n, true
			}
		}
	}
	return engine.Node{}, false
}

// ---------- operation screen ----------

func (m Model) onOp(s string) (tea.Model, tea.Cmd) {
	bh := m.opBodyH()
	switch s {
	case "esc", "q", "backspace":
		if m.opRunning {
			if !mutating(m.opKind) {
				if m.cancel != nil {
					m.cancel()
				}
				m.screen = scrDash
				return m.refresh()
			}
			if m.cancel != nil {
				m.cancel()
			}
			return m.warn("cancelling " + m.opTitle + "…"), nil
		}
		m.screen = scrDash
		return m.refresh()
	case "up", "k":
		return m.scrollOp(-1), nil
	case "down", "j":
		return m.scrollOp(1), nil
	case "pgup", "ctrl+u", "b":
		return m.scrollOp(-bh), nil
	case "pgdown", "ctrl+f", " ":
		return m.scrollOp(bh), nil
	case "g", "home":
		m.opFollow = false
		m.opOff = 0
		return m, nil
	case "G", "end":
		m.opFollow = true
		return m, nil
	case "f":
		m.opFollow = !m.opFollow
		if !m.opFollow {
			m.opOff = m.opStart()
		}
		return m, nil
	case "?":
		m.help = true
		return m, nil
	}
	return m, nil
}

func (m Model) scrollOp(delta int) Model {
	start := m.opStart()
	maxStart := max(0, len(m.opLines)-m.opBodyH())
	next := clamp(start+delta, 0, maxStart)
	m.opOff = next
	m.opFollow = next >= maxStart && delta > 0
	return m
}

// ---------- create form ----------

func (m Model) onCreate(s string, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s {
	case "esc":
		m.screen = scrDash
		m.formErr = ""
		m = m.blur()
		return m, nil
	case "tab", "down":
		return m.formMove(1)
	case "shift+tab", "up":
		return m.formMove(-1)
	case "left":
		if m.fields()[m.formField] == 1 {
			return m.cycleShape(-1), nil
		}
		return m.passInput(k)
	case "right", " ":
		if m.fields()[m.formField] == 1 {
			return m.cycleShape(1), nil
		}
		return m.passInput(k)
	case "enter":
		return m.submitCreate()
	default:
		return m.passInput(k)
	}
}

func (m Model) fields() []int {
	if m.shape == shapeCustom {
		return []int{0, 1, 2, 3, 4}
	}
	return []int{0, 1, 3, 4}
}

func (m Model) formMove(d int) (tea.Model, tea.Cmd) {
	f := m.fields()
	m.formField += d
	if m.formField < 0 {
		m.formField = len(f) - 1
	}
	if m.formField >= len(f) {
		m.formField = 0
	}
	return m.focusForm()
}

func (m Model) focusForm() (Model, tea.Cmd) {
	m = m.blur()
	idx := m.currentInput()
	if idx < 0 {
		return m, nil
	}
	return m, m.inputs[idx].Focus()
}

func (m Model) currentInput() int {
	if m.formField < 0 || m.formField >= len(m.fields()) {
		return -1
	}
	switch m.fields()[m.formField] {
	case 0:
		return 0
	case 2:
		return 1
	case 3:
		return 2
	case 4:
		return 3
	default:
		return -1
	}
}

func (m Model) cycleShape(d int) Model {
	m.shapeTouched = true
	m.shape += d
	if m.shape < shapeSingle {
		m.shape = shapeCustom
	}
	if m.shape > shapeCustom {
		m.shape = shapeSingle
	}
	m.formErr = ""
	if f := m.fields(); m.formField >= len(f) {
		m.formField = len(f) - 1
	}
	return m
}

func (m Model) passInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	idx := m.currentInput()
	if idx < 0 {
		return m, nil
	}
	if idx == 0 {
		m.nameTouched = true
	}
	m.formErr = ""
	var cmd tea.Cmd
	m.inputs[idx], cmd = m.inputs[idx].Update(k)
	return m, cmd
}

func (m Model) onFilter(s string, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch s {
	case "esc":
		m.filter.SetValue("")
		m.filtering = false
		m.filter.Blur()
		return m.afterMove()
	case "enter":
		m.filtering = false
		m.filter.Blur()
		return m.afterMove()
	case "up", "down":
		m.cursor += map[string]int{"up": -1, "down": 1}[s]
		return m.afterMove()
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	m.cursor = 0
	m = m.syncList()
	return m, cmd
}

// ---------- modals ----------

func (m Model) onModal(s string, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if s == "esc" || (m.modal == modalConfirm && (s == "n" || s == "N" || s == "q")) {
		return m.closeModal(), nil
	}
	if m.modal == modalConfirm {
		if s == "y" || s == "Y" || s == "enter" {
			act, cluster, node := m.modalAct, m.modalCluster, m.modalNode
			m = m.closeModal()
			return m.runAct(act, cluster, node, "")
		}
		return m, nil
	}
	if s == "enter" {
		val := strings.TrimSpace(m.modalInput.Value())
		if m.modal == modalType && val != m.modalCluster.Name {
			return m.warn("type " + m.modalCluster.Name + " exactly to delete"), nil
		}
		if m.modal == modalImage && (val == "" || !engine.ValidImage(val)) {
			return m.warn("enter a valid image name"), nil
		}
		act, cluster := m.modalAct, m.modalCluster
		m = m.closeModal()
		return m.runAct(act, cluster, "", val)
	}
	var cmd tea.Cmd
	m.modalInput, cmd = m.modalInput.Update(k)
	return m, cmd
}

func (m Model) closeModal() Model {
	m.modal = modalNone
	m.modalAct = ""
	m.modalNode = ""
	m.modalInput.Blur()
	m.modalInput.SetValue("")
	return m
}

func (m Model) runAct(act string, cluster engine.Cluster, node, extra string) (tea.Model, tea.Cmd) {
	eng := m.eng
	switch act {
	case "stop":
		return m.begin("stop", "Stop "+cluster.Name, m.shellFor("stop", ""), 3*time.Minute, true, func(ctx context.Context, emit func(string)) error {
			return eng.Stop(ctx, cluster, emit)
		})
	case "restart":
		return m.begin("restart", "Restart "+cluster.Name, m.shellFor("restart", ""), 4*time.Minute, true, func(ctx context.Context, emit func(string)) error {
			return eng.Restart(ctx, cluster, emit)
		})
	case "node-stop", "node-restart", "node-start":
		verb := strings.TrimPrefix(act, "node-")
		return m.begin("node", strings.ToUpper(verb[:1])+verb[1:]+" "+node, m.shellFor(act, node), 2*time.Minute, true, func(ctx context.Context, emit func(string)) error {
			return eng.NodeAction(ctx, verb, node, emit)
		})
	case "delete":
		return m.begin("delete", "Delete "+cluster.Name, m.shellFor("delete", ""), 5*time.Minute, true, func(ctx context.Context, emit func(string)) error {
			return eng.Delete(ctx, cluster.Name, emit)
		})
	case "image":
		if !engine.ValidImage(extra) || extra == "" {
			return m.warn("invalid image " + extra), nil
		}
		return m.begin("load", "Load "+extra+" → "+cluster.Name, "kind load docker-image "+extra+" --name "+cluster.Name, 10*time.Minute, true, func(ctx context.Context, emit func(string)) error {
			return eng.LoadImage(ctx, cluster.Name, extra, emit)
		})
	}
	return m, nil
}

func (m Model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case m.modal == modalType || m.modal == modalImage:
		m.modalInput, cmd = m.modalInput.Update(msg)
	case m.palette:
		m.paletteInput, cmd = m.paletteInput.Update(msg)
	case m.screen == scrRef && m.refFiltering:
		m.refInput, cmd = m.refInput.Update(msg)
	case m.cmdMode:
		m.cmdInput, cmd = m.cmdInput.Update(msg)
	case m.filtering:
		m.filter, cmd = m.filter.Update(msg)
	case m.screen == scrCreate:
		if idx := m.currentInput(); idx >= 0 {
			m.inputs[idx], cmd = m.inputs[idx].Update(msg)
		}
	}
	return m, cmd
}

// ---------- mouse ----------

func (m Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.help || m.palette || m.modal != modalNone || m.cmdMode || m.filtering {
		return m, nil
	}
	if m.screen == scrOp {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.scrollOp(-3), nil
		case tea.MouseButtonWheelDown:
			return m.scrollOp(3), nil
		}
		return m, nil
	}
	if m.screen != scrDash {
		return m, nil
	}
	g := m.geom()
	inNodes := g.nodes.has(msg.X, msg.Y)
	inClusters := g.clusters.has(msg.X, msg.Y)
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		d := 1
		if msg.Button == tea.MouseButtonWheelUp {
			d = -1
		}
		if inNodes {
			m.focus = paneNodes
		} else if inClusters {
			m.focus = paneClusters
		}
		return m.moveFocused(d)
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	now := time.Now()
	double := !m.lastClick.IsZero() && now.Sub(m.lastClick) < 400*time.Millisecond && msg.Y == m.lastClickY
	m.lastClick, m.lastClickY = now, msg.Y
	switch {
	case inClusters:
		vis := m.visible()
		off := tableOffset(m.cursor, len(vis), g.clusters.h-3)
		idx := off + msg.Y - g.clusters.y - 2
		m.focus = paneClusters
		if idx < 0 || idx >= len(vis) {
			return m, nil
		}
		m.cursor = idx
		mm, cmd := m.afterMove()
		if double {
			m2, c2 := mm.(Model).enterNodes()
			return m2, tea.Batch(cmd, c2)
		}
		return mm, cmd
	case inNodes:
		nodes := m.currentNodes()
		off := tableOffset(m.nodeCursor, len(nodes), g.nodes.h-3)
		idx := off + msg.Y - g.nodes.y - 2
		m.focus = paneNodes
		if idx < 0 || idx >= len(nodes) {
			return m, nil
		}
		m.nodeCursor = idx
		if double {
			return m.shellNode(m.selectedNode())
		}
	}
	return m, nil
}

// ---------- selection ----------

func (m Model) visible() []engine.Cluster {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		return m.clusters
	}
	out := make([]engine.Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		if strings.Contains(strings.ToLower(c.Name+" "+string(c.Status)), q) {
			out = append(out, c)
		}
	}
	return out
}

func (m Model) selected() (engine.Cluster, bool) {
	vis := m.visible()
	if len(vis) == 0 || m.cursor < 0 || m.cursor >= len(vis) {
		return engine.Cluster{}, false
	}
	return vis[m.cursor], true
}

func (m Model) syncList() Model {
	n := len(m.visible())
	m.cursor = clamp(m.cursor, 0, n-1)
	m.nodeCursor = clamp(m.nodeCursor, 0, len(m.currentNodes())-1)
	return m
}

func (m Model) applyClusters(cs []engine.Cluster) Model {
	name := ""
	if c, ok := m.selected(); ok {
		name = c.Name
	}
	m.clusters = cs
	for i, c := range m.visible() {
		if c.Name == name {
			m.cursor = i
			break
		}
	}
	return m.syncList()
}

// currentNodes is the fresh container list overlaid with the last detail
// (IP, readiness, stats) for the selected cluster.
func (m Model) currentNodes() []engine.Node {
	c, ok := m.selected()
	if !ok {
		return nil
	}
	nodes := append([]engine.Node(nil), c.Nodes...)
	if m.detail == nil || m.detailName != c.Name {
		return nodes
	}
	byName := map[string]engine.Node{}
	for _, n := range m.detail.Cluster.Nodes {
		byName[n.Name] = n
	}
	for i := range nodes {
		d, ok := byName[nodes[i].Name]
		if !ok {
			continue
		}
		nodes[i].IP, nodes[i].Image, nodes[i].Ports = d.IP, d.Image, d.Ports
		nodes[i].Ready, nodes[i].Kubelet, nodes[i].OS, nodes[i].Runtime = d.Ready, d.Kubelet, d.OS, d.Runtime
		if strings.EqualFold(nodes[i].State, "running") {
			nodes[i].CPU, nodes[i].Mem, nodes[i].MemPct = d.CPU, d.Mem, d.MemPct
		}
	}
	return nodes
}

func (m Model) selectedNode() (engine.Node, bool) {
	nodes := m.currentNodes()
	if m.focus != paneNodes || m.nodeCursor < 0 || m.nodeCursor >= len(nodes) {
		return engine.Node{}, false
	}
	return nodes[m.nodeCursor], true
}

// firstNode is the selected cluster's control plane.
func (m Model) firstNode() (engine.Node, bool) {
	nodes := m.currentNodes()
	if len(nodes) == 0 {
		return engine.Node{}, false
	}
	return nodes[0], true
}

// ---------- actions ----------

func (m Model) nodeReady(n engine.Node, ok bool) (Model, bool) {
	if !ok {
		return m.note("select a node first (tab or enter)"), false
	}
	if !strings.EqualFold(n.State, "running") {
		return m.warn(n.Name + " is " + n.State + " — press u to start it"), false
	}
	if m.eng == nil {
		return m.warn("engine is not ready"), false
	}
	return m, true
}

func (m Model) shellNode(n engine.Node, ok bool) (Model, tea.Cmd) {
	m, ready := m.nodeReady(n, ok)
	if !ready {
		return m, nil
	}
	cmd, err := m.eng.ShellCmd(n.Name)
	if err != nil {
		return m.warn(err.Error()), nil
	}
	name := n.Name
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return shellDoneMsg{what: name, err: err}
	})
}

func (m Model) nodeLogs(n engine.Node, ok bool) (Model, tea.Cmd) {
	if !ok {
		return m.note("select a node first"), nil
	}
	eng := m.eng
	return m.begin("logs", "Logs("+n.Name+") · container log", m.shellFor("logs", n.Name), 30*time.Minute, true, func(ctx context.Context, emit func(string)) error {
		return eng.NodeLogs(ctx, n.Name, true, emit)
	})
}

func (m Model) journal(n engine.Node, ok bool, unit string) (Model, tea.Cmd) {
	m, ready := m.nodeReady(n, ok)
	if !ready {
		return m, nil
	}
	if !engine.ValidIdent(unit) {
		return m.warn("bad unit name " + unit), nil
	}
	eng := m.eng
	return m.begin("journal", "Journal("+n.Name+") · "+unit, strings.Replace(m.shellFor("journal", n.Name), "UNIT", unit, 1), 30*time.Minute, true, func(ctx context.Context, emit func(string)) error {
		return eng.Journal(ctx, n.Name, unit, true, emit)
	})
}

func (m Model) describeNode(n engine.Node, ok bool) (Model, tea.Cmd) {
	c, cok := m.clusterOf(n)
	m, ready := m.nodeReady(n, ok)
	if !ready || !cok {
		return m, nil
	}
	eng := m.eng
	return m.begin("describe", "Describe(node/"+n.Name+")", m.shellFor("describe", n.Name), time.Minute, false, func(ctx context.Context, emit func(string)) error {
		return eng.DescribeNode(ctx, c, n.Name, emit)
	})
}

func (m Model) nodePods(n engine.Node, ok bool) (Model, tea.Cmd) {
	c, cok := m.clusterOf(n)
	m, ready := m.nodeReady(n, ok)
	if !ready || !cok {
		return m, nil
	}
	eng := m.eng
	return m.begin("pods", "Pods(node/"+n.Name+")", m.shellFor("pods", n.Name), time.Minute, false, func(ctx context.Context, emit func(string)) error {
		return eng.NodePods(ctx, c, n.Name, emit)
	})
}

func (m Model) clusterOf(n engine.Node) (string, bool) {
	for _, c := range m.clusters {
		for _, x := range c.Nodes {
			if x.Name == n.Name {
				return c.Name, true
			}
		}
	}
	return "", false
}

func (m Model) nodeAct(verb string, n engine.Node, ok bool) (tea.Model, tea.Cmd) {
	if !ok {
		return m.note("select a node first"), nil
	}
	if verb == "start" && strings.EqualFold(n.State, "running") {
		return m.note(n.Name + " is already running"), nil
	}
	return m.runAct("node-"+verb, engine.Cluster{}, n.Name, "")
}

func (m Model) confirmNode(verb string, n engine.Node, ok bool) (tea.Model, tea.Cmd) {
	if !ok {
		return m.note("select a node first"), nil
	}
	if verb == "stop" && !strings.EqualFold(n.State, "running") {
		return m.note(n.Name + " is not running"), nil
	}
	m.modal = modalConfirm
	m.modalAct = "node-" + verb
	m.modalNode = n.Name
	m.modalTitle = strings.ToUpper(verb[:1]) + verb[1:] + " node"
	body := n.Name + "  (" + n.Role + ")\n\n"
	if n.Role == "control-plane" {
		body += paint(amberC).Render("This is the control plane. The API server is unavailable\nuntil it is running again.")
	} else {
		body += "Pods on this node stop until it is running again."
	}
	m.modalBody = body
	return m, nil
}

func (m Model) openK9s() (Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok {
		return m.note("no cluster selected"), nil
	}
	if c.Status != engine.StatusRunning && c.Status != engine.StatusDegraded {
		return m.warn("start " + c.Name + " before opening k9s"), nil
	}
	if m.eng == nil {
		return m.warn("engine is not ready"), nil
	}
	eng := m.eng
	name := c.Name
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd, err := eng.K9sCmd(ctx, name)
		return execMsg{what: "k9s", cmd: cmd, err: err}
	}
}

func (m Model) openCreate() (Model, tea.Cmd) {
	if m.toolsReady && m.tools.Kind == "" {
		return m.warn("kind is not installed"), nil
	}
	if m.toolsReady && !m.tools.DaemonOK {
		return m.warn(m.bannerText()), nil
	}
	m.screen = scrCreate
	m.formErr = ""
	m.formField = 0
	m = m.blur()
	return m, m.inputs[0].Focus()
}

func (m Model) startSelected() (Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok {
		return m.note("no cluster selected"), nil
	}
	if c.Status == engine.StatusRunning {
		return m.note(c.Name + " is already running"), nil
	}
	eng := m.eng
	return m.begin("start", "Start "+c.Name, m.shellFor("start", ""), 3*time.Minute, true, func(ctx context.Context, emit func(string)) error {
		return eng.Start(ctx, c, emit)
	})
}

func (m Model) confirmLife(act, title, body string) (Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok {
		return m.note("no cluster selected"), nil
	}
	if act == "stop" && c.Status == engine.StatusStopped {
		return m.note(c.Name + " is already stopped"), nil
	}
	m.modal = modalConfirm
	m.modalAct = act
	m.modalTitle = title
	m.modalBody = bold(textC).Render(c.Name) + "  " + paint(dimC).Render(c.Topology()) + "\n\n" + body
	m.modalCluster = c
	return m, nil
}

func (m Model) confirmDelete() (Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok {
		return m.note("no cluster selected"), nil
	}
	m.modal = modalType
	m.modalAct = "delete"
	m.modalTitle = "Delete " + c.Name
	m.modalBody = paint(redC).Render("This runs kind delete cluster and removes all "+strconv.Itoa(len(c.Nodes))+" nodes.") +
		"\nType the cluster name to confirm."
	m.modalCluster = c
	m.modalInput.SetValue("")
	m.modalInput.Placeholder = c.Name
	return m, m.modalInput.Focus()
}

func (m Model) openImage() (Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok {
		return m.note("no cluster selected"), nil
	}
	m.modal = modalImage
	m.modalAct = "image"
	m.modalTitle = "Load image into " + c.Name
	m.modalBody = "A local image to copy onto every node, for example nginx:1.27"
	m.modalCluster = c
	m.modalInput.SetValue("")
	m.modalInput.Placeholder = "nginx:1.27"
	return m, m.modalInput.Focus()
}

func (m Model) useContext() (Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok {
		return m.note("no cluster selected"), nil
	}
	if m.eng == nil {
		return m.warn("engine is not ready"), nil
	}
	eng := m.eng
	name := c.Name
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := eng.UseContext(ctx, name); err != nil {
			return toastMsg{err: err}
		}
		return toastMsg{text: "kubectl context → kind-" + name}
	}
}

func (m Model) exportLogs() (Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok {
		return m.note("no cluster selected"), nil
	}
	eng := m.eng
	return m.begin("export", "Export logs "+c.Name, m.shellFor("export", ""), 5*time.Minute, true, func(ctx context.Context, emit func(string)) error {
		_, err := eng.ExportLogs(ctx, c.Name, emit)
		return err
	})
}

func (m Model) startRuntime() (Model, tea.Cmd) {
	if !m.toolsReady {
		return m.note("still checking the runtime"), nil
	}
	if m.tools.DaemonOK {
		return m.note("runtime is already running"), nil
	}
	if m.tools.StartWith == "" {
		if m.tools.Hint != "" {
			return m.warn(m.tools.Hint), nil
		}
		return m.warn("no runtime to start"), nil
	}
	title := "Start Colima"
	if m.tools.StartWith == "podman" {
		title = "Start Podman machine"
	}
	eng := m.eng
	return m.begin("runtime", title, m.shellFor("runtime", ""), 10*time.Minute, true, func(ctx context.Context, emit func(string)) error {
		return eng.StartRuntime(ctx, emit)
	})
}

func (m Model) submitCreate() (Model, tea.Cmd) {
	spec, err := m.spec()
	if err != nil {
		m.formErr = err.Error()
		return m, nil
	}
	for _, c := range m.clusters {
		if c.Name == spec.Name {
			m.formErr = "cluster " + spec.Name + " already exists"
			return m, nil
		}
	}
	eng := m.eng
	timeout := 15*time.Minute + spec.Wait
	return m.begin("create", "Create "+spec.Name, engine.CreatePreview(spec), timeout, true, func(ctx context.Context, emit func(string)) error {
		return eng.Create(ctx, spec, emit)
	})
}

func (m Model) spec() (engine.CreateSpec, error) {
	name := strings.TrimSpace(m.inputs[0].Value())
	if !engine.ValidName(name) {
		return engine.CreateSpec{}, errors.New("name must be a lowercase DNS label")
	}
	image := strings.TrimSpace(m.inputs[2].Value())
	if !engine.ValidImage(image) {
		return engine.CreateSpec{}, errors.New("image contains unsupported characters")
	}
	waitStr := strings.TrimSpace(m.inputs[3].Value())
	if waitStr == "" {
		waitStr = "180"
	}
	secs, err := strconv.Atoi(waitStr)
	if err != nil || secs < 0 || secs > 3600 {
		return engine.CreateSpec{}, errors.New("wait must be 0–3600 seconds")
	}
	spec := engine.CreateSpec{Name: name, Image: image, Wait: time.Duration(secs) * time.Second}
	switch m.shape {
	case shapeRepo:
		if m.repoPath == "" {
			return spec, errors.New("no kind config found; set KIND_CONFIG or switch to custom")
		}
		spec.ConfigPath = m.repoPath
	case shapeCustom:
		w, err := strconv.Atoi(strings.TrimSpace(m.inputs[1].Value()))
		if err != nil || w < 0 || w > 20 {
			return spec, errors.New("workers must be 0–20")
		}
		spec.Workers = w
	}
	return spec, nil
}

func (m Model) resizeInputs() Model {
	w := clamp(m.width-24, 16, 64)
	for i := range m.inputs {
		m.inputs[i].Width = w
	}
	m.inputs[1].Width = 8
	m.inputs[3].Width = 8
	m.filter.Width = clamp(m.width-8, 12, 60)
	m.cmdInput.Width = clamp(m.width-8, 12, 80)
	m.modalInput.Width = clamp(m.width-16, 12, 48)
	return m
}
