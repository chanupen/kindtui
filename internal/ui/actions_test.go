package ui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRegistryIsConsistent(t *testing.T) {
	ids := map[string]bool{}
	cmds := map[string]string{}
	keys := map[string]string{}
	for _, a := range actions {
		if ids[a.id] {
			t.Fatalf("duplicate id %q", a.id)
		}
		ids[a.id] = true
		if a.desc == "" || a.run == nil {
			t.Fatalf("%s needs desc and run", a.id)
		}
		for _, c := range append([]string{a.cmd}, a.aliases...) {
			if c == "" {
				continue
			}
			if prev, ok := cmds[c]; ok {
				t.Fatalf(":%s used by %s and %s", c, prev, a.id)
			}
			cmds[c] = a.id
		}
		for _, k := range a.keys {
			slot := a.scope.String() + "/" + k
			if prev, ok := keys[slot]; ok {
				t.Fatalf("key %s used by %s and %s", slot, prev, a.id)
			}
			keys[slot] = a.id
		}
	}
	// A global key must not be shadowed by a pane key without intent.
	for slot, id := range keys {
		if !strings.HasPrefix(slot, "global/") {
			continue
		}
		k := strings.TrimPrefix(slot, "global/")
		for _, p := range []string{"cluster/", "node/"} {
			if other, ok := keys[p+k]; ok {
				t.Fatalf("key %q is global (%s) and %s (%s)", k, id, p, other)
			}
		}
	}
	for p, list := range hintIDs {
		for _, id := range list {
			if actionByID(id) == nil {
				t.Fatalf("pane %v hint %q has no action", p, id)
			}
		}
	}
}

func TestCheatsheetMatchesDocs(t *testing.T) {
	want := Cheatsheet()
	got, err := os.ReadFile("../../docs/commands.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("docs/commands.md is stale: run `make docs`")
	}
	for _, s := range []string{"docker exec -it <node> bash -l", "kind delete cluster --name <cluster>", "journalctl -u kubelet"} {
		if !strings.Contains(want, s) {
			t.Fatalf("cheatsheet missing %q", s)
		}
	}
}

func TestReferenceScreenFillsSelection(t *testing.T) {
	m := press(sample(), "tab", "down", "K")
	if m.screen != scrRef {
		t.Fatalf("screen %d", m.screen)
	}
	v := checkFrame(t, m)
	for _, want := range []string{
		"Commands(dev › dev-worker)",
		"docker exec -it dev-worker bash -l",
		"docker start dev-control-plane dev-worker dev-worker2",
	} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q\n%s", want, v)
		}
	}
	if got := m.target("").stop(); strings.Join(got, " ") != "dev-worker2 dev-worker dev-control-plane" {
		t.Fatalf("stop order %v", got)
	}
	d := press(m, "/", "describe", "enter")
	if v := checkFrame(t, d); !strings.Contains(v, "$ kubectl --context kind-dev describe node dev-worker") {
		t.Fatalf("filtered reference\n%s", v)
	}
	f := press(m, "/", "kubelet", "enter")
	if items := matchActions(f.refInput.Value(), false); len(items) == 0 || items[0].id != "kubelet" {
		t.Fatalf("filter first item %v", items)
	}
	if back := press(f, "esc", "esc"); back.screen != scrDash {
		t.Fatalf("esc should leave reference, screen %d", back.screen)
	}
	if r := press(sample(), ":", "cmds", "enter"); r.screen != scrRef {
		t.Fatal(":cmds should open the reference")
	}
}

func TestPaletteSearchAndRun(t *testing.T) {
	m := sample()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Model)
	if !m.palette {
		t.Fatal("ctrl+p should open the palette")
	}
	m = press(m, "kubel")
	v := checkFrame(t, m)
	if !strings.Contains(v, "Command palette") || !strings.Contains(v, "$ docker exec dev-control-plane journalctl -u kubelet") {
		t.Fatalf("palette view\n%s", v)
	}
	m = press(m, "enter")
	if m.palette || !strings.Contains(m.toast, "engine is not ready") {
		t.Fatalf("palette run: open=%v toast=%q", m.palette, m.toast)
	}
	stop := press(sample(), "tab")
	next, _ = stop.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	stop = press(next.(Model), "stop", "enter")
	if stop.modal != modalConfirm {
		t.Fatalf("stop from palette should confirm, modal %v", stop.modal)
	}
}

func TestPaneKeysDispatchByFocus(t *testing.T) {
	c := press(sample(), "x")
	if c.modal != modalConfirm || c.modalAct != "stop" {
		t.Fatalf("cluster x: %v %q", c.modal, c.modalAct)
	}
	n := press(sample(), "tab", "down", "x")
	if n.modal != modalConfirm || n.modalAct != "node-stop" || n.modalNode != "dev-worker" {
		t.Fatalf("node x: %v %q %q", n.modal, n.modalAct, n.modalNode)
	}
	cmd := press(sample(), ":", "node-restart worker", "enter")
	if cmd.modalAct != "node-restart" || cmd.modalNode != "dev-worker" {
		t.Fatalf(":node-restart: %q %q", cmd.modalAct, cmd.modalNode)
	}
}

func TestOpViewShowsCommand(t *testing.T) {
	m := sample()
	m.screen = scrOp
	m.opTitle = "Logs(dev-worker)"
	m.opCmd = m.shellFor("logs", "dev-worker")
	m.opLines = []string{"hello"}
	v := checkFrame(t, m)
	if !strings.Contains(v, "$ docker logs -f --tail 300 dev-worker") || !strings.Contains(v, "hello") {
		t.Fatalf("op view\n%s", v)
	}
}
