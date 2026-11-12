# Contributing

## Setup

```sh
git clone https://github.com/chanupen/kindtui.git
cd kindtui
make build && ./bin/kindtui
```

You need Go at the version in `go.mod`. To try the app against real clusters, you also need kind and Docker or Podman.

## Make targets

| Target | What it does |
| --- | --- |
| `make build` | `bin/kindtui`, version from `git describe` |
| `make run` | Build and start |
| `make test` | Unit tests. No daemon needed |
| `make vet` | `go vet` plus a gofmt check (CI runs this) |
| `make fmt` | `gofmt -w .` |
| `make dist` | Release binaries for linux/darwin × amd64/arm64 in `dist/` |
| `make install` | `go install` |

To probe the tools actually installed on your machine:

```sh
KINDTUI_LIVE=1 go test ./internal/engine/ -run TestLiveDiscover -v
```

## Conventions

- `View` is pure. Every `docker`, `kind`, and `kubectl` call runs in a `tea.Cmd`.
- Validate every value passed to `exec` with `ValidName`, `ValidIdent`, or `ValidImage`.
- Data commands (JSON, kubeconfig) use `captureOut` so stderr warnings cannot corrupt them.
- A new key needs: a handler in `update.go`, a hint in `keys.go` `headerHints`, a row in `helpBox`, and a line in `docs/usage.md`.
- Interactive programs (shells, k9s) go through `tea.ExecProcess`.

[docs/architecture.md](docs/architecture.md) has the details.

## Releases

1. Update `CHANGELOG.md` (new `## [x.y.z] — date` section and link) and `version` in `main.go`. Also update the version in the README and quickstart download commands.
2. `make vet test`, then commit and push to `main`.
3. `git tag -a vX.Y.Z -m "kindtui X.Y.Z" && git push origin vX.Y.Z`
4. CI (`.github/workflows/ci.yml`) tests the tag, runs `make dist VERSION=vX.Y.Z`, and attaches `kindtui-{linux,darwin}-{amd64,arm64}` to a GitHub release.
5. Check it: `gh release view vX.Y.Z -R chanupen/kindtui`.
6. Update your local install: `make build VERSION=vX.Y.Z && install -m 0755 bin/kindtui ~/.local/bin/`.

Released so far: [v0.2.0](https://github.com/chanupen/kindtui/releases/tag/v0.2.0).
