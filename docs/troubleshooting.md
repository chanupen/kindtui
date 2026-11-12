# Troubleshooting

Press `I` in kindtui to see which binaries it resolved, their versions, and any broken symlinks it skipped.

## The banner says the runtime is down

kindtui never runs `sudo` or `systemctl` itself.

| Setup | Fix |
| --- | --- |
| Linux + Docker | `sudo systemctl start docker`. If it fails with `permission denied`, add yourself to the `docker` group and log in again |
| Linux + Podman (rootless) | `systemctl --user start podman.socket` |
| macOS + Colima | Press `C` in kindtui, or run `colima start` |
| Podman machine (macOS) | Press `C`, or run `podman machine start` |

## kind uses the wrong runtime ("enabling experimental podman provider")

kindtui runs kind with `KIND_EXPERIMENTAL_PROVIDER` set to the runtime it detected, and puts that runtime first on `PATH`. If your shell exports a different `KIND_EXPERIMENTAL_PROVIDER`, kindtui overrides it for its own child processes only.

On Fedora, the `docker` command is often the `podman-docker` wrapper. kindtui recognizes it and treats it as Podman.

## READY, VERSION, or PODS show `—`

These need kubectl and a running control plane.

- kubectl is missing: install it. The header shows `Kubectl: missing`.
- The cluster was just started: the API server needs 10–30 seconds. The overview shows the kubectl error until then.
- To see the error yourself: `kind get kubeconfig --name <cluster> > /tmp/k && kubectl --kubeconfig /tmp/k get nodes`.

## CPU or MEM show `—`

These come from `docker stats --no-stream` / `podman stats --no-stream`. Rootless Podman needs cgroups v2 with the cpu and memory controllers delegated to your user. Check with:

```sh
cat /sys/fs/cgroup/user.slice/user-$(id -u).slice/user@$(id -u).service/cgroup.controllers
```

## Logging in to a node fails

- The node must be running. Press `u` to start it.
- kindtui runs `<runtime> exec -it <node> sh -c 'exec bash -l || sh -l'`. Try the same command by hand to see the error.
- After you exit, kindtui returns. A non-zero exit code from your last shell command is not treated as an error.

## `o` (k9s) does nothing or errors

- k9s must be on `PATH`.
- The cluster must be Running or Degraded.
- k9s uses kindtui's private kubeconfig. If it complains about the config, delete `$TMPDIR/kindtui-<name>.kubeconfig` and press `o` again.

## The screen looks wrong

- Use a terminal with 256-color or truecolor support, and a font that has box-drawing characters (`╭─╮`) and braille spinners.
- Under tmux or screen, set `TERM` to `tmux-256color` or `screen-256color`.
- The minimum size is 60×16. Below 24 rows the header shrinks to one line.

## A cluster I created elsewhere does not show

kindtui lists clusters from containers labelled `io.x-k8s.kind.cluster` on the runtime it detected. A cluster created with Docker does not show while kindtui is using Podman, and the reverse. Check `I` to see which runtime is in use.

## Reporting a bug

Please include the output of `kindtui --version`, `kind version`, `docker version` or `podman version`, your OS, and what the `I` screen shows.
