package engine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ParseClusterPS groups `docker ps` lines into clusters.
// Each line is ID, name, cluster label, role label, state, status, createdAt,
// separated by tabs.
func ParseClusterPS(out string) []Cluster {
	groups := map[string][]Node{}
	var order []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 7 {
			continue
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		cluster := parts[2]
		name := containerName(parts[1])
		if cluster == "" || !ValidName(cluster) || !ValidIdent(name) {
			continue
		}
		role := parts[3]
		if role == "" {
			role = "unknown"
		}
		node := Node{
			ID:      parts[0],
			Name:    name,
			Role:    role,
			State:   strings.ToLower(parts[4]),
			Summary: parts[5],
			Created: parseTime(parts[6]),
		}
		if _, ok := groups[cluster]; !ok {
			order = append(order, cluster)
		}
		groups[cluster] = append(groups[cluster], node)
	}
	clusters := make([]Cluster, 0, len(order))
	for _, name := range order {
		nodes := groups[name]
		sortNodes(nodes)
		c := Cluster{Name: name, Nodes: nodes, Status: DeriveStatus(nodes)}
		for _, n := range nodes {
			if n.Created.IsZero() {
				continue
			}
			if c.Created.IsZero() || n.Created.Before(c.Created) {
				c.Created = n.Created
			}
		}
		clusters = append(clusters, c)
	}
	sort.SliceStable(clusters, func(i, j int) bool {
		ri, rj := statusRank(clusters[i].Status), statusRank(clusters[j].Status)
		if ri != rj {
			return ri < rj
		}
		return clusters[i].Name < clusters[j].Name
	})
	return clusters
}

func containerName(raw string) string {
	raw = strings.TrimPrefix(raw, "/")
	if i := strings.IndexByte(raw, ','); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw)
}

func statusRank(s Status) int {
	switch s {
	case StatusRunning:
		return 0
	case StatusDegraded:
		return 1
	case StatusStopped:
		return 2
	default:
		return 3
	}
}

func sortNodes(nodes []Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		ri, rj := roleRank(nodes[i].Role), roleRank(nodes[j].Role)
		if ri != rj {
			return ri < rj
		}
		return nodes[i].Name < nodes[j].Name
	})
}

func roleRank(role string) int {
	switch role {
	case "control-plane":
		return 0
	case "worker":
		return 1
	default:
		return 2
	}
}

func parseTime(s string) time.Time {
	layouts := []string{
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	s = strings.TrimSpace(s)
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// OrderStart returns nodes with control-plane containers first.
func OrderStart(nodes []Node) []Node {
	out := append([]Node(nil), nodes...)
	sortNodes(out)
	return out
}

// OrderStop returns nodes with workers first so the control plane is last.
func OrderStop(nodes []Node) []Node {
	out := OrderStart(nodes)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

type kubeNodeList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
			NodeInfo struct {
				KubeletVersion          string `json:"kubeletVersion"`
				OSImage                 string `json:"osImage"`
				ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
			} `json:"nodeInfo"`
		} `json:"status"`
	} `json:"items"`
}

