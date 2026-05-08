# Validation Rules (`mtcf validate`)

Every rule MTCF enforces, with rationale and severity. Hard-fails block the build/deploy; warnings inform. Profile column shows when a rule is escalated under stricter profiles.

`mtcf validate` is the entrypoint. It runs at three points:

1. **CI** — on every push and PR via `.github/workflows/validate.yml`.
2. **Local pre-commit** — when authors run `mtcf validate` themselves.
3. **Implicit on deploy** — `mtcf deploy` runs validation first; failures abort.

## Hard failures (exit code 1)

### Schema

| ID | Rule | Severity by profile |
|---|---|---|
| `S001` | Missing `challenge/challenge.yml` | always hard |
| `S002` | Manifest fails JSON-schema validation (`schema/challenge.schema.json`) | always hard |
| `S003` | `challenge.category` does not match parent directory name | always hard |
| `S004` | `challenge.category` not in core 10 + extended (enabled) + custom categories | always hard |
| `S005` | `challenge.difficulty` not in `{easy, medium, hard, extreme}` | always hard |
| `S006` | `challenge.flags` empty or absent | always hard |
| `S007` | `challenge.flags` length > 1 (multi-flag deferred to v0.2) | always hard |
| `S008` | Flag string doesn't match `<event.flag_prefix>{...}` | always hard |
| `S009` | `challenge.description` > 4000 chars | always hard |
| `S010` | `challenge.wave` outside `1..event.waves_number` | always hard |
| `S011` | Challenge dir name uses disallowed chars (anything outside `[a-zA-Z0-9_-]`) | always hard |

### Archetype consistency

| ID | Rule | Severity by profile |
|---|---|---|
| `A001` | `need_deploy: true` but no `deploy:` block | always hard |
| `A002` | `dynamic_instances: true` and `port` is not null | always hard |
| `A003` | `dynamic_instances: true` and `internal_port` is null | always hard |
| `A004` | `dynamic_instances: false` and `port` is null | always hard |
| `A005` | `dynamic_instances: false` and `internal_port` is not null | always hard |
| `A006` | Deployable category (`web`/`pwn`/`jailbreak`) without `Dockerfile` AND `docker-compose.yml` in `challenge/` | always hard |
| `A007` | `subdomain` missing or empty (required for all deployable challenges; used for HTTP routing and TLS-SNI on TCP) | always hard |
| `A008` | `subdomain` violates DNS label regex `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$` | always hard |

### Event-wide uniqueness

| ID | Rule | Severity by profile |
|---|---|---|
| `U001` | Two challenges declare the same `deploy.port` | always hard |
| `U002` | Two challenges declare the same `deploy.subdomain` (when both have one) | always hard |
| `U003` | Two challenges share the same flag value (case-aware) | always hard |
| `U004` | Two challenges have the same `<category>/<slug>` path | always hard *(filesystem prevents this anyway)* |

### Security

| ID | Rule | Severity by profile |
|---|---|---|
| `SEC001` | Static flag value found inside any file under `challenge/src/` (image-leak guard) — exempt: `flag.txt`, files explicitly env-loaded | always hard |
| `SEC002` | `privileged: true` in `docker-compose.yml` | warn / warn / **hard** |
| `SEC003` | `pwn` or `jailbreak` Dockerfile lacks a non-root `USER` directive after the last `FROM` | warn / warn / **hard** |
| `SEC004` | `cap_add` lists privileged caps (`SYS_ADMIN`, `NET_ADMIN`, `SYS_PTRACE`, `SYS_MODULE`) without `deploy.security_opts.allow_caps` allowlisting them | warn / warn / **hard** |
| `SEC005` | Unique flag value not present at all (e.g. challenge ships unflagged) | always hard |

### Deployment

