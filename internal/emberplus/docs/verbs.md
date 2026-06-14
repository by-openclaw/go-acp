# Ember+ — verbs & configuration reference

Per-connector verb reference (roadmap A3), same section order as every other
connector. Samples are **real captures** against a loopback producer built from
the committed manifest fixture (`dhs-emberplus-integration`, DTD 2.60) and the
vendor TinyEmber+ tools. Scope is **DTD 2.60** (ADR-0025); TinyEmber+ reports
2.31 and is used as a live vendor oracle for integration.

Wire format: [`../CLAUDE.md`](../CLAUDE.md). Verbs: `info walk tree get set
matrix invoke stream ensure watch export import diff convert discover profile`.

---

## 1. Transport configs

Ember+ is **TCP with S101 framing** (CRC-16, keep-alive). Default port 9000;
Lawo boxes also use 9090 / 9092. No UDP.

```
dhs consumer emberplus walk 10.6.239.113 --port 9000
dhs producer emberplus serve --manifest device.json --cache-dir . --port 9000
```

## 2. Controllers & redundancy

**Default = one connection.** Redundancy = **2+ instances** (one per provider).
Ember+ has no client identity on the wire, so failover/dedup is an app-layer
concern (consume from the active provider; the standby is a separate connection).

## 3. Logging & severity

Logs go to **stderr**, data to **stdout** (`2>` captures logs, `1>` the result).

| Side | Flag | Values |
|---|---|---|
| consumer | `--log-level` | `trace \| debug \| info \| warn \| error \| critical` (default `info`) |
| consumer | `--verbose` | shortcut for `--log-level debug` |
| producer | `--log-level` / `--log-format` | `debug..error` / `text \| json` |

```
dhs consumer emberplus walk 10.6.239.113 --port 9000 --verbose
dhs consumer emberplus walk 10.6.239.113 --port 9000 --log-level debug 2>run.log
```

## 4. info / walk

```
$ dhs consumer emberplus info 127.0.0.1 --port 19000
device       127.0.0.1:19000
protocol     emberplus v1
dtd_version  2.60
slots        1
per-slot status:
  slot  0   status=present    online=true

$ dhs consumer emberplus walk 127.0.0.1 --port 19000
... gain  int  RW-  0  -1000..100 step 1
... s-0   string RW- "Pri-S0"
```

## 5. get / set / matrix / invoke / stream

Ember+ addresses by **`--path`**, which accepts **either** a dotted **OID**
(`1.1.4.3.1`) **or** the **identifier path** (`dhs-emberplus-integration.oneToN.targetParams.3.gain`)
— both resolve to the same element. There is no inc/dec/reset (Ember+ writes a
parameter value directly).

```
$ dhs consumer emberplus get 127.0.0.1 --port 19000 --path dhs-emberplus-integration.oneToN.targetParams.3.gain
value = 0
$ dhs consumer emberplus get 127.0.0.1 --port 19000 --path 1.1.4.3.1     # same element, by OID
value = 0

$ dhs consumer emberplus set 127.0.0.1 --port 19000 --path ...gain --value 5
confirmed value = 5
```

`min`/`max`/`step`/`enum` come from the parameter metadata (`gain int -1000..100
step 1`); `set` is validated against them before sending.

**matrix** — connect or query a crosspoint (`--matrix`/`--target`/`--source`):
```
dhs consumer emberplus matrix 127.0.0.1 --port 19000 --path dhs-emberplus-integration.oneToN --target 3 --source 1   # connect t←s
dhs consumer emberplus matrix 127.0.0.1 --port 19000 --path dhs-emberplus-integration.oneToN --target 3              # query target 3
```

**invoke** — call an Ember+ function (RPC), returns an InvocationResult:
```
dhs consumer emberplus invoke 127.0.0.1 --port 19000 --path dhs-emberplus-integration.functions.getSalvo --arg 1
```

**stream** — subscribe to a stream parameter's live values:
```
dhs consumer emberplus stream 127.0.0.1 --port 19000 --path <streamParam>
```

