# kindtui

A terminal UI for [kind](https://kind.sigs.k8s.io/) (Kubernetes in Docker) clusters on Linux and macOS.

You can see every cluster and every node at a glance, log in to any node with one key, follow node and kubelet logs, and create, start, stop, or delete clusters without remembering `kind` and `docker` flags. It is one static binary that drives the `kind`, `docker`/`podman`, and `kubectl` you already have installed.

```
 Runtime:  podman 6.1.3 ● up    <enter> shell (login)  <u> start node     <:> command      _    _         _ _        _
 Kind:     0.33.0               <l>     node logs      <x> stop node      <?> help        | | _(_)_ _  __| | |_ _  _(_)
 Kubectl:  1.37.2               <b>     kubelet logs   <r> restart node   <q> quit        | |/ / | ' \/ _' |  _| || | |
 Context:  kind-lab             <d>     describe       <o> k9s                            |_|\_\_|_||_\__,_|\__|\_,_|_|
 Clusters: 1 · 1 running · 3    <p>     pods on node   <esc> clusters                       kind cluster manager

╭─ Clusters[1] ───────────────────────╮╭─ Nodes(lab)[3] ─────────────────────────────────────────────────────────╮
│NAME  STATUS     NODES TOPOLOGY   AGE││NAME               ROLE          STATE     READY IP          CPU    MEM PODS│
│lab   ● Running      3 1cp + 2w    8d││lab-control-plane  control-plane ● running yes   10.89.1.2 24.9% 2.0GiB    9│
│                                     ││lab-worker         worker        ● running yes   10.89.1.3 59.2% 2.7GiB   28│
│                                     ││lab-worker2        worker        ● running yes   10.89.1.4 25.0% 2.6GiB   20│
╰─────────────────────────────────────╯╰─────────────────────────────────────────────────────────────────────────╯
╭─ Overview(lab) ──────────────────────────────────────────────────────────────────────────────────────────────╮
│ Kubernetes v1.36.4   API https://127.0.0.1:39795   Context kind-lab (current)                                │
│ Pods       57 total  55 running  0 pending  0 failed  2 completed/other  725 restarts                        │
│ Node image kindest/node  Debian GNU/Linux 13 (trixie)  containerd://2.3.4                                    │
│ Events     no warnings                                                                                       │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
  clusters   lab   nodes                                                                             16:04:02
```

## Features

- **Clusters and nodes side by side.** For each node: role, container state, Kubernetes readiness, IP, CPU, memory, pod count, kubelet version, uptime, and published ports.
- **Log in to any node.** Select it and press `enter`, double-click it, or type `:shell worker2`. You get a root shell; `exit` brings you back.
- **Logs and diagnostics.** Follow the node container log, the kubelet or containerd journal, or any systemd unit. Run `kubectl describe node` or list the pods on a node.
- **Lifecycle.** Create clusters (single node, your kind config, or N workers), then start, stop, restart, or delete them. You can also start, stop, or restart a single node.
- **Cluster overview.** Kubernetes version, API endpoint, pod health, restart count, and the newest warning event.
- **Integrations.** Open [k9s](https://k9scli.io/) on a cluster, switch your kubectl context, load local images, or export logs.
- **Works with Docker, Podman, and Colima.** kindtui finds a working runtime even when `docker` is a broken symlink, and pins kind's provider so it uses the same one.
- **Keyboard first, mouse optional.** The header shows k9s-style hints, `:` opens a command line with tab completion, `/` filters, and you can click or scroll.

## Requirements

| Tool | Needed for |
| --- | --- |
| [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) | Everything |
| Docker, Podman, or Colima | Running the nodes |
| `kubectl` | Readiness, pods, events, describe, and context switching (optional) |
| [k9s](https://k9scli.io/) | The `o` key (optional) |
| Go (version in `go.mod`) | Building from source only |

## Install

**From source**

```sh
git clone https://github.com/chanupen/kindtui.git
cd kindtui
make build            # writes bin/kindtui
sudo install bin/kindtui /usr/local/bin/
```

**With `go install`**

```sh
go install github.com/chanupen/kindtui@latest
```

The repository is private, so first set `GOPRIVATE=github.com/chanupen/*` and make sure git can authenticate to GitHub.

**Prebuilt binaries:** each `v*` tag builds Linux and macOS binaries for amd64 and arm64 and attaches them to the GitHub release.

## Quick start

```sh
kindtui
```

1. Press `n`, accept the defaults, and press `enter` to create a cluster. The output streams live.
2. Press `esc` to go back, then `enter` (or `tab`) to move into the nodes pane.
3. Pick a node with `↑`/`↓` and press `enter` to log in. Type `exit` to come back.
4. Press `b` for the kubelet log, `d` for describe, `?` for every key, and `q` to quit.

The full walkthrough is in **[docs/quickstart.md](docs/quickstart.md)**.

## Keys at a glance

| Clusters pane | | Nodes pane | |
| --- | --- | --- | --- |
| `enter` | open nodes | `enter` / `s` | log in (shell) |
| `n` | create | `l` | follow node log |
| `s` `x` `r` | start, stop, restart | `b` / `J` | kubelet / containerd journal |
| `ctrl-d` | delete | `d` | describe node |
| `o` | k9s | `p` | pods on node |
| `c` | use kubectl context | `u` `x` `r` | start, stop, restart node |
| `i` | load image | `esc` | back to clusters |
| `e` | export logs | | |

Everywhere: `tab` switches panes, `/` filters, `:` opens the command line, `ctrl-r` refreshes, `?` shows help, and `q` quits. The full reference is in **[docs/usage.md](docs/usage.md)**.

## Documentation

| Document | Contents |
| --- | --- |
| [Quick start](docs/quickstart.md) | Install, first cluster, logging in to nodes, logs, cleanup |
| [Usage](docs/usage.md) | Every screen, key, and `:` command |
| [Troubleshooting](docs/troubleshooting.md) | Runtime down, Podman and Colima, empty columns, k9s, shells |
| [Architecture](docs/architecture.md) | How it works, code layout, and how to add a command |
| [Examples](examples/) | kind config files |
| [Contributing](CONTRIBUTING.md) | Building, testing, conventions |
| [Changelog](CHANGELOG.md) | Release notes |

## How it works

kindtui shells out to `docker`/`podman`, `kind`, and `kubectl` instead of linking their libraries, so it always matches the versions you have installed. It lists clusters from container labels (`io.x-k8s.kind.cluster`), which means stopped clusters still appear. It never edits `~/.kube/config` unless you press `c`; k9s and kubectl queries use a private temporary kubeconfig. See [docs/architecture.md](docs/architecture.md).

## License

[MIT](LICENSE)