| ID | Rule | Severity by profile |
|---|---|---|
| `D001` | `compose.ports` host port doesn't match `deploy.port` for service archetype | always hard |
| `D002` | `compose.ports` container port doesn't match `deploy.internal_port` for instanced archetype | always hard |
| `D003` | `compose.ports` for instanced doesn't use `${PORT_<n>:-<default>}` env-var convention | always hard |
| `D004` | `instanced` archetype without a healthcheck (neither `deploy.healthcheck` nor Dockerfile `HEALTHCHECK`) | warn / **hard** / **hard** |
| `D005` | `instanced` archetype without `deploy.resources.{cpu, memory}` | warn / warn / **hard** |
| `D006` | Service-archetype `deploy.port` outside the category's port range | warn / **hard** / **hard** |
| `D007` | `assets/files` path resolves outside the challenge directory | always hard |
| `D008` | Any file under `assets/` exceeds 100 MB | warn / warn / **hard** |
| `D009` | Total `assets/` directory size exceeds 500 MB | warn / warn / **hard** |

### Solution

| ID | Rule | Severity by profile |
|---|---|---|
| `SOL001` | Missing `solution/writeup.md` | always hard |
| `SOL002` | `writeup.md` is empty (zero non-whitespace bytes) | always hard |

## Warnings (exit code 0; logged)

| ID | Rule |
|---|---|
| `W001` | `pwn` challenge without `deploy.resources.pids` |
| `W002` | `web` archetype `service` but description hints at mutable state — suggests `instanced` |
| `W003` | Dropped (use W008 for healthchecks) |
| `W004` | Static archetype with empty `assets/` |
| `W005` | Slug-vs-name case-only mismatch (e.g. dir `cub3` / name `CUB3`) |
| `W006` | Missing `solution/hints.md` and `challenge.hints[]` empty |
| `W007` | `challenge.wave` > 5 (likely typo, since events rarely run that many waves) |
| `W008` | Dockerfile lacks `HEALTHCHECK` and manifest has no `deploy.healthcheck` |
| `W009` | Crypto service-archetype challenge with no rate-limit hint in description |
| `W010` | `challenge.description` exceeds 1500 chars (UX warning; hard-fail at 4000) |
| `W011` | Asset path missing in `assets/` but referenced from `description` (broken handout link) |
| `W012` | Schema version field omitted from manifest (defaults to 1) |

## Info-level notes (logged, never block)

| ID | Rule |
|---|---|
| `I001` | Category-typical tag missing (e.g. `web` challenge with no sub-tag like `sqli`/`xss`/`ssrf`) |
| `I002` | Manifest uses deprecated field (none in v1; reserved for future migrations) |

## Disabling rules

A challenge can opt out of specific warnings via a `# mtcf-disable: W001 W005` comment at the top of its `challenge.yml`. **Hard-fails cannot be disabled** — fix the issue or open a PR to change the rule.

## CI integration

The `validate.yml` workflow reports validation results as a job summary on the PR. On hard-fails, the summary lists each violation with rule ID, file, and line number for easy navigation.

```
❌ challenges/web/leaky-app/challenge/challenge.yml:14
   SEC001: static flag value "mctf{...}" found in challenge/src/app.py
   This bakes the flag into the public image. Move it to flag.txt or load from env.
```

## Why these rules

Many rules are direct improvements over what MCTF 5.0's TypeScript validator catches today (see [CHANGELOG.md](./CHANGELOG.md) for the full list). The most consequential additions:

- **`SEC001` (static-flag image-leak guard)** — the most common bug in CTF prep is shipping a `src/app.py` that hardcodes the flag, then publishing the image to a public registry. MCTF 5.0 has no check for this; MTCF makes it a hard-fail.
- **`D004` (instanced healthcheck mandatory)** — without a healthcheck, the instancer can't tell when a spawn is ready. Players hit the URL too early, see HTTP 502, blame the organizers. Hard-failing at validation time avoids the live-event symptom.
- **`D006` (port range enforcement)** — MCTF 5.0 designed per-category port ranges (10000–10999 for web, 11000–11999 for pwn, etc.) but disabled the validation. MTCF re-enables it.
- **`U001`/`U002`/`U003` (uniqueness)** — duplicate ports cause silent deploy collisions; duplicate flags cause cross-solving. MCTF 5.0 doesn't check; MTCF does.
- **`S005` (difficulty enum)** — MCTF 5.0 accepts any non-empty string for difficulty. MTCF locks it to four values so the CTFd UI is consistent.
