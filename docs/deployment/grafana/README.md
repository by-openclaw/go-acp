# dhs — Grafana / Prometheus / Loki stack

Self-contained observability stack for dhs. Spins Prometheus, Loki,
Promtail, and Grafana with pre-provisioned data sources, alert rules,
and a dhs overview dashboard.

## Quick start

```bash
cd docs/deployment/grafana
docker compose up -d
```

Then start dhs with metrics + JSON logs:

```bash
dhs producer probel-sw08p serve \
    --tree internal/probel-sw08p/assets/demo_2mtx_2lvl_64x64.json \
    --port 2008 \
    --metrics-addr :9100 \
    --log-format json > /var/log/dhs.log 2>&1
```

Open Grafana at <http://localhost:3000> (admin / admin). Dashboard
"dhs / dhs — overview" pre-loads rx/tx rates, latency p99, per-cmd
top-N, and the Loki log stream.

Prometheus UI: <http://localhost:9090>. Alerts tab shows the
rule-set in `alerts.yml`.

Loki direct: <http://localhost:3100>.

## Files

| File | Purpose |
|---|---|
| `docker-compose.yml` | full stack (Prom, Loki, Promtail, Grafana) |
| `prometheus.yml` | scrape config — points at `host.docker.internal:9100/metrics` |
| `alerts.yml` | PromQL alert rules: memory leak, goroutine leak, latency, NAK surge, stalled session, reconnect storm |
| `loki-config.yml` | minimal single-node Loki |
| `promtail-config.yml` | tails `/var/log/dhs*.log`, parses slog JSON |
| `grafana-provisioning/` | auto-wires Prom + Loki data sources and the dashboards folder |
| `dashboards/dhs-overview.json` | one dashboard with process + connector + per-cmd + logs panels |

## Alerts (seed set)

| Alarm | Trigger | Severity |
|---|---|---|
| Memory leak | `deriv(go_memstats_heap_alloc_bytes[30m]) > 0` for 30 min | warning |
| Goroutine leak | `deriv(go_goroutines[15m]) > 0` for 15 min | warning |
| High CPU | `rate(process_cpu_seconds_total[1m]) > 0.8` for 5 min | warning |
| GC pressure | `rate(go_gc_duration_seconds_sum[5m]) > 0.1` for 10 min | warning |
| Latency p99 | `histogram_quantile(0.99, …handler_latency…) > 100000` for 5 min | warning |
| Session stalled | `time() - dhs_connector_last_rx_timestamp > 120` for 2 min | critical |
| NAK surge | `rate(dhs_connector_naks_total[5m]) > 1` for 2 min | warning |
| Reconnect storm | `rate(dhs_connector_reconnects_total[5m]) > 0.1` for 5 min | warning |
| Connector memory growth | `deriv(dhs_connector_memory_bytes[30m]) > 0` for 30 min | info |

Edit `alerts.yml` and `curl -X POST http://localhost:9090/-/reload`
to pick up changes without bouncing the container.

## Linux hosts

`host.docker.internal` works out of the box on Docker Desktop (Mac,
Windows). On Linux either:

1. Replace the Prom target with your LAN IP in `prometheus.yml`, or
2. Add `network_mode: host` to the `prometheus` service and change
   the target to `localhost:9100`.

## What's missing (tracked)

- Per-protocol panels (ACP1 / ACP2 / Ember+) land as their providers
  wire up `metrics.Connector` in D8.
- Walk-duration heatmap lands with the Span API in D2.
- Alertmanager + Slack/email routes — out of scope here; bring your
  own.
