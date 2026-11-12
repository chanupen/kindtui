package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Engine is a concurrency-safe handle on the local kind toolchain.
type Engine struct {
	mu    sync.Mutex
	tools Tools
	kube  map[string]string
}

// New returns an engine that has not probed the machine yet.
func New() *Engine {
	return &Engine{kube: map[string]string{}}
}

// Probe resolves binaries and stores them for later commands.
func (e *Engine) Probe(ctx context.Context) Tools {
	tools := Discover(ctx, os.Getenv("PATH"), ExtraRuntimes())
	if tools.Kubectl != "" {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var b strings.Builder
		if streamCmd(cctx, os.Environ(), "", tools.Kubectl, []string{"config", "current-context"}, func(l string) { b.WriteString(l) }) == nil {
			tools.Context = strings.TrimSpace(b.String())
		}
		cancel()
	}
	e.mu.Lock()
	e.tools = tools
	e.mu.Unlock()
	return tools
}

// Tools returns the last probe result.
func (e *Engine) Tools() Tools {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tools
}

func (e *Engine) environ() []string {
	tools := e.Tools()
	base := os.Environ()
	out := make([]string, 0, len(base)+2)
	path := os.Getenv("PATH")
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "PATH="):
			path = strings.TrimPrefix(kv, "PATH=")
		case strings.HasPrefix(kv, "KIND_EXPERIMENTAL_PROVIDER="):
			continue
		default:
			out = append(out, kv)
		}
	}
	if tools.Runtime != "" {
		path = filepath.Dir(tools.Runtime) + string(os.PathListSeparator) + path
	}
	if tools.Kind != "" {
		path = filepath.Dir(tools.Kind) + string(os.PathListSeparator) + path
	}
	out = append(out, "PATH="+path)
	provider := "docker"
	if tools.RuntimeName == "podman" {
		provider = "podman"
	}
	// Pin the provider. kind 0.33 otherwise switches to Podman when the
	// docker name on PATH is missing or a broken symlink.
	out = append(out, "KIND_EXPERIMENTAL_PROVIDER="+provider)
	return out
}

func (e *Engine) requireKind() error {
	if e.Tools().Kind == "" {
		return errors.New("kind is not installed")
	}
	return nil
}

func (e *Engine) requireDaemon() error {
	t := e.Tools()
	if t.Runtime == "" {
		return errors.New("no container runtime found")
	}
	if !t.DaemonOK {
		if t.Hint != "" {
			return errors.New(t.Hint)
		}
		return errors.New("container runtime is not running")
	}
	return nil
}

func (e *Engine) capture(ctx context.Context, name string, args ...string) (string, error) {
	var b strings.Builder
	err := streamCmd(ctx, e.environ(), "", name, args, func(line string) {
		b.WriteString(line)
		b.WriteByte('\n')
	})
	return b.String(), err
}

// captureOut returns stdout only. Stderr is kept for the error message, so
// warnings such as kind's "enabling experimental podman provider" cannot leak
// into data like a kubeconfig.
func (e *Engine) captureOut(ctx context.Context, name string, args ...string) (string, error) {
	var out, errb strings.Builder
	err := streamSplit(ctx, e.environ(), "", name, args,
		func(l string) { out.WriteString(l); out.WriteByte('\n') },
		func(l string) { errb.WriteString(l); errb.WriteByte('\n') })
	if err != nil && strings.TrimSpace(errb.String()) != "" {
		return out.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.String(), err
}

// ShellCmd builds an interactive shell in a node container.
// Bubble Tea attaches the terminal; the command's stdio is left unset.
func (e *Engine) ShellCmd(container string) (*exec.Cmd, error) {
	if err := e.requireDaemon(); err != nil {
		return nil, err
	}
	if !ValidIdent(container) {
		return nil, fmt.Errorf("unsafe container name %q", container)
	}
	// bash when the image has it, sh otherwise. The banner says how to get back.
	script := `printf '\033[1;36m» kindtui:\033[0m logged in to \033[1m%s\033[0m as root. Type \033[1mexit\033[0m or press ctrl+d to return.\n\n' "$KINDTUI_NODE"; ` +
		`if command -v bash >/dev/null 2>&1; then exec bash -l; else exec sh -l; fi`
	cmd := exec.Command(e.Tools().Runtime, "exec", "-it",
		"-e", "KINDTUI_NODE="+container,
		"-e", "TERM="+termName(),
		"-w", "/root",
		container, "sh", "-c", script)
	cmd.Env = e.environ()
	return cmd, nil
}

func termName() string {
	t := os.Getenv("TERM")
	if t == "" || !ValidIdent(t) {
		return "xterm-256color"
	}
	return t
}

// NodeAction runs start, stop, or restart on one node container.
func (e *Engine) NodeAction(ctx context.Context, action, container string, onLine func(string)) error {
	if err := e.requireDaemon(); err != nil {
		return err
	}
	switch action {
	case "start", "stop", "restart":
	default:
		return fmt.Errorf("unknown node action %q", action)
	}
	if !ValidIdent(container) {
		return fmt.Errorf("unsafe container name %q", container)
	}
	if onLine != nil {
		onLine(action + " " + container)
	}
	return streamCmd(ctx, e.environ(), "", e.Tools().Runtime, []string{action, container}, onLine)
}

// NodeLogs streams the node container's own log (systemd boot and kind entrypoint).
func (e *Engine) NodeLogs(ctx context.Context, container string, follow bool, onLine func(string)) error {
	if err := e.requireDaemon(); err != nil {
		return err
	}
	if !ValidIdent(container) {
		return fmt.Errorf("unsafe container name %q", container)
	}
	args := []string{"logs", "--tail", "300"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, container)
	return streamCmd(ctx, e.environ(), "", e.Tools().Runtime, args, onLine)
}

// Journal streams journalctl for a unit inside the node, such as kubelet or containerd.
func (e *Engine) Journal(ctx context.Context, container, unit string, follow bool, onLine func(string)) error {
	if err := e.requireDaemon(); err != nil {
		return err
	}
	if !ValidIdent(container) || !ValidIdent(unit) {
		return fmt.Errorf("unsafe journal target")
	}
	args := []string{"exec", container, "journalctl", "-u", unit, "-n", "300", "--no-pager", "-o", "short-iso"}
	if follow {
		args = append(args, "-f")
	}
	return streamCmd(ctx, e.environ(), "", e.Tools().Runtime, args, onLine)
}

const psFormat = "{{.ID}}\t{{.Names}}\t{{.Label \"io.x-k8s.kind.cluster\"}}\t{{.Label \"io.x-k8s.kind.role\"}}\t{{.State}}\t{{.Status}}\t{{.CreatedAt}}"

// List returns every kind cluster the runtime still has containers for.
func (e *Engine) List(ctx context.Context) ([]Cluster, error) {
	if err := e.requireDaemon(); err != nil {
		return nil, err
	}
	t := e.Tools()
	out, err := e.capture(ctx, t.Runtime, "ps", "-a",
		"--filter", "label=io.x-k8s.kind.cluster",
		"--format", psFormat)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w\n%s", err, strings.TrimSpace(out))
	}
	return ParseClusterPS(out), nil
}