## 6. export / import

`export` → **json / yaml / csv** (`jsonl` is the raw-frame capture log, §8); a
committed sample lives at [`../testdata/exports/device.yaml`](../testdata/exports/device.yaml)
/ `device.csv`. `import` applies a snapshot with `--dry-run`.

```
$ dhs consumer emberplus export 127.0.0.1 --port 19000 --format csv
ip,protocol,slot,oid,path,id,label,kind,access,value,...
127.0.0.1,emberplus,0,1.3.3.4.6.1,dhs-emberplus-integration.nToN.params.4.6.gain,1,gain,int,RW-,0,...

$ dhs consumer emberplus import 127.0.0.1 --port 19000 --file device.yaml --dry-run
```

## 7. reports — tree ASCII & PlantUML mindmap

```
$ dhs consumer emberplus tree 127.0.0.1 --port 19000 --format ascii
+-- dhs-emberplus-integration [oid=1]
    +-- dynamic [oid=1.4]
    |   +-- labelsPrimary [oid=1.4.1]
    |   |   +-- sources [oid=1.4.1.2]
    |   |   |   +-- s-0  string  RW- = "Pri-S0"

$ dhs consumer emberplus tree 127.0.0.1 --port 19000 --format plantuml
@startmindmap
* device
** dhs-emberplus-integration [oid=1]
@endmindmap
```

## 8. play / watch

`watch` subscribes to parameter/matrix-connection announcements; `stream` (§5)
handles stream parameters. `--capture <path>.jsonl` logs raw S101 frames.

```
dhs consumer emberplus watch 10.6.239.113 --port 9000 --capture run.jsonl
```

## 9. ensure() — idempotent converge (ADR-0007)

```
$ dhs consumer emberplus ensure 127.0.0.1 --port 19000 --path ...gain --value 5
changed=true   current="5"                 # first run from a different value
$ dhs consumer emberplus ensure ... --path ...gain --value 5
changed=false  current="5" (already converged)     # idempotent
```

## 10. Wireshark

Dissector: [`../wireshark/dhs_emberplus.lua`](../wireshark/dhs_emberplus.lua) —
decodes S101 framing + GlowDTD BER, every glow type / command, per-frame Info.

| OS | Plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

```
#   display filter:  dhs_emberplus
#   port decode:     tcp.port == 9000 || tcp.port == 9092
tshark -r capture.pcapng -O dhs_emberplus -Y dhs_emberplus
```

## 11. Ansible (the exclusive integration / deploy driver — no .ps1)

Integration drives the consumer against the vendor TinyEmber+ tools
(`ansible/playbooks/emberplus-integration.yml`).

```yaml
# EMBERPLUS_TEST_HOST=10.6.239.113 EMBERPLUS_TEST_PORT=9000 DHS_BIN=/usr/local/bin/dhs \
#   ansible-playbook -i inventory/hosts.ini playbooks/emberplus-integration.yml
- hosts: localhost
  connection: local
  tasks:
    - command:
        argv: ["{{ dhs_bin }}", consumer, emberplus, walk, "{{ ember_host }}",
               --port, "{{ ember_port }}", --log-level, debug]
      register: walk
      changed_when: false
    - assert: { that: ["'objects' in walk.stdout"] }
    - debug: { var: walk.stderr_lines }   # surface dhs logs (stderr)
```

dhs logs → **stderr**: `register` + `debug: var=<r>.stderr_lines` shows them;
`<r>.stdout_lines` is the data. `ansible-playbook -v` echoes task output.

## 12. See also

- [`../CLAUDE.md`](../CLAUDE.md) — S101 / GlowDTD / BER, matrix, functions
- [`./README.md`](./README.md) · [`./consumer.md`](./consumer.md) · [`./provider.md`](./provider.md) · [`./runbook.md`](./runbook.md)
- [`../../../docs/adr/0025-per-connector-definition-of-done.md`](../../../docs/adr/0025-per-connector-definition-of-done.md)
