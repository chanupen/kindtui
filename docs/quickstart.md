# Quick start

This guide takes about ten minutes, from nothing to logging in to a node of a three-node cluster.

## 1. Install the prerequisites

You need a container runtime and kind. kubectl is strongly recommended.

**Linux (Docker)**

```sh
# Docker Engine: https://docs.docker.com/engine/install/
sudo usermod -aG docker "$USER"   # then log out and back in

# kind
curl -Lo kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64
chmod +x kind && sudo mv kind /usr/local/bin/

# kubectl
curl -LO "https://dl.k8s.io/release/$(curl -Ls https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl && sudo mv kubectl /usr/local/bin/
```

**Linux (Podman, e.g. Fedora)**

```sh
sudo dnf install podman podman-docker kind kubectl
```

kindtui detects Podman, including the `podman-docker` `docker` wrapper, and runs kind with `KIND_EXPERIMENTAL_PROVIDER=podman`.

**macOS**

```sh
brew install kind kubectl colima docker
colima start            # or let kindtui start it with the C key
```

Optional: `brew install k9s` (or your distro's package) to enable the `o` key.

Check the setup:

```sh
kind version && docker info >/dev/null && echo ok
```

## 2. Install kindtui

```sh
git clone https://github.com/chanupen/kindtui.git
cd kindtui
make build
sudo install bin/kindtui /usr/local/bin/
kindtui --version
```

## 3. Start it

```sh
kindtui
```

The header shows what kindtui found: the runtime and whether it is up, the kind and kubectl versions, and your current kubectl context. If the runtime is down, a yellow banner explains why. On macOS with Colima, or with a Podman machine, press `C` to start it.

## 4. Create a cluster

1. Press `n`.
2. **Name**: type a name such as `lab`.
3. **Shape**: use `←`/`→` to pick one:
   - `single`: one control-plane node, which is the fastest.
   - `repo config`: the first `kind-cluster-config.yaml`, `kind-config.yaml`, `kind.yaml`, or `kind.yml` found by walking up from the current directory, or the file named by `KIND_CONFIG`. Try `KIND_CONFIG=examples/kind-cluster-config.yaml kindtui` for one control plane and two workers.
   - `custom`: you type a worker count.
4. **Image** (optional): a node image such as `kindest/node:v1.33.1`. Leave it empty for kind's default.
5. **Wait**: how long kind waits for the control plane to be ready. Use `0` to skip.
6. Press `enter`. The bottom of the form shows the exact `kind create cluster ...` command it runs.

The output streams live. When it finishes, press `esc`.

## 5. Look at the nodes

The clusters pane is on the left (or on top in narrow terminals), and the nodes of the selected cluster are on the right. Within about two seconds the READY, IP, CPU, MEM, PODS, and VERSION columns fill in.

Press `enter` or `tab` to move into the nodes pane, then use `↑`/`↓` to choose a node.

## 6. Log in to a node

Press `enter` (or `s`) on a node:

```
» kindtui: logged in to lab-worker as root. Type exit or press ctrl+d to return.

root@lab-worker:~# crictl ps
root@lab-worker:~# systemctl status kubelet
root@lab-worker:~# exit
```

You come back to kindtui exactly where you left it.

You can log in from anywhere with the command line: `:shell lab-worker2`, or `:shell worker2` for the selected cluster. `tab` completes node names.

## 7. Logs and diagnostics

On a node in the nodes pane:

| Key | Shows |
| --- | --- |
| `l` | The node container's own log (kind entrypoint, systemd), followed live |
| `b` | `journalctl -u kubelet -f` inside the node |
| `J` | The containerd journal |
| `d` | `kubectl describe node` |
| `p` | Pods scheduled on this node |

For any other systemd unit, use `:journal <unit> [node]`.

In the log view, `↑↓`/`pgup`/`pgdn` scroll, `g`/`G` jump to the top or bottom, `f` toggles follow, and `esc` goes back.

## 8. Use the cluster

- `c` makes `kind-lab` your current kubectl context, so `kubectl get pods -A` works in another terminal.
- `o` opens k9s on the cluster without touching your kubeconfig.
- `i` loads a local image (for example `myapp:dev`) onto every node.

## 9. Stop, start, and clean up

| Key | Action |
| --- | --- |
| `x` | Stop all nodes. Containers are kept, so it costs no CPU and starts again fast |
| `s` | Start again (control plane first) |
| `r` | Restart |
| `ctrl-d` | Delete. Type the cluster name to confirm |

In the nodes pane, `x`, `u`, and `r` stop, start, and restart a single node. This is useful for testing how workloads behave when a node goes down.

## Next

- Every key and command: [usage.md](usage.md)
- Something not working: [troubleshooting.md](troubleshooting.md)
