# Contributing

> **Internal alpha — MicroClub USTHB devs only.** This guide is the working agreement while the project is private. A public-facing version will replace it before the v1.0 release.

## Who this is for

Anyone with write access to `MicroClub-USTHB/mctf-cli`. If you're a challenge author looking for how to write a challenge, you want [`docs/spec/challenge-manifest.md`](docs/spec/challenge-manifest.md), not this file.

## Prerequisites

- **Go 1.23+**
- **GNU make** (any recent version)
- **git**
- **Python 3.10+** with `pyyaml` and `jsonschema` (only needed if you run the script-based sanity checks locally — CI runs them either way)
- A working **Docker** setup for any deploy/build commands (none required for `validate` or `generate`)

## Local setup

```sh
git clone git@github.com:MicroClub-USTHB/mctf-cli.git
cd mtcf-cli
make build
./mtcf -C examples/sample-event validate
go test ./...
```

If `make build` fails on a fresh checkout, run `go mod download` first.

## Project layout

| Path | What it's for |
|---|---|
| `cmd/` | Cobra command definitions. One file per command; flags live next to the command. |
| `internal/config/` | `mtcf.config.yml` loader + typed `Config` struct. |
| `internal/challenge/` | Per-challenge manifest loader, filesystem discovery, archetype derivation. |
| `internal/validate/` | Cross-cutting checks beyond schema (uniqueness, leak guard, healthcheck rules). |
| `internal/generate/` | Deterministic emitters for the four derived files. |
| `internal/jsonschema/` | Thin wrapper around `santhosh-tekuri/jsonschema/v5` with our error formatting. |
| `internal/schemas/` | **Embedded copy** of the JSON Schemas. Synced from `schema/` by `make schemas` — never edit directly. |
| `schema/` | The **canonical** JSON Schemas. Edit these when the spec changes. |
| `docs/spec/` | The spec itself. Move with the schemas. |
| `examples/sample-event/` | Reference event that exercises every code path. |
| `scripts/` | Python sanity-check validators (used by CI). |

## Five rules that aren't negotiable

1. **One PR per concern.** Validation rule changes, generator changes, CLI surface changes, and refactors go in separate PRs. Reviewers can't sign off on a 30-file diff that does five things.
2. **The spec moves with the code.** Any change that affects on-disk file shapes touches:
   - the relevant JSON Schema in `schema/`,
   - the relevant doc in `docs/spec/`,
   - the embedded copy (`make schemas`),
   - tests that exercise the new shape.
   PRs that update one without the others get bounced.
3. **Don't edit `internal/schemas/*.json` directly.** Edit `schema/*.json` and run `make schemas`. The Makefile detects drift and CI catches it either way.
4. **CI must be green.** Both the `go` and `schemas` jobs. `mtcf generate --check` on the sample event has to pass.
5. **No new commands without spec coverage.** Every new CLI verb lands with its semantics documented under `docs/spec/`. Internal helpers are exempt; user-facing commands are not.

## How to: add a validation rule

Two cases:

### Schema-level (covered by JSON Schema alone)

1. Edit `schema/challenge.schema.json` (or the relevant schema).
2. Run `make schemas` to sync the embedded copy.
3. Document the rule under `docs/spec/validation.md` with a stable rule ID. Schema-level rules use `S###` IDs.
4. Add or update an example under `examples/sample-event/` that exercises the rule.
5. Run `python3 scripts/validate-examples.py` to confirm sources still validate.

### Cross-cutting (needs Go code)

1. Add the check to `internal/validate/checks.go`. Use `r.Errorf` for hard-fails, `r.Warnf` for warnings, `r.Infof` for nudges. Keep the rule ID consistent with `docs/spec/validation.md`.
2. Document the rule in `docs/spec/validation.md` (rule ID + rationale).
3. Add a unit test in `internal/validate/` that constructs a minimal `[]*challenge.Challenge` triggering the rule.
4. Update `examples/sample-event/` if the rule is new enough that no existing fixture covers it.

