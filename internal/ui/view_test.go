package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/chanupen/kindtui/internal/engine"
)

func strip(s string) string { return ansi.Strip(s) }

func sample() Model {
	m := NewWith(nil)
	m.width = 120
	m.height = 36
	m.now = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	m.toolsReady = true
	m.toolsBusy = false
	m.spinning = false
	m.listLoaded = true
	m.tools = engine.Tools{
		RuntimeName: "docker", ClientVer: "29.8.2", KindVer: "0.33.0", KubectlVer: "1.32.1",
		DaemonOK: true, Kind: "/usr/bin/kind", Kubectl: "/usr/bin/kubectl", Runtime: "/usr/bin/docker",
		Context: "kind-dev",
	}
	created := m.now.Add(-3 * time.Hour)
	m.clusters = []engine.Cluster{
		{
			Name: "dev", Status: engine.StatusRunning, Created: created,
			Nodes: []engine.Node{
				{Name: "dev-control-plane", Role: "control-plane", State: "running", Created: created},
				{Name: "dev-worker", Role: "worker", State: "running", Created: created},
				{Name: "dev-worker2", Role: "worker", State: "exited", Created: created},
			},
		},
		{
			Name: "old", Status: engine.StatusStopped, Created: created,
			Nodes: []engine.Node{{Name: "old-control-plane", Role: "control-plane", State: "exited", Created: created}},
		},
	}
	d := engine.Detail{Cluster: m.clusters[0]}
	d.Cluster.Nodes = append([]engine.Node(nil), m.clusters[0].Nodes...)
	d.Cluster.Nodes[0].IP, d.Cluster.Nodes[0].Ready, d.Cluster.Nodes[0].Kubelet = "172.18.0.2", "True", "v1.33.1"
	d.Cluster.Nodes[0].Ports = "127.0.0.1:41234->6443/tcp"
	d.Cluster.Nodes[1].IP, d.Cluster.Nodes[1].Ready, d.Cluster.Nodes[1].CPU = "172.18.0.3", "True", "3.2%"
	d.Pods = []engine.Pod{{Namespace: "kube-system", Name: "a", Phase: "Running", Node: "dev-worker"}}
	m.detail, m.detailName = &d, "dev"
	return m.syncList()
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "ctrl+d":
			msg = tea.KeyMsg{Type: tea.KeyCtrlD}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func checkFrame(t *testing.T, m Model) string {
	t.Helper()
	raw := m.View()
	lines := strings.Split(raw, "\n")
	if len(lines) != m.height {
		t.Fatalf("height %d, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line %d width %d, want %d: %q", i, w, m.width, strip(l))
		}
	}
	return strip(raw)
}

func TestDashboardShowsEveryNode(t *testing.T) {
	for _, w := range []int{120, 90, 160} {
		m := sample()
		m.width = w
		view := checkFrame(t, m)
		for _, want := range []string{"Clusters[2]", "Nodes(dev)[3]", "dev-control-plane", "dev-worker2", "172.18.0.3",
			"Overview(dev)", "https://127.0.0.1:41234", "Runtime:", "kind-dev"} {
			if !strings.Contains(view, want) {
				t.Fatalf("width %d missing %q\n%s", w, want, view)
			}
		}
	}
}

func TestBannerWhenDaemonDown(t *testing.T) {
	m := sample()
	m.tools.DaemonOK = false
	m.tools.StartWith = "colima"
	m.tools.Hint = "Docker is installed, but the daemon is down."
	view := checkFrame(t, m)
	if !strings.Contains(view, "press C to start colima") {
		t.Fatalf("banner missing\n%s", view)
	}
}

func TestNodeFocusAndActions(t *testing.T) {
	m := press(sample(), "tab", "down")
	if m.focus != paneNodes {
		t.Fatal("tab should focus nodes")
	}
	n, ok := m.selectedNode()
	if !ok || n.Name != "dev-worker" {
		t.Fatalf("selected node %+v", n)
	}
	view := checkFrame(t, m)
	if !strings.Contains(view, "shell (login)") || !strings.Contains(view, "kubelet logs") {
		t.Fatalf("node hints missing\n%s", view)
	}
	// A stopped node cannot be logged into.
	stopped := press(m, "down", "enter")
	if !strings.Contains(stopped.toast, "dev-worker2 is exited") {
		t.Fatalf("toast %q", stopped.toast)
	}
	// Stop asks first.
	confirm := press(m, "x")
	if confirm.modal != modalConfirm || confirm.modalNode != "dev-worker" || confirm.modalAct != "node-stop" {
		t.Fatalf("modal %v %q %q", confirm.modal, confirm.modalNode, confirm.modalAct)
	}
	if v := checkFrame(t, confirm); !strings.Contains(v, "Stop node") {
		t.Fatalf("confirm view\n%s", v)
	}
	back := press(m, "esc")
	if back.focus != paneClusters {
		t.Fatal("esc should return to clusters")
	}
}

