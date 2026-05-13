# Challenge Manifest (`challenge/challenge.yml`)

The author-edited source-of-truth manifest. One per challenge. JSON Schema: [`schema/challenge.schema.json`](../../schema/challenge.schema.json).

## Full schema (annotated)

```yaml
schema: 1                              # OPTIONAL — defaults to 1; future migrations bump this

challenge:
  name: "CUB3"                         # REQUIRED — display name; underscored slug also acceptable
  author: "$h3rL0ck"                   # REQUIRED — single string in v1
  category: web                        # REQUIRED — must match parent directory; see categories.md
  difficulty: easy                     # REQUIRED — easy | medium | hard | extreme (locked enum)
  description: |                       # REQUIRED — markdown, max 4000 chars
    Can you solve a randomly scrambled Rubik's cube?
    `nc {{host}} {{port}}` or visit `{{url}}`
  wave: 1                              # REQUIRED — integer 1..waves_number from mctf.config.yml
  force_hidden: false                  # OPTIONAL — when true, registered as hidden in CTFd
  flags:                               # REQUIRED — exactly one in v1
    - { flag: "mctf{l0g1n_byp4ss}", case_sensitive: true }
  need_deploy: true                    # REQUIRED — true if Dockerfile + compose are present in challenge/
  links: []                            # OPTIONAL — external URLs shown to players
  tags: ["sqli", "auth-bypass"]        # OPTIONAL — sub-classification (see categories.md)
  hints:                               # OPTIONAL — structured hints (legacy: solution/hints.md)
    - cost: 0
      content: "Look at how the input is sanitized."
    - cost: 50
      content: "It's a UNION-based injection."
  scoring:                             # OPTIONAL — overrides mctf.config.yml::event.scoring
    type: dynamic                      #   static | dynamic
    initial: 500
    decay: 10
    minimum: 100

deploy:                                # REQUIRED IF need_deploy: true
  subdomain: "cub3"                    # REQUIRED — DNS-safe slug; routed as <subdomain>.<event-domain>
  port: null                           # null when dynamic_instances; integer otherwise
  internal_port: 8000                  # required when dynamic_instances; container's listening port
  dynamic_instances: true              # REQUIRED — false=service archetype, true=instanced archetype
  tcp: false                           # REQUIRED — true for raw TCP (pwn), false for HTTP
  noSsl: false                         # OPTIONAL — opt out of TLS

  resources:                           # OPTIONAL — overrides defaults
    cpu: "0.5"
    memory: "256M"
    pids: 256                          # strongly recommended for pwn

  healthcheck:                         # OPTIONAL but mandatory for instanced archetype
    type: tcp                          # tcp | http | cmd
    path: "/"                          # for type=http
    cmd: ["./healthcheck.sh"]          # for type=cmd
    interval: "30s"
    timeout: "5s"
    retries: 3

  instance:                            # OPTIONAL — only meaningful when dynamic_instances: true
    timeout: 1800                      # auto-destroy seconds (default 1800)
    extend_time: 900                   # seconds players can extend
    max_per_team: 1

  security_opts:                       # OPTIONAL
    allow_caps: ["NET_BIND_SERVICE"]   # explicit Linux capability allowlist
    allow_privileged: false            # true to permit privileged:true in compose under pro profile
```

## Per-archetype required fields

| Field | static | service | instanced |
|---|---|---|---|
| `challenge.name`, `author`, `category`, `difficulty`, `description`, `wave`, `flags` | ✓ | ✓ | ✓ |
| `challenge.need_deploy` | `false` | `true` | `true` |
| `deploy:` block | — | ✓ | ✓ |
| `Dockerfile`, `docker-compose.yml` files in `challenge/` | — | ✓ | ✓ |
| `deploy.port` (integer) | — | ✓ | must be `null` |
| `deploy.internal_port` (integer) | — | must be `null` | ✓ |
| `deploy.healthcheck` *or* `HEALTHCHECK` in Dockerfile | — | recommended | **required** |
| `deploy.resources.{cpu, memory}` | — | recommended | **required** |
| `solution/writeup.md` | ✓ | ✓ | ✓ |

## Examples

