# MCTF Backlog

Open work organized by size. Each line is intentionally GitHub-issue-shaped: title + 1–2 line scope. Pick from the **Suggested wave-1** list at the bottom for the highest-leverage starting set.

## Big rocks (multi-day each)

- **K8s deploy mode** — `mctf deploy --mode kubernetes`. Generate K8s Deployment / Service / Ingress manifests from per-challenge compose, `kubectl apply -f`. Honor `cfg.Deploy.Kubernetes.{Kubeconfig, Namespace, IngressClass}`.
- **SSH deploy mode** — `mctf deploy --mode ssh`. scp the compose tree to the configured host, run `docker compose up -d` over ssh. Single-VPS workflow.
- **Plugin system** — subprocess + JSON-RPC over stdio (Terraform-style). Lifecycle hooks: `before_build`, `after_build`, `before_deploy`, `after_deploy`, `on_instance_spawn`, `on_health_failure`. Language-agnostic, crash-isolated.
- **Per-team dynamic flags** — instancer mints per-team flags at spawn time (currently every team gets the manifest's static flag). Schema additions: `flags[].dynamic: true`, optional `flags[].generator`.
- **Prometheus / Grafana wiring** — `mctf monitoring up` brings up prom + grafana + cAdvisor with prebuilt dashboards. Generate scrape config from challenge labels.
- **Terraform wrapper** — `mctf infra plan|apply|destroy` for VPS, clusters, networking, DNS. Templates for DigitalOcean, Hetzner, DOKS, EKS, GKE.

## Medium features (1–2 days each)

- **Rollback support** — `.mctf/state.json` snapshot before each deploy; `mctf deploy --rollback` reverts. Prerequisite: compose must use `image: <reg>/<name>:<sha>` instead of `build:` so a previous SHA can be redeployed.
- **Image-tag pinning in deploy** — `mctf deploy` stamps `IMAGE_TAG` env so compose pulls the SHA from the last successful `mctf build` (the rollback prerequisite).
- **rCTF + generic-webhook platforms** — second + third platform targets behind the existing platform interface, ideally via plugin once the system lands.
- **Per-team auth on the instancer** — currently bearer tokens have full power; add team-scoped tokens (`POST /spawn` only allows the request when the token's team_id matches the body).
- **CTFd file diff by content hash** — currently match by basename; switch to sha256 so updated handouts get re-uploaded.
- **`mctf solve --against-instance <id>`** — run a solver against a per-team spawned instance (current `solve` only targets static / pre-deployed services).
- **Native instancer: docker-network isolation** — currently spawns share the event network; isolate each per-team instance to its own bridge.
- **Better `mctf doctor` checks** — D040 DNS resolves for `<sub>.<domain>`, D041 free disk space, D042 docker daemon storage driver.
- **`mctf platform configure` extensions** — sync `start` / `end` timestamps (needs schema addition `event.start_at` / `event.end_at`), `ctf_description`.
- **`mctf release`** — tag git, build via goreleaser, draft a release notes file from conventional-commits since last tag.

## Small wins (~half-day each)

- **W003** — `web` archetype `service` but description hints at mutable state → suggest `instanced` (regex-heuristic).
- **W009** — `crypto` service-archetype with no rate-limit hint.
- **W011** — asset filename referenced in description but missing from `assets/` (broken handout link).
- **W012** — schema field omitted from manifest (warn; default-filled to 1).
- **I001** — category-typical sub-tag missing (e.g. `web` with no `sqli` / `xss` / `ssrf`).
- **`mctf generate --json`** — list of written / skipped / drift files for CI.
- **`mctf instance ps`** — live container state from docker (uptime, healthcheck status) for one instance ID.
- **`mctf instance flag <id>`** — print the per-team flag (read-only convenience).
- **`mctf platform unsync`** — remove a challenge from CTFd by category / slug.
- **Drop W004** when `assets/.gitkeep` is the only file (currently fires on directories that contain only the placeholder).
- **Fuzzy slug match** — `mctf info crypto/rs` suggests `crypto/rsa-baby` instead of "no challenge".
- **Test coverage for `cmd/init` prompts** — `promptIn` is already injectable; just write the test.

## Polish & docs

- **Update `docs/spec/validation.md`** to reflect every rule that shipped (D001–D003, SEC002–004, W006–W010, A007, etc.).
- **Update `docs/spec/CHANGELOG.md`** with the v0 → v0.1 delta.
- **Architecture doc** (`docs/spec/architecture.md`) — how the packages fit together (`config` + `challenge` + `validate` → `generate`, `build` → `deploy` + `ingress`, `instancer` state store + lifecycle + server + reaper).
- **Per-command man pages** — `cobra` can generate them; ship under `docs/man/`.
- **Update CONTRIBUTING.md** — new packages (`instancer`, `ingress`, `notify`, `solve`, `status`, `info`, `platform/ctfd`).
- **Refresh `examples/sample-event/`** — add one challenge per core category (currently 5 challenges, 10 categories). Helps new contributors.

## Distribution

- **goreleaser** — cross-platform binaries (darwin-arm64 / amd64, linux-amd64 / arm64, windows-amd64).
- **Homebrew tap** — formula in `MicroClub-USTHB/homebrew-tap`.
- **Scoop bucket** — for Windows.
- **`.deb` / `.rpm`** — via goreleaser.
- **Docker image of `mctf` itself** — `ghcr.io/microclub-usthb/mctf:<version>` for CI use.

## CI / quality

- **golangci-lint** in CI (`go vet` is currently the only static check).
- **Coverage reporting** — `go test -coverprofile` upload to Codecov / similar.
- **Fuzz tests** for the manifest loader and the compose port parser.
- **End-to-end integration test** — sample-event fixture: validate → generate → build → scan → deploy → solve → down. Real docker daemon, gated on an `e2e` build tag.

## Security

- **SEC005: capability allowlist enforcement** — honor `deploy.security_opts.allow_caps` so SEC004 only fires on caps not in the allowlist.
- **SEC006: image vulnerability scan** — opt-in `mctf build --scan-vulns` via trivy or similar.
- **Secret hygiene** — git-history scan for `MCTF_*_TOKEN` patterns; warn under `mctf doctor`.

---

## Suggested wave-1 issues

Highest leverage, smallest blast radius — pick from these to start:

1. **golangci-lint in CI** — easy quality lift across the whole tree.
2. **goreleaser + Homebrew tap** — unlock distribution.
3. **K8s deploy mode** — biggest deployment-flexibility win.
4. **Image-tag pinning in deploy + rollback** — operational safety.
5. **W003 / W011 / I001 batch** — cheap validate improvements.
6. **`docs/spec/` refresh** — backfill what shipped this session.

## Conventions for new tasks

- One PR per concern (see `CONTRIBUTING.md`).
- Spec moves with code: any change to on-disk shape touches `schema/`, `docs/spec/`, `make schemas`, and tests in the same PR.
- Validation rules use the established ID prefix scheme: `S###` schema, `A###` archetype consistency, `U###` event-wide uniqueness, `D###` deployment, `SEC###` security, `SOL###` solution, `W###` warning, `I###` info.
- New commands ship with their semantics documented under `docs/spec/`.
