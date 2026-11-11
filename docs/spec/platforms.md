# Platforms

`mctf platform sync|unsync|configure` push the event to a CTF platform. The
target is picked by `platform.type` in `mctf.config.yml`:

```yaml
platform:
  type: webhook                       # ctfd | webhook
  url: "https://scoreboard.example.com/mctf-hook"
```

| `platform.type` | `platform.url` | Credentials |
| --- | --- | --- |
| `ctfd` | CTFd base URL | `MCTF_CTFD_TOKEN` (required) — CTFd admin API token |
| `webhook` | Endpoint that receives the POSTs | `MCTF_WEBHOOK_SECRET` (optional) — HMAC signing secret |

Every target implements the same interface (`internal/platform.Platform`):
`Sync`, `ResolveUnsync` + `Unsync`, and `Configure`. Each target owns its own
planning and output, so `--dry-run` and `--json` mean "show what would happen"
in whatever form fits that target.

## CTFd

Diffs the local challenges against the CTFd API and creates (or, with
`--update`, reconciles) challenges, flags, tags, hints and files. `unsync`
deletes the matching CTFd challenge; `configure` sets `ctf_name`,
`ctf_description`, `start` and `end`.

## Generic webhook

For custom scoreboards. Each command sends one `POST` with a JSON body:

| Command | `event` | Body |
| --- | --- | --- |
| `mctf platform sync` | `sync` | Full snapshot of the (filtered) challenges |
| `mctf platform unsync <category>/<slug>` | `unsync` | The removed challenge's category and slug |
| `mctf platform configure` | `configure` | Event name, description, start, end |

### Transport

- Headers: `Content-Type: application/json`, `User-Agent: mctf/<version>`,
  `X-MCTF-Event: sync|unsync|configure`, and — when `MCTF_WEBHOOK_SECRET` is
  set — `X-MCTF-Signature: sha256=<hex HMAC-SHA256 of the raw body>`.
- Payloads carry flags, so `platform.url` must be `https://`. Plain `http://`
  is accepted only for loopback hosts (`localhost`, `127.0.0.0/8`, `::1`) or
  with `--insecure-http`.
- Any 2xx response is success. Anything else fails the command with
  `webhook <event>: HTTP <code>: <first 512 bytes of the response body>`.
  There are no retries; re-run the command.
- 30 s timeout per request.

### `sync` semantics

- A sync is a **full snapshot** of the challenges selected by `--category` /
  `--slug`. A challenge missing from a filtered sync means "not in this sync",
  not "deleted" — deletions arrive as `unsync` events.
- If the filters match no challenge, nothing is sent and the command fails.
- `--update` has no effect (the webhook has no remote state to reconcile).
- `--dry-run` prints the payload and sends nothing. `--json` prints only the
  payload.

### Payloads (schema 1)

Every body starts with the same envelope:

| Field | Type | Meaning |
| --- | --- | --- |
| `schema` | int | Payload contract version, currently `1` |
| `event` | string | `sync`, `unsync` or `configure` |
| `event_slug` | string | `event.slug` from `mctf.config.yml` |
| `sent_at` | string | RFC 3339 UTC timestamp |

`sync`:

```json
{
  "schema": 1,
  "event": "sync",
  "event_slug": "mctf-5",
  "sent_at": "2026-11-11T20:00:00Z",
  "challenges": [
    {
      "category": "web",
      "slug": "cub3",
      "name": "CUB3",
      "author": "$h3rL0ck",
      "difficulty": "easy",
      "wave": 1,
      "archetype": "instanced",
      "hidden": false,
      "description": "Find the flag.\n\n**Author: $h3rL0ck**",
      "scoring": { "type": "dynamic", "initial": 500, "decay": 10, "minimum": 100 },
      "flags": [ { "flag": "mctf{example}", "case_sensitive": true } ],
      "tags": ["wave 1", "web"],
      "hints": [ { "content": "Look at the cookies.", "cost": 0 } ],
      "connection_info": "https://instancer.mctf.example",
      "files": [ { "name": "handout.zip", "sha256": "9f86d0…" } ]
    }
  ]
}
```

- `challenges` is sorted by `category/slug`.
- `scoring` is `{ "type": "static", "points": N }` or
  `{ "type": "dynamic", "initial": N, "decay": N, "minimum": N }`, resolved
  from the challenge override or the event default.
- `description` and `tags` match what CTFd receives (author line appended;
  `wave N` tag first).
- `files` lists the challenge's `assets/` by name and sha256; the bytes are not
  sent.
- `flags`, `tags`, `hints` and `files` are always arrays, possibly empty.

`unsync`:

```json
{ "schema": 1, "event": "unsync", "event_slug": "mctf-5", "sent_at": "2026-11-11T20:00:00Z",
  "category": "web", "slug": "cub3" }
```

`configure` (`description`, `start_at`, `end_at` omitted when empty):

```json
{ "schema": 1, "event": "configure", "event_slug": "mctf-5", "sent_at": "2026-11-11T20:00:00Z",
  "name": "MCTF 5", "description": "…", "start_at": "2026-10-15T10:00:00Z", "end_at": "2026-10-17T18:00:00Z" }
```

### Verifying the signature

Compute the HMAC over the raw request body, before any JSON parsing, and
compare in constant time:

```go
func verify(secret, body []byte, header string) bool {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(header))
}
```