## How to: add a CLI command

1. New file under `cmd/<verb>.go`. Cobra command + flags + a `RunE` that delegates to `internal/...`.
2. Register the command in its `init()` via `rootCmd.AddCommand(...)`.
3. Real logic goes in `internal/<area>/`. Keep `cmd/` thin — it's just argument parsing and human/JSON output.
4. If the command consumes the on-disk contract, document the data flow in `docs/spec/derived-files.md` (or a new doc).
5. Add an end-to-end test if practical: drive `cmd.Execute()` against a copy of `examples/sample-event/` in a tempdir.

## How to: add a derived-file format

1. Add the schema at `schema/<name>.schema.json`. JSON Schema 2020-12.
2. Add the generator at `internal/generate/<name>.go`. **Output must be byte-deterministic** — no timestamps, no map iteration, sort lists explicitly.
3. Wire it into `Plan()` in `internal/generate/generate.go`.
4. Document the format under `docs/spec/derived-files.md` with an example and the input → output mapping table.
5. Add a Python sanity check in `scripts/validate-generated.py` so CI catches schema drift in generated output.

## Tests

- `go test ./...` runs the unit tests. Add new tests next to the package they cover.
- Tests should not require network, Docker, or a running CTFd. If you need fixtures, put them under `internal/<pkg>/testdata/`.
- The sample event under `examples/sample-event/` is the integration fixture; CI uses it directly.

## Branching & PRs

- Branch off `main`. Name your branch `<area>/<short-description>` — examples: `validate/duplicate-port-check`, `generate/csv-utf8`, `cmd/init-skeleton`.
- Open the PR against `main`. Request review from at least one other member.
- Squash-merge by default unless the branch contains intentionally separate logical commits.
- Never push directly to `main` once branch protection is enabled.

## Commit messages

Conventional-commits style, one-line subject + optional body:

```
feat(generate): emit hints in CTFd manifest output

Pulls challenge.hints[] into the auto-generated CTFd manifest under the
"hints" key. Manifests without hints stay omitted from the output.
```

Common scopes: `validate`, `generate`, `config`, `challenge`, `cmd`, `schema`, `docs`, `ci`, `chore`.

## Code style

- `gofmt` + `go vet ./...` clean. (We'll add `golangci-lint` once it stops bouncing on Go 1.23 toolchain.)
- Errors wrap with `fmt.Errorf("...: %w", err)`.
- Comments explain *why*, not *what*. If you have to explain *what* the code does, the code probably needs to change instead.
- No trailing summary blocks at the top of files (no `// Package X does Y, then Z, then W.` paragraphs). Keep package docs to one short paragraph.
- Public types/functions get doc comments; package-private ones only when behavior isn't obvious.

## CI gates

GitHub Actions runs two jobs on every push and PR:

- **`go`** — `go vet`, `go test -race`, `make build`, `mtcf validate`, `mtcf generate --check` against the sample event.
- **`schemas`** — Python sanity checks: source manifests vs `challenge.schema.json`, generated files vs their schemas.

Both must be green. If you can't get them green locally, push the WIP branch and let CI tell you why — that's faster than guessing.

## Reviewing PRs

- Read the spec/doc changes first. The doc tells you what the code is supposed to do.
- Pull the branch, run `make build && ./mtcf -C examples/sample-event validate && ./mtcf -C examples/sample-event generate --check`. If those don't pass, the PR isn't ready.
- For new validation rules: think of three challenges that *shouldn't* trigger the rule. The rule should be specific enough to catch real bugs without false positives.
- For new generators: think about what happens on a re-run. Output must be idempotent.

## Releases

Pre-v0.1, releases are git tags only — no published artifacts. Tag format `v0.0.<N>`. Use `make build` to produce a binary locally.

Once v0.1 ships, `goreleaser` takes over for cross-compiled binaries.

## Questions

If something here is wrong, unclear, or out of date — open a PR fixing it. This file is part of the spec.
