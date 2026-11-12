# Usage

```
kindtui              start the UI (needs a terminal)
kindtui --version    print the version
kindtui --help       short help
```

| Environment | Effect |
| --- | --- |
| `KIND_CONFIG` | kind config used by the **repo config** create shape. Without it, kindtui walks up from the current directory looking for `kind-cluster-config.yaml`, `kind-config.yaml`, `kind.yaml`, or `kind.yml` |
| `TERM` | Passed into node shells (defaults to `xterm-256color`) |

## Screen layout

| Area | Contents |
| --- | --- |
| Header | Runtime and whether it is up, kind and kubectl versions, current kubectl context, cluster and node counts, and the keys for the focused pane |
| Clusters pane | Name, status (Running, Degraded, Stopped, Broken), running/total nodes, topology, age |
| Nodes pane | Every node of the selected cluster (see below) |
| Overview | Kubernetes version, API endpoint, context, pod counts, node image, OS, container runtime, newest warning event |
| Footer | Breadcrumbs (`clusters › lab › nodes`), messages, activity spinner |

At 110 columns or more, the panes sit side by side; below that they stack. Below 24 rows the header shrinks to one line and the key hints move to the footer. The minimum size is 60×16.

### Node columns

| Column | Source |
| --- | --- |
| NAME, ROLE, STATE, UPTIME | `docker ps` (refreshed every 2s) |
| READY, VERSION, PODS | `kubectl get nodes` and `get pods` (every 5s, selected cluster only) |
| IP, PORTS | `docker inspect` |
| CPU, MEM | `docker stats --no-stream` |

`—` means the value is not known yet, or the node is not running. When the terminal is narrow, PORTS, UPTIME, VERSION, PODS, CPU/MEM, and IP are hidden in that order.

Cluster status:

| Status | Meaning |
| --- | --- |
| Running | Every node container is running |
| Degraded | Some nodes are running and some are not |
| Stopped | No nodes are running |
| Broken | No control-plane container exists |

## Keys

### Everywhere on the dashboard

| Key | Action |
| --- | --- |
| `tab` | Switch between the clusters and nodes panes |
| `↑` `↓` / `k` `j` | Move |
| `pgup` `pgdn` | Move 10 rows |
| `g` `G` / `home` `end` | First / last row |
| `/` | Filter clusters by name or status (`enter` applies, `esc` clears) |
| `:` | Command line |
| `ctrl-r` / `R` | Refresh now |
| `n` | Create a cluster |
| `o` | Open k9s on the selected cluster |
| `c` | Make `kind-<name>` the current kubectl context |
| `I` | Tool paths and versions |
| `C` | Start Colima or a Podman machine (shown when the runtime is down) |
| `?` | Help overlay |
| `q` / `ctrl+c` | Quit |

### Clusters pane

| Key | Action |
| --- | --- |
| `enter` / `→` | Move into the nodes pane |
| `s` | Start: control plane first, then workers |
| `x` | Stop: workers first, then the control plane. Asks first |
| `r` | Restart (stop, then start). Asks first |
| `ctrl-d` / `D` | Delete. Type the cluster name to confirm |
| `i` | Load a local image onto every node (`kind load docker-image`) |
| `e` | Export logs to `~/kind-logs/<name>-<timestamp>` (`kind export logs`) |
| `esc` | Clear the filter |

### Nodes pane

| Key | Action |
| --- | --- |
| `enter` / `s` | Log in to the node: an interactive root shell (`bash -l`, or `sh -l`) |
| `l` | Follow the node container log (`docker logs -f --tail 300`) |
| `b` | Follow the kubelet journal (`journalctl -u kubelet -f`) |
| `J` | Follow the containerd journal |
| `d` | `kubectl describe node` |
| `p` | `kubectl get pods -A -o wide` for this node |
| `u` | Start this node container |
| `x` | Stop this node container. Asks first, with a warning for the control plane |
| `r` | Restart this node container. Asks first |
| `esc` / `←` / `h` | Back to the clusters pane |

### Mouse

| Action | Effect |
| --- | --- |
| Click | Select a row and focus its pane |
| Double-click a cluster | Open its nodes |
| Double-click a node | Log in |
| Wheel | Move in the pane under the pointer, or scroll the log view |

## Command line (`:`)

Type `:` and then a command. `tab` completes command names and, for the second word, node or cluster names. `esc` cancels.

`NODE` can be a full container name (`lab-worker2`), a role suffix of the selected cluster (`worker2`, `control-plane`), or left out to use the selected node (or the control plane).

| Command | Action |
| --- | --- |
| `shell [NODE]` (`ssh`, `login`, `exec`, `sh`) | Log in to a node in any cluster |
| `NODE` | Typing just a node name logs in to it |
| `logs [NODE]` | Follow the node container log |
| `kubelet [NODE]` | Follow the kubelet journal |
| `journal UNIT [NODE]` | Follow any systemd unit's journal, e.g. `:journal containerd lab-worker` |
| `describe [NODE]` | `kubectl describe node` |
| `pods [NODE]` | Pods on the node |
| `cluster NAME` | Select a cluster |
| `nodes [CLUSTER]` / `clusters` | Focus a pane |
| `start` `stop` `restart` `delete` | Lifecycle for the selected cluster |
| `create` / `new` | Create form |
| `k9s` | Open k9s |
| `context` / `ctx` | Switch the kubectl context |
| `image [IMG]` / `load [IMG]` | Load an image; with `IMG` it starts immediately |
| `export` | Export logs |
| `filter TEXT` | Filter the cluster list (empty clears it) |
| `runtime` | Start Colima or Podman |
| `refresh`, `info`, `help`, `quit` | Refresh, tool paths, help, quit |

## Create form

| Field | Notes |
| --- | --- |
| Name | A lowercase DNS label, 1–63 characters. Must not already exist |
| Shape | `single`, `repo config`, or `custom` (`←`/`→` or `space`) |
| Workers | `custom` only, 0–20. The control plane is extra |
| Image | Optional node image, e.g. `kindest/node:v1.33.1` |
| Wait | `--wait` in seconds, 0–3600. `0` skips it |

`tab` and `shift+tab` move between fields, `enter` creates, and `esc` goes back. The form previews the exact `kind create cluster` command and, for generated shapes, the YAML. A generated config file is deleted once kind returns.

## Log and operation view

Logs, journals, describe, pods, and long operations (create, delete, start, stop, image load, log export) all open in one view.

| Key | Action |
| --- | --- |
| `↑` `↓` / `k` `j` | Scroll one line |
| `pgup` `pgdn` / `space` | Scroll one page |
| `g` / `G` | Top / bottom (`G` resumes following) |
| `f` | Toggle follow |
| `esc` / `q` | Logs and describe: stop and go back. Lifecycle operations: cancel while running, go back when finished |
| `?` | Help |

The view keeps the last 5000 lines. Lines that look like errors are red, warnings are amber, and `✓` lines are green. Cancelling an operation kills its whole process group, including kind's child processes.

## Refresh and side effects

- The cluster list refreshes every 2s, node detail every 5s (selected cluster only), and tool detection every 15s.
- `~/.kube/config` is changed only by `c` / `:context`. Everything else uses a private kubeconfig at `$TMPDIR/kindtui-<name>.kubeconfig` (mode 0600), which is removed when you delete the cluster.
- Only one operation runs at a time. Starting a new one cancels the previous one.
