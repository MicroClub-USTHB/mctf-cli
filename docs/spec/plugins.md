# Plugins

MCTF plugins are external processes that receive **lifecycle hook** events and
can run arbitrary logic (notifications, policy checks, custom provisioning, …)
without being compiled into the CLI. They are language-agnostic: any executable
that speaks the protocol below works.

> **Status:** The plugin foundation, deploy hooks (before_deploy / after_deploy), teardown hooks (before_down / after_down), build hooks, and instance-spawn hook are implemented. Required plugins can block commands when a required plugin fails during startup or a blocking before_* hook.

## Trust model

A plugin entry is a command the CLI **executes** during a run. Treat
`mctf.config.yml` plugins as exactly as trusted as the `docker-compose.yml`
files beside them — only configure plugins you trust.

## Configuration

Add a `plugins:` list to `mctf.config.yml`:

```yaml
plugins:
  - name: audit          # display name (defaults to the command)
    command: ./plugins/logger.sh   # executable; relative paths resolve to the event root
    args: ["--verbose"]  # optional
    enabled: true        # optional (default true)
    required: false      # optional; defaults to false
    config:              # optional, opaque — passed to the plugin at initialize
      file: ./audit.log
```

## Protocol

**Framing:** newline-delimited JSON (one JSON object per line), JSON-RPC 2.0.
The host writes requests to the plugin's **stdin**; the plugin writes responses
to its **stdout**.

> **stdout is protocol-only.** A plugin MUST write nothing but response objects
> to stdout. All human/debug logging goes to **stderr** (forwarded to the
> host's stderr) or a file. A stray stdout line desyncs the stream and the
> plugin is dropped.

### 1. `initialize` (handshake)

Host → plugin:
```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocol_version":1,"mctf_version":"...","config":{...}}}
```
Plugin → host — its name and the hooks it wants:
```json
{"jsonrpc":"2.0","id":1,"result":{"name":"audit","hooks":["before_deploy","after_deploy"]}}
```

### 2. Hook calls

For each subscribed hook the host sends a request whose `method` is the hook
name and whose `params` is the hook payload; the plugin replies with any result
(the host ignores the body, only success/failure matters):
```json
{"jsonrpc":"2.0","id":2,"method":"before_deploy","params":{ ...payload... }}
{"jsonrpc":"2.0","id":2,"result":{"ok":true}}
```

### 3. `shutdown`

The host sends `{"method":"shutdown"}` and closes stdin; the plugin exits.

## Semantics: best-effort and required plugins

Plugins are best-effort by default. A plugin with `required: false` (the default)
that fails to start, fails its handshake, returns an error, crashes, or times out
is logged and dropped; the command continues.

Setting `required: true` opts a plugin into blocking behavior:

- If a required plugin fails to start or complete its initialization handshake,
  the command is blocked when the plugin manager reports that failure.
- If a required plugin returns an error or fails during a blocking `before_*`
  hook, the command is aborted before the corresponding operation runs.
- Failures in `after_*` hooks do not undo an operation that has already completed.
- Non-required plugins remain best-effort, and their failures do not block the
  command.

Each hook call is bounded by a timeout. A hung plugin is terminated. Only
opt-in required-plugin failures can block commands; ordinary plugin failures
remain isolated.


## Hook roster

| Hook | Payload | Status |
| --- | --- | --- |
| `before_deploy` | Deploy payload (event, mode, units, dry_run) | **Shipped** |
| `after_deploy` | Deploy payload plus `outcome` (`success` / `failed`) and `error` | **Shipped** |
| `before_down` | Teardown payload (event, mode, units, dry_run) | **Shipped** |
| `after_down` | Teardown payload plus `outcome` (`success` / `failed`) and `error` | **Shipped** |
| `before_build` | Build payload (event, sha, dry_run, images) | **Shipped** |
| `after_build` | Build payload plus `outcome` (`success` / `failed`) and `error` | **Shipped** |
| `on_instance_spawn` | Instance spawn payload (event, instance_id, challenge_slug, team_id, ports, flag, containers, expires_at) | **Shipped** |
| `on_health_failure` | — | Planned (needs a health-monitoring loop) |

### Deploy payload

```json
{
  "event": "myctf",
  "phase": "before",           // "before" | "after"
  "mode": "docker",            // "docker" | "ssh" | "kubernetes"
  "dry_run": false,
  "units": [
    {"challenge":"web/echo","category":"web","slug":"echo",
     "project_name":"mctf-myctf-web-echo","image_tag":"a1b2c3d"}
  ],
  "outcome": "success",        // after only
  "error": ""                  // after + failed only
}
```

`after_deploy(success)` fires only after a real, non-dry-run deploy. `after_deploy(failed)` fires when the deploy operation fails. Teardown uses the same payload structure with `before_down` and `after_down`. The `mode` field identifies the execution mode: `docker`, `ssh`, or `kubernetes`. Successful `after_*` hooks fire only after a real, non-dry-run operation; failure hooks fire when the operation fails.

### Build payload

```json
{
  "event": "myctf",
  "phase": "before",           // "before" | "after"
  "sha": "a1b2c3d",
  "no_cache": false,
  "push": false,
  "dry_run": false,
  "images": [
    {
      "challenge": "web/echo",
      "category": "web",
      "slug": "echo",
      "tags": [
        "ghcr.io/example/myctf-web-echo:a1b2c3d",
        "ghcr.io/example/myctf-web-echo:latest"
      ],
      "labels": {
        "mctf.challenge": "web/echo"
      }
    }
  ],
  "outcome": "success",        // after only
  "error": ""                  // after + failed only
}
```

`before_build` fires before `build.Apply` executes image builds.
`after_build` fires after building, leak scanning, and optional registry pushing complete (with `outcome: "success"` or `outcome: "failed"` and `error`).

### Instance spawn payload

```json
{
  "event": "myctf",
  "instance_id": "inst-a1b2c3d4e5f6",
  "challenge_slug": "web-sqli",
  "team_id": "team-42",
  "ports": {
    "80": 32768
  },
  "flag": "mctf{demo_flag}",
  "containers": [
    "mctf-myctf-web-sqli-inst-a1b2c3d4e5f6"
  ],
  "expires_at": "2026-10-12T16:00:00Z"
}
```

`on_instance_spawn` fires whenever a new challenge instance is spawned (both via
the `mctf instance spawn` CLI command and the `mctf instancer serve` HTTP daemon).
Fields `ports`, `flag`, and `containers` are omitted when empty.

## Example

A minimal POSIX-`sh` plugin lives at
[`examples/sample-event/plugins/logger.sh`](../../examples/sample-event/plugins/logger.sh) —
it logs each hook to a file and shows the full request/response contract.
