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

1. Update `CHANGELOG.md` and `version` in `main.go`.
2. `git tag v0.x.y && git push --tags`.
3. CI tests the tag, builds `dist/*`, and attaches the binaries to a GitHub release.
