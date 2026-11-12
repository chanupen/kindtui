package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseClusterPS(t *testing.T) {
	out := strings.Join([]string{
		"aaa\tkind-worker\tkind\tworker\trunning\tUp 3 hours\t2026-10-25 10:00:02 +0000 UTC",
		"bbb\tkind-control-plane\tkind\tcontrol-plane\trunning\tUp 3 hours\t2026-10-25 10:00:00 +0000 UTC",
		"ccc\tdev-control-plane\tdev\tcontrol-plane\texited\tExited (0)\t2026-10-20 10:00:00 +0000 UTC",
		"bad line",
		"ddd\t/other-worker\tother\tworker\trunning\tUp\t2026-10-25 11:00:00 +0000 UTC",
		"",
	}, "\n")
	clusters := ParseClusterPS(out)
	if len(clusters) != 3 {
		t.Fatalf("clusters: %d", len(clusters))
	}
	if clusters[0].Name != "kind" || clusters[0].Status != StatusRunning {
		t.Fatalf("first: %+v", clusters[0])
	}
	if clusters[0].Nodes[0].Role != "control-plane" || clusters[0].Nodes[0].Name != "kind-control-plane" {
		t.Fatalf("node order: %+v", clusters[0].Nodes)
	}
	if clusters[0].Created.IsZero() {
		t.Fatal("missing created")
	}
	if clusters[1].Name != "dev" || clusters[1].Status != StatusStopped {
		t.Fatalf("stopped cluster: %+v", clusters[1])
	}
	if clusters[2].Name != "other" || clusters[2].Status != StatusBroken {
		t.Fatalf("worker-only cluster should be broken: %+v", clusters[2])
	}
}

func TestDeriveStatus(t *testing.T) {
	nodes := []Node{{Role: "control-plane", State: "running"}, {Role: "worker", State: "exited"}}
	if DeriveStatus(nodes) != StatusDegraded {
		t.Fatal(DeriveStatus(nodes))
	}
	nodes[1].State = "running"
	if DeriveStatus(nodes) != StatusRunning {
		t.Fatal(DeriveStatus(nodes))
	}
}

func TestOrderStartStop(t *testing.T) {
	nodes := []Node{{Name: "w", Role: "worker"}, {Name: "c", Role: "control-plane"}}
	start := OrderStart(nodes)
	if start[0].Name != "c" || start[1].Name != "w" {
		t.Fatalf("start %+v", start)
	}
	stop := OrderStop(nodes)
	if stop[0].Name != "w" || stop[1].Name != "c" {
		t.Fatalf("stop %+v", stop)
	}
}

