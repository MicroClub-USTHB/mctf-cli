# MTCF CLI

> Modern CTF Framework — an opinionated, end-to-end CLI for building, deploying, and operating Capture The Flag competitions as reproducible infrastructure.

[![ci](https://github.com/MicroClub-USTHB/mctf-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/MicroClub-USTHB/mctf-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Status: alpha.** The challenge contract (Phase 0) is locked at schema v1. The Go CLI (Phase 1) currently ships `validate` and `generate`; `init`, `build`, `deploy`, and `doctor` land next. See the [roadmap](#roadmap).

## What it does

MTCF turns a CTF event into a single declarative repository. One [`mtcf.config.yml`](docs/spec/repository-layout.md), the per-challenge contract under [`challenges/`](docs/spec/repository-layout.md), and a strict source-of-truth-vs-derived-files separation give you reproducible builds, deterministic deployment, and per-team instancing — all from one binary.

| Command | Status | What it does |
|---|:---:|---|
| `mtcf validate` | ✅ shipped | Runs every spec rule against the repo. Hard-fails block, warnings inform. |
| `mtcf generate [--check]` | ✅ shipped | Re-emits the four derived files (CTFd manifest, `instance.toml`, CSV, JSON index). `--check` is the CI integration point. |
| `mtcf version` | ✅ shipped | Prints version, commit, build date. |
| `mtcf init` | 🚧 next | Scaffold a new event repo from embedded templates. |
| `mtcf build [--push]` | 🚧 next | Build and tag every deployable challenge image, optionally push to the registry. |
| `mtcf deploy [--dry-run]` | 🚧 planned | Generate compose / k8s manifests, bring up containers, register in CTFd. |
| `mtcf doctor` | 🚧 planned | Preflight: docker daemon, registry auth, CTFd token, ports free. |
| `mtcf logs [chall]` | 🚧 planned | Stream container logs. |
| `mtcf dev`, `mtcf down` | 🚧 planned | Local dev mode and teardown. |

## Install

### From source (recommended while alpha)

```sh
git clone https://github.com/MicroClub-USTHB/mctf-cli.git
cd mtcf-cli
make build           # produces ./mtcf
./mtcf version
```

Requirements: Go 1.23+, GNU make.

## Quick start

A complete reference event lives at [`examples/sample-event/`](examples/sample-event/) — five challenges covering all three archetypes (static / service / instanced).

```sh
# from the repo root, against the bundled sample event:
./mtcf -C examples/sample-event validate
./mtcf -C examples/sample-event generate
./mtcf -C examples/sample-event generate --check     # CI integration point
```

To run against your own event:

```sh
./mtcf -C path/to/your-event validate
```

## How it works

Authors only edit four folders per challenge — `challenge/`, `solution/`, `assets/`, `other/`. The CLI does the rest:

```
challenges/<cat>/<slug>/                           ← author edits the four folders
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

Run `mtcf generate` and the derived files at the challenge root + `challenges/challenges.csv` + `challenges/index.json` are re-emitted deterministically. CI runs `mtcf generate --check` on every PR and refuses to merge if the committed derived files would change — the contract that keeps sources and derived files in lockstep.

## Documentation

The contract is the docs.

- **[Repository layout](docs/spec/repository-layout.md)** — directory contract for an event repo.
- **[Categories](docs/spec/categories.md)** — 10 core + 12 extended challenge categories.
- **[Challenge manifest](docs/spec/challenge-manifest.md)** — every field of `challenge.yml`, with examples.
- **[Derived files](docs/spec/derived-files.md)** — exact format of generated files.
- **[Validation](docs/spec/validation.md)** — every hard-fail and warning, with rationale.
- **[CHANGELOG](docs/spec/CHANGELOG.md)** — schema version history.

JSON Schemas are in [`schema/`](schema/) for editor autocomplete and external tooling.

## Roadmap

- **v0 (alpha, current)** — Phase 0 spec + `validate` + `generate`.
- **v0.1** — `mtcf init` + `mtcf build` + `mtcf deploy` (Docker mode) + CTFd integration.
- **v0.2** — SSH deploy, native instancer, Discord notifications, plugin system (subprocess + JSON-RPC stdio).
- **v0.3** — Kubernetes deploy, Terraform infra wrapper, Prometheus + Grafana monitoring.
- **v0.4** — additional platform integrations (rCTF, generic webhook), web dashboard.

## Background

MTCF is the spiritual successor to the tooling that grew up around the [MCTF 5.0 challenge repository](https://github.com/MicroClub-USTHB/MCTF-5.0-Challenges). MTCF 5.0's on-disk format works — 50+ challenges across 10 categories ran a real event on it. But the surrounding tooling fragmented into a Bun/TypeScript validator, a Windows `.bat` sync script calling Python `ctfcli`, hand-edited Traefik configs, and a separate "Whaley" instancer service.

MTCF replaces all of that with one Go binary that reads (almost) exactly the same on-disk format — challenge authors keep writing challenges the way they already do.

## Contributing

This repo is currently in private alpha for [MicroClub-USTHB](https://github.com/MicroClub-USTHB) members. A `CONTRIBUTING.md` will land before the public release.

Local development:

```sh
make build       # build the binary
make test        # run all tests
go vet ./...     # static analysis
```

## License

MIT — see [LICENSE](LICENSE).
