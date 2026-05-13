# Challenge Categories

MCTF defines two tiers of categories: a **core tier** (always available) and an **extended tier** (opt-in via `mctf.config.yml::categories.extended`). Organizers can also declare entirely **custom** categories without forking the CLI.

This separation keeps the default category set small (the core 10 cover what most events ship today, inspired by the MCTF 5.0 catalog) while letting events that need cloud, blockchain, or boot2root categories enable them with one line of config.

## Core tier — always on (v1)

| Slug | Name | Deployable | Default archetype | Default port range |
|---|---|:---:|---|---|
| `web` | Web Exploitation | ✅ | service / instanced | 10000–10999 |
| `pwn` | Binary Exploitation | ✅ | service / instanced | 11000–11999 |
| `jailbreak` | Sandbox Escape | ✅ | instanced | 16000–16999 |
| `crypto` | Cryptography | — *(may be service)* | static *(or service)* | 12000–12999 |
| `reverse` | Reverse Engineering | — | static | 15000–15999 |
| `forensics` | Digital Forensics | — | static | (no service) |
| `osint` | Open-Source Intel | — | static | (no service) |
| `ai` | AI / Prompt | — *(may be service)* | static / service | 13000–13999 |
| `hardware` | Hardware / Embedded | — *(may be service)* | static / service | 17000–17999 (default) |
| `misc` | Miscellaneous | — | any | 14000–14999 |

## Extended tier — opt-in

Enable in `mctf.config.yml`:

```yaml
categories:
  extended:
    - blockchain
    - cloud
    - welcome
```

Each gets a 1000-port slice from the default range (17000+) on activation, allocated in the order declared.

| Slug | Name | Deployable | Notes |
|---|---|:---:|---|
| `stego` | Steganography | — | Image / audio / video / text covert channels. |
| `mobile` | Mobile (Android/iOS) | — *(may be service)* | APK/IPA handouts; sometimes a backend service. |
| `blockchain` | Blockchain / Web3 | ✅ | Per-team chain RPC + faucet — almost always **instanced**. |
| `network` | Network Recon / Forensics | ✅ | Multi-host topology behind one entrypoint; often instanced. |
| `ppc` | Programming / Algorithms | ✅ | TCP problem-server with timeouts; usually instanced. |
| `realistic` | Boot2Root / Pentest | ✅ | HTB-style full-machine; large RAM, instanced. |
| `cloud` | Cloud (AWS/GCP/Azure) | ✅ | Localstack / k3d / per-team accounts; instanced. |
| `iot` | IoT / Embedded | ✅ | Often qemu-system; instanced for stateful firmware. |
| `sysadmin` | SysAdmin / Linux | ✅ | Per-team VM / container with a misconfig. |
| `defensive` | DFIR / Blue Team | — *(may be service)* | Log analysis, IR scenarios. |
| `welcome` | Welcome / Sanity | — | Free-flag intros; lint relaxes most checks. |
| `quiz` | Quiz / Trivia | — | Static; multiple-choice handout. |

## Custom categories

For one-off or org-specific categories, declare them in `mctf.config.yml`:

```yaml
categories:
  custom:
    - slug: arg
      name: "Alternate Reality Game"
      deployable: false
    - slug: physical
      name: "Physical Security"
      deployable: true
      port_range: { min: 18000, max: 18999 }
```

Custom categories are first-class to the validator: they can have any of the same lint rules applied via plugin in v0.2+.

## Sub-classification: tags, not categories

Don't create a category for every variation. The `challenge.tags` field is the right place for sub-types — players filter on tags in CTFd, and the category count stays small.

Recommended tag conventions per category:

- **`pwn`** — `kernel`, `browser`, `heap`, `format-string`, `rop`, `bof`, `uaf`, `race`
- **`web`** — `sqli`, `xss`, `ssrf`, `xxe`, `idor`, `jwt`, `prototype-pollution`, `deserialization`, `auth-bypass`, `csrf`
- **`crypto`** — `rsa`, `aes`, `ecc`, `lattice`, `hash`, `prng`, `padding-oracle`, `dlp`
- **`reverse`** — `binary`, `dotnet`, `java`, `wasm`, `obfuscation`, `vm`, `unpacking`
- **`forensics`** — `pcap`, `memory`, `disk`, `windows`, `linux`, `mobile`, `email`
- **`ai`** — `prompt-injection`, `model-extraction`, `jailbreak`, `data-poisoning`, `rag`
- **`jailbreak`** — `container-escape`, `vm-escape`, `sandbox-escape`, `seccomp`, `apparmor`
- **`blockchain`** — `evm`, `solidity`, `defi`, `nft`, `mev`, `oracle`
- **`cloud`** — `aws`, `gcp`, `azure`, `iam`, `s3`, `lambda`, `iac`

The validator emits an `info`-level note (not a warning) when a category-typical tag is missing — purely as a nudge.

## Archetypes

Every challenge resolves to exactly one archetype, **derived** from `challenge.yml` (no separate field — the CLI infers):

- **`static`** — `need_deploy: false`. No runtime, no Dockerfile required. Only `assets/` are uploaded to CTFd.
- **`service`** — `need_deploy: true`, `deploy.dynamic_instances: false`. One shared container; `deploy.port` exposed.
- **`instanced`** — `need_deploy: true`, `deploy.dynamic_instances: true`. Per-team container/pod spawned by the MCTF instancer; `deploy.internal_port` is what the container listens on.

## Per-category lint rules (selected)

Hard-fails:

- **deployable category** + `need_deploy: false` → mismatch, hard-fail (the category wouldn't make sense).
- **`pwn`** with `privileged: true` in compose → hard-fail under `pro` profile.
- **`pwn`** / **`jailbreak`** without a non-root `USER` in Dockerfile → hard-fail under `pro` profile.

Warnings:

- **`pwn`** without `deploy.resources.pids` set.
- **`web`** with mutable state and `dynamic_instances: false` → suggests instancing.
- **service archetype** without a `HEALTHCHECK` in Dockerfile *or* `deploy.healthcheck` in manifest.
- **`static`** archetype with empty `assets/` directory.
- **`crypto`** with archetype `service` (oracle) and no rate-limit hint in description.
- **`blockchain`** challenge with archetype `service` (almost always should be `instanced`).

Full validation rules live in [validation.md](./validation.md).
