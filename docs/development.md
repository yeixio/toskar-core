# Development

## Prerequisites

- Go 1.26.3 or newer (`go.mod`)
- Node.js 22
- pnpm 9 (the root `package.json` pins `pnpm@9.15.0`)
- Git

Linux package builds also need `nfpm` 2.41.3, as used by the release workflow. Day-to-day daemon work does not.

## Setup

```bash
git clone https://github.com/yeixio/yggdrasil-core.git
cd yggdrasil-core
make start
```

`make` with no target prints `make help`. `make start` installs web dependencies, writes `web/dist`, builds `bin/yggdrasil-daemon` and `bin/yggctl`, and runs the daemon with `YGGDRASIL_WEB_UI_DIR` set to `web/dist`. Open `http://127.0.0.1:7331`.

`make daemon` stamps the current git commit into both binaries. A build from this tree reports `0.1.0-dev` unless `-ldflags` sets `internal/version.Version`. `yggctl version` and `yggdrasil-daemon -version` print the license and the corresponding-source URL. Release packaging sets the version as well, so a tagged build points at `tree/v<version>`.

A fork that serves a modified daemon over the network sets its own source URL at build time:

```text
-X github.com/yeixio/yggdrasil-core/internal/version.SourceURL=<url-of-your-corresponding-source>
```

## Run

```bash
make start
```

That is the same as `make run-daemon`. It rebuilds `web/dist` and the binaries, then serves the UI from `web/dist`.

Optional data directory, after `make daemon`:

```bash
YGGDRASIL_WEB_UI_DIR="$PWD/web/dist" ./bin/yggdrasil-daemon -data-dir "$PWD/.ygg-dev-data"
```

`make ui` only builds the web UI. `make frontend` does that and runs the web tests. The Vite dev server is separate and proxies `/api` and `/v1` to the daemon:

```bash
make run-web
```

That listens on `http://127.0.0.1:5173`. The daemon API stays on port 7331.

## Test

```bash
make test
make vet
```

`make test` is `go test ./...`. `make ci` runs format, vet, tests, and the frontend job locally. A push to `main` publishes the README coverage badge from `go test ./... -coverprofile`. CI does not enforce a coverage percentage.

Web tests alone:

```bash
cd web && pnpm test
```

Cluster check (Docker, stub inference, not a GPU test):

```bash
make test-cluster
```

Documentation snapshots:

```bash
python3 scripts/test_publish_docs.py
python3 scripts/publish-docs.py --destination /tmp/ygg-docs-check \
  --version 0.0.0-test --commit "$(git rev-parse HEAD)"
```

## Format and lint

Go formatting is `gofmt` via `make fmt`. CI fails if a Go file outside `web/`, `vendor/`, and `node_modules/` is not `gofmt`-clean.

`make lint` runs golangci-lint v2.14.0 (`.golangci.yml`: errcheck, govet, ineffassign, staticcheck, unused) and `pnpm lint` in `web/`. CI uses the same golangci-lint version. Install it with the upstream install script, or `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`. `go vet ./...` still runs on its own.

The web check in CI is `pnpm lint`, `pnpm exec tsc -b --pretty false`, `pnpm test`, and `pnpm build`. ESLint covers `web/src` and `web/vite.config.ts`. Generated files in `web/wailsjs` are ignored. Hook rules are `rules-of-hooks` and `exhaustive-deps`. Warnings do not fail the job.

## CI

[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) on Ubuntu:

- user-guide publish checks
- `gofmt`, `go vet`, golangci-lint, `go test ./...`
- cross-compiles of `yggdrasil-daemon` and `yggctl` for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64 (`CGO_ENABLED=0`)
- web lint, typecheck, test, and build

[`.github/workflows/security.yml`](../.github/workflows/security.yml) runs `govulncheck ./...` and `pnpm audit --prod` in `web/`. The audit step does not fail the job (`|| true`).

[`.github/workflows/release.yml`](../.github/workflows/release.yml) runs on tags matching `v*`. It builds packages, writes `SHA256SUMS.txt`, publishes a GitHub Release, and freezes `docs/<version>.json` from `docs/user-guide/guide.json`. It does not sign binaries.

[`.github/workflows/screenshots.yml`](../.github/workflows/screenshots.yml) recaptures `docs/screenshots` on a tag or when started by hand, then holds those stills into `demo.mp4` and `demo.gif`.

Dependabot is configured for Go modules, the web and screenshot npm trees, and GitHub Actions.

## Labels

Issue labels are defined in [`.github/labels.yml`](../.github/labels.yml). GitHub does not create them from that file. Maintainers synchronize them with `./scripts/sync-github-labels.sh`. CI does not run that script.

## Release flow

Tag `v*` → release workflow → Linux `.deb` and `.rpm` (amd64 and arm64), macOS headless archives (arm64 and amd64), Windows amd64 headless archive, `SHA256SUMS.txt` → GitHub Release.

The same job opens a Homebrew formula pull request, updates the `apt` branch, and merges a documentation snapshot onto `main`. A failure in the formula step does not fail the release. Signing is not part of this workflow. The checklist is [release-checklist.md](release-checklist.md).

## Conventions

This repository does not include an `AGENTS.md`. Match the package you are editing. Keep handlers thin and put behavior in `internal/`. Do not add license headers file by file. The project license is the root `LICENSE`.
