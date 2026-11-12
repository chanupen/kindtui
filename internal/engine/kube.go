package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Detail loads containers plus, when the control plane is running, kubectl data.
func (e *Engine) Detail(ctx context.Context, name string) (Detail, error) {
	if !ValidName(name) {
		return Detail{}, fmt.Errorf("unsafe cluster name %q", name)
	}
	clusters, err := e.List(ctx)
	if err != nil {
		return Detail{}, err
	}
	var cluster Cluster
	found := false
	for _, c := range clusters {
		if c.Name == name {
			cluster = c
			found = true
			break
		}
	}
	if !found {
		return Detail{}, fmt.Errorf("cluster %s not found", name)
	}
	d := Detail{Cluster: cluster}
	if cluster.Status == StatusStopped {
		e.attachIPs(ctx, d.Cluster.Nodes)
		return d, nil
	}
	// Container facts and kubectl queries are independent; run them together.
	var wg sync.WaitGroup
	var mu sync.Mutex
	insp := make([]Node, len(cluster.Nodes))
	copy(insp, cluster.Nodes)
	statNodes := make([]Node, len(cluster.Nodes))
	copy(statNodes, cluster.Nodes)
	wg.Add(2)
	go func() { defer wg.Done(); e.attachIPs(ctx, insp) }()
	go func() { defer wg.Done(); d.Stats = e.stats(ctx, statNodes) }()

	var kube map[string]Node
	if e.Tools().Kubectl == "" {
		d.KubeErr = "kubectl is not installed"
	} else if kubeconfig, kerr := e.ensureKubeconfig(ctx, name); kerr != nil {
		d.KubeErr = kerr.Error()
	} else {
		setErr := func(msg string) {
			mu.Lock()
			if d.KubeErr == "" {
				d.KubeErr = msg
			}
			mu.Unlock()
		}
		wg.Add(3)
		go func() {
			defer wg.Done()
			raw, err := e.kubectl(ctx, kubeconfig, name, "get", "nodes", "-o", "json")
			if err != nil {
				setErr(trimErr(raw, err))
				return
			}
			k, _, perr := ParseKubeNodes([]byte(raw))
			if perr != nil {
				setErr(perr.Error())
				return
			}
			mu.Lock()
			kube = k
			mu.Unlock()
		}()
		go func() {
			defer wg.Done()
			raw, err := e.kubectl(ctx, kubeconfig, name, "get", "pods", "-A", "-o", "json")
			if err != nil {
				setErr(trimErr(raw, err))
				return
			}
			if pods, perr := ParsePods([]byte(raw)); perr == nil {
				mu.Lock()
				d.Pods = pods
				mu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			raw, err := e.kubectl(ctx, kubeconfig, name, "get", "events", "-A", "-o", "json")
			if err == nil {
				if events, perr := ParseEvents([]byte(raw)); perr == nil {
					mu.Lock()
					d.Events = events
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	for i := range d.Cluster.Nodes {
		d.Cluster.Nodes[i].IP = insp[i].IP
		d.Cluster.Nodes[i].Image = insp[i].Image
		d.Cluster.Nodes[i].Ports = insp[i].Ports
		d.Cluster.Nodes[i].CPU = statNodes[i].CPU
		d.Cluster.Nodes[i].Mem = statNodes[i].Mem
		d.Cluster.Nodes[i].MemPct = statNodes[i].MemPct
	}
	if kube != nil {
		ApplyKube(d.Cluster.Nodes, kube)
	}
	return d, nil
}

const inspectFormat = `{{.Name}}{{"\t"}}{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}{{"\t"}}{{.Config.Image}}{{"\t"}}{{range $p, $c := .NetworkSettings.Ports}}{{range $c}}{{.HostIp}}:{{.HostPort}}->{{$p}} {{end}}{{end}}`

// attachIPs fills IP, image, and published ports from one inspect call.
func (e *Engine) attachIPs(ctx context.Context, nodes []Node) {
	if len(nodes) == 0 {
		return
	}
	args := []string{"inspect", "--format", inspectFormat}
	for _, n := range nodes {
		if ValidIdent(n.Name) {
			args = append(args, n.Name)
		}
	}
	out, err := e.captureOut(ctx, e.Tools().Runtime, args...)
	if err != nil && out == "" {
		return
	}
	rows := ParseInspect(out)
	for i := range nodes {
		if r, ok := rows[nodes[i].Name]; ok {
			nodes[i].IP = r.IP
			nodes[i].Image = r.Image
			nodes[i].Ports = r.Ports
		}
	}
}

func (e *Engine) stats(ctx context.Context, nodes []Node) []Stat {
	var names []string
	for _, n := range nodes {
		if strings.EqualFold(n.State, "running") && ValidIdent(n.Name) {
			names = append(names, n.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"stats", "--no-stream", "--format", "{{json .}}"}, names...)
	out, err := e.captureOut(ctx, e.Tools().Runtime, args...)
	if err != nil {
		return nil
	}
	stats := ParseStats(out)
	byName := map[string]Stat{}
	for _, s := range stats {
		byName[s.Name] = s
	}
	for i := range nodes {
		if s, ok := byName[nodes[i].Name]; ok {
			nodes[i].CPU = s.CPU
			nodes[i].Mem = s.Mem
			nodes[i].MemPct = s.MemPct
		}
	}
	return stats
}

func (e *Engine) ensureKubeconfig(ctx context.Context, name string) (string, error) {
	if err := e.requireKind(); err != nil {
		return "", err
	}
	e.mu.Lock()
	if e.kube == nil {
		e.kube = map[string]string{}
	}
	if p := e.kube[name]; p != "" {
		if _, err := os.Stat(p); err == nil {
			e.mu.Unlock()
			return p, nil
		}
	}
	e.mu.Unlock()

	out, err := e.captureOut(ctx, e.Tools().Kind, "get", "kubeconfig", "--name", name)
	if err != nil {
		return "", fmt.Errorf("kubeconfig: %w", err)
	}
	if i := strings.Index(out, "apiVersion:"); i > 0 {
		out = out[i:]
	}
	if !strings.Contains(out, "apiVersion:") {
		return "", fmt.Errorf("kubeconfig for %s was empty", name)
	}
	path := filepath.Join(os.TempDir(), "kindtui-"+name+".kubeconfig")
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return "", err
	}
	e.mu.Lock()
	e.kube[name] = path
	e.mu.Unlock()
	return path, nil
}

func (e *Engine) kubectl(ctx context.Context, kubeconfig, cluster string, args ...string) (string, error) {
	full := []string{"--kubeconfig", kubeconfig, "--context", "kind-" + cluster}
	full = append(full, args...)
	return e.captureOut(ctx, e.Tools().Kubectl, full...)
}

// Logs streams kubectl logs for one pod.
func (e *Engine) Logs(ctx context.Context, cluster, namespace, pod string, follow bool, onLine func(string)) error {
	if !ValidName(cluster) || !ValidIdent(namespace) || !ValidIdent(pod) {
		return fmt.Errorf("unsafe log target")
	}
	if e.Tools().Kubectl == "" {
		return fmt.Errorf("kubectl is not installed")
	}
	kubeconfig, err := e.ensureKubeconfig(ctx, cluster)
	if err != nil {
		return err
	}
	args := []string{"--kubeconfig", kubeconfig, "--context", "kind-" + cluster,
		"logs", "-n", namespace, pod, "--tail=300"}
	if follow {
		args = append(args, "-f")
	}
	return streamCmd(ctx, e.environ(), "", e.Tools().Kubectl, args, onLine)
}

// K9sCmd runs k9s against this cluster. The kubeconfig is the temp file from
// kind get kubeconfig, so the user's default kubeconfig is left alone.
func (e *Engine) K9sCmd(ctx context.Context, name string) (*exec.Cmd, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("unsafe cluster name %q", name)
	}
	bin, err := exec.LookPath("k9s")
	if err != nil {
		return nil, fmt.Errorf("k9s is not installed")
	}
	kubeconfig, err := e.ensureKubeconfig(ctx, name)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "--kubeconfig", kubeconfig, "--context", "kind-"+name)
	cmd.Env = e.environ()
	return cmd, nil
}

// UseContext merges the cluster kubeconfig into the default file and selects it.
func (e *Engine) UseContext(ctx context.Context, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("unsafe cluster name %q", name)
	}
	if err := e.requireKind(); err != nil {
		return err
	}
	if _, err := e.capture(ctx, e.Tools().Kind, "export", "kubeconfig", "--name", name); err != nil {
		return fmt.Errorf("export kubeconfig: %w", err)
	}
	if e.Tools().Kubectl == "" {
		return nil
	}
	if _, err := e.capture(ctx, e.Tools().Kubectl, "config", "use-context", "kind-"+name); err != nil {
		return fmt.Errorf("use context: %w", err)
	}
	return nil
}

// DescribeNode streams `kubectl describe node`.
func (e *Engine) DescribeNode(ctx context.Context, cluster, node string, onLine func(string)) error {
	if !ValidName(cluster) || !ValidIdent(node) {
		return fmt.Errorf("unsafe node target")
	}
	if e.Tools().Kubectl == "" {
		return fmt.Errorf("kubectl is not installed")
	}
	kubeconfig, err := e.ensureKubeconfig(ctx, cluster)
	if err != nil {
		return err
	}
	return streamCmd(ctx, e.environ(), "", e.Tools().Kubectl,
		[]string{"--kubeconfig", kubeconfig, "--context", "kind-" + cluster, "describe", "node", node}, onLine)
}

// NodePods streams `kubectl get pods -A -o wide` filtered to one node.
func (e *Engine) NodePods(ctx context.Context, cluster, node string, onLine func(string)) error {
	if !ValidName(cluster) || !ValidIdent(node) {
		return fmt.Errorf("unsafe node target")
	}
	if e.Tools().Kubectl == "" {
		return fmt.Errorf("kubectl is not installed")
	}
	kubeconfig, err := e.ensureKubeconfig(ctx, cluster)
	if err != nil {
		return err
	}
	return streamCmd(ctx, e.environ(), "", e.Tools().Kubectl,
		[]string{"--kubeconfig", kubeconfig, "--context", "kind-" + cluster,
			"get", "pods", "-A", "-o", "wide", "--field-selector", "spec.nodeName=" + node}, onLine)
}

func trimErr(out string, err error) string {
	msg := strings.TrimSpace(out)
	if msg == "" {
		return err.Error()
	}
	if len(msg) > 400 {
		msg = msg[:400]
	}
	return msg
}

// ForgetKubeconfig drops a cached kubeconfig. Called after delete.
func (e *Engine) ForgetKubeconfig(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p := e.kube[name]; p != "" {
		_ = os.Remove(p)
		delete(e.kube, name)
	}
}
