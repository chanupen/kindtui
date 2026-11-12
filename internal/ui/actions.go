package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chanupen/kindtui/internal/engine"
)

// scope says where an action's key works.
type scope int

const (
	scopeGlobal scope = iota
	scopeCluster
	scopeNode
)

func (s scope) String() string {
	switch s {
	case scopeCluster:
		return "cluster"
	case scopeNode:
		return "node"
	}
	return "global"
}

// action is one thing kindtui can do. This registry is the single source for
// key dispatch, header hints, the help overlay, the : command line, the
// command palette, the command reference screen, and `kindtui --cheatsheet`.
type action struct {
	id      string
	keys    []string // as tea.KeyMsg.String() reports them
	cmd     string   // ":" command name, "" when there is none
	aliases []string
	arg     string // argument shown in docs, e.g. "NODE"
	scope   scope
	hint    string // short label for the header; "" keeps it out of the header
	desc    string // one-line description
	// shell is the equivalent command a user could run themselves.
	shell func(t target) string
	// run performs the action. arg is everything after the : command name.
	run func(m Model, arg string) (tea.Model, tea.Cmd)
}

func (a *action) keyLabel() string {
	if len(a.keys) == 0 {
		return ""
	}
	return prettyKey(a.keys[0])
}

func (a *action) cmdLabel() string {
	if a.cmd == "" {
		return ""
	}
	if a.arg != "" {
		return ":" + a.cmd + " " + a.arg
	}
	return ":" + a.cmd
}

func prettyKey(k string) string {
	switch k {
	case "ctrl+d":
		return "ctrl-d"
	case "ctrl+r":
		return "ctrl-r"
	case "ctrl+p":
		return "ctrl-p"
	}
	return k
}

// target is what an action's shell command is about. In docs it holds
// placeholders; in the UI it holds the selected cluster and node.
type target struct {
	rt      string
	cluster string
	node    string
	start   []string // node containers in start order
	create  string   // kind create preview from the form
	runtime string   // colima or podman when the daemon can be started
}

func genericTarget() target {
	return target{
		rt:      "docker",
		cluster: "<cluster>",
		node:    "<node>",
		start:   []string{"<cluster>-control-plane", "<cluster>-worker", "…"},
	}
}

// stop is the start order reversed: workers first, control plane last.
func (t target) stop() []string {
	for _, n := range t.start {
		if n == "…" {
			return []string{"<cluster>-worker", "…", "<cluster>-control-plane"}
		}
	}
	out := make([]string, 0, len(t.start))
	for i := len(t.start) - 1; i >= 0; i-- {
		out = append(out, t.start[i])
	}
	return out
}

func (t target) ctx() string { return "kind-" + t.cluster }

// target builds the shell-command target for the current selection.
// node overrides the node; empty means the selected node or the control plane.
func (m Model) target(node string) target {
	t := target{rt: "docker", cluster: "<cluster>", node: "<node>"}
	if m.tools.RuntimeName != "" {
		t.rt = m.tools.RuntimeName
	}
	if c, ok := m.selected(); ok {
		t.cluster = c.Name
		for _, n := range engine.OrderStart(c.Nodes) {
			t.start = append(t.start, n.Name)
		}
	}
	if len(t.start) == 0 {
		t.start = genericTarget().start
	}
	if node != "" {
		t.node = node
	} else if n, ok := m.selectedNode(); ok {
		t.node = n.Name
	} else if n, ok := m.firstNode(); ok {
		t.node = n.Name
	}
	if spec, err := m.spec(); err == nil {
		t.create = engine.CreatePreview(spec)
	}
	t.runtime = m.tools.StartWith
	return t
}

// shellFor is the equivalent command for an action id, for the op view.
func (m Model) shellFor(id, node string) string {
	a := actionByID(id)
	if a == nil || a.shell == nil {
		return ""
	}
	return a.shell(m.target(node))
}

