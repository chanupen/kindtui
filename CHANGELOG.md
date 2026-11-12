# Changelog

## 0.2.0 — 2026-11-12

### New
- k9s-style layout: a header with tool status and per-pane key hints, side-by-side clusters and nodes panes, a cluster overview strip, and a breadcrumb footer.
- Every node is listed with role, state, readiness, IP, CPU, memory, pod count, kubelet version, uptime, and ports.
- Log in to any node (`enter`, double-click, or `:shell NODE`). Uses bash when available and falls back to sh.
- Per-node actions: container log, kubelet and containerd journals, `:journal UNIT`, describe, pods on node, and start/stop/restart.
- `:` command line with tab completion of commands and node names.
- Overview: Kubernetes version, API endpoint, pod health, restarts, node image and OS, newest warning event.
- Log view with follow toggle, paging, a 5000-line buffer, and error/warning coloring.
- Mouse: click to select, double-click to open or log in, wheel scrolling.
- MIT license.
- Repository: root Go module `github.com/chanupen/kindtui`, CI on Linux and macOS, release binaries on tags, and full docs.

### Fixed
- kind's stderr warning on Podman was written into the cached kubeconfig, which broke kubectl detail and k9s.
- CPU and memory were always empty on Podman (`stats` JSON uses numbers there).
- The node shell always opened the control plane.
- Input widths never followed the terminal size.

### Faster
- Node detail runs inspect, stats, and kubectl queries in parallel: about 0.4s warm, down from several seconds.

## 0.1.0

- First release: numbered-menu dashboard, create/start/stop/delete, k9s, context switch, image load, log export.
