package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var versionRE = regexp.MustCompile(`v?\d+\.\d+(?:\.\d+)?`)

// ExtraRuntimes returns container CLIs that are often installed but not on PATH.
// Homebrew's docker formula is a common case: /usr/local/bin/docker can be a
// broken OrbStack symlink while the real client lives in the Cellar.
func ExtraRuntimes() []string {
	globs := []string{
		"/usr/local/Cellar/docker/*/bin/docker",
		"/opt/homebrew/Cellar/docker/*/bin/docker",
		"/Applications/Docker.app/Contents/Resources/bin/docker",
		"/opt/homebrew/bin/docker",
		"/usr/local/bin/docker",
		"/opt/podman/bin/podman",
		"/usr/local/bin/podman",
		"/opt/homebrew/bin/podman",
	}
	var out []string
	seen := map[string]bool{}
	for _, g := range globs {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

type candidate struct {
	path   string
	kind   string
	client string
	server string
	daemon bool
}

// Discover walks pathEnv and extras for docker, podman, kind, kubectl, and colima.
// A live daemon beats a client that cannot connect. A Docker client beats a
// stopped Podman, because Colima and Docker Desktop both speak the Docker API.
func Discover(ctx context.Context, pathEnv string, extras []string) Tools {
	var tools Tools
	var cands []candidate
	seen := map[string]bool{}
	consider := func(path, kind string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		c := probeRuntime(ctx, path, kind)
		if c.client == "" && !c.daemon {
			return
		}
		cands = append(cands, c)
	}
	for _, kind := range []string{"docker", "podman"} {
		found, broken := walkBins(pathEnv, kind)
		tools.Broken = append(tools.Broken, broken...)
		for _, p := range found {
			consider(p, kind)
		}
	}
	for _, extra := range extras {
		base := filepath.Base(extra)
		kind := ""
		switch base {
		case "docker":
			kind = "docker"
		case "podman":
			kind = "podman"
		default:
			continue
		}
		if target, err := os.Readlink(extra); err == nil {
			if _, statErr := os.Stat(extra); statErr != nil {
				tools.Broken = append(tools.Broken, extra+" -> "+target)
				continue
			}
		}
		if !isExec(extra) {
			continue
		}
		consider(extra, kind)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		si, sj := candScore(cands[i]), candScore(cands[j])
		if si != sj {
			return si > sj
		}
		return cmpVer(cands[i].client, cands[j].client) > 0
	})
	if len(cands) > 0 {
		best := cands[0]
		tools.Runtime = best.path
		tools.RuntimeName = best.kind
		tools.ClientVer = best.client
		tools.ServerVer = best.server
		tools.DaemonOK = best.daemon
	}

	if p := firstExec(pathEnv, "kind"); p != "" {
		tools.Kind = p
		tools.KindVer = probeVersion(ctx, p, []string{"version"})
	}
	if p := firstExec(pathEnv, "kubectl"); p != "" {
		tools.Kubectl = p
		tools.KubectlVer = probeVersion(ctx, p, []string{"version", "--client", "-o", "json"})
	}
	if p := firstExec(pathEnv, "colima"); p != "" {
		tools.Colima = p
		tools.ColimaVer = probeVersion(ctx, p, []string{"version"})
	}
	tools.Broken = uniqueStrings(tools.Broken)
	explain(&tools)
	return tools
}

func uniqueStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func candScore(c candidate) int {
	score := 0
	if c.daemon {
		score += 100
	}
	if c.kind == "docker" {
		score += 20
	} else {
		score += 10
	}
	return score
}

func explain(t *Tools) {
	name := "Container runtime"
	if t.RuntimeName == "docker" {
		name = "Docker"
	} else if t.RuntimeName == "podman" {
		name = "Podman"
	}
	switch {
	case t.Runtime == "":
		t.Hint = "No Docker or Podman binary found."
	case !t.DaemonOK && t.RuntimeName == "docker" && t.Colima != "" && runtime.GOOS == "darwin":
		t.StartWith = "colima"
		t.Hint = name + " " + emptyDash(t.ClientVer) + " is installed, but the daemon is down. Press C to start Colima."
	case !t.DaemonOK && t.RuntimeName == "podman":
		t.StartWith = "podman"
		t.Hint = "Podman " + emptyDash(t.ClientVer) + " is installed, but the machine is stopped. Press C to start it."
	case !t.DaemonOK && runtime.GOOS == "linux":
		t.Hint = name + " is installed, but the daemon is down. Start it with your service manager."
	case !t.DaemonOK:
		t.Hint = name + " is installed, but the daemon is down."
	default:
		t.Hint = ""
	}
	if len(t.Broken) > 0 && t.Hint != "" {
		t.Hint += " Ignored broken link " + t.Broken[0] + "."
	} else if len(t.Broken) > 0 && t.DaemonOK {
		t.Hint = "Ignored broken link " + t.Broken[0] + "."
	}
}

func emptyDash(s string) string {
	if s == "" {
		return "client"
	}
	return s
}

func probeRuntime(ctx context.Context, path, kind string) candidate {
	c := candidate{path: path, kind: kind}
	out, code := execCombined(ctx, 2*time.Second, path, "--version")
	if code != 0 && strings.TrimSpace(out) == "" {
		return c
	}
	c.client = firstVersion(out)
	// Fedora's podman-docker package installs /usr/bin/docker as a script
	// that execs podman. kind auto-detects that and uses the podman provider.
	// Leaving the name as docker makes Engine pin KIND_EXPERIMENTAL_PROVIDER
	// to docker and overrides that detection.
	if kind == "docker" && dockerIsPodman(out) {
		c.kind = "podman"
	}
	infoOut, infoCode := execCombined(ctx, 3*time.Second, path, "info")
	c.daemon = infoCode == 0
	if c.daemon {
		serverOut, serverCode := execCombined(ctx, 2*time.Second, path, "version", "--format", "{{.Server.Version}}")
		if serverCode == 0 {
			c.server = firstVersion(serverOut)
		}
		if c.server == "" {
			c.server = firstVersion(infoOut)
		}
	}
	if c.client == "" && c.daemon {
		c.client = c.server
	}
	return c
}

func dockerIsPodman(versionOut string) bool {
	s := strings.ToLower(versionOut)
	return strings.Contains(s, "podman version") || strings.Contains(s, "emulate docker cli using podman")
}

func probeVersion(ctx context.Context, path string, args []string) string {
	out, _ := execCombined(ctx, 3*time.Second, path, args...)
	if strings.Contains(out, "gitVersion") {
		if v := jsonGitVersion(out); v != "" {
			return v
		}
	}
	return firstVersion(out)
}

func jsonGitVersion(s string) string {
	i := strings.Index(s, `"gitVersion"`)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	return firstVersion(rest)
}

func firstVersion(s string) string {
	m := versionRE.FindString(s)
	return strings.TrimPrefix(m, "v")
}

func cmpVer(a, b string) int {
	as := splitVer(a)
	bs := splitVer(b)
	for i := 0; i < 3; i++ {
		if as[i] != bs[i] {
			if as[i] > bs[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func splitVer(s string) [3]int {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n := 0
		for _, r := range parts[i] {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
}

func walkBins(pathEnv, name string) (ok []string, broken []string) {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if _, statErr := os.Stat(p); statErr != nil {
				if target, rerr := os.Readlink(p); rerr == nil {
					broken = append(broken, p+" -> "+target)
				} else {
					broken = append(broken, p)
				}
				continue
			}
		}
		if isExec(p) {
			ok = append(ok, p)
		}
	}
	return ok, broken
}

func firstExec(pathEnv, name string) string {
	ok, _ := walkBins(pathEnv, name)
	if len(ok) == 0 {
		return ""
	}
	return ok[0]
}

func isExec(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

func execCombined(ctx context.Context, timeout time.Duration, name string, args ...string) (string, int) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
	}
	return buf.String(), code
}