func TestParsePodsAndEvents(t *testing.T) {
	pods, err := ParsePods([]byte(`{"items":[
		{"metadata":{"name":"ok","namespace":"default","creationTimestamp":"2026-10-25T00:00:00Z"},
		 "spec":{"nodeName":"n1"},
		 "status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":1},{"ready":false,"restartCount":2}]}},
		{"metadata":{"name":"bad","namespace":"kube-system","creationTimestamp":"2026-10-25T00:00:00Z"},
		 "spec":{"nodeName":"n1"},
		 "status":{"phase":"Pending","containerStatuses":null}}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 2 || pods[0].Name != "bad" {
		t.Fatalf("sort: %+v", pods)
	}
	if pods[1].Ready != "1/2" || pods[1].Restarts != 3 {
		t.Fatalf("ready: %+v", pods[1])
	}
	events, err := ParseEvents([]byte(`{"items":[
		{"metadata":{"namespace":"default","creationTimestamp":"2026-10-25T00:00:00Z"},
		 "type":"Warning","reason":"BackOff","message":"back off","count":4,
		 "involvedObject":{"kind":"Pod","name":"ok"}},
		{"metadata":{"namespace":"default"},"type":"Normal","reason":"Pulled","message":"ok",
		 "lastTimestamp":"2026-10-25T01:00:00Z","involvedObject":{"kind":"Pod","name":"ok"}}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Reason != "Pulled" {
		t.Fatalf("events: %+v", events)
	}
	if events[1].Object != "pod/ok" || events[1].Count != 4 {
		t.Fatalf("warning: %+v", events[1])
	}
	warn := WarningEvents(events, 5)
	if len(warn) != 1 || warn[0].Reason != "BackOff" {
		t.Fatalf("warn %+v", warn)
	}
}

func TestParseKubeNodes(t *testing.T) {
	kube, ver, err := ParseKubeNodes([]byte(`{"items":[{"metadata":{"name":"kind-control-plane"},
		"status":{"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.32.2"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if ver != "v1.32.2" || kube["kind-control-plane"].Ready != "True" {
		t.Fatalf("%s %+v", ver, kube)
	}
	nodes := []Node{{Name: "kind-control-plane", Role: "control-plane"}}
	ApplyKube(nodes, kube)
	if KubeVersion(nodes) != "v1.32.2" {
		t.Fatal(KubeVersion(nodes))
	}
}

func TestParseStats(t *testing.T) {
	stats := ParseStats("noise\n{\"Name\":\"/kind-control-plane\",\"CPUPerc\":\"1.50%\",\"MemUsage\":\"10MiB / 1GiB\",\"MemPerc\":\"2.00%\",\"NetIO\":\"1B / 2B\",\"BlockIO\":\"3B / 4B\",\"PIDs\":\"9\"}\n")
	if len(stats) != 1 || stats[0].Name != "kind-control-plane" || stats[0].CPU != "1.50%" {
		t.Fatalf("%+v", stats)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kind-cluster-config.yaml")
	if err := os.WriteFile(path, []byte("kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nnodes:\n- role: control-plane\n- role: worker\n- role: worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIND_CONFIG", "")
	found, err := FindConfig(dir)
	if err != nil || found != path {
		t.Fatalf("found %s %v", found, err)
	}
	sum, err := ParseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ControlPlanes != 1 || sum.Workers != 2 || sum.Topology() != "1 control-plane · 2 workers" {
		t.Fatalf("%+v %s", sum, sum.Topology())
	}
	missing := filepath.Join(dir, "sub")
	if err := os.Mkdir(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(missing, "kind.yaml")
	if err := os.WriteFile(bare, []byte("kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err = ParseConfig(bare)
	if err != nil {
		t.Fatal(err)
	}
	if sum.NodesSet || sum.ControlPlanes != 1 || sum.Name != "demo" {
		t.Fatalf("default nodes: %+v", sum)
	}
	body := RenderConfig("demo", 1)
	rendered := filepath.Join(dir, "out.yaml")
	if err := os.WriteFile(rendered, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err = ParseConfig(rendered)
	if err != nil || sum.Workers != 1 || sum.Name != "demo" {
		t.Fatalf("render %+v %v", sum, err)
	}
	t.Setenv("KIND_CONFIG", rendered)
	found, err = FindConfig(dir)
	if err != nil || found != rendered {
		t.Fatalf("env %s %v", found, err)
	}
}

func TestValidators(t *testing.T) {
	if !ValidName("kind-1") || ValidName("Kind") || ValidName("-a") || ValidName("") {
		t.Fatal("name")
	}
	if !ValidImage("") || !ValidImage("kindest/node:v1.32.2") || ValidImage("bad image") || ValidImage("a;b") {
		t.Fatal("image")
	}
	preview := CreatePreview(CreateSpec{Name: "dev", Workers: 2, Wait: 3 * time.Minute})
	if !strings.Contains(preview, "--name dev") || !strings.Contains(preview, "--wait 3m0s") {
		t.Fatal(preview)
	}
}

func TestDiscoverPrefersLiveDaemonAndSkipsBrokenLink(t *testing.T) {
	root := t.TempDir()
	downDir := filepath.Join(root, "down")
	upDir := filepath.Join(root, "up")
	brokenDir := filepath.Join(root, "broken")
	for _, d := range []string{downDir, upDir, brokenDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeScript(t, filepath.Join(downDir, "docker"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo "Docker version 29.8.2, build x"; exit 0; fi
if [ "$1" = "info" ]; then echo "down" >&2; exit 1; fi
exit 1
`)
	writeScript(t, filepath.Join(upDir, "podman"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo "podman version 5.1.0"; exit 0; fi
if [ "$1" = "info" ]; then exit 0; fi
if [ "$1" = "version" ]; then echo "5.1.0"; exit 0; fi
exit 0
`)
	writeScript(t, filepath.Join(root, "kind"), "#!/bin/sh\necho 'kind v0.33.0 go1.22'\n")
	writeScript(t, filepath.Join(root, "kubectl"), "#!/bin/sh\necho '{\"clientVersion\":{\"gitVersion\":\"v1.32.1\"}}'\n")
	writeScript(t, filepath.Join(root, "colima"), "#!/bin/sh\necho 'colima version 0.10.3'\n")
	if err := os.Symlink(filepath.Join(root, "missing-orbstack"), filepath.Join(brokenDir, "docker")); err != nil {
		t.Fatal(err)
	}
	path := strings.Join([]string{brokenDir, downDir, upDir, root}, string(os.PathListSeparator))
	tools := Discover(context.Background(), path, nil)
	if tools.RuntimeName != "podman" || !tools.DaemonOK {
		t.Fatalf("want live podman, got %+v", tools)
	}
	if tools.ClientVer != "5.1.0" {
		t.Fatalf("client %q", tools.ClientVer)
	}
	if len(tools.Broken) == 0 || !strings.Contains(tools.Broken[0], "missing-orbstack") {
		t.Fatalf("broken: %+v", tools.Broken)
	}
	if tools.KindVer != "0.33.0" || tools.KubectlVer != "1.32.1" || tools.ColimaVer != "0.10.3" {
		t.Fatalf("tools %+v", tools)
	}
	if tools.StartWith != "" {
		t.Fatalf("daemon is up, start=%q", tools.StartWith)
	}
}

func TestDiscoverDockerClientBeatsStoppedPodman(t *testing.T) {
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "docker"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo "Docker version 29.0.0, build x"; exit 0; fi
if [ "$1" = "info" ]; then exit 1; fi
exit 1
`)
	writeScript(t, filepath.Join(root, "podman"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo "podman version 5.8.9"; exit 0; fi
exit 1
`)
	tools := Discover(context.Background(), root, nil)
	if tools.RuntimeName != "docker" || tools.DaemonOK {
		t.Fatalf("%+v", tools)
	}
	if tools.ClientVer != "29.0.0" {
		t.Fatalf("ver %s", tools.ClientVer)
	}
}

func TestDiscoverPodmanDockerShim(t *testing.T) {
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "docker"), `#!/bin/sh
echo "Emulate Docker CLI using podman. Create /etc/containers/nodocker to quiet msg." >&2
if [ "$1" = "--version" ]; then echo "podman version 6.1.3"; exit 0; fi
if [ "$1" = "info" ]; then echo "host:"; exit 0; fi
if [ "$1" = "version" ]; then echo "6.1.3"; exit 0; fi
exit 0
`)
	writeScript(t, filepath.Join(root, "podman"), `#!/bin/sh
if [ "$1" = "--version" ]; then echo "podman version 6.1.3"; exit 0; fi
if [ "$1" = "info" ]; then exit 0; fi
if [ "$1" = "version" ]; then echo "6.1.3"; exit 0; fi
exit 0
`)
	tools := Discover(context.Background(), root, nil)
	if tools.RuntimeName != "podman" || !tools.DaemonOK {
		t.Fatalf("want podman shim, got %+v", tools)
	}
	if tools.ClientVer != "6.1.3" {
		t.Fatalf("ver %s", tools.ClientVer)
	}
}

func TestStreamEchoAndCancel(t *testing.T) {
	var lines []string
	err := streamCmd(context.Background(), os.Environ(), "", "/bin/echo", []string{"hello"}, func(s string) {
		lines = append(lines, s)
	})
	if err != nil || len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("echo %v %q", err, lines)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	start := time.Now()
	err = streamCmd(ctx, os.Environ(), "", "/bin/sleep", []string{"30"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("cancel was too slow")
	}
}

func TestLiveDiscover(t *testing.T) {
	if os.Getenv("KINDTUI_LIVE") == "" {
		t.Skip("set KINDTUI_LIVE=1 to probe this machine")
	}
	tools := Discover(context.Background(), os.Getenv("PATH"), ExtraRuntimes())
	t.Logf("runtime=%s (%s) daemon=%v client=%s server=%s kind=%s kubectl=%s colima=%s start=%s",
		tools.Runtime, tools.RuntimeName, tools.DaemonOK, tools.ClientVer, tools.ServerVer,
		tools.KindVer, tools.KubectlVer, tools.ColimaVer, tools.StartWith)
	t.Logf("hint=%s", tools.Hint)
	for _, b := range tools.Broken {
		t.Logf("broken %s", b)
	}
	if tools.Kind == "" || tools.Kubectl == "" {
		t.Fatal("expected kind and kubectl")
	}
	if tools.Runtime == "" {
		t.Fatal("expected a container client")
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestParseStatsPodmanNumbers(t *testing.T) {
	out := `{"Name":"lab-worker","CPU":59.46,"MemUsage":2817196032,"MemLimit":24550367232,"MemPerc":11.47}
{"Name":"dev-worker","CPUPerc":"1.25%","MemUsage":"120MiB / 7.6GiB","MemPerc":"1.5%"}`
	got := ParseStats(out)
	if len(got) != 2 {
		t.Fatalf("rows %d", len(got))
	}
	if got[0].CPU != "59.5%" || got[0].Mem != "2.6GiB / 22.9GiB" || got[0].MemPct != "11.5%" {
		t.Fatalf("podman row %+v", got[0])
	}
	if got[1].CPU != "1.25%" || got[1].Mem != "120MiB / 7.6GiB" {
		t.Fatalf("docker row %+v", got[1])
	}
}

func TestParseInspect(t *testing.T) {
	out := "/lab-control-plane\t10.89.1.2 \tdocker.io/kindest/node@sha256:abc\t127.0.0.1:39795->6443/tcp \n"
	r := ParseInspect(out)["lab-control-plane"]
	if r.IP != "10.89.1.2" || r.Image != "kindest/node" || r.Ports != "127.0.0.1:39795->6443/tcp" {
		t.Fatalf("%+v", r)
	}
}
