package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Create runs `kind create cluster` and streams its log.
func (e *Engine) Create(ctx context.Context, spec CreateSpec, onLine func(string)) error {
	if err := e.requireKind(); err != nil {
		return err
	}
	if err := e.requireDaemon(); err != nil {
		return err
	}
	if !ValidName(spec.Name) {
		return fmt.Errorf("invalid cluster name %q", spec.Name)
	}
	if spec.Image != "" && !ValidImage(spec.Image) {
		return fmt.Errorf("invalid image %q", spec.Image)
	}
	if spec.Workers < 0 || spec.Workers > 20 {
		return fmt.Errorf("worker count must be between 0 and 20")
	}
	args := []string{"create", "cluster", "--name", spec.Name}
	var cleanup string
	if spec.ConfigPath != "" {
		if !isFile(spec.ConfigPath) {
			return fmt.Errorf("config %s is not a file", spec.ConfigPath)
		}
		args = append(args, "--config", spec.ConfigPath)
	} else {
		body := RenderConfig(spec.Name, spec.Workers)
		f, err := os.CreateTemp("", "kindtui-*.yaml")
		if err != nil {
			return err
		}
		cleanup = f.Name()
		if _, err := f.WriteString(body); err != nil {
			f.Close()
			os.Remove(cleanup)
			return err
		}
		if err := f.Close(); err != nil {
			os.Remove(cleanup)
			return err
		}
		args = append(args, "--config", cleanup)
	}
	if cleanup != "" {
		defer os.Remove(cleanup)
	}
	if spec.Image != "" {
		args = append(args, "--image", spec.Image)
	}
	if spec.Wait > 0 {
		args = append(args, "--wait", spec.Wait.String())
	}
	return streamCmd(ctx, e.environ(), "", e.Tools().Kind, args, onLine)
}

// Delete runs `kind delete cluster`.
func (e *Engine) Delete(ctx context.Context, name string, onLine func(string)) error {
	if err := e.requireKind(); err != nil {
		return err
	}
	if err := e.requireDaemon(); err != nil {
		return err
	}
	if !ValidName(name) {
		return fmt.Errorf("invalid cluster name %q", name)
	}
	err := streamCmd(ctx, e.environ(), "", e.Tools().Kind, []string{"delete", "cluster", "--name", name}, onLine)
	e.ForgetKubeconfig(name)
	return err
}

// Start starts node containers, control plane first.
func (e *Engine) Start(ctx context.Context, c Cluster, onLine func(string)) error {
	return e.eachNode(ctx, OrderStart(c.Nodes), "start", true, onLine)
}

// Stop stops node containers, workers first.
func (e *Engine) Stop(ctx context.Context, c Cluster, onLine func(string)) error {
	return e.eachNode(ctx, OrderStop(c.Nodes), "stop", false, onLine)
}

// Restart stops the cluster and starts it again.
func (e *Engine) Restart(ctx context.Context, c Cluster, onLine func(string)) error {
	if err := e.Stop(ctx, c, onLine); err != nil {
		return err
	}
	return e.Start(ctx, c, onLine)
}

// RestartNode restarts a single node container.
func (e *Engine) RestartNode(ctx context.Context, container string, onLine func(string)) error {
	if err := e.requireDaemon(); err != nil {
		return err
	}
	if !ValidIdent(container) {
		return fmt.Errorf("unsafe container name %q", container)
	}
	return streamCmd(ctx, e.environ(), "", e.Tools().Runtime, []string{"restart", container}, onLine)
}

func (e *Engine) eachNode(ctx context.Context, nodes []Node, action string, stopOnErr bool, onLine func(string)) error {
	if err := e.requireDaemon(); err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fmt.Errorf("cluster has no nodes")
	}
	var first error
	for _, n := range nodes {
		if !ValidIdent(n.Name) {
			err := fmt.Errorf("unsafe container name %q", n.Name)
			if stopOnErr {
				return err
			}
			if first == nil {
				first = err
			}
			continue
		}
		if onLine != nil {
			onLine(action + " " + n.Name)
		}
		err := streamCmd(ctx, e.environ(), "", e.Tools().Runtime, []string{action, n.Name}, onLine)
		if err != nil {
			if stopOnErr {
				return fmt.Errorf("%s %s: %w", action, n.Name, err)
			}
			if first == nil {
				first = fmt.Errorf("%s %s: %w", action, n.Name, err)
			}
		}
	}
	return first
}

// LoadImage runs `kind load docker-image`.
func (e *Engine) LoadImage(ctx context.Context, cluster, image string, onLine func(string)) error {
	if err := e.requireKind(); err != nil {
		return err
	}
	if err := e.requireDaemon(); err != nil {
		return err
	}
	if !ValidName(cluster) {
		return fmt.Errorf("invalid cluster name %q", cluster)
	}
	if !ValidImage(image) || image == "" {
		return fmt.Errorf("invalid image %q", image)
	}
	return streamCmd(ctx, e.environ(), "", e.Tools().Kind,
		[]string{"load", "docker-image", image, "--name", cluster}, onLine)
}

// ExportLogs writes `kind export logs` under ~/kind-logs and returns the directory.
func (e *Engine) ExportLogs(ctx context.Context, name string, onLine func(string)) (string, error) {
	if err := e.requireKind(); err != nil {
		return "", err
	}
	if !ValidName(name) {
		return "", fmt.Errorf("invalid cluster name %q", name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	dir := filepath.Join(home, "kind-logs", name+"-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if onLine != nil {
		onLine("writing logs to " + dir)
	}
	err = streamCmd(ctx, e.environ(), "", e.Tools().Kind, []string{"export", "logs", "--name", name, dir}, onLine)
	return dir, err
}

// StartRuntime starts Colima or a Podman machine, whichever Probe selected.
func (e *Engine) StartRuntime(ctx context.Context, onLine func(string)) error {
	t := e.Tools()
	switch t.StartWith {
	case "colima":
		if t.Colima == "" {
			return fmt.Errorf("colima is not installed")
		}
		return streamCmd(ctx, e.environ(), "", t.Colima, []string{"start"}, onLine)
	case "podman":
		if t.Runtime == "" {
			return fmt.Errorf("podman is not installed")
		}
		return streamCmd(ctx, e.environ(), "", t.Runtime, []string{"machine", "start"}, onLine)
	default:
		if t.Hint != "" {
			return fmt.Errorf("%s", t.Hint)
		}
		return fmt.Errorf("nothing to start")
	}
}

// PodCounts summarizes phases for the overview.
func PodCounts(pods []Pod) (running, pending, failed, other int) {
	for _, p := range pods {
		switch p.Phase {
		case "Running":
			running++
		case "Pending":
			pending++
		case "Failed":
			failed++
		default:
			other++
		}
	}
	return
}

// WarningEvents returns up to n warning events.
func WarningEvents(events []Event, n int) []Event {
	var out []Event
	for _, ev := range events {
		if !strings.EqualFold(ev.Type, "Warning") {
			continue
		}
		out = append(out, ev)
		if len(out) == n {
			break
		}
	}
	return out
}
