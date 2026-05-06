# MTCF Spec Changelog

## v1 — Phase 0 (initial spec)

First locked version of the on-disk contract. Status: **draft**, accepting feedback before freeze.

### Scope

- **Author manifest** (`challenge/challenge.yml`) shape inspired by [MCTF 5.0](https://github.com/MicroClub-USTHB/MCTF-5.0-Challenges); existing MCTF 5.0 challenges port over with zero changes.
- **Auto-generated derived files** at the challenge root (`challenge.yml` for CTFd, `instance.toml` for the MTCF instancer).
- **Event config** (`mtcf.config.yml`) — new in MTCF.
- **Profiles** — `simple`, `instanced`, `pro` (curated presets).
- **Plugin lifecycle hooks** declared but not yet wired (delivered in v0.2 of the CLI).

### Categories

- 10 core categories locked: `web`, `pwn`, `crypto`, `reverse`, `forensics`, `osint`, `misc`, `ai`, `hardware`, `jailbreak`.
- 12 extended categories opt-in: `stego`, `mobile`, `blockchain`, `network`, `ppc`, `realistic`, `cloud`, `iot`, `sysadmin`, `defensive`, `welcome`, `quiz`.
- Custom categories declarable per-event in `mtcf.config.yml`.

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
