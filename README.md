# MTCF CLI

> **Modern CTF Framework** — opinionated, end-to-end CLI for running CTF competitions as reproducible infrastructure.

[![ci](https://github.com/MicroClub-USTHB/mctf-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/MicroClub-USTHB/mctf-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> ⚠️ **Internal alpha — MicroClub USTHB devs only.** Public release happens once Phase 1 (`init` / `build` / `deploy`) is complete. The on-disk contract (Phase 0, schema v1) is locked. This README is the internal handbook; the public-facing README + `CONTRIBUTING.md` will replace it before going public.

---

## What's in this repo

| Path | Purpose |
|---|---|
| `cmd/`, `internal/`, `main.go`, `go.mod` | The Go CLI source. |
| `schema/` | The 4 canonical JSON Schemas (the contract). |
| `internal/schemas/` | Embedded copy used by the binary. Synced from `schema/` by `make schemas`. |
| `docs/spec/` | The 6 spec docs every contributor should read first. |
| `examples/sample-event/` | Reference event (5 challenges, one per archetype) — the e2e fixture. |
| `scripts/` | Python sanity-check validators used by CI. |
| `.github/workflows/ci.yml` | CI: `go vet/test/build`, `validate`, `generate --check`, schema sanity. |
| `Makefile` | `make build / test / lint / tidy / schemas / clean`. |

## Quick start (internal devs)

```sh
git clone git@github.com:MicroClub-USTHB/mctf-cli.git
cd mtcf-cli
make build                                            # ./mtcf

./mtcf -C examples/sample-event validate              # spec rules
./mtcf -C examples/sample-event generate              # write derived files
./mtcf -C examples/sample-event generate --check      # CI integration point

go test ./...                                         # unit tests
```

Requires Go 1.23+, GNU make. Python (`pyyaml`, `jsonschema`) only needed if you run the script-based sanity checks.

---

## Features

### Shipped (alpha)

#### Scaffolding
- **`mtcf init`** — scaffold a new event repo from embedded templates (config, README, CI workflow, gitignore, starter challenge template).

#### Core contract
- **CTF-as-Infrastructure-as-Code** — every event is one declarative repo.
- **Single source of truth** — `mtcf.config.yml` + per-challenge `challenge.yml`.
- **Strict challenge contract** — 4-folder layout (`challenge/`, `solution/`, `assets/`, `other/`).
- **Three archetypes** — `static`, `service`, `instanced`. Derived from the manifest, not declared separately.
- **22 challenge categories** — 10 core (web, pwn, crypto, reverse, forensics, osint, misc, ai, hardware, jailbreak), 12 opt-in extended, plus arbitrary custom slugs.
- **Per-category port-range allocation** (10000–17999, deterministic).
- **Source-of-truth vs derived files** separation.

#### Validation
- **Schema-driven validation** (Draft 2020-12 JSON Schema).
- **Event-wide uniqueness checks** (port, subdomain, flag).
- **Static-flag image-leak guard** — scans `challenge/src/` for hardcoded flags before they bake into a public image.
- **Hard-fail vs warning catalog** — every rule has an ID for clean CI output.

#### Generation
- **Auto-generated derived artifacts** — CTFd-format manifest, instancer `instance.toml`, `challenges.csv`, `index.json`.
- **Deterministic, idempotent** — same input → byte-identical output.
- **`--check` mode** — exits non-zero on drift; the CI gate that keeps sources and derived files locked together.

#### Distribution
- **Single static Go binary**, cross-platform.
- **GitHub Actions CI** runs Go build/test + schema sanity on every push and PR.
- **Embedded JSON Schemas** — no external file lookups at runtime.

### Coming

#### Build & deploy
- **Multi-mode deployment** — Docker (compose), SSH (single VPS), Kubernetes (cluster).
- **Opinionated profiles** — `simple` / `instanced` / `pro` curated presets.
- **Build-once-deploy-many** — images built ahead of time, deploys are near-instant pulls.
- **Container image registry integration** — Docker Hub, GHCR, others.
- **Image tagging convention** — `<registry>/<event>-<cat>-<chall>:<sha>` + `:latest`.
- **Auto-generated CI/CD pipelines** for the event repo (GitHub Actions, GitLab CI).
- **Compose / Kubernetes manifest generation** from the manifest.
- **Ingress & TLS routing** — Traefik (Docker mode), Nginx/IngressRoute (K8s mode).

#### Runtime
- **Native instancer** — per-team isolated challenges, predictable naming, TTL cleanup, no Whaley dependency.
- **Per-team isolation** — containers under Docker, Pods under Kubernetes.
- **Dynamic flags** — per-team flag values minted at spawn time.
- **Resource limits & sandbox isolation** — `cpu` / `memory` / `pids` caps, `nsjail` / `redpwn/jail` for pwn.
- **Continuous health checks** with optional auto-restart.

#### Infrastructure
- **Terraform wrapper** — `mtcf infra plan|apply|destroy` for VPS, clusters, networking, DNS.
- **VPS provisioning templates** — DigitalOcean, Hetzner.
- **Cluster provisioning templates** — DOKS, EKS, GKE.
- **DNS automation** for `<subdomain>.<event-domain>` routing.
- **Autoscaling** under the `pro` profile.

#### Observability & comms
- **Monitoring stack** — Prometheus + Grafana with prebuilt dashboards.
- **Container metrics** via cAdvisor.
- **Real-time notifications** — Discord webhooks (deploy success/failure, downtime, instancer errors); generic webhook target.

#### Extensibility
- **Plugin system** — subprocess + JSON-RPC over stdio (Terraform-style); language-agnostic, crash-isolated.
- **Lifecycle hooks** — `before/after_build`, `before/after_deploy`, `on_instance_spawn`, `on_health_failure`.
- **Pluggable platform integration** — CTFd first-class; rCTF and generic-webhook platforms via plugin.

#### DX & ops
- **`mtcf init challenge <category>/<slug>`** — scaffold a single challenge from a local template.
- **`mtcf doctor`** — preflight: docker daemon, registry auth, CTFd token, ports free, disk, DNS.
- **Local dev mode** — full stack on a laptop with hot reload.
- **Per-challenge log streaming**.
- **Dry-run + safe teardown** — preview every change; tear environments down without orphans.

#### Security
- **Secrets hygiene** — env-only, never on disk.
- **Network isolation** — explicit ports, ingress rules, per-event Docker network.
- **Privileged-mode policy** — warn under `simple`, block under `pro`.
- **Capability allowlist** — `cap_add` audited.

#### Distribution
- **Homebrew tap, Scoop bucket, `.deb`/`.rpm`** via goreleaser.
- **Cross-platform binaries** — darwin-arm64/amd64, linux-amd64/arm64, windows-amd64.

---

## Roadmap (versions)

| Version | Focus |
|---|---|
| **v0 (alpha, current)** | Phase 0 spec + `validate` + `generate` + `init`. |
| **v0.1** | `mtcf init challenge`, `mtcf build`, `mtcf deploy` (Docker mode), CTFd integration, `mtcf doctor`, log streaming, dev mode, safe teardown. |
| **v0.2** | SSH deploy, native instancer, Discord notifications, plugin system, dynamic flags. |
| **v0.3** | Kubernetes deploy, Terraform infra wrapper, Prometheus + Grafana, `pro` profile fully wired. |
| **v0.4** | rCTF + generic-webhook platform integrations, read-only web dashboard, goreleaser distribution. |

## How the contract works

```
challenges/<cat>/<slug>/                       ← author edits these four folders
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

`mtcf generate` re-emits the derived files deterministically. CI runs `mtcf generate --check` on every PR and refuses to merge if the committed derived files would change.

## Documentation

- **[Repository layout](docs/spec/repository-layout.md)** — directory contract.
- **[Categories](docs/spec/categories.md)** — 22 categories + sub-tag conventions.
- **[Challenge manifest](docs/spec/challenge-manifest.md)** — every field of `challenge.yml`.
- **[Derived files](docs/spec/derived-files.md)** — exact format of generated artifacts.
- **[Validation](docs/spec/validation.md)** — every rule with rationale.
- **[CHANGELOG](docs/spec/CHANGELOG.md)** — schema version history.

## Contributing

Internal MCTF devs only while alpha — see [`CONTRIBUTING.md`](CONTRIBUTING.md) for setup, project layout, the five non-negotiable rules, and how-to recipes for adding validation rules, commands, and derived-file formats.

Quick local loop:

```sh
make build       # produce ./mtcf
make test        # unit tests
make schemas     # re-sync embedded schemas from schema/
go vet ./...
```

## Background

MTCF is the spiritual successor to the tooling that grew up around the [MCTF 5.0 challenge repository](https://github.com/MicroClub-USTHB/MCTF-5.0-Challenges). The on-disk format works (50+ challenges, 10 categories, ran a real event), but the surrounding tooling fragmented into a Bun/TypeScript validator, a Windows `.bat` sync script calling Python `ctfcli`, hand-edited Traefik configs, and a separate "Whaley" instancer. **MTCF replaces all of it with one Go binary** that reads (almost) exactly the same on-disk format — challenge authors keep writing challenges the way they already do.

## License

MIT — see [LICENSE](LICENSE).
