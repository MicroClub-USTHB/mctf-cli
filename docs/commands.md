# MCTF CLI — Commands

Everything below is shipped on `develop`. One binary (`mctf`) takes a CTF event
repo from scaffolding → validation → build → deploy → platform sync.

## Deploy modes (all three shipped)

`mctf deploy` / `mctf down` support three modes via `deploy.mode` in
`mctf.config.yml` or `--mode`:

| Mode | What it does |
| --- | --- |
| `docker` (default) | `docker compose up -d` locally. Publishes host ports; Traefik ingress is **opt-in** (`deploy.docker.ingress: traefik`). Best for local dev / single machine. |
| `ssh` | scp the compose tree to a remote host and run compose over SSH. Supports wait-for-healthy, `IdentitiesOnly`, key-perm checks, and `ProxyJump`. For a single VPS. |
| `kubernetes` | Generate Deployment/Service/Ingress manifests and `kubectl apply`. Deterministic NodePorts, pod-security hardening. For a cluster. |

Deploys use **immutable pinned image tags** (built by `mctf build`), enabling
deterministic deploys and `deploy --rollback`.

## Core workflow

| Command | Purpose |
| --- | --- |
| `mctf init` | Scaffold a new event repo |
| `mctf validate` | Validate the repo against the MCTF spec |
| `mctf generate` | (Re)generate derived files (CTFd manifest, instance.toml, CSV, index.json); `--check` for CI, `--json` for tooling |
| `mctf build` | Build container images for deployable challenges (`--push`, `--scan`) |
| `mctf deploy` / `mctf down` | Bring challenges up / down (`--mode`, `--dry-run`, `--rollback`, `--volumes`) |
| `mctf doctor` | Preflight: verify repo + host are deploy-ready |

## Monitoring

| Command | Purpose |
| --- | --- |
| `mctf monitoring up` | Render + start Prometheus + Grafana + cAdvisor (scrape config auto-generated from challenges). `--dry-run` writes nothing |
| `mctf monitoring down` | Stop the stack (`--volumes` drops data) |

## Per-team instancer

| Command | Purpose |
| --- | --- |
| `mctf instancer` | Run the per-team instancer daemon (local dev; deprecated) |
| `mctf instance spawn` | Spawn a per-team instance (remote when `instancer.url` is set, else local) |
| `mctf instance list` / `ps` / `flag` | Inspect live instances (remote when `instancer.url` is set, else local) |
| `mctf instance extend` | Bump an instance's expiry (`--by`, default 15m; remote or local) |
| `mctf instance destroy` / `reap` | Tear down one / all expired instances (remote or local) |

## Platform (CTFd)

| Command | Purpose |
| --- | --- |
| `mctf platform sync` | Sync challenges to the configured platform (re-uploads assets on content change) |
| `mctf platform unsync` | Remove a challenge from the platform |
| `mctf platform configure` | Sync event-level config (`ctf_name`, `ctf_description`, `start`, `end`) |

## Inspect

| Command | Purpose |
| --- | --- |
| `mctf list` | List challenges with archetype, difficulty, wave |
| `mctf status` | Runtime state for every challenge |
| `mctf info` | Derived facts for one challenge (`--all` for all) |
| `mctf event` | Event-wide summary |
| `mctf categories` | Active category catalog |
| `mctf logs` | Stream a deployed challenge's compose logs |
| `mctf solve` | Run a challenge's solver and verify the flag (`--against-instance`) |

## Challenge archetypes

- **static** — `need_deploy: false`. No container.
- **service** — `need_deploy: true`, fixed shared instance. Handled by `deploy`.
- **instanced** — `need_deploy: true` + `dynamic_instances: true`. Spawned per-team by the instancer, skipped by `deploy`.
