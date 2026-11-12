# Architecture and engineering notes

Reference for the next change to this tree. User docs: [README](../README.md), [quickstart](quickstart.md), [usage](usage.md).

kindtui is a Bubble Tea program in ``. It shells out to `docker` or `podman`, `kind`, and `kubectl`. It does not link the kind or client-go libraries. That keeps the binary small and matches the CLIs already installed on the machine.

Version is `main.version` in `main.go`, stamped by the Makefile ldflags. Current release: [v0.3.0](https://github.com/chanupen/kindtui/releases/tag/v0.3.0). In a git checkout, `make` stamps `git describe` instead. Release binaries are built by CI on `v*` tags (see [CONTRIBUTING](../CONTRIBUTING.md#releases)).

## File map

| Path | Role |
| --- | --- |
| `main.go` | Flags, TTY check, Bubble Tea program |
| `internal/engine/types.go` | Cluster, node, pod, event, and name checks |
| `internal/engine/parse.go` | `docker ps`, kubectl JSON, `docker stats` |
| `internal/engine/config.go` | Find, parse, and render kind manifests |
| `internal/engine/runtime.go` | Find binaries and decide which runtime to use |
| `internal/engine/proc_unix.go` | Stream a command and kill its process group. `//go:build unix` |
| `internal/engine/engine.go` | `Engine` handle, env, `List`, node shell, node start/stop/restart, node log, journal |
| `internal/engine/kube.go` | Detail (parallel), describe node, pods on node, kubeconfig cache, k9s, context switch |
| `internal/engine/ops.go` | Create, delete, start, stop, load, export, Colima/Podman |
| `internal/ui/actions.go` | **Action registry**: every action's keys, `:` command, scope, hint, description, shell equivalent, and handler. `Cheatsheet()` |
| `internal/ui/palette.go` | Command palette (`ctrl-p`), command reference screen (`K`), action search, clipboard copy |
| `internal/ui/model.go` | State, background commands, operation runner |
| `internal/ui/update.go` | Keys per pane, `:` command line, mouse, actions |
| `internal/ui/view.go` | `geom` layout, header, clusters/nodes/overview panes, footer |
| `internal/ui/screens.go` | Create form, operation/log view, info, modals |
| `internal/ui/keys.go` | Context-sensitive hints and the help overlay |
| `internal/ui/styles.go` | Palette, `box`, `table`, `overlay`, `pad` |

`View` must stay pure. It only reads the model. Every `docker`, `kind`, and `kubectl` call happens inside a `tea.Cmd`.

## Runtime discovery

`Engine.Probe` calls `Discover(ctx, PATH, ExtraRuntimes())`.

Search order for the container CLI:

1. Walk `PATH` for `docker` and `podman`. Skip broken symlinks and remember them on `Tools.Broken`. Go's `LookPath` also skips broken symlinks, which is why a dead `/usr/local/bin/docker` must not be put back in front of a working client.
2. Probe extra locations that are often not on `PATH`: Homebrew Cellar (`/usr/local/Cellar/docker/*/bin/docker`, `/opt/homebrew/...`), Docker Desktop, and `/opt/podman/bin/podman`.
3. A candidate is usable when `bin --version` prints a version. The daemon is up when `bin info` exits 0.
4. Scoring prefers a live daemon, then a Docker client over a Podman client. A stopped Docker plus Colima beats a stopped Podman, because Colima exposes the Docker API.

kind, kubectl, and Colima come from `PATH` only.

`Tools.StartWith` is `colima` on darwin when the chosen client is Docker, the daemon is down, and `colima` exists. It is `podman` when the chosen client is Podman and the machine is down. The `C` key runs that. Linux with a stopped Docker daemon gets a hint only. kindtui does not call `sudo` or `systemctl`.

`Engine.environ` rebuilds the environment for every child:

- `PATH` gets the runtime directory and the kind directory first, so `kind` invokes the same Docker binary the TUI probed.
- `KIND_EXPERIMENTAL_PROVIDER` is set to `docker` or `podman`. kind 0.33 prints `enabling experimental podman provider` and switches when the `docker` on `PATH` is missing. Pinning the provider stops that. A `docker` binary whose `--version` text is Podman (Fedora's `podman-docker` script) is recorded as `podman`, so the pin matches kind's own detection.

## Cluster list

One shot:

```text
docker ps -a --filter label=io.x-k8s.kind.cluster --format <tab-separated template>
```

Columns: id, name, `io.x-k8s.kind.cluster`, `io.x-k8s.kind.role`, state, status, created. `ParseClusterPS` groups by the cluster label. Names that fail `ValidName` / `ValidIdent` are dropped so a hostile container name cannot be passed back to `docker`.

Status:

| Condition | Status |
| --- | --- |
| Every node running, and a control plane exists | `running` |
| No node running, control plane exists | `stopped` |
| Mix of running and stopped | `degraded` |
| No control plane | `broken` |

Nodes sort control-plane first. Clusters sort running, degraded, stopped, broken, then by name. The UI keeps the selection by name across refreshes.

Start order is control plane, then workers. Stop order is the reverse. Each container is a separate `docker start` or `docker stop` so one failure is visible in the log. Start aborts on the first error. Stop keeps going and returns the first error.

## Node detail and k9s

`Engine.Detail` runs for the selected cluster when it changes and every five seconds after. It lists containers, then runs these at the same time: `inspect` (IP, image, published ports), `stats --no-stream`, and `kubectl get nodes|pods|events -o json`. About 0.4s warm on Podman. The nodes pane overlays the result on the fresher two-second container list, by node name.

Data commands use `captureOut`, which keeps stderr out of the result. kind prints `enabling experimental podman provider` on stderr, and before this split it ended up inside the cached kubeconfig, which broke every kubectl call and k9s.

`stats` JSON differs: Docker prints strings (`"CPUPerc":"1.2%"`), Podman prints numbers (`"CPU":1.2`, `"MemUsage":<bytes>`). `ParseStats` accepts both.

`o` on a running or degraded cluster calls `Engine.K9sCmd`:

1. `kind get kubeconfig --name <cluster>` is written to `$TMPDIR/kindtui-<name>.kubeconfig` with mode `0600` and cached on the engine. This does not edit `~/.kube/config`.
2. `k9s --kubeconfig <that file> --context kind-<name>` takes the terminal through `tea.ExecProcess`.

Node login is `Engine.ShellCmd`: `<runtime> exec -it -e TERM -w /root <node> sh -c '<banner>; exec bash -l || sh -l'`, also through `tea.ExecProcess`.

Pod sort is unhealthy first, then restart count, then namespace and name. Events are newest first, capped at 80. The overview shows the newest warning.

## Create

`CreateSpec` is either an existing config path or a worker count. `RenderConfig` writes a temp `kindtui-*.yaml` and deletes it when `kind create` returns, including on cancel.

Flags: `--name`, `--config`, optional `--image`, optional `--wait`. The name must be a DNS label (`ValidName`, 1–63). The image must match `ValidImage` (no spaces or shell metacharacters). Workers are 0–20. Wait is 0–3600 seconds. The UI also refuses a name that is already in the list.

The preview at the bottom of the form is `CreatePreview`. It shows `<generated>` instead of the temp path, which does not exist until submit.

## Operations and cancel

`Model.begin` owns one operation at a time. It cancels the previous context, switches to the operation screen, and streams lines through a buffered channel. The log keeps 500 lines. `esc` cancels. Stick-to-bottom turns off when the user scrolls up. `G` turns it back on.

`streamCmd` puts the child in its own process group (`Setpgid`) and on cancel sends `SIGTERM`, then `SIGKILL` after 400ms, to the group. kind's grandchildren die with it. A mutex records that `Wait` has returned so a late signal cannot hit a recycled pid. Stdout and stderr callbacks are serialized on a different mutex so a slow UI callback cannot block the killer.

Timeouts: list 6s, detail 8s, probe commands 2–3s each, create is `15m + --wait`, log follow 30m, Colima or Podman start 10m. Most other mutations are 2–5m.

## Action registry

`internal/ui/actions.go` holds one `action` per thing kindtui can do:

```go
{id: "kubelet", keys: []string{"b"}, cmd: "kubelet", arg: "[NODE]", scope: scopeNode, hint: "kubelet logs",
    desc:  "Follow the kubelet journal inside the node",
    shell: func(t target) string { return t.rt + " exec " + t.node + " journalctl -u kubelet -f -n 300" },
    run:   onNode(func(m Model, n engine.Node, ok bool) (Model, tea.Cmd) { return m.journal(n, ok, "kubelet") })},
```

Everything below reads the registry, so it cannot drift:

| Consumer | Uses |
| --- | --- |
| `onDash` key dispatch | `actionByKey(key, focus)`: the pane's binding wins over a global one |
| `:` command line | `actionByCmd(name)` plus aliases; `commandNames()` for tab completion |
| Header hints | `paneHints(pane)` in the order of `hintIDs` |
| Help overlay | Grouped by scope, plus every command that takes an argument |
| Palette and reference | `matchActions(query)`; `shell(m.target(""))` fills in the selection |
| Op view `$` line | `m.shellFor(id, node)` passed to `begin` |
| `docs/commands.md` | `Cheatsheet()` via `kindtui --cheatsheet` / `make docs`, with a placeholder target |

`target` is what a shell command is about: runtime name, cluster, node, and node start order. In the UI it comes from the selection; in docs it is `<cluster>`, `<node>`, and `docker`.

Navigation keys (arrows, `tab`, `/`, `esc` to clear the filter) are handled in `onDash` before the registry, because they are not actions.

Tests enforce that IDs, `:` names, and keys are unique per scope, that a global key is never also a pane key, that every hint ID exists, and that `docs/commands.md` equals `Cheatsheet()`.

## UI model

Screens: dashboard, create, operation, info, command reference. Overlays: help, command palette, modals. The dashboard has two panes, clusters and nodes, and `focus` says which one gets keys. Help and modals (yes/no confirm, type-the-name delete, image name) are drawn over the current screen with `overlay`. `:` and `/` open a one-line prompt box under the header.

`geom()` computes every rectangle: header (6 rows, or 1 below 24 rows), prompt, panes, overview, footer. `View` and the mouse handler both use it, so click hit-testing cannot drift from what is drawn. Every frame is exactly `width × height`. Tests check each line's width.

`table` lays out columns by priority: when space runs out, the highest `prio` column is hidden first. `NAME` sizes itself to the longest name.

`begin(kind, title, shown, timeout, follow, fn)` owns one operation at a time. `follow` pins the view to the newest line (logs, create) or starts at the top (describe, pods). Lines arrive in batches, so a chatty log costs one render per batch. Viewer kinds (`logs`, `journal`, `describe`, `pods`, `export`) do not pause the background refresh, and `esc` stops them and goes back. Mutating kinds cancel on `esc`.

Background ticks are one second. The cluster list refreshes every two, detail every five, and a new tool probe every fifteen. In-flight list and detail calls are single-flight. Stale tool probes are dropped with `toolsSeq`.

Colors are fixed mid-tones and body text uses the terminal's own foreground, so nothing depends on light/dark detection.

## Adding a command

1. Put the process work on `Engine` and stream with `streamCmd` or `capture`. Validate every argument with `ValidName`, `ValidIdent`, or `ValidImage` before it reaches `exec`.
2. Add one entry to `buildActions()` in `internal/ui/actions.go`: id, keys, `:` name, scope, description, `shell` equivalent, and `run`. Add its id to `hintIDs` if it belongs in the header. Key dispatch, `:` command, completion, help, palette, and reference pick it up automatically.
3. Long work goes through `begin(kind, title, m.shellFor(id, node), …)`, which gives cancel, a log, and the `$` command line. Short work can return `toastMsg`.
4. Run `make docs` to regenerate `docs/commands.md` (a test fails if you forget), and describe new screens in `docs/usage.md`.
5. Interactive tools (`docker exec -it`, `k9s`) must use `tea.ExecProcess`. Build the command with `Engine.ShellCmd` or `Engine.K9sCmd`. Leave the command's stdio unset. Bubble Tea attaches the terminal.
6. Extend `View` only with data already on the model.

## Tests

`internal/engine/engine_test.go` covers ps parsing, status, start/stop order, pod and event JSON, kind YAML, validators, runtime preference (live Podman beats a stopped Docker; a Docker client beats a stopped Podman; broken symlinks are skipped), echo streaming, and cancel of `sleep`.

`internal/ui/view_test.go` renders a fixed model at several widths and checks every line's width, that all nodes appear, the banner, node focus and actions, confirm and delete modals, filter, help, compact layout, create, the `:` command line and completion, and mouse selection. `internal/ui/actions_test.go` covers the registry rules, the generated docs, the reference screen, the palette, pane-specific key dispatch, and the `$` command line. They pass a nil engine. `begin` refuses to start without an engine.

`KINDTUI_LIVE=1 go test ./internal/engine/ -run TestLiveDiscover -v` prints the runtime this machine actually resolved. It is skipped by `make test`.

## Limits

- Unix only. `proc_unix.go` is behind the `unix` build tag, so Windows does not build.
- Needs a real TTY. Piped stdout exits with a message.
- One mutation at a time.
- Stats come from `docker stats`, not `kubectl top`, so metrics-server is not required.
- Node shells use `bash` when the node has it, `sh` otherwise.
- The app does not edit your kind config. Repo shape passes the file through. Custom shape generates a throwaway manifest.
- Log export and the kubeconfig cache are best-effort files. Export goes to `~/kind-logs`. The cache file is removed on delete via `ForgetKubeconfig`.
- A cluster that exists only as a kubeconfig entry, with no labeled containers, does not appear. `docker ps -a` is the source of truth, which is what still shows stopped nodes.
