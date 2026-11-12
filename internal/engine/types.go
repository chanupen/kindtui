// Package engine talks to Docker or Podman, kind, and kubectl.
// The TUI renders snapshots from this package and never shells out itself.
package engine

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Status is the roll-up of every node container in a cluster.
type Status string

const (
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusDegraded Status = "degraded"
	StatusBroken   Status = "broken"
)

// Cluster is one kind cluster, built from container labels.
type Cluster struct {
	Name    string
	Status  Status
	Nodes   []Node
	Created time.Time
}

// Node is one kind node container.
type Node struct {
	ID      string
	Name    string
	Role    string
	State   string
	Summary string
	Created time.Time
	IP      string
	Ready   string
	Kubelet string
	CPU     string
	Mem     string
	MemPct  string
	Image   string
	Ports   string
	OS      string
	Runtime string
}

// Pod is one Kubernetes pod.
type Pod struct {
	Namespace string
	Name      string
	Phase     string
	Ready     string
	Restarts  int
	Node      string
	Created   time.Time
}

// Event is one Kubernetes event.
type Event struct {
	Namespace string
	Type      string
	Reason    string
	Object    string
	Message   string
	Count     int
	Time      time.Time
}

// Stat is one non-streaming docker stats row.
type Stat struct {
	Name   string
	CPU    string
	Mem    string
	MemPct string
	Net    string
	Block  string
	PIDs   string
}

// Detail is the inspector payload for one cluster.
type Detail struct {
	Cluster Cluster
	Pods    []Pod
	Events  []Event
	Stats   []Stat
	KubeErr string
}

// CreateSpec is a cluster the user asked to create.
type CreateSpec struct {
	Name       string
	ConfigPath string
	Workers    int
	Image      string
	Wait       time.Duration
}

// Tools is what Probe found on this machine.
type Tools struct {
	Runtime     string
	RuntimeName string // "docker" or "podman"
	ClientVer   string
	ServerVer   string
	DaemonOK    bool

	Kind       string
	KindVer    string
	Kubectl    string
	KubectlVer string
	Context    string // kubectl current-context, best effort
	Colima     string
	ColimaVer  string

	// StartWith is "colima" or "podman" when a key can bring the daemon up.
	StartWith string
	Broken    []string
	Hint      string
}

// Age formats a duration the way cluster lists usually do.
func Age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Topology is a short node summary such as "1 control-plane · 2 workers".
func (c Cluster) Topology() string {
	cp, wk, other := 0, 0, 0
	for _, n := range c.Nodes {
		switch n.Role {
		case "control-plane":
			cp++
		case "worker":
			wk++
		default:
			other++
		}
	}
	parts := []string{plural(cp, "control-plane")}
	if wk > 0 {
		parts = append(parts, plural(wk, "worker"))
	}
	if other > 0 {
		parts = append(parts, plural(other, "other"))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

var (
	nameRe  = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	identRe = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)
	imageRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,255}$`)
)

// ValidName reports whether s is a DNS label kind will accept as a cluster name.
func ValidName(s string) bool {
	return len(s) >= 1 && len(s) <= 63 && nameRe.MatchString(s)
}

// ValidIdent reports whether s is safe to pass as a node, namespace, or pod name.
func ValidIdent(s string) bool {
	return len(s) >= 1 && len(s) <= 253 && identRe.MatchString(s) && !strings.HasPrefix(s, "-")
}

// ValidImage reports whether s looks like a container image reference.
func ValidImage(s string) bool {
	if s == "" {
		return true
	}
	return imageRe.MatchString(s)
}

// DeriveStatus rolls node container states into one cluster status.
func DeriveStatus(nodes []Node) Status {
	if len(nodes) == 0 {
		return StatusBroken
	}
	cp := false
	running := 0
	for _, n := range nodes {
		if n.Role == "control-plane" {
			cp = true
		}
		if strings.EqualFold(n.State, "running") {
			running++
		}
	}
	if !cp {
		return StatusBroken
	}
	switch {
	case running == len(nodes):
		return StatusRunning
	case running == 0:
		return StatusStopped
	default:
		return StatusDegraded
	}
}