type kubePodList struct {
	Items []struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Ready        bool `json:"ready"`
				RestartCount int  `json:"restartCount"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

type kubeEventList struct {
	Items []struct {
		Metadata struct {
			Namespace         string    `json:"namespace"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Type           string    `json:"type"`
		Reason         string    `json:"reason"`
		Message        string    `json:"message"`
		Count          int       `json:"count"`
		EventTime      time.Time `json:"eventTime"`
		LastTimestamp  time.Time `json:"lastTimestamp"`
		InvolvedObject struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"involvedObject"`
	} `json:"items"`
}

// ParseKubeNodes reads `kubectl get nodes -o json`.
func ParseKubeNodes(data []byte) (map[string]Node, string, error) {
	var list kubeNodeList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, "", err
	}
	out := make(map[string]Node, len(list.Items))
	ver := ""
	for _, item := range list.Items {
		n := Node{Name: item.Metadata.Name, Kubelet: item.Status.NodeInfo.KubeletVersion, Ready: "Unknown",
			OS: item.Status.NodeInfo.OSImage, Runtime: item.Status.NodeInfo.ContainerRuntimeVersion}
		for _, c := range item.Status.Conditions {
			if c.Type == "Ready" {
				n.Ready = c.Status
			}
		}
		if ver == "" {
			ver = n.Kubelet
		}
		out[n.Name] = n
	}
	return out, ver, nil
}

// ParsePods reads `kubectl get pods -A -o json`.
func ParsePods(data []byte) ([]Pod, error) {
	var list kubePodList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	pods := make([]Pod, 0, len(list.Items))
	for _, item := range list.Items {
		ready, total, restarts := 0, len(item.Status.ContainerStatuses), 0
		for _, cs := range item.Status.ContainerStatuses {
			total = len(item.Status.ContainerStatuses)
			if cs.Ready {
				ready++
			}
			restarts += cs.RestartCount
		}
		readyText := "0/0"
		if total > 0 {
			readyText = itoa(ready) + "/" + itoa(total)
		}
		pods = append(pods, Pod{
			Namespace: item.Metadata.Namespace,
			Name:      item.Metadata.Name,
			Phase:     item.Status.Phase,
			Ready:     readyText,
			Restarts:  restarts,
			Node:      item.Spec.NodeName,
			Created:   item.Metadata.CreationTimestamp,
		})
	}
	sort.SliceStable(pods, func(i, j int) bool {
		bi, bj := podBad(pods[i]), podBad(pods[j])
		if bi != bj {
			return bi && !bj
		}
		if pods[i].Restarts != pods[j].Restarts {
			return pods[i].Restarts > pods[j].Restarts
		}
		if pods[i].Namespace != pods[j].Namespace {
			return pods[i].Namespace < pods[j].Namespace
		}
		return pods[i].Name < pods[j].Name
	})
	return pods, nil
}

func podBad(p Pod) bool {
	switch p.Phase {
	case "Running", "Succeeded":
		return false
	default:
		return true
	}
}

// ParseEvents reads `kubectl get events -A -o json`.
func ParseEvents(data []byte) ([]Event, error) {
	var list kubeEventList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(list.Items))
	for _, item := range list.Items {
		when := item.LastTimestamp
		if when.IsZero() {
			when = item.EventTime
		}
		if when.IsZero() {
			when = item.Metadata.CreationTimestamp
		}
		kind := item.InvolvedObject.Kind
		if kind == "" {
			kind = "Object"
		}
		name := item.InvolvedObject.Name
		if name == "" {
			name = "unknown"
		}
		events = append(events, Event{
			Namespace: item.Metadata.Namespace,
			Type:      item.Type,
			Reason:    item.Reason,
			Object:    strings.ToLower(kind) + "/" + name,
			Message:   strings.TrimSpace(item.Message),
			Count:     item.Count,
			Time:      when,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Time.Equal(events[j].Time) {
			return events[i].Reason < events[j].Reason
		}
		return events[i].Time.After(events[j].Time)
	})
	if len(events) > 80 {
		events = events[:80]
	}
	return events, nil
}

// ParseStats reads newline-delimited `stats --format '{{json .}}'` output.
// Docker prints strings ("1.25%", "120MiB / 7.6GiB"). Podman prints numbers
// (CPU 1.25, MemUsage bytes, MemLimit bytes). Both are accepted.
func ParseStats(out string) []Stat {
	var stats []Stat
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		name := containerName(jsonString(row["Name"]))
		if name == "" {
			continue
		}
		st := Stat{Name: name, Net: jsonString(row["NetIO"]), Block: jsonString(row["BlockIO"]), PIDs: jsonString(row["PIDs"])}
		if s := jsonString(row["CPUPerc"]); s != "" {
			st.CPU = s
		} else if f, ok := jsonFloat(row["CPU"]); ok {
			st.CPU = fmt.Sprintf("%.1f%%", f)
		}
		if s := jsonString(row["MemUsage"]); s != "" {
			st.Mem = s
		} else if f, ok := jsonFloat(row["MemUsage"]); ok {
			st.Mem = humanBytes(f)
			if l, ok := jsonFloat(row["MemLimit"]); ok && l > 0 {
				st.Mem += " / " + humanBytes(l)
			}
		}
		if s := jsonString(row["MemPerc"]); s != "" {
			st.MemPct = s
		} else if f, ok := jsonFloat(row["MemPerc"]); ok {
			st.MemPct = fmt.Sprintf("%.1f%%", f)
		}
		stats = append(stats, st)
	}
	return stats
}

func jsonString(raw json.RawMessage) string {
	var s string
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func jsonFloat(raw json.RawMessage) (float64, bool) {
	var f float64
	if len(raw) == 0 || json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	return f, true
}

func humanBytes(b float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f%s", b, units[i])
	}
	return fmt.Sprintf("%.1f%s", b, units[i])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// InspectRow is one line of the node inspect template.
type InspectRow struct {
	IP    string
	Image string
	Ports string
}

// ParseInspect reads name, IPs, image, and ports separated by tabs.
func ParseInspect(out string) map[string]InspectRow {
	rows := map[string]InspectRow{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(parts) < 2 {
			continue
		}
		name := containerName(strings.TrimSpace(parts[0]))
		if name == "" {
			continue
		}
		r := InspectRow{IP: strings.Join(strings.Fields(parts[1]), ",")}
		if len(parts) > 2 {
			r.Image = ShortImage(strings.TrimSpace(parts[2]))
		}
		if len(parts) > 3 {
			r.Ports = strings.Join(strings.Fields(parts[3]), " ")
		}
		rows[name] = r
	}
	return rows
}

// ShortImage drops the registry prefix and digest from an image reference.
func ShortImage(s string) string {
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[:i]
	}
	for _, p := range []string{"docker.io/", "registry-1.docker.io/"} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

// ApplyKube overlays kubectl node readiness onto container nodes.
func ApplyKube(nodes []Node, kube map[string]Node) {
	for i := range nodes {
		k, ok := kube[nodes[i].Name]
		if !ok {
			continue
		}
		nodes[i].Ready = k.Ready
		nodes[i].Kubelet = k.Kubelet
		nodes[i].OS = k.OS
		nodes[i].Runtime = k.Runtime
	}
}

// KubeVersion prefers the control-plane kubelet version.
func KubeVersion(nodes []Node) string {
	for _, n := range nodes {
		if n.Role == "control-plane" && n.Kubelet != "" {
			return n.Kubelet
		}
	}
	for _, n := range nodes {
		if n.Kubelet != "" {
			return n.Kubelet
		}
	}
	return ""
}
