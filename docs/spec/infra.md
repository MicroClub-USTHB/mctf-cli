# Infrastructure (Terraform)

`mctf infra` provisions the host an event runs on. It wraps Terraform around
configuration rendered from the `infra` block of `mctf.config.yml`, so the
server is reproducible Infrastructure-as-Code next to the challenges.

Supported today: a **single VPS** on **Hetzner Cloud** or **DigitalOcean**.
Managed clusters (DOKS, EKS, GKE) and DNS records are planned follow-ups.

## Commands

```
mctf infra plan                 # render .mctf/infra/, terraform init + plan
mctf infra plan --dry-run       # render .mctf/infra/, print the terraform commands
mctf infra apply                # render, terraform init + apply (asks to confirm)
mctf infra apply --auto-approve # same, without the confirmation prompt
mctf infra destroy              # terraform init + destroy (asks to confirm)
```

`plan` and `apply` regenerate the configuration on every run, so edits to the
rendered files are overwritten: change `mctf.config.yml` instead. `--dry-run`
still renders the files, so they can be reviewed without Terraform installed
and without a provider token.

`destroy` does **not** re-render. It tears down exactly what the configuration
and state in `.mctf/infra/` describe, even if `mctf.config.yml` changed since
the last `apply`. It is a no-op when there is no state.

## Requirements

- **Terraform** ≥ 1.5 on `PATH`. Set `MCTF_TERRAFORM_BIN` to use another
  binary, for example `tofu` for OpenTofu. `--dry-run` does not need it.
- **Provider API token** in the environment. The Terraform providers read it
  directly; it is never written to the rendered files.

| Provider | Token env var |
| --- | --- |
| `hetzner` | `HCLOUD_TOKEN` |
| `digitalocean` | `DIGITALOCEAN_TOKEN` |

`plan` and `apply` fail before starting Terraform when the token is missing.

## Config

```yaml
infra:
  provider: hetzner              # required: hetzner | digitalocean
  region: fsn1                   # optional
  size: cx23                     # optional
  image: ubuntu-24.04            # optional
  ssh_public_key: ~/.ssh/id_ed25519.pub   # optional
  open_ports: ["8443", "20000-20100"]     # optional, extra inbound TCP
```

| Key | Hetzner default | DigitalOcean default | Notes |
| --- | --- | --- | --- |
| `region` | `fsn1` | `fra1` | Hetzner location / DigitalOcean region slug. |
| `size` | `cx23` | `s-2vcpu-4gb` | Server type / droplet size. |
| `image` | `ubuntu-24.04` | `ubuntu-24-04-x64` | Must ship cloud-init and apt (see below). |
| `ssh_public_key` | `~/.ssh/id_ed25519.pub` | same | Installed for `root`. A relative path is resolved from the event root. |
| `open_ports` | — | — | Ports or ranges (`"1337"`, `"20000-20100"`) opened on top of the defaults. |

## What gets provisioned

For both providers:

- An SSH key resource holding `ssh_public_key`.
- One server named `mctf-<event.slug>`, labelled / tagged with the event.
- A firewall allowing inbound ICMP and these TCP ports:
  - `22` (ssh), plus `deploy.ssh.port` when it is set;
  - `80` and `443` for ingress;
  - every active category's challenge port range (core, `categories.extended`,
    and `categories.custom` with a `port_range`), merged into as few rules as
    possible (the core catalog alone collapses to `10000-17999`);
  - each entry of `open_ports`.

  On DigitalOcean, where firewalls block egress unless it is allowed, all
  outbound TCP, UDP, and ICMP is allowed.
- cloud-init user data that installs `docker.io` and `docker-compose-v2` and
  starts docker, so the host is ready for `deploy.mode: ssh` once it boots.

Terraform outputs `ipv4` and `ipv6`. After `apply`, read the address with:

```
terraform -chdir=.mctf/infra output ipv4
```

Then set `deploy.ssh.host` to it and point the event DNS (`event.domain` and
its challenge subdomains) at it.

## Rendered layout and state

```
.mctf/infra/
  main.tf              # rendered from the provider template (overwritten)
  cloud-init.yml       # server user data (overwritten)
  .terraform/          # providers downloaded by terraform init
  .terraform.lock.hcl  # provider version lock
  terraform.tfstate    # Terraform state — the only record of what exists
```

`.mctf/` is git-ignored, and the state stays local. **Keep
`terraform.tfstate`**: without it, Terraform can no longer see or destroy the
server, and the resources have to be removed by hand in the provider console.
State can contain sensitive values, so do not commit it.

Run `mctf infra destroy` before changing `infra.provider`. The new provider's
configuration does not describe the old provider's resources.
