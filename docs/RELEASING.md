# Releasing

Releases are cut by [GoReleaser](https://goreleaser.com) on a `vX.Y.Z` tag.

## Cut a release

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The `Release` workflow (`.github/workflows/release.yml`) then builds and
publishes, for linux/macOS/windows on amd64 + arm64:

- archives (`.tar.gz`, `.zip` on Windows) + `checksums.txt`
- Linux packages (`.deb`, `.rpm`)
- a Homebrew cask pushed to `MicroClub-USTHB/homebrew-tap`
- a Scoop manifest pushed to `MicroClub-USTHB/scoop-bucket`
- a Docker image at `ghcr.io/microclub-usthb/mctf:<version>` and `:latest`
- man pages (one per command), bundled in the archives (`man/`) and installed to `/usr/share/man/man1` by the `.deb`/`.rpm` packages

Man pages are generated at build time (goreleaser `before` hook, same as
`make man`), so there are no generated files committed to the repo.

## Docker image

`ghcr.io/microclub-usthb/mctf` is published on each release (amd64). It bundles
`git` + `ssh`; mount a Docker socket for `build`/`deploy`:

```bash
docker run --rm -v "$PWD:/event" -w /event ghcr.io/microclub-usthb/mctf:latest validate
```

GHCR auth uses the workflow's `GITHUB_TOKEN` (needs `packages: write`, already
set) — no extra secret required.

Version/commit/date are injected into `cmd.version` / `cmd.commit` /
`cmd.date` via ldflags, so `mctf version` reports the tagged build.

## One-time setup for Homebrew

1. Create the tap repo **`MicroClub-USTHB/homebrew-tap`**.
2. Add a repo secret **`HOMEBREW_TAP_TOKEN`** (a PAT with `contents:write` on
   the tap) to `mctf-cli`.

Until that secret exists the cask push is skipped automatically — binaries and
packages still publish. Once set up:

```bash
brew install MicroClub-USTHB/tap/mctf
```

## One-time setup for Scoop (Windows)

1. Create the bucket repo **`MicroClub-USTHB/scoop-bucket`**.
2. Add a repo secret **`SCOOP_BUCKET_TOKEN`** (a PAT with `contents:write` on
   the bucket) to `mctf-cli`.

Until that secret exists the manifest push is skipped automatically — binaries and
packages still publish. Once set up:

```powershell
scoop bucket add microclub https://github.com/MicroClub-USTHB/scoop-bucket
scoop install mctf
```

## Dry run

```bash
goreleaser check                              # validate .goreleaser.yaml
goreleaser release --snapshot --clean --skip=publish   # full local build, no publish
```
