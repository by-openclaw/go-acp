# Finding a device in Prometheus, Loki and Grafana

The question in the control room is always the same: **what is wrong
with 10.6.255.102?** This page is how you answer it in each of the
three tools, and why typing the address is enough.

## The one rule: `device` is the address you typed

Every series and every log line dhs emits carries the same label, with
the same name, holding the same value — the host as it appears on the
command line:

```
dhs consumer acp2 watch 10.6.255.102 --metrics-addr :9110 \
    --log /var/log/dhs-acp2-10.6.255.102.log --log-format json
```

```
dhs_alarm_severity{device="10.6.255.102", proto="acp2", path="PSU.1.Temperature", …}
{job="dhs", device="10.6.255.102", proto="acp2", severity="critical"} …
```

So the address is the index into the whole stack. Nothing needs
translating between Prometheus and Loki, and a Grafana variable built
on it drives both.

| Label | Where it comes from | Values |
|---|---|---|
| `device` | the host argument | `10.6.255.102`, `10.6.40.53` |
| `proto` | the connector | `acp1`, `acp2`, `mnset`, `emberplus`, … |
| `role` | the verb | `consumer` (watch) / `provider` (serve) |
| `severity` | the alarm verdict | `normal`, `minor`, `major`, `critical`, `error` |
| `path` / `label` / `band` | the object and the rule that fired | `PSU.1.Status`, `high major` |

`path` is a Prometheus label but **not** a Loki label — it would split
the log stream into thousands. In Loki it is a field: `| json` then
`| path =~ "PSU.*"`.

## Type-ahead ("intellisense") in each tool

### Grafana — the dashboard variable

The **dhs — alarms by device** dashboard (`dhs-alarms.json`) has a
`device` picker at the top, built from:

```
label_values(dhs_alarm_rules, device)
```

`dhs_alarm_rules` exists for **every watched device, healthy or not**,
so the dropdown lists the whole plant rather than only what is
currently broken. Grafana filters the list as you type, so `10.6.2`
narrows to that subnet and `.102` to that host. Every panel on the
page — the Prometheus tables and the Loki logs panel — follows the
same `$device`.

Chained variables work the same way: `$proto` is
`label_values(dhs_alarm_rules{device=~"$device"}, proto)`, so picking
a device shrinks the protocol list to the ones that device speaks.

### Prometheus — the expression browser

The browser completes metric names as you type `dhs_alarm`. For label
values, start from the metric and open the label:

```promql
dhs_alarm_rules                        # every watched device, one line each
dhs_alarm_rules{device="10.6.255.102"} # that one
count by (device) (dhs_alarm_rules)    # the plant, as a list
```

The API answers the same question without the UI — this is what an
Ansible check or a script uses:

```bash
curl -s 'http://prometheus:9090/api/v1/label/device/values' | jq -r '.data[]'
curl -s --get 'http://prometheus:9090/api/v1/series' \
     --data-urlencode 'match[]=dhs_alarm_severity{device="10.6.255.102"}' | jq
```

### Loki — the label browser

In Explore, the label browser lists `device` with every value, and the
query bar completes them. The queries an operator actually uses:

```logql
{job="dhs", device="10.6.255.102"}                        # everything from that box
{job="dhs", device="10.6.255.102", severity=~"major|critical"}
{job="dhs", device=~"10\\.6\\.255\\..*"}                  # a whole subnet
{job="dhs", device="10.6.255.102"} | json | msg = `alarm` # only the verdicts
{job="dhs"} | json | path =~ `PSU\\..*` | severity != `normal`
```

## The five questions, and the query for each

| Question | Prometheus | Loki |
|---|---|---|
| What is wrong on this device? | `dhs_alarm_severity{device="X"}` | `{job="dhs",device="X",severity=~"major\|critical"}` |
| How bad is the plant right now? | `sum by (severity) (dhs_alarm_active)` | — |
| Is anything judging this device? | `dhs_alarm_rules{device="X"}` (0 = no template) | — |
| When did it start? | `dhs_alarm_severity{device="X"}` over a range | `{job="dhs",device="X"} \| json \| msg = \`alarm\`` |
| Is it flapping? | `rate(dhs_alarm_transitions_total{device="X"}[5m])` | the same lines, visibly repeating |

Severity is a number in Prometheus so a graph can threshold on it:
`0 info · 1 normal · 2 minor · 3 major · 4 critical · 5 error`
(`dhs_alarm_severity >= 4` is the critical alert in `alerts.yml`). The
log line carries both the name and the RFC 5424 code
(`severity="critical"`, `syslog_severity=2`).

## Why a cleared alarm disappears

`dhs_alarm_severity` exists only while an object is above normal —
that is what "no alarm" looks like in Prometheus. The zero stays
visible on `dhs_alarm_active{severity="critical"}`, which is the
series to graph and to alert an *absence* on. The history of the
transition is in Loki, and in
`dhs_alarm_transitions_total`.

## Scraping the plant

One watch per device, one port each, deployed by Ansible
(`ansible/playbooks/alarm.yml`); Prometheus scrapes them from a file
the same play writes:

```yaml
scrape_configs:
  - job_name: dhs-watch
    file_sd_configs:
      - files: ["/etc/prometheus/file_sd/dhs-watch.yml"]
```

```yaml
# /etc/prometheus/file_sd/dhs-watch.yml — written by the play
- targets: ["10.6.250.101:9110"]
  labels: { service: dhs, watched: "10.6.255.102", proto: acp2 }
```

The scrape target is the *host running dhs*; the device it watches is
the `device` label inside the metrics, which is why you navigate by
`device` and not by `instance`.
