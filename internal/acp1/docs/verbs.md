# ACP1 — verbs & configuration reference

Per-connector verb reference (roadmap A1). Same section order for every
connector so the docs don't drift. All samples below are **real captures**
(against the Axon Synapse Simulator on `10.6.239.113:2071`, or a loopback
producer built from the committed manifest fixture) — not hand-written.

Wire format lives in [`../CLAUDE.md`](../CLAUDE.md); this file is the operator
how-to. Verbs: `info walk tree get set inc dec reset ensure watch export import
extract diff convert discover profile`.

---

## 1. Transport configs

ACP1 speaks three transports (see `../CLAUDE.md` "Transport modes"):

| Mode | Flag | Port | Notes |
|---|---|---|---|
| A — UDP direct | `--transport udp` | 2071 | one datagram = one message; announcements are subnet broadcast (same VLAN only) |
| B — TCP direct | `--transport tcp` | 2071 | 4-byte MLEN prefix; routable, survives cross-VLAN |
| C — AN2/TCP | `--transport an2` | 2072 | AN2 dlen frames, proto=1 |

Consumer auto-falls back TCP→UDP if `--transport` is omitted. Producer serves a
single transport, `--transport udp+tcp`, or `--transport all`.

```
dhs consumer acp1 walk 10.6.239.113 --transport udp --port 2071
dhs producer acp1 serve --tree tree.json --transport all --port 2071 --an2-port 2072
```

## 2. Controllers & redundancy

**Default = one connection** to one controller. ACP1 has no client identity on
the wire, so redundancy is an **app/deployment** concern, not a protocol one:

