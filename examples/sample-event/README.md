# Sample MTCF Event

A complete reference event covering one challenge per archetype. Used both as a documentation example and as the e2e fixture for the future MTCF CLI.

## What's here

| Path | Category | Archetype | Highlights |
|---|---|---|---|
| [`challenges/misc/welcome/`](./challenges/misc/welcome/) | misc | service | Fixed-port HTTP service. Smallest working compose. |
| [`challenges/web/cub3-clone/`](./challenges/web/cub3-clone/) | web | instanced | HTTP, per-team, dynamic-port via `${PORT_8000}` convention. |
| [`challenges/pwn/baby-overflow/`](./challenges/pwn/baby-overflow/) | pwn | instanced (TCP) | Raw TCP; jail-style; `pids` limit. |
| [`challenges/crypto/rsa-baby/`](./challenges/crypto/rsa-baby/) | crypto | static | No Docker; assets only. |
| [`challenges/forensics/pcap-101/`](./challenges/forensics/pcap-101/) | forensics | static | pcap handout. |

Every challenge uses the four-folder layout: `challenge/`, `solution/`, `assets/`, `other/`.

## Running with the future CLI

Once the MTCF Go CLI is built (Phase 1):

```sh
mtcf validate                    # check all 5 challenges parse cleanly
mtcf generate                    # produce the derived files (CTFd manifest + instance.toml + CSV + index.json)
mtcf doctor                      # preflight: docker daemon, ctfd token, ports free
mtcf build                       # build images for the 3 deployable challenges
mtcf deploy                      # docker compose up + register in CTFd
mtcf logs cub3-clone             # tail logs
mtcf down                        # tear down
```

## Status: derived files are not yet committed

Phase 0 is the spec. The author manifests below all parse against `schema/challenge.schema.json`, but the auto-generated `challenge.yml` (CTFd format) and `instance.toml` files at each challenge root are intentionally **not yet present** — the CLI that produces them lands in Phase 1. Adding them now would commit handwritten "derived" files that drift from the future generator.

When the CLI ships, `mtcf generate` will populate them, and CI will keep them in sync.