### Static crypto challenge

```yaml
challenge:
  name: "rsa-baby"
  author: "alice"
  category: crypto
  difficulty: easy
  description: |
    Tiny modulus, tiny exponent. Decrypt the ciphertext.
  wave: 1
  flags:
    - { flag: "mctf{f4ct0r1ng_15_h4rd_unt1l_1t_15nt}", case_sensitive: true }
  need_deploy: false
  tags: ["rsa"]
```

### Service-archetype web challenge (fixed port)

```yaml
challenge:
  name: "welcome"
  author: "ops"
  category: misc
  difficulty: easy
  description: "Visit the site and read the flag."
  wave: 1
  flags:
    - { flag: "mctf{w3lc0m3_t0_th3_g4me}", case_sensitive: true }
  need_deploy: true

deploy:
  subdomain: "welcome"
  port: 14000
  internal_port: null
  dynamic_instances: false
  tcp: false
  noSsl: false
```

### Instanced pwn challenge

```yaml
challenge:
  name: "baby-overflow"
  author: "spinel"
  category: pwn
  difficulty: easy
  description: "A classic stack smash. `nc {{host}} {{port}}`"
  wave: 1
  flags:
    - { flag: "mctf{cl4ss1c_buff3r_0v3rfl0w}", case_sensitive: true }
  need_deploy: true
  tags: ["bof", "x86_64"]

deploy:
  subdomain: "baby-overflow"           # required even for TCP — used for TLS-SNI routing
  port: null
  internal_port: 5000
  dynamic_instances: true
  tcp: true
  noSsl: false

  resources:
    cpu: "0.25"
    memory: "64M"
    pids: 32

  healthcheck:
    type: tcp
    interval: "10s"
    timeout: "3s"
    retries: 3

  instance:
    timeout: 1800
    extend_time: 900
    max_per_team: 1
```

## Flag rules

- **Format:** `<prefix>{...}` where `<prefix>` is `mctf.config.yml::event.flag_prefix` (default `mctf`).
- **Case sensitivity:** per-flag (default `true`). Case-insensitive flags use a hashed compare.
- **One flag per challenge in v1.** Multi-flag deferred to v0.2.
- **Static flags only in v1.** Per-team dynamic flags require the instancer + a plugin (v0.2+).

## How archetype is derived

You don't write `archetype: ...` in the manifest. The CLI computes it:

```
need_deploy=false                              -> static
need_deploy=true, dynamic_instances=false      -> service
need_deploy=true, dynamic_instances=true       -> instanced
```

This keeps the author surface identical to the format MCTF 5.0 already uses.

## How docker-compose.yml is wired up

For service / instanced archetypes, `challenge/docker-compose.yml` is built and run alongside the Dockerfile. Conventions:

- The "main" service must build from the local `Dockerfile`:
  ```yaml
  services:
    app:
      build:
        context: .
        dockerfile: Dockerfile
  ```
- For **instanced** challenges, the instancer dynamically maps the host port via the env-var convention `${PORT_<internal_port>:-<default>}`:
  ```yaml
  ports:
    - "${PORT_8000:-8000}:8000"
  ```
- For **service** challenges with a fixed `deploy.port`, the host port is the literal:
  ```yaml
  ports:
    - "14000:8000"
  ```

The CLI lints these patterns; mismatches between `deploy.port` / `deploy.internal_port` and the compose `ports:` array are hard-fails.

## What goes where in the four folders

- **`challenge/src/`** — application code copied by the Dockerfile. **Never put a static flag value here.** Use `flag.txt` (env-loaded at runtime) or an env var.
- **`solution/writeup.md`** — required for review. Markdown, intended-solve walkthrough.
- **`solution/hints.md`** — optional. Markdown headings `## Hint 1 (cost: 50)` are auto-converted into `challenge.hints[]` if not already declared.
- **`solution/solve.{sh,py}`** — optional. When present, `mctf doctor --smoke` runs each solver against the deployed challenge and verifies the flag is recovered.
- **`assets/`** — files uploaded to CTFd as downloadable handouts. Recommended to ship as zips.
- **`other/`** — private. Flag generators, scratch notes, decoys not used in build.
