# Security Policy

## Supported versions

MCTF is pre-1.0. Security fixes land on the latest release and `develop`.

## Reporting a vulnerability

**Please report security issues privately — do not open a public issue.**

Use GitHub's private vulnerability reporting for this repository
(**Security → Report a vulnerability**), or contact the
[MicroClub USTHB](https://github.com/MicroClub-USTHB) maintainers directly.

Include:

- affected version or commit SHA,
- reproduction steps or a proof of concept,
- the impact you observed.

We aim to acknowledge reports within 72 hours and to keep you updated through
remediation. Please give us a reasonable window to fix the issue before any
public disclosure.

## Scope / areas of particular interest

MCTF runs challenge containers and shells out to `docker`, `ssh`, and
`kubectl`, and executes configured plugin subprocesses. Reports touching these
areas are especially valuable:

- **Plugin execution** — the subprocess/JSON-RPC runtime (`internal/plugin`).
- **Deploy / compose translation** — `internal/deploy` (incl. image-tag pinning).
- **Instancer isolation** — per-team instance separation and token scoping.
- **Secret handling** — tokens/keys in config, env, and generated files.

> Note: MCTF deliberately executes config-declared commands (plugins) and
> compose files. Those are trusted inputs, equivalent to running the repo's
> code — that is by design, not a vulnerability.
