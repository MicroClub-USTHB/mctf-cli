# Repository Layout

> An MTCF event lives in its own git repository. This document defines the directory contract.

## At a glance

```
my-event/                              # the event repo
├── mtcf.config.yml                    # central event config (NEW in MTCF)
├── .env.example                       # template for secrets (registry creds, CTFd token, webhooks)
├── .gitignore                         # ignores .mtcf/, .env, *.flag
├── .github/workflows/
│   ├── validate.yml                   # runs `mtcf validate` on every push/PR
│   └── autogen.yml                    # runs `mtcf generate` and commits derived files
├── challenges/
│   ├── challenges.csv                 # AUTO-GENERATED tracking sheet
│   ├── index.json                     # AUTO-GENERATED machine-parseable index
│   └── <category>/<challenge_name>/   # one directory per challenge
│       ├── challenge.yml              # AUTO-GENERATED CTFd manifest
│       ├── instance.toml              # AUTO-GENERATED instancer config (only when dynamic_instances)
│       ├── challenge/                 # ── AUTHOR EDITS BELOW ──
│       │   ├── challenge.yml          # SOURCE OF TRUTH manifest (required)
│       │   ├── Dockerfile             # required for deployable archetype
│       │   ├── docker-compose.yml     # required for deployable archetype
│       │   └── src/                   # application source (optional)
│       ├── solution/
│       │   ├── writeup.md             # required — author's intended solve
│       │   ├── hints.md               # optional — progressive hints
│       │   └── solve.sh|.py           # optional — smoke-test solver
│       ├── assets/                    # downloadable handouts (uploaded to CTFd)
│       └── other/                     # private author-only files
├── templates/
│   └── challenge_name/                # `mtcf init challenge` copies this
├── ingress/                           # AUTO-GENERATED routing config
│   └── traefik/
│       ├── traefik.yml
│       └── dynamic/{http-routers,http-services,tcp-routers,tcp-services,middlewares,tls}.yml
├── infra/                             # OPTIONAL — terraform modules (`mtcf init --infra`)
├── plugins/                           # OPTIONAL — local plugin binaries (v0.2+)
└── .mtcf/                             # CLI working dir (gitignored)
    ├── compose.generated.yml
    ├── ports.json                     # deterministic port allocations
    ├── state.json                     # last-deploy snapshot (for --rollback)
    └── deploy.log
```

## Source-of-truth vs derived files

The single most important rule:

- **Authors only edit the four folders inside `challenges/<cat>/<name>/`** — `challenge/`, `solution/`, `assets/`, `other/`.
- `mtcf generate` produces **derived files** at the challenge root and the event root:
  - `challenges/<cat>/<name>/challenge.yml` — CTFd-format manifest
  - `challenges/<cat>/<name>/instance.toml` — instancer config (only when applicable)
  - `challenges/challenges.csv` — event tracking sheet
  - `challenges/index.json` — machine-parseable manifest
  - `ingress/traefik/dynamic/*.yml` — routing rules

Both the author files and the derived files are **committed**. Reviewers see exactly what will be deployed; CI re-runs `mtcf generate` on every push and fails if the committed derived files diverge from regenerated ones.

## Why four folders per challenge

Each subdirectory under a challenge has a single, predictable purpose:

| Folder | Visibility | Purpose |
|---|---|---|
| `challenge/` | **internal** (used to build the image) | `challenge.yml`, `Dockerfile`, `docker-compose.yml`, `src/`. Everything needed to run the challenge. |
| `solution/` | **never published** | `writeup.md` (intended path), `hints.md`, optional `solve.{sh,py}`. Used for review and post-event publishing. |
| `assets/` | **public** (uploaded to CTFd as files) | What players download. Recommended to ship as zips. |
| `other/` | **private** | Flag generators, scratch notes, decoys not used in build. |

## Naming rules

- Directory immediately under `challenges/` must be a category slug (matches `challenge.category` exactly).
- Directory immediately under `<category>/` is the challenge slug. Allowed characters: `[a-zA-Z0-9_-]+`. **No spaces.**
- The dir slug should match `challenge.name` (lowercased, spaces → `_`). Case-only mismatch is a warning, not a hard fail.
- Two challenges can never share the same fixed `deploy.port` or `deploy.subdomain` within an event.

## Profiles

`mtcf.config.yml::profile` selects a curated preset. Profiles affect *defaults and lint strictness*, not the on-disk layout.

| Profile | Deploy mode | Instancer | Monitoring | Hard-fails added |
|---|---|---|---|---|
| `simple` | Docker (compose, single host) | none | none | none |
| `instanced` | Docker | native MTCF instancer | none | healthcheck required for instanced challenges |
| `pro` | Kubernetes | native MTCF instancer (K8s mode) | Prometheus + Grafana | `privileged:true` blocked, capability allowlist enforced, resource limits required |

Profiles are *defaults* — anything in `mtcf.config.yml` can override them.

## CI workflow contract

Every event repo ships with two GitHub Actions:

- **`validate.yml`** — runs on every push and PR; fails the build on any hard-fail rule from [validation.md](./validation.md).
- **`autogen.yml`** — runs `mtcf generate`; if any derived file diverges, commits the regenerated version on `main`/`production`.

These workflows are scaffolded by `mtcf init` and live under `.github/workflows/`. Authors don't touch them.