func TestDeleteNeedsName(t *testing.T) {
	m := press(sample(), "ctrl+d")
	if m.modal != modalType || m.modalCluster.Name != "dev" {
		t.Fatalf("modal %v", m.modal)
	}
	if v := checkFrame(t, m); !strings.Contains(v, "Delete dev") {
		t.Fatalf("view\n%s", v)
	}
	m = press(m, "dex", "enter")
	if m.modal != modalType || !strings.Contains(m.toast, "type dev") {
		t.Fatalf("wrong name should not delete: %v %q", m.modal, m.toast)
	}
}

func TestFilterHelpAndResize(t *testing.T) {
	m := press(sample(), "/", "o", "l", "d", "enter")
	if c, ok := m.selected(); !ok || c.Name != "old" {
		t.Fatalf("filter selected %+v %v", c, ok)
	}
	h := press(sample(), "?")
	if v := checkFrame(t, h); !strings.Contains(v, "COMMANDS") || !strings.Contains(v, "shell into node") {
		t.Fatalf("help\n%s", v)
	}
	if press(h, "esc").help {
		t.Fatal("esc should close help")
	}
	small := sample()
	small.width, small.height = 40, 10
	if v := strip(small.View()); !strings.Contains(v, "60×16") {
		t.Fatalf("small:\n%s", v)
	}
	compact := sample()
	compact.height = 20
	if v := checkFrame(t, compact); !strings.Contains(v, "<enter> nodes") {
		t.Fatalf("compact view should carry hints in the footer\n%s", v)
	}
}

func TestCreateScreen(t *testing.T) {
	m := press(sample(), "n")
	if m.screen != scrCreate {
		t.Fatalf("screen %d", m.screen)
	}
	m.shape = shapeSingle
	if v := checkFrame(t, m); !strings.Contains(v, "Create cluster") || !strings.Contains(v, "kind create cluster") {
		t.Fatalf("create\n%s", v)
	}
}

func TestCommandLine(t *testing.T) {
	m := sample()
	if got := m.complete("shell dev-w"); got != "shell dev-worker" {
		t.Fatalf("complete %q", got)
	}
	if got := m.complete("kub"); got != "kubelet " {
		t.Fatalf("complete %q", got)
	}
	if n, ok := m.findNode("worker2"); !ok || n.Name != "dev-worker2" {
		t.Fatalf("suffix lookup %+v", n)
	}
	r := press(m, ":", "shell dev-worker", "enter")
	if !strings.Contains(r.toast, "engine is not ready") {
		t.Fatalf("shell toast %q", r.toast)
	}
	r = press(m, ":", "bogus", "enter")
	if !strings.Contains(r.toast, "unknown command") {
		t.Fatalf("toast %q", r.toast)
	}
}

func TestK9sNeedsRunningCluster(t *testing.T) {
	m := press(sample(), "down", "o")
	if !strings.Contains(m.toast, "start old") {
		t.Fatalf("toast %q", m.toast)
	}
}

func TestMouseSelectsNode(t *testing.T) {
	m := sample()
	g := m.geom()
	next, _ := m.Update(tea.MouseMsg{X: g.nodes.x + 3, Y: g.nodes.y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := next.(Model)
	if got.focus != paneNodes || got.nodeCursor != 1 {
		t.Fatalf("focus %v cursor %d", got.focus, got.nodeCursor)
	}
}

func TestColWidthsDropsLowPriority(t *testing.T) {
	cols := []column{{title: "A", min: 10, prio: 0}, {title: "B", min: 10, prio: 5}, {title: "C", min: 10, prio: 1}}
	ws := colWidths(cols, 22)
	if ws[0] != 10 || ws[1] != 0 || ws[2] != 10 {
		t.Fatalf("widths %v", ws)
	}
}
