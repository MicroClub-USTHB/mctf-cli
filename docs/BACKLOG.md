# MCTF Backlog

Open work organized by size. Each line is intentionally GitHub-issue-shaped: title + 1–2 line scope. Pick from the **Suggested wave-1** list at the bottom for the highest-leverage starting set.

## Big rocks (multi-day each)

- **K8s deploy mode** — `mctf deploy --mode kubernetes`. Generate K8s Deployment / Service / Ingress manifests from per-challenge compose, `kubectl apply -f`. Honor `cfg.Deploy.Kubernetes.{Kubeconfig, Namespace, IngressClass}`.
- **Plugin system** — subprocess + JSON-RPC over stdio (Terraform-style), language-agnostic, crash-isolated. **Foundation + `before_deploy`/`after_deploy` shipped** (`internal/plugin`, `docs/spec/plugins.md`). *Remaining:* `before_build`/`after_build`, `on_instance_spawn` (two sites — `cmd/instance.go` + `internal/instancer/server.go`), `on_health_failure` (needs a health-monitoring loop — largest), dispatch on `down`/kubernetes paths, and a blocking/`required` mode (a plugin that can veto).
- ~~**Prometheus / Grafana wiring** — `mctf monitoring up` brings up prom + grafana + cAdvisor with prebuilt dashboards. Generate scrape config from challenge labels.~~ **Shipped:** `mctf monitoring up` / `down` render and run the stack under `monitoring/stack/` (see `docs/spec/monitoring.md`).
- **Terraform wrapper** — `mctf infra plan|apply|destroy` for VPS, clusters, networking, DNS. Templates for DigitalOcean, Hetzner, DOKS, EKS, GKE.

## Medium features (1–2 days each)

- **rCTF + generic-webhook platforms** — second + third platform targets behind the existing platform interface, ideally via plugin once the system lands.
- **Per-team auth on the instancer** — currently bearer tokens have full power; add team-scoped tokens (`POST /spawn` only allows the request when the token's team_id matches the body).
- **Native instancer: docker-network isolation** — currently spawns share the event network; isolate each per-team instance to its own bridge.
- **`mctf platform configure` extensions** — sync `start` / `end` timestamps (needs schema addition `event.start_at` / `event.end_at`), `ctf_description`.
- **`mctf release`** — tag git, build via goreleaser, draft a release notes file from conventional-commits since last tag.

## Small wins (~half-day each)

*(All wave 1 small wins have been shipped!)*

## Polish & docs

- **Update `docs/spec/validation.md`** to reflect every rule that shipped (D001–D003, SEC002–004, W006–W010, A007, etc.).
- **Update `docs/spec/CHANGELOG.md`** with the v0 → v0.1 delta.
- ~~**Architecture doc** (`docs/spec/architecture.md`) — how the packages fit together.~~ **Shipped** — pipeline diagram, package map, archetypes, deploy modes, and the two instancer implementations.
- ~~**Per-command man pages** — `cobra` can generate them; ship under `docs/man/`.~~ **Shipped** via `mctf gen-man` / `make man`; generated at release time and bundled in the archives + `.deb`/`.rpm` (not committed, to avoid build-date drift).
- **Update CONTRIBUTING.md** — new packages (`instancer`, `ingress`, `notify`, `solve`, `status`, `info`, `platform/ctfd`).
- **Refresh `examples/sample-event/`** — add one challenge per core category (currently 5 challenges, 10 categories). Helps new contributors.

## Distribution

- ~~**goreleaser** — cross-platform binaries (darwin-arm64 / amd64, linux-amd64 / arm64, windows-amd64).~~ **Shipped** (`.goreleaser.yaml` + release workflow; see `docs/RELEASING.md`).
- ~~**Homebrew tap** — formula in `MicroClub-USTHB/homebrew-tap`.~~ **Shipped** (Homebrew cask; needs one-time tap repo + `HOMEBREW_TAP_TOKEN`).
- ~~**`.deb` / `.rpm`** — via goreleaser.~~ **Shipped** (nfpms).
- **Scoop bucket** — for Windows (needs a bucket repo; config to follow).
- ~~**Docker image of `mctf` itself** — `ghcr.io/microclub-usthb/mctf:<version>` for CI use.~~ **Shipped** (goreleaser `dockers` → GHCR; amd64, multi-arch to follow).

## CI / quality

- **golangci-lint** in CI (`go vet` is currently the only static check).
- ~~**Coverage reporting** — `go test -coverprofile` upload to Codecov / similar.~~ **Shipped:** CI runs atomic coverprofile, reports step summary, uploads to Codecov + artifacts, with local `make test-coverage`.
- **Fuzz tests** for the manifest loader and the compose port parser.
- **End-to-end integration test** — sample-event fixture: validate → generate → build → scan → deploy → solve → down. Real docker daemon, gated on an `e2e` build tag.

## Security

- **SEC005: capability allowlist enforcement** — honor `deploy.security_opts.allow_caps` so SEC004 only fires on caps not in the allowlist.
- **SEC006: image vulnerability scan** — opt-in `mctf build --scan-vulns` via trivy or similar.
- **Secret hygiene** — ~~warn under `mctf doctor` on `MCTF_*_TOKEN` patterns.~~ **Shipped** as `doctor` check `D043` (scans the tracked working tree for `MCTF_*_{TOKEN,KEY,SECRET,WEBHOOK}` values, filtering placeholders/env-refs; never prints the value). *Follow-up:* deep git-**history** scan (left to a dedicated tool like gitleaks).

---

## Suggested wave-1 issues

Highest leverage, smallest blast radius — pick from these to start:

1. **goreleaser + Homebrew tap** — unlock distribution.
2. **K8s deploy mode** — biggest deployment-flexibility win.
3. **`docs/spec/` refresh** — backfill what shipped this session.

## Conventions for new tasks

- One PR per concern (see `CONTRIBUTING.md`).
- Spec moves with code: any change to on-disk shape touches `schema/`, `docs/spec/`, `make schemas`, and tests in the same PR.
- Validation rules use the established ID prefix scheme: `S###` schema, `A###` archetype consistency, `U###` event-wide uniqueness, `D###` deployment, `SEC###` security, `SOL###` solution, `W###` warning, `I###` info.
- New commands ship with their semantics documented under `docs/spec/`.
