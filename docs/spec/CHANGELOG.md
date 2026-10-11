# MCTF Spec Changelog

## v0.1.0 — develop (unreleased)

All features below are merged into `develop` and pass CI. Not yet released to `main`.

### Features

- **`feat(instancer)`: per-team dynamic flags** — the instancer now mints a unique flag per spawn when `flags[].dynamic: true` is set on a challenge. `SpawnOptions.FlagOverride` lets callers pass a pre-generated value; `Instance.Flag` persists it in the store. Schema extended with `flags[].dynamic` and `flags[].generator` (reserved). Validation rules added: `A010` (dynamic flag on non-instanced challenge) and `A011` (static flag + `dynamic: true` conflict).

- **`feat(generate)`: Prometheus scrape config generator** — `mctf generate` now emits `monitoring/prometheus.yml` with one scrape job per instanced challenge (sorted, deterministic). Only emitted when the event has at least one instanced challenge or `monitoring:` is explicitly set in `mctf.config.yml`. Previously the field was reserved and unconditionally written; it is now fully conditional via `shouldEmitPrometheus()`.

- **`feat(monitoring)`: `mctf monitoring up` / `down`** — brings up the observability stack (Prometheus + Grafana + cAdvisor) via docker compose. `up` renders `monitoring/stack/` from the current config and challenge set — a full Prometheus config (self + cAdvisor + one job per instanced challenge, targeting `host.docker.internal` so targets are reachable from inside the container), a docker-compose with pinned images, and Grafana auto-provisioned with the Prometheus datasource and a container-overview dashboard. `--dry-run` prints the plan and compose command without touching disk; `down --volumes` also drops the data volumes. Ports, Grafana password, and retention are tunable under the `monitoring:` block in `mctf.config.yml`. See `docs/spec/monitoring.md`.

- **`feat(doctor)`: advanced host checks D040–D042** — `mctf doctor` gained three new checks: DNS resolution (`D040`), free disk space (`D041`, warns < 15 GB / fails < 5 GB), and Docker storage driver (`D042`, warns on `vfs`).

- **`feat(validate)`: SEC004 capability allowlist** — `mctf validate` now enforces `SEC004`: `cap_add` lists containing privileged capabilities (`SYS_ADMIN`, `NET_ADMIN`, `SYS_PTRACE`, `SYS_MODULE`) are hard-failed unless `deploy.security_opts.allow_caps` explicitly allowlists them.

- **`feat(solve)`: `--against-instance` flag** — `mctf solve` can now be pointed at a live per-team instance. The solver script receives the instance's host-port mapping and the per-team flag is accepted in addition to the manifest's static flag.

- **`feat(platform)`: CTFd asset content-hash re-upload** — `mctf platform sync` now re-uploads CTFd assets when their content hash changes, not just when the filename changes. Prevents stale handout files on CTFd.

- **`feat(deploy)`: GitOps state sync + rollback** — `mctf deploy` gained a rollback system. On failure, the previous compose state is restored from git history. Registry push is now gated on `registry.url` presence to avoid crashes in registry-less setups.

- **`feat(generate)`: `--json` output** — `mctf generate` and `mctf generate --check` support `--json` to emit a machine-parseable drift/apply report. Used by CI tooling.

