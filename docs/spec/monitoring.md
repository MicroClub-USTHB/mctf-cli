# Monitoring stack

`mctf monitoring up` brings up an observability stack for a running event:
**Prometheus** (metrics + scraping), **cAdvisor** (per-container metrics), and
**Grafana** (dashboards). `mctf monitoring down` tears it back down.

This complements `mctf generate`, which emits the operator-facing
`monitoring/prometheus.yml` scrape config. The stack renders and runs its own
self-contained copy under `monitoring/stack/` and never touches the generated
file.

## Commands

```
mctf monitoring up            # render monitoring/stack/ and `docker compose up -d`
mctf monitoring up --dry-run  # print the plan + compose command, write nothing
mctf monitoring down          # `docker compose down`
mctf monitoring down --volumes# also drop the prometheus/grafana data volumes
```

`up` regenerates the stack files on every run from the current event config and
challenge set, so scrape targets always match the deployed challenges. Edits to
files under `monitoring/stack/` are overwritten — tune behavior through config
instead (see below).

## Rendered layout

```
monitoring/stack/
  docker-compose.yml                               # prometheus + cadvisor + grafana
  prometheus.yml                                   # full scrape config
  grafana/provisioning/datasources/datasource.yml  # Prometheus datasource
  grafana/provisioning/dashboards/dashboards.yml   # dashboard provider
  grafana/dashboards/mctf-overview.json            # container CPU/memory overview
```

## Scrape targets

The stack's `prometheus.yml` scrapes:

- **prometheus** itself (`localhost:<prometheus_port>`),
- **cadvisor** (`cadvisor:8080`, over the compose network),
- **one job per instanced challenge** with an `internal_port`, at
  `host.docker.internal:<internal_port>`.

Challenge jobs target `host.docker.internal` (mapped to the host gateway via
`extra_hosts`) rather than `localhost`, so they are reachable from inside the
Prometheus container. This is the stack-side resolution of the `localhost`
limitation noted on the generate-side scrape config.

## Configuration

All keys are optional and live under the `monitoring:` block in
`mctf.config.yml`:

| Key                | Type   | Default  | Meaning                              |
| ------------------ | ------ | -------- | ------------------------------------ |
| `prometheus_port`  | int    | `9090`   | host port for the Prometheus UI      |
| `grafana_port`     | int    | `3000`   | host port for the Grafana UI         |
| `cadvisor_port`    | int    | `8081`   | host port for the cAdvisor UI        |
| `grafana_password` | string | `admin`  | Grafana `admin` user password        |
| `retention`        | string | `15d`    | Prometheus TSDB retention window     |

```yaml
monitoring:
  grafana_port: 3001
  grafana_password: change-me
  retention: 30d
```

Setting any `monitoring:` value also makes `mctf generate` emit the scrape
config even for events with no instanced challenges.

## Access

After `mctf monitoring up`:

- Grafana — `http://localhost:<grafana_port>` (`admin` / `grafana_password`)
- Prometheus — `http://localhost:<prometheus_port>`
- cAdvisor — `http://localhost:<cadvisor_port>`