var actions []*action

func init() { actions = buildActions() }

func actionByID(id string) *action {
	for _, a := range actions {
		if a.id == id {
			return a
		}
	}
	return nil
}

func actionByCmd(name string) *action {
	for _, a := range actions {
		if a.cmd == name {
			return a
		}
		for _, al := range a.aliases {
			if al == name {
				return a
			}
		}
	}
	return nil
}

// actionByKey finds the action bound to key in the focused pane.
// Pane-scoped bindings win over global ones.
func actionByKey(key string, focus pane) *action {
	want := scopeCluster
	if focus == paneNodes {
		want = scopeNode
	}
	var global *action
	for _, a := range actions {
		for _, k := range a.keys {
			if k != key {
				continue
			}
			if a.scope == want {
				return a
			}
			if a.scope == scopeGlobal && global == nil {
				global = a
			}
		}
	}
	return global
}

// commandNames lists every : command and alias, for completion.
func commandNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range actions {
		for _, n := range append([]string{a.cmd}, a.aliases...) {
			if n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// nodeArg resolves an optional node argument: a name in any cluster, a role
// suffix of the selected cluster, or empty for the selected node / control plane.
func (m Model) nodeArg(arg string) (engine.Node, bool, string) {
	arg = strings.TrimSpace(arg)
	if arg != "" {
		if n, ok := m.findNode(arg); ok {
			return n, true, ""
		}
		return engine.Node{}, false, "no node named " + arg
	}
	if n, ok := m.selectedNode(); ok {
		return n, true, ""
	}
	if n, ok := m.firstNode(); ok {
		return n, true, ""
	}
	return engine.Node{}, false, "no node selected"
}

func onNode(fn func(m Model, n engine.Node, ok bool) (Model, tea.Cmd)) func(Model, string) (tea.Model, tea.Cmd) {
	return func(m Model, arg string) (tea.Model, tea.Cmd) {
		n, ok, why := m.nodeArg(arg)
		if !ok {
			return m.warn(why), nil
		}
		return fn(m, n, ok)
	}
}

func plain(fn func(m Model) (Model, tea.Cmd)) func(Model, string) (tea.Model, tea.Cmd) {
	return func(m Model, _ string) (tea.Model, tea.Cmd) { return fn(m) }
}

func buildActions() []*action {
	return []*action{
		// ----- global -----
		{id: "help", keys: []string{"?"}, cmd: "help", aliases: []string{"h"}, scope: scopeGlobal, hint: "help",
			desc: "Help overlay with every key",
			run:  func(m Model, _ string) (tea.Model, tea.Cmd) { m.help = true; return m, nil }},
		{id: "command", keys: []string{":"}, scope: scopeGlobal, hint: "command",
			desc: "Command line (tab completes commands and node names)",
			run:  plain(func(m Model) (Model, tea.Cmd) { return m.openCmd() })},
		{id: "palette", keys: []string{"ctrl+p"}, cmd: "palette", scope: scopeGlobal, hint: "palette",
			desc: "Search every action by name and run it",
			run:  plain(func(m Model) (Model, tea.Cmd) { return m.openPalette() })},
		{id: "reference", keys: []string{"K"}, cmd: "cmds", aliases: []string{"commands", "ref"}, scope: scopeGlobal, hint: "cmd reference",
			desc: "Command reference: every action with its equivalent shell command",
			run:  plain(func(m Model) (Model, tea.Cmd) { return m.openRef() })},
		{id: "filter", keys: []string{"/"}, cmd: "filter", arg: "TEXT", scope: scopeGlobal, hint: "filter",
			desc: "Filter clusters by name or status",
			shell: func(t target) string {
				return "kind get clusters | grep TEXT"
			},
			// The / key opens the filter prompt (onDash); :filter TEXT sets it, :filter clears it.
			run: func(m Model, arg string) (tea.Model, tea.Cmd) {
				m.filter.SetValue(strings.TrimSpace(arg))
				return m.afterMove()
			}},
		{id: "refresh", keys: []string{"ctrl+r", "R"}, cmd: "refresh", scope: scopeGlobal,
			desc: "Refresh clusters and node detail now",
			shell: func(t target) string {
				return t.rt + " ps -a --filter label=io.x-k8s.kind.cluster"
			},
			run: plain(func(m Model) (Model, tea.Cmd) {
				m.detailName = ""
				m, c1 := m.refresh()
				m, c2 := m.requestDetail()
				return m.note("refreshing…"), tea.Batch(c1, c2)
			})},
		{id: "create", keys: []string{"n"}, cmd: "create", aliases: []string{"new"}, scope: scopeGlobal, hint: "new",
			desc: "Create a cluster",
			shell: func(t target) string {
				if t.create != "" {
					return t.create
				}
				return "kind create cluster --name NAME --config FILE --wait 180s"
			},
			run: plain(func(m Model) (Model, tea.Cmd) { return m.openCreate() })},
		{id: "k9s", keys: []string{"o"}, cmd: "k9s", scope: scopeGlobal, hint: "k9s",
			desc: "Open k9s on the selected cluster",
			shell: func(t target) string {
				return "k9s --context " + t.ctx()
			},
			run: plain(func(m Model) (Model, tea.Cmd) { return m.openK9s() })},
		{id: "context", keys: []string{"c"}, cmd: "context", aliases: []string{"ctx", "use"}, scope: scopeGlobal, hint: "use context",
			desc: "Make kind-<cluster> the current kubectl context",
			shell: func(t target) string {
				return "kind export kubeconfig --name " + t.cluster + " && kubectl config use-context " + t.ctx()
			},
			run: plain(func(m Model) (Model, tea.Cmd) { return m.useContext() })},
		{id: "info", keys: []string{"I"}, cmd: "info", scope: scopeGlobal,
			desc: "Tool paths and versions",
			shell: func(t target) string {
				return t.rt + " version && kind version && kubectl version --client"
			},
			run: func(m Model, _ string) (tea.Model, tea.Cmd) { m.screen = scrInfo; return m, nil }},
		{id: "runtime", keys: []string{"C"}, cmd: "runtime", scope: scopeGlobal,
			desc: "Start Colima or the Podman machine when the runtime is down",
			shell: func(t target) string {
				if t.runtime == "podman" {
					return "podman machine start"
				}
				return "colima start"
			},
			run: plain(func(m Model) (Model, tea.Cmd) { return m.startRuntime() })},
		{id: "quit", keys: []string{"q"}, cmd: "quit", aliases: []string{"q", "exit", "q!"}, scope: scopeGlobal, hint: "quit",
			desc: "Quit kindtui",
			run:  func(m Model, _ string) (tea.Model, tea.Cmd) { return m.quit() }},

		// ----- clusters pane -----
		{id: "nodes", keys: []string{"enter", "right"}, cmd: "nodes", aliases: []string{"no", "node"}, arg: "[CLUSTER]", scope: scopeCluster, hint: "nodes",
			desc: "Open the nodes of the cluster",
			shell: func(t target) string {
				return "kubectl --context " + t.ctx() + " get nodes -o wide"
			},
			run: func(m Model, arg string) (tea.Model, tea.Cmd) {
				if arg = strings.TrimSpace(arg); arg != "" {
					mm, ok := m.selectCluster(arg)
					if !ok {
						return m.warn("no cluster named " + arg), nil
					}
					m = mm
				}
				return m.enterNodes()
			}},
		{id: "cluster", cmd: "cluster", arg: "NAME", scope: scopeCluster,
			desc: "Select a cluster by name",
			shell: func(t target) string {
				return "kind get clusters"
			},
			run: func(m Model, arg string) (tea.Model, tea.Cmd) {
				mm, ok := m.selectCluster(strings.TrimSpace(arg))
				if !ok {
					return m.warn("no cluster named " + arg), nil
				}
				mm.focus = paneClusters
				return mm.afterMove()
			}},
		{id: "start", keys: []string{"s"}, cmd: "start", scope: scopeCluster, hint: "start",
			desc: "Start every node, control plane first",
			shell: func(t target) string {
				return t.rt + " start " + strings.Join(t.start, " ")
			},
			run: plain(func(m Model) (Model, tea.Cmd) { return m.startSelected() })},
		{id: "stop", keys: []string{"x"}, cmd: "stop", scope: scopeCluster, hint: "stop",
			desc: "Stop every node, workers first (asks first)",
			shell: func(t target) string {
				return t.rt + " stop " + strings.Join(t.stop(), " ")
			},
			run: plain(func(m Model) (Model, tea.Cmd) {
				return m.confirmLife("stop", "Stop cluster", "Stop every node container? The cluster can be started again.")
			})},
		{id: "restart", keys: []string{"r"}, cmd: "restart", scope: scopeCluster, hint: "restart",
			desc: "Stop, then start every node (asks first)",
			shell: func(t target) string {
				return t.rt + " stop " + strings.Join(t.stop(), " ") + " && " + t.rt + " start " + strings.Join(t.start, " ")
			},
			run: plain(func(m Model) (Model, tea.Cmd) {
				return m.confirmLife("restart", "Restart cluster", "Stop the nodes, then start them, control plane first.")
			})},
		{id: "delete", keys: []string{"ctrl+d", "D"}, cmd: "delete", aliases: []string{"del", "rm"}, scope: scopeCluster, hint: "delete",
			desc: "Delete the cluster (type its name to confirm)",
			shell: func(t target) string {
				return "kind delete cluster --name " + t.cluster
			},
			run: plain(func(m Model) (Model, tea.Cmd) { return m.confirmDelete() })},
		{id: "image", keys: []string{"i"}, cmd: "image", aliases: []string{"load"}, arg: "[IMAGE]", scope: scopeCluster, hint: "load image",
			desc: "Load a local image onto every node",
			shell: func(t target) string {
				return "kind load docker-image IMAGE --name " + t.cluster
			},
			run: func(m Model, arg string) (tea.Model, tea.Cmd) {
				if arg = strings.TrimSpace(arg); arg != "" {
					c, ok := m.selected()
					if !ok {
						return m.note("no cluster selected"), nil
					}
					return m.runAct("image", c, "", arg)
				}
				return m.openImage()
			}},
		{id: "export", keys: []string{"e"}, cmd: "export", scope: scopeCluster, hint: "export logs",
			desc: "Export node logs to ~/kind-logs/<cluster>-<time>",
			shell: func(t target) string {
				return "kind export logs --name " + t.cluster + " ~/kind-logs/" + t.cluster + "-$(date +%Y%m%d-%H%M%S)"
			},
			run: plain(func(m Model) (Model, tea.Cmd) { return m.exportLogs() })},

		// ----- nodes pane -----
		{id: "shell", keys: []string{"enter", "s"}, cmd: "shell", aliases: []string{"ssh", "login", "exec", "sh"}, arg: "[NODE]", scope: scopeNode, hint: "shell (login)",
			desc: "Log in to the node as root (bash, or sh)",
			shell: func(t target) string {
				return t.rt + " exec -it " + t.node + " bash -l"
			},
			run: onNode(func(m Model, n engine.Node, ok bool) (Model, tea.Cmd) { return m.shellNode(n, ok) })},
		{id: "logs", keys: []string{"l"}, cmd: "logs", aliases: []string{"log"}, arg: "[NODE]", scope: scopeNode, hint: "node logs",
			desc: "Follow the node container log",
			shell: func(t target) string {
				return t.rt + " logs -f --tail 300 " + t.node
			},
			run: onNode(func(m Model, n engine.Node, ok bool) (Model, tea.Cmd) { return m.nodeLogs(n, ok) })},
		{id: "kubelet", keys: []string{"b"}, cmd: "kubelet", arg: "[NODE]", scope: scopeNode, hint: "kubelet logs",
			desc: "Follow the kubelet journal inside the node",
			shell: func(t target) string {
				return t.rt + " exec " + t.node + " journalctl -u kubelet -f -n 300"
			},
			run: onNode(func(m Model, n engine.Node, ok bool) (Model, tea.Cmd) { return m.journal(n, ok, "kubelet") })},
		{id: "containerd", keys: []string{"J"}, cmd: "containerd", arg: "[NODE]", scope: scopeNode,
			desc: "Follow the containerd journal inside the node",
			shell: func(t target) string {
				return t.rt + " exec " + t.node + " journalctl -u containerd -f -n 300"
			},
			run: onNode(func(m Model, n engine.Node, ok bool) (Model, tea.Cmd) { return m.journal(n, ok, "containerd") })},
		{id: "journal", cmd: "journal", arg: "UNIT [NODE]", scope: scopeNode,
			desc: "Follow any systemd unit's journal inside the node",
			shell: func(t target) string {
				return t.rt + " exec " + t.node + " journalctl -u UNIT -f -n 300"
			},
			run: func(m Model, arg string) (tea.Model, tea.Cmd) {
				f := strings.Fields(arg)
				unit, node := "kubelet", ""
				if len(f) > 0 {
					unit = f[0]
				}
				if len(f) > 1 {
					node = f[1]
				}
				n, ok, why := m.nodeArg(node)
				if !ok {
					return m.warn(why), nil
				}
				return m.journal(n, ok, unit)
			}},
		{id: "describe", keys: []string{"d"}, cmd: "describe", aliases: []string{"desc"}, arg: "[NODE]", scope: scopeNode, hint: "describe",
			desc: "kubectl describe node",
			shell: func(t target) string {
				return "kubectl --context " + t.ctx() + " describe node " + t.node
			},
			run: onNode(func(m Model, n engine.Node, ok bool) (Model, tea.Cmd) { return m.describeNode(n, ok) })},
		{id: "pods", keys: []string{"p"}, cmd: "pods", aliases: []string{"po"}, arg: "[NODE]", scope: scopeNode, hint: "pods on node",
			desc: "Pods scheduled on the node",
			shell: func(t target) string {
				return "kubectl --context " + t.ctx() + " get pods -A -o wide --field-selector spec.nodeName=" + t.node
			},
			run: onNode(func(m Model, n engine.Node, ok bool) (Model, tea.Cmd) { return m.nodePods(n, ok) })},
		{id: "node-start", keys: []string{"u"}, cmd: "node-start", arg: "[NODE]", scope: scopeNode, hint: "start node",
			desc: "Start this node container",
			shell: func(t target) string {
				return t.rt + " start " + t.node
			},
			run: func(m Model, arg string) (tea.Model, tea.Cmd) {
				n, ok, why := m.nodeArg(arg)
				if !ok {
					return m.warn(why), nil
				}
				return m.nodeAct("start", n, ok)
			}},
		{id: "node-stop", keys: []string{"x"}, cmd: "node-stop", arg: "[NODE]", scope: scopeNode, hint: "stop node",
			desc: "Stop this node container (asks first)",
			shell: func(t target) string {
				return t.rt + " stop " + t.node
			},
			run: func(m Model, arg string) (tea.Model, tea.Cmd) {
				n, ok, why := m.nodeArg(arg)
				if !ok {
					return m.warn(why), nil
				}
				return m.confirmNode("stop", n, ok)
			}},
		{id: "node-restart", keys: []string{"r"}, cmd: "node-restart", arg: "[NODE]", scope: scopeNode, hint: "restart node",
			desc: "Restart this node container (asks first)",
			shell: func(t target) string {
				return t.rt + " restart " + t.node
			},
			run: func(m Model, arg string) (tea.Model, tea.Cmd) {
				n, ok, why := m.nodeArg(arg)
				if !ok {
					return m.warn(why), nil
				}
				return m.confirmNode("restart", n, ok)
			}},
		{id: "back", keys: []string{"esc", "left", "h", "backspace"}, cmd: "clusters", aliases: []string{"cl"}, scope: scopeNode, hint: "clusters",
			desc: "Back to the clusters pane",
			run:  func(m Model, _ string) (tea.Model, tea.Cmd) { m.focus = paneClusters; return m, nil }},
	}
}

// hintIDs orders the header hints per pane. Less important ones go last,
// because a narrow header drops columns from the right.
var hintIDs = map[pane][]string{
	paneClusters: {"nodes", "create", "start", "stop", "restart", "delete", "k9s", "context", "image", "export",
		"filter", "command", "palette", "reference", "help", "quit"},
	paneNodes: {"shell", "logs", "kubelet", "describe", "pods", "node-start", "node-stop", "node-restart", "k9s",
		"back", "command", "palette", "reference", "help", "quit"},
}

func paneHints(p pane) []hint {
	var out []hint
	for _, id := range hintIDs[p] {
		if a := actionByID(id); a != nil {
			out = append(out, hint{a.keyLabel(), a.hint})
		}
	}
	return out
}

// Cheatsheet renders the registry as Markdown. `kindtui --cheatsheet` prints
// it and docs/commands.md is generated from it.
func Cheatsheet() string {
	var b strings.Builder
	t := genericTarget()
	b.WriteString("# Command reference\n\n")
	b.WriteString("<!-- Generated by `kindtui --cheatsheet`. Do not edit by hand; run `make docs`. -->\n\n")
	b.WriteString("Every kindtui action, its key, its `:` command, and the shell command it is equivalent to. ")
	b.WriteString("Inside kindtui, press `K` (or `:cmds`) for the same table filled in with the selected cluster and node, ")
	b.WriteString("`y` to copy a command, and `ctrl-p` to search and run any action.\n\n")
	b.WriteString("Placeholders: `<cluster>`, `<node>` (a node container such as `<cluster>-worker`), `docker` (or `podman`). ")
	b.WriteString("`kubectl --context kind-<cluster>` works once `kind create` or `kind export kubeconfig` has written the context.\n")
	for _, sc := range []scope{scopeGlobal, scopeCluster, scopeNode} {
		title := map[scope]string{scopeGlobal: "Global", scopeCluster: "Clusters pane", scopeNode: "Nodes pane"}[sc]
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		b.WriteString("| Key | Command | Action | Equivalent shell command |\n| --- | --- | --- | --- |\n")
		for _, a := range actions {
			if a.scope != sc {
				continue
			}
			keys := make([]string, len(a.keys))
			for i, k := range a.keys {
				keys[i] = "`" + prettyKey(k) + "`"
			}
			cmd := ""
			if a.cmd != "" {
				cmd = "`" + a.cmdLabel() + "`"
			}
			sh := ""
			if a.shell != nil {
				sh = "`" + strings.ReplaceAll(a.shell(t), "|", "\\|") + "`"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", strings.Join(keys, " "), cmd, a.desc, sh)
		}
	}
	b.WriteString("\n## Navigation\n\n| Key | Action |\n| --- | --- |\n")
	for _, h := range navHelp {
		fmt.Fprintf(&b, "| `%s` | %s |\n", h.key, h.desc)
	}
	return b.String()
}

var navHelp = []hint{
	{"tab", "Switch between the clusters and nodes panes"},
	{"↑ ↓ / k j", "Move"},
	{"pgup pgdn", "Move 10 rows"},
	{"g G / home end", "First / last row"},
	{"mouse", "Click selects, double-click opens nodes or logs in, wheel scrolls"},
}
