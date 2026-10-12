# Architecture

How the `mctf` CLI is put together — the packages, how data flows through
them, and where the on-disk contract fits. For the commands themselves see
[`../commands.md`](../commands.md); for the file layout see
[`repository-layout.md`](repository-layout.md).

## One-line model

An MCTF event is **Infrastructure-as-Code**: a single `mctf.config.yml` plus a
per-challenge on-disk contract under `challenges/`. The CLI reads that source,
validates it, and drives every downstream step — scaffold, generate, build,
deploy, instance, platform sync, monitor — from it. No hidden state: the repo
is the source of truth, and derived files are regenerated, never hand-edited.

## The pipeline

```
          ┌─────────┐   ┌───────────┐   ┌────────────┐
 author → │ config  │ + │ challenge │ → │  validate  │ ─ fail fast on errors
 edits    └─────────┘   └───────────┘   └────────────┘
               │              │                │
               └──────────────┴────────────────┘
                              ▼
                        ┌───────────┐     derived files:
                        │ generate  │ →   CTFd challenge.yml, instance.toml,
                        └───────────┘     challenges.csv, index.json,
                              │           ingress + monitoring config
          ┌───────────────────┼───────────────────┐
          ▼                    ▼                   ▼
     ┌─────────┐         ┌───────────┐       ┌───────────┐
     │  build  │ ──tags→ │  deploy   │ +     │ instancer │  (per-team,
     └─────────┘         │ + ingress │       └───────────┘   at game time)
      images to          └───────────┘
      registry            docker / ssh / kubernetes
                              │
                    ┌─────────┴──────────┐
                    ▼                    ▼
              ┌───────────┐        ┌────────────┐
              │ platform  │        │ monitoring │
              │  (CTFd)   │        │ prom+graf  │
              └───────────┘        └────────────┘
```

Supporting commands read the same model: `solve` smoke-tests a challenge's
solver, `status`/`info` report runtime and derived state, `doctor` preflights
the repo and host.

## Package map (`internal/`)

| Package | Responsibility |
| --- | --- |
| `config` | Load + schema-validate `mctf.config.yml` into a typed `Config`. |
| `challenge` | Load per-challenge manifests, derive the **archetype** (static / service / instanced). |
| `validate` | Cross-cutting spec checks (`S`/`A`/`U`/`D`/`SEC`/`SOL`/`W`/`I` rule families). `generate` hard-fails on errors. |
| `generate` | Pure `Plan()` → derived files (CTFd manifest, `instance.toml`, CSV, `index.json`, ingress, Prometheus scrape config). `Apply()`/`Diff()` for write vs `--check`. |
| `build` | Plan + run `docker build` per deployable challenge; tag `:<sha>` + `:latest`; optional push + leak scan. |
| `deploy` | Plan + run per-challenge `docker compose` (or kubectl) — `docker`/`ssh`/`kubernetes` modes; immutable image-tag pinning; rollback. |
| `ingress` | Traefik routing config (opt-in, docker mode). |
| `kubernetes` | K8s Deployment/Service/Ingress manifests + `kubectl apply` lifecycle. |
| `instancer` | Per-team instances: on-disk store, lifecycle, docker runner, daemon, reaper; **plus an API client** for the remote Python instancer. |
| `monitoring` | Render + run the Prometheus + Grafana + cAdvisor stack. |
| `platform/ctfd` | Sync challenges + assets to CTFd (content-hash aware), unsync, configure. |
| `solve` | Run a challenge's solver and verify the recovered flag (incl. `--against-instance`). |
| `status` / `info` | Runtime state and derived-fact reporting (human + `--json`). |
| `doctor` | Preflight checks on the repo and host (DNS, disk, docker, etc.). |
| `scaffold` | Render embedded templates for `init` / `init-challenge`. |
| `notify` | Best-effort deploy/build outcome messages (Discord webhook). |
| `git` | Thin `git` helpers (SHA, archive, tags) for registry-pinned deploy + rollback. |
| `jsonschema` | Wrapper over `santhosh-tekuri/jsonschema` for schema validation. |
| `schemas` | Embeds the canonical JSON Schemas so the binary is self-contained. |

`cmd/` is a thin cobra layer: each command loads config + discovers
challenges, then calls the matching `internal/` package. Logic lives in
`internal/`; `cmd/` wires flags and renders output.

## Challenge archetypes

Derived from the manifest by `challenge.DeriveArchetype`:

- **static** — `need_deploy: false`. No container; `generate`/`platform` only.
- **service** — `need_deploy: true`, shared instance. Handled by `build` + `deploy`.
- **instanced** — `need_deploy: true` + `dynamic_instances: true`. Skipped by
  `deploy`; spawned per-team at game time by the instancer.

## Deploy modes

`deploy`/`down` select a runtime via `deploy.mode` or `--mode`:

- **docker** (default) — local `docker compose`; Traefik ingress opt-in.
- **ssh** — scp the compose tree to a VPS and run compose over SSH.
- **kubernetes** — generate manifests and `kubectl apply`.

Deploys consume **immutable pinned image tags** from `build`, enabling
deterministic deploys and `deploy --rollback`.

## Instancer: two implementations

- **Native (Go, `internal/instancer`)** — a lightweight single-host daemon +
  on-disk store, driven by `mctf instance` / `mctf instancer`. **Deprecated**
  for production; kept for offline local dev.
- **Remote ([MCTF Instancer](https://github.com/MicroClub-USTHB/mctf-instancer))** —
  the production Python service (CTFd-integrated, per-team auth, Traefik
  routing). When `instancer.url` is set, `mctf instance list` / `destroy` drive
  it over its admin API (`internal/instancer.APIClient`).

## Design notes

- **Derived files are never authored.** `generate --check` enforces that the
  committed derived files match their sources (used in CI).
- **Pure planners, injected runners.** `build`/`deploy`/`generate`/`monitoring`
  split a pure `Plan()` from an effectful runner, so planning is unit-tested
  without docker.
- **Self-contained binary.** Schemas and scaffold templates are embedded; the
  only runtime externals are `docker`/`git`/`ssh`/`kubectl` when a step needs
  them.