- **Single controller (default):** one consumer ↔ one producer.
- **Redundant controllers:** run **2+ instances** — one producer per NIC/controller
  (`--bind <nic-ip>` pins the broadcast source IP, #263), and on the consumer side
  one connection per controller. UDP announces are per-link (single NIC); a
  redundant pair therefore requires **tcp** on every endpoint (manifest validator
  enforces this — udp is only valid as the sole endpoint).

```
# redundant pair = two producer instances, one per controller NIC
dhs producer acp1 serve --tree tree.json --transport tcp --bind 10.100.0.103 --port 2071
dhs producer acp1 serve --tree tree.json --transport tcp --bind 10.100.0.109 --port 2071
```

## 3. Logging & severity

Severity is set per invocation; **logs go to stderr, data to stdout** (so
`2>` captures logs, `1>` captures the result).

| Side | Flag | Values |
|---|---|---|
| consumer | `--log-level` | `trace \| debug \| info \| warn \| error \| critical` (default `info`) |
| consumer | `--verbose` | shortcut for `--log-level debug` |
| producer | `--log-level` | `debug \| info \| warn \| error` (default `info`) |
| producer | `--log-format` | `text` (default) \| `json` (Loki/Promtail) |

```
dhs consumer acp1 walk 10.6.239.113 --transport udp --port 2071 --verbose      # = --log-level debug
dhs consumer acp1 walk 10.6.239.113 ... --log-level warn
dhs producer acp1 serve --tree tree.json --log-level debug --log-format json
dhs consumer acp1 walk 10.6.239.113 ... --log-level debug 2>run.log            # logs→file, tree→stdout
```

## 4. info / walk

```
$ dhs consumer acp1 info 10.6.239.113 --transport udp --port 2071
device       10.6.239.113:2071
protocol     acp1 v1
slots        31
per-slot status:
  slot  0   status=present    online=false
  ...

$ dhs consumer acp1 walk 10.6.239.113 --transport udp --port 2071 --slot 0
slot 0 — 59 objects
[control]
      4  Broadcasts            enum    RWD  "On"                [Off, On]
      8  ErrorThreshold        uint    RWD  10                  0..255 step 1
      0  IP_Conf               enum    RWD  "DHCP"              [Manual, DHCP]
```

## 5. get / set / inc / dec / reset

ACP1 addresses an object by **slot + group + label** (the spec's stable
identifier — "use the Label, not ObjId"; ObjIds shift across firmware). The
numeric id is shown in `walk` for reference. (`reset` = setDefValue;
`inc`/`dec` step by the object's `step_size` — ACP1 setInc/setDecValue.)

```
$ dhs consumer acp1 get 10.6.239.113 --transport udp --port 2071 --slot 0 --group control --label Broadcasts
value = "On"  (enum idx 1)
raw  = 01
kind = enum  access = RW-   items = [Off, On]  (default idx 1)

$ dhs consumer acp1 set  ... --slot 0 --group control --label ErrorThreshold --value 20
confirmed value = 20      raw = 14
$ dhs consumer acp1 inc  ... --label ErrorThreshold
confirmed value = 21      raw = 15
$ dhs consumer acp1 dec  ... --label ErrorThreshold
confirmed value = 20      raw = 14
$ dhs consumer acp1 reset ... --label ErrorThreshold
confirmed value = 10      raw = 0a       # back to default
```

`min`/`max`/`step`/`enum` come from the object metadata (shown in `walk`:
`ErrorThreshold uint 0..255 step 1`); `set` is validated client-side against
them before sending.

## 6. export / import

`export` dumps a walked tree to **json / yaml / csv** (`jsonl` is *not* an export
format — it is the raw-frame capture log, see §8). `import` applies a snapshot
back, with `--dry-run`.

```
$ dhs consumer acp1 export 10.6.239.113 ... --slot 0 --format yaml
# acp snapshot — generated by cmd/acp
device: { ip: 10.6.239.113, protocol: acp1, num_slots: 31 }
slots:
  - slot: 0
    status: present
    identity: { ... }

$ dhs consumer acp1 export ... --format csv        # one row per object
ip,protocol,slot,oid,path,id,label,kind,access,value,...,slot_status
10.6.239.113,acp1,0,,identity.Card name,0,Card name,string,R--,RRS18,...

$ dhs consumer acp1 export ... --format json --out device.json

$ dhs consumer acp1 import 127.0.0.1 ... --file device.yaml --dry-run
would apply 15, skipped 44, failed 0
skipped rows (dry-run detail):
  read_only (38): slot=0 id=0 kind=string access=R-- path="identity.Card name"
```

## 7. reports — tree ASCII & PlantUML mindmap

The `tree` verb renders the walked device as an ASCII tree or a PlantUML
mindmap (`--format ascii|plantuml`).

```
$ dhs consumer acp1 tree 127.0.0.1 ... --slot 0 --format ascii
+-- alarm
|   +-- Temperature  alarm  RWD = 1
+-- control
|   +-- Broadcasts  enum  RWD = "On"
|   +-- ErrorThreshold  uint  RWD = 20

$ dhs consumer acp1 tree 127.0.0.1 ... --slot 0 --format plantuml
@startmindmap
* device
** control
*** ErrorThreshold (uint) = 20
@endmindmap
```

## 8. play / watch

`watch` subscribes to spontaneous announcements (MTID=0). The producer can
self-drive value drift with `--play` so there is something to watch without an
external controller; `--capture <path>.jsonl` logs the raw wire frames.

```
# producer side: oscillate objects, emit status announces every 2s
dhs producer acp1 serve --tree tree.json --transport all --play all --play-interval 2s

# consumer side: live announcements (+ optional raw-frame capture)
dhs consumer acp1 watch 10.6.239.113 --transport udp --port 2071 --capture run.jsonl
```

## 9. ensure() — idempotent converge (ADR-0007)

`ensure` converges an object to `--value` and reports whether it changed; `--check`
is a dry-run. Run twice ⇒ second run is a no-op (idempotent).

```
$ dhs consumer acp1 ensure ... --label ErrorThreshold --value 20
changed=true   previous="10"  current="20"
$ dhs consumer acp1 ensure ... --label ErrorThreshold --value 20      # again
changed=false  current="20" (already converged)
$ dhs consumer acp1 ensure ... --label ErrorThreshold --value 99 --check
would_change=true  current="20"  target="99"
```

## 10. Wireshark

Dissector: [`../wireshark/dhs_acpv1.lua`](../wireshark/dhs_acpv1.lua) — decodes
Mode A/B/C, every method + object group, with a per-frame Info column.

| OS | Plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

```
# copy the dissector, then filter on the dhs proto:
#   display filter:  dhs_acpv1
#   port decode:     udp.port == 2071 || tcp.port == 2071 || tcp.port == 2072
tshark -r capture.pcapng -O dhs_acpv1 -Y dhs_acpv1
```

## 11. Ansible (the exclusive integration / deploy driver — no .ps1)

Inventory (`ansible/inventory/hosts.ini`), role (`ansible/roles/dhs_acp1/`), and
playbooks (`ansible/playbooks/`). Integration is driven by Ansible on a control
node against the oracle (idempotent, run-twice = 0 changes).

```ini
# inventory/hosts.ini
[producer]
dhs-ubuntu ansible_host=10.100.0.103 ansible_user=root
```

```yaml
# run the tier-3 integration vs the live Synapse oracle (playbooks/acp1-integration.yml)
# ACP1_TEST_HOST=10.6.239.113 DHS_BIN=/usr/local/bin/dhs \
#   ansible-playbook -i inventory/hosts.ini playbooks/acp1-integration.yml
- hosts: localhost
  connection: local
  tasks:
    - command:
        argv: ["{{ dhs_bin }}", consumer, acp1, walk, "{{ acp1_host }}",
               --transport, udp, --port, "2071", --log-level, debug, --timeout, 20s]
      register: walk
      changed_when: false
    - assert: { that: ["'[control]' in walk.stdout"] }
    - debug: { var: walk.stderr_lines }   # surface dhs logs (stderr) in the play
```

dhs logs go to **stderr** → `register` the task and `debug: var=<r>.stderr_lines`
to show them; `<r>.stdout_lines` is the data. Run `ansible-playbook -v` (`-vv`/`-vvv`)
for Ansible's own echo of task stdout/stderr. The idempotency contract test
`ansible/playbooks/test-idempotency.yml` runs `ensure` twice and asserts the
second pass reports `changed=false`.

## 12. See also

- [`../CLAUDE.md`](../CLAUDE.md) — wire format, methods, object groups
- [`./README.md`](./README.md) · [`./consumer.md`](./consumer.md) · [`./provider.md`](./provider.md) · [`./runbook.md`](./runbook.md)
- [`../../../docs/adr/0025-per-connector-definition-of-done.md`](../../../docs/adr/0025-per-connector-definition-of-done.md)
