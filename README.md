# MCTF CLI

> **Modern CTF Framework** — an opinionated, end-to-end CLI for running CTF
> competitions as reproducible infrastructure.

[![ci](https://github.com/MicroClub-USTHB/mctf-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/MicroClub-USTHB/mctf-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

MCTF turns a CTF into **Infrastructure-as-Code**. One `mctf.config.yml` at the
event root, plus a per-challenge on-disk contract under `challenges/`, drives
everything through a single static binary: scaffolding, validation, build,
multi-mode deploy, per-team instancing, platform sync, monitoring, and plugins.
No hidden state — the repo is the source of truth, and derived files are
generated, never hand-edited.

> **Status:** pre-1.0, under active development. The on-disk contract
> (schema v1) is stable; commands and flags may still evolve.

---

## Install

```sh
# Homebrew (macOS / Linux)
brew install MicroClub-USTHB/tap/mctf

# Scoop (Windows)
scoop bucket add microclub https://github.com/MicroClub-USTHB/scoop-bucket
scoop install mctf

# Go
go install github.com/MicroClub-USTHB/mctf-cli@latest

# Docker
docker run --rm -v "$PWD:/event" -w /event ghcr.io/microclub-usthb/mctf:latest validate
```

Or grab a binary / `.deb` / `.rpm` from the
[releases page](https://github.com/MicroClub-USTHB/mctf-cli/releases).

### From source

```sh
git clone https://github.com/MicroClub-USTHB/mctf-cli.git
cd mctf-cli && make build   # produces ./mctf   (needs Go 1.23+ and make)
```

## Quick start

```sh
mctf init my-ctf && cd my-ctf        # scaffold an event repo
mctf init challenge web/welcome      # scaffold a challenge
mctf validate                        # check against the spec
mctf generate                        # (re)write derived files
mctf build                           # build challenge images
mctf deploy                          # bring challenges up (docker compose)
mctf platform sync                   # push to CTFd
```

Run `mctf --help` for the full command set, or see
[`docs/commands.md`](docs/commands.md).

## What it does

**Author & validate**
- `mctf init` / `init challenge` scaffold an event and challenges from templates.
- Strict, schema-driven **validation** (JSON Schema Draft 2020-12) with
  event-wide uniqueness checks, a static-flag image-leak guard, a committed-secret
  scan (`doctor` D043), and a rule catalog with stable IDs for clean CI output.
- Three **archetypes** derived from the manifest: `static`, `service`, `instanced`.

**Generate**
- Deterministic, idempotent **derived files** — CTFd manifest, instancer
  `instance.toml`, CSV, `index.json`, Traefik ingress, Prometheus scrape config.
- `generate --check` fails CI on drift, keeping sources and derived files locked.

**Build & deploy**
- `mctf build` — one image per deployable challenge, tagged `:<sha>` + `:latest`,
  with a post-build flag-leak scan.
- `mctf deploy` / `down` in **three modes** — `docker` (compose + optional Traefik),
  `ssh` (single VPS), `kubernetes` (manifests + `kubectl`).
- **Immutable image-tag pinning** for deterministic deploys and `--rollback`.

**Per-team instancing**
- A built-in instancer (CLI + HTTP daemon) for per-team challenge instances, or
  drive the dedicated [MCTF Instancer](https://github.com/MicroClub-USTHB/mctf-instancer)
  service by setting `instancer.url` — the CLI then manages it over its admin API.

**Operate**
- `mctf platform sync|unsync|configure` — CTFd integration (content-hash-aware
  asset re-upload).
- `mctf monitoring up` — a Prometheus + Grafana + cAdvisor stack with scrape
  config generated from your challenges.
- `mctf doctor` — preflight the repo and host; `mctf list|info|status|event` for
  inspection; `mctf solve` to smoke-test solvers (incl. `--against-instance`).

**Extend & distribute**
- **Plugin system** — language-agnostic plugins (subprocess + JSON-RPC over stdio)
  receive lifecycle hooks (`before_deploy` / `after_deploy` today); best-effort and
  crash-isolated. See [`docs/spec/plugins.md`](docs/spec/plugins.md).
- Releases ship cross-platform binaries, `.deb`/`.rpm`, a Homebrew cask, a
  Scoop manifest, and a GHCR image via goreleaser.

See the [backlog issues](https://github.com/MicroClub-USTHB/mctf-cli/issues) for
what's planned (Terraform `mctf infra`, rCTF/webhook platforms, more plugin
hooks, image vuln scanning, …).

## How the contract works

```
challenges/<cat>/<slug>/                   ← author edits these four folders
├── challenge.yml          ← AUTO-GENERATED (CTFd format)
├── instance.toml          ← AUTO-GENERATED (only when instanced)
├── challenge/
│   ├── challenge.yml      ← THE source-of-truth manifest
│   ├── Dockerfile
│   └── docker-compose.yml
├── solution/{writeup.md, hints.md, solve.py}
├── assets/                ← downloadable handouts
└── other/                 ← private author-only files
```

`mctf generate` re-emits the derived files deterministically; CI runs
`mctf generate --check` and refuses to merge if committed derived files would
change.

## Documentation

- **[Commands](docs/commands.md)** — the full command reference.
- **[Architecture](docs/spec/architecture.md)** — how the packages fit together.
- **[Repository layout](docs/spec/repository-layout.md)** · **[Categories](docs/spec/categories.md)** · **[Challenge manifest](docs/spec/challenge-manifest.md)** · **[Derived files](docs/spec/derived-files.md)** · **[Validation](docs/spec/validation.md)**
- **[Monitoring](docs/spec/monitoring.md)** · **[Plugins](docs/spec/plugins.md)** · **[Releasing](docs/RELEASING.md)**
- **[CHANGELOG](docs/spec/CHANGELOG.md)** · **[Backlog](docs/BACKLOG.md)**

## Contributing

Contributions are welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for setup,
project layout, and conventions, and the
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md). PRs target `develop`, one concern
per PR. Security issues: see [`SECURITY.md`](SECURITY.md) — please report
privately.

```sh
make build    # ./mctf
make test     # unit tests
make lint     # go vet + golangci-lint
make schemas  # re-sync embedded schemas from schema/
```

## Background

MCTF is the spiritual successor to the tooling that grew up around the
[MCTF 5.0 challenge repository](https://github.com/MicroClub-USTHB/MCTF-5.0-Challenges).
The on-disk format ran a real event (50+ challenges), but the surrounding
tooling had fragmented into a TypeScript validator, a `.bat` sync script, hand-
edited Traefik configs, and a separate instancer. **MCTF replaces all of it with
one Go binary** that reads (almost) exactly the same on-disk format — authors
keep writing challenges the way they already do.

## License

MIT — see [LICENSE](LICENSE).