- **`fix(kubernetes)`: deterministic NodePort mapping** — TCP challenges now expose `nodePort = node_port_base + (deploy.port − category range min)` (new `deploy.kubernetes.node_port_base`, default `30000`) instead of reusing `deploy.port`, which fell outside the stock `30000–32767` NodePort range. New rules `K001`/`K002`/`K003` (NodePort mapping, DNS-1123 name ≤ 63 chars, NodePort collisions).
- **`fix(deploy)`: kubernetes mode shares the deploy lock** — `mctf deploy --mode kubernetes` takes `.mctf/deploy.lock` (now created atomically with `O_EXCL`), rejects `--rollback`, validates `--mode` (`docker|ssh|kubernetes`, no silent ignore of docker/ssh overrides) and no longer mutates the loaded config.
- **`feat(kubernetes)`: pod security hardening** — Deployments now enforce `automountServiceAccountToken: false`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]` (with opt-in capabilities via `deploy.security_opts.allow_caps`), and `seccompProfile: RuntimeDefault`.
- **`feat(kubernetes)`: TCP probes & startupProbe** — Added support for `type: tcp` socket probes, separate probe objects, and automatic `startupProbe` generation to eliminate CrashLoopBackOffs during slow startup.
- **`feat(kubernetes)`: auto-namespace & rollout status wait** — `KubeRunner` now automatically creates missing namespaces idempotently and waits for deployment rollouts (`kubectl rollout status`) before reporting deployment success.

### Fixes & Refactors

- `fix(generate)`: `PrometheusConfig` no longer mutates the caller's challenge slice (defensive copy before sort).
- `fix(generate)`: Asset paths normalised with `filepath.ToSlash` — fixes Windows path separator in generated YAML.
- `refactor(generate)`: `Apply()` signature cleaned — removed the always-empty `removed []string` return value; stale-file cleanup is the explicit responsibility of `CleanStale()`.
- `fix(deploy)`: Registry/translate path gated on `registry.url` presence.
- `fix(generate)`: `shouldEmitPrometheus()` guard added — `monitoring/prometheus.yml` is no longer emitted for static-only events.

### Tests

- `test(generate)`: First unit tests for `internal/generate` — `TestPrometheusConfig` (6 table-driven cases, golden YAML) and `TestShouldEmitPrometheus` (5 cases). Package went from `[no test files]` to covered.
- `test(instancer)`: Tests for per-team dynamic flag minting in `lifecycle_test.go`.
- `test(init)`: Prompt injection tests for `promptString` and `promptChoice`.
- `test(deploy)`: `Plan` defaults verified for docker-compose path resolution.

### Docs

- `docs/spec/derived-files.md`: Added §6 documenting `monitoring/prometheus.yml` shape and generation rules.
- `docs/spec/repository-layout.md`: Added `monitoring/` to the event repo tree.
- `docs/spec/validation.md`: Added `D040`–`D042` (doctor), `SEC004` capability allowlist, `A010`/`A011` (dynamic flag rules).

---

## v1 — Phase 0 (initial spec)

First locked version of the on-disk contract. Status: **implemented** (see v0.1 for what shipped on top of this baseline).

### Scope

- **Author manifest** (`challenge/challenge.yml`) shape inspired by [MCTF 5.0](https://github.com/MicroClub-USTHB/MCTF-5.0-Challenges); existing MCTF 5.0 challenges port over with zero changes.
- **Auto-generated derived files** at the challenge root (`challenge.yml` for CTFd, `instance.toml` for the MCTF instancer).
- **Event config** (`mctf.config.yml`) — new in MCTF.
- **Profiles** — `simple`, `instanced`, `pro` (curated presets).
- **Plugin lifecycle hooks** declared but not yet wired (delivered in v0.2 of the CLI).

### Categories

- 10 core categories locked: `web`, `pwn`, `crypto`, `reverse`, `forensics`, `osint`, `misc`, `ai`, `hardware`, `jailbreak`.
- 12 extended categories opt-in: `stego`, `mobile`, `blockchain`, `network`, `ppc`, `realistic`, `cloud`, `iot`, `sysadmin`, `defensive`, `welcome`, `quiz`.
- Custom categories declarable per-event in `mctf.config.yml`.

### Improvements over MCTF 5.0 conventions baked into v1

- `difficulty` is a locked enum (`easy|medium|hard|extreme`) instead of a free string.
- Per-category port-range validation **enforced** (was designed but disabled in MCTF 5.0).
- Static-flag image-leak guard: scans `challenge/src/**` for the literal flag string.
- Event-wide subdomain and flag uniqueness checks.
- `schema:` version field on the manifest (v1).
- Description length cap (4000 chars, CTFd column limit).
- `hints[]` structured form on the manifest (in addition to `solution/hints.md`).
- `solution/solve.{sh,py}` optional smoke-test runner integration.

### Reserved (not yet enforced in v1)

- Multi-flag challenges (deferred to v0.2).
- Multi-author `author: [...]` (deferred to v0.2).
- Dynamic / per-team flag plugin (`flag.type: dynamic`) — schema slot reserved, runtime in v0.2.

### Sources

- Reference repo: `/Users/mac/MCTF-5.0-Challenges` (MCTF 5.0 by MicroClub USTHB).
- Validator inspiration: `MCTF-5.0-Challenges/.github/scripts/validation/src/`.
- Discrepancies and improvements catalogued in [validation.md](./validation.md).
