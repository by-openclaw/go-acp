# Probel SW-P-02 — verbs & configuration reference

Per-connector verb reference. Same section order for every connector so
the docs don't drift. All command samples below are **real captures**
against a loopback `dhs producer probel-sw02p serve` instance built from
the committed demo matrix fixture
([`../testdata/exports/matrix_tree.json`](../testdata/exports/matrix_tree.json),
a single `oneToN` matrix, 8×8) — not hand-written. Each verb's hex is the
exact `--capture <verb>.jsonl` frame log produced by the run shown, and
matches the `probel-sw02p TX/RX … hex=…` lines the CLI logs on stderr.

Wire format lives in [`../CLAUDE.md`](../CLAUDE.md); this file is the
operator how-to. SW-P-02 is a **matrix-router** protocol — addressing is
by **matrix / level / destination / source**, not slot / group / label,
so the get/set/inc/dec/reset shape of a Tree/DM connector does not apply
(see §5). Consumer verbs: `interrogate connect connect-on-go go
salvo-connect protect-connect protect-disconnect protect-interrogate
protect-dump protect-name dual-status lock-status status router-config
watch`.

---

## 1. Transport configs

SW-P-02 speaks a **single** transport (see [`../CLAUDE.md`](../CLAUDE.md)
"Transport — SW-P-02 §3.1"):

| Transport | Flag | Port | Notes |
|---|---|---|---|
| TCP direct | (default; only option) | 2002 | `SOM(0xFF) COMMAND MESSAGE CHECKSUM`. No DLE stuffing — bytes inside a frame are transparent. No framing-layer ACK/NAK. |

There is **no `--transport` flag** (only one transport exists) and **no
UDP**. The target is `host:port`; port defaults to 2002 when omitted.

```
dhs consumer probel-sw02p interrogate 127.0.0.1:2002 --dst 5
dhs producer probel-sw02p serve --tree matrix.json --port 2002
```

## 2. Controllers & redundancy

**Default = one connection** to one matrix controller. SW-P-02 carries no
client identity on the wire, so redundancy is both an **app/deployment**
concern *and* an in-protocol exchange:

- **Single controller (default):** one consumer ↔ one matrix.
- **Redundant controllers:** SW-P-02 defines DUAL CONTROLLER STATUS
  REQUEST / RESPONSE (rx 50 / tx 51, §3.2.45/46) so a controller can poll
  which of a redundant pair is active (`dhs consumer probel-sw02p
  dual-status`). At the deployment layer, run one provider per controller
  NIC (`--bind <nic-ip>`) and one consumer connection per controller.

```
# two provider instances, one per controller NIC
dhs producer probel-sw02p serve --tree matrix.json --bind 10.100.0.103 --port 2002
dhs producer probel-sw02p serve --tree matrix.json --bind 10.100.0.109 --port 2002
```

## 3. Logging & severity

Severity is set per invocation; **logs go to stderr, data to stdout** (so
`2>` captures logs, `1>` captures the result). The consumer logs every
frame as `probel-sw02p TX/RX cmd=… wire_len=… hex=…`.

| Side | Flag | Values |
|---|---|---|
| consumer | `--log-level` | `trace \| debug \| info \| warn \| error \| critical` (default `info`) |
| consumer | `--verbose` | shortcut for `--log-level debug` |
| producer | `--log-level` | `debug \| info \| warn \| error` (default `info`) |
| producer | `--log-format` | `text` (default) \| `json` (Loki/Promtail) |

Real producer log lines (`--log-format json --log-level debug`):

```json
{"time":"2026-06-18T11:14:00.939+02:00","level":"INFO","msg":"probel-sw02p provider listening","addr":"[::]:12002","matrices":1}
{"time":"2026-06-18T11:14:05.102+02:00","level":"INFO","msg":"probel-sw02p session opened","remote":"127.0.0.1:62607"}
{"time":"2026-06-18T11:14:24.462+02:00","level":"DEBUG","msg":"probel-sw02p session rx","remote":"127.0.0.1:62615","cmd":1,"payload_len":2,"wire_len":5,"hex":"ff 01 00 02 7d"}
```

Real consumer log lines (text format, default) — data on stdout, logs on stderr:

```
$ dhs consumer probel-sw02p interrogate 127.0.0.1:12002 --dst 2 2>run.log 1>tally.out
# stderr (run.log):
time=2026-06-18T11:14:24.454+02:00 level=INFO msg="probel-sw02p connected" host=127.0.0.1 port=12002
time=2026-06-18T11:14:24.454+02:00 level=INFO msg="probel-sw02p TX" cmd=1 payload_len=2 wire_len=5 hex="ff 01 00 02 7d"
time=2026-06-18T11:14:24.464+02:00 level=INFO msg="probel-sw02p RX" cmd=3 payload_len=3 wire_len=6 hex="ff 03 07 02 7f 75"
# stdout (tally.out):
tally  dst=2 → src=1023 bad_source=false
```

## 4. info / walk

SW-P-02 has **no wire-side discovery primitive every controller honours** —
VSM and Commie both configure matrix size + id + level in their UI per
matrix. The consumer therefore takes the matrix shape from flags rather
than probing it. The two closest analogues to "info/walk" are:

- **`router-config`** (rx 75 → tx 76/77 ROUTER CONFIGURATION) — the one
  in-protocol "info" call: returns the level bitmap + per-level dst/src
  counts. Most controllers ignore it and configure size externally, but
  our provider answers it from the served tree.
- **the rx 01 bootstrap sweep** (`watch --dsts N`, §8) — interrogates
  dst 0..N-1 so the matrix tallies back the current source for each
  destination, the closest equivalent to "walk one level".

`router-config` — real capture (loopback, 8×8 demo matrix):

```
$ dhs consumer probel-sw02p router-config 127.0.0.1:12002
router-config (response-1)  level_map=0x0000001 levels=1
  level[0]  dsts=8 srcs=8
```

```
tx:  ff 4b 35                          # SOM cmd=75 (no MESSAGE)            cksum=35
rx:  ff 4c 00 00 00 01 00 08 00 08 23  # SOM cmd=76 level_map=0x0000001 lvl0: dsts=8 srcs=8  cksum=23
```

The provider exposes the same matrix shape on the served-device side via
`--metrics-addr :9100` → `/snapshot.json` (real scrape after four rx 01
INTERROGATE frames):

```json
{"connector":{"RxFrames":4,"TxFrames":4,"RxBytes":20,"TxBytes":24,
 "RxHitsByCmd":[0,4,0,...],"TxHitsByCmd":[0,0,0,4,0,...]}}
```

`RxHitsByCmd[1] = 4` confirms the matrix received four rx 01 INTERROGATE
frames; `TxHitsByCmd[3] = 4` confirms four tx 03 TALLY replies.

## 5. interrogate / connect / connect-on-go / go (matrix get/set)

SW-P-02 addresses a crosspoint by **(matrix, level, destination)** and
carries one **source** — this is the matrix equivalent of get/set. There
is **no inc/dec/reset** (a crosspoint is a discrete route, not a numeric
parameter with a step), and **no source/dest label set command** — SW-P-02
has no command to rename a source or destination on the wire (labels live
only in the served canonical tree / controller UI). The provider mirrors
the matrix-verb handling of the Ember+ `matrix` verb rather than ACP1's
get/set/inc/dec/reset.

| Verb | Cmd byte(s) | Dir | What it does |
|---|---:|:---:|---|
| **interrogate** | rx 01 → tx 03 | request → tally | Read the source currently routed to a destination (`--extended` → rx 65 → tx 67) |
| **connect** | rx 02 → tx 04 | request → connected | Route a source to a destination, broadcast to all sessions (`--extended` → rx 66 → tx 68) |
| **connect-on-go** | rx 05 → tx 12 | request → ack | Stage one crosspoint into the pending salvo buffer |
| **go** | rx 06 → tx 04 + tx 13 | request → connected + done-ack | Commit (`--op set`) or discard (`--op clear`) the pending buffer |

### interrogate (real capture)

`interrogate` on dst 2 of the demo matrix (no route declared yet):

```
$ dhs consumer probel-sw02p interrogate 127.0.0.1:12002 --dst 2
tally  dst=2 → src=1023 bad_source=false
```

```
tx:  ff 01 00 02 7d        # SOM cmd=01 MESSAGE=00 02       cksum=7d
rx:  ff 03 07 02 7f 75     # SOM cmd=03 MESSAGE=07 02 7f    cksum=75
                           #   Multiplier 0x07 → Source DIV-128 = 7
                           #   Destination = 2, Source MOD-128 = 0x7f
                           #   → Source = 7*128 + 127 = 1023 (dst out of range, §3.2.5)
```

Source value **1023** is the §3.2.5 "destination out of range" sentinel —
the demo fixture ships with no routes, so every interrogate returns it
until a connect lands (see below).

`interrogate --extended` (rx 65 → tx 67, separate 7-bit Multipliers per
axis), real capture:

```
$ dhs consumer probel-sw02p interrogate 127.0.0.1:12002 --dst 2 --extended
extended tally  dst=2 → src=1023 bad_source=false update_off=false
tx:  ff 41 00 02 3d                 # SOM cmd=65 dst=2          cksum=3d
rx:  ff 43 00 02 07 7f 00 35        # SOM cmd=67 dst=2 srcMul=07 srcMod=7f (=1023)  cksum=35
```

### connect (real capture)

`connect` dst 2 ← src 5, then read it back:

```
$ dhs consumer probel-sw02p connect 127.0.0.1:12002 --dst 2 --src 5
connected  dst=2 src=5 bad_source=false
$ dhs consumer probel-sw02p interrogate 127.0.0.1:12002 --dst 2
tally  dst=2 → src=5 bad_source=false        # route stuck
```

```
# connect tx/rx:
tx:  ff 02 00 02 05 77     # SOM cmd=02 Multiplier=00 Dest=02 Src=05   cksum=77
rx:  ff 04 00 02 05 75     # SOM cmd=04 CROSSPOINT CONNECTED dst=2 src=5  cksum=75
# read-back tx/rx:
tx:  ff 01 00 02 7d        # interrogate dst 2
rx:  ff 03 00 02 05 76     # tally dst=2 → src=5  (Multiplier 0 ⇒ DIV-128=0, src=5)
```

Checksum check for the connect frame: 7-bit two's-complement of
`COMMAND||MESSAGE` = `-(0x02+0x00+0x02+0x05) & 0x7F` = `-9 & 0x7F` =
`0x77`. ✔

`connect --extended` (rx 66 → tx 68) and `connect --bad-source` (sets the
narrow Multiplier bad-source bit, rx 02 only) are also available; the
extended capture:

```
$ dhs consumer probel-sw02p connect 127.0.0.1:12002 --dst 2 --src 5 --extended
extended connected  dst=2 src=5 bad_source=false update_off=false
tx:  ff 42 00 02 00 05 37        # SOM cmd=66 dst=2 src=5      cksum=37
rx:  ff 44 00 02 00 05 00 35     # SOM cmd=68 EXTENDED CONNECTED dst=2 src=5  cksum=35
```

### connect-on-go / go (staged commit, real capture)

`connect-on-go` stages a crosspoint into the pending buffer; `go --op set`
commits it (broadcasting tx 04 CONNECTED) and acks with tx 13:

```
$ dhs consumer probel-sw02p connect-on-go 127.0.0.1:12002 --dst 3 --src 6
connect-on-go staged  dst=3 src=6 (call `go --op set` to commit)
$ dhs consumer probel-sw02p go 127.0.0.1:12002 --op set
go done  op=set result=set
```

```
# connect-on-go tx/rx:
tx:  ff 05 00 03 06 72     # SOM cmd=05 CONNECT ON GO dst=3 src=6   cksum=72
rx:  ff 0c 00 03 06 6b     # SOM cmd=12 CONNECT ON GO ACK dst=3 src=6  cksum=6b
# go tx/rx (two rx frames: the committed tx 04 then the tx 13 done-ack):
tx:  ff 06 00 7a           # SOM cmd=06 GO op=set                 cksum=7a
rx:  ff 04 00 03 06 73     # SOM cmd=04 CROSSPOINT CONNECTED dst=3 src=6 (commit)
rx:  ff 0d 00 73           # SOM cmd=13 GO DONE ACK                cksum=73
```

`go --op clear` discards the pending buffer instead of committing it.

## 6. export / import

`export` dumps a walked matrix to **json / yaml / csv**; `import` applies a
snapshot back. SW-P-02 export/import rides the same canonical exporter the
rest of `dhs` uses (Device → Matrix → level → crosspoints), keyed by
(matrix, level, destination). The served fixture **is** the canonical JSON
shape — this is the exact tree used to drive every capture in this doc
([`../testdata/exports/matrix_tree.json`](../testdata/exports/matrix_tree.json)):

```json
{
  "root": {
    "identifier": "router", "oid": "1",
    "children": [
      { "identifier": "matrix-0", "oid": "1.1",
        "type": "oneToN", "mode": "linear",
        "targetCount": 8, "sourceCount": 8,
        "labels": [ { "basePath": "router.matrix-0.video" } ] }
    ]
  }
}
```

`dhs producer probel-sw02p serve --tree <file>` imports a hand-authored
canonical JSON like the one above; `--manifest <file>` is the
manifest-assembled import path (per ADR-0022). A dedicated `export`
consumer subcommand is not yet wired for SW-P-02 (the matrix shape is
caller-supplied, §4, not walked); the served tree is the canonical export
source the rest of the toolchain consumes.

## 7. reports — tree ASCII & PlantUML mindmap

The matrix structure rendered from the demo fixture served in every
capture above (a single matrix, single level, 8×8):

```
router  (Probel SW-P-02 demo matrix)
+-- matrix-0   oneToN  linear  8x8
    +-- level 0
        +-- destinations  0 .. 7   (targetCount=8)
        +-- sources       0 .. 7   (sourceCount=8)
        +-- crosspoints   (matrix, level, dst) -> src
            +-- dst 2 -> src 5     (after the §5 connect)
            +-- dst 3 -> src 6     (after the §5 connect-on-go + go)
```

PlantUML mindmap of the same matrix:

```
@startmindmap
* router
** matrix-0 (oneToN / linear / 8x8)
*** level 0
**** destinations: 0..7
**** sources: 0..7
@endmindmap
```

A dedicated `tree`/`report` consumer subcommand is not yet wired for
SW-P-02; the structure above is rendered from the served canonical
fixture, which is the same source a report verb would consume.

## 8. play / watch

`watch` opens the TCP session, fires the rx 01 INTERROGATE bootstrap sweep
(when `--dsts` is set), and prints every spontaneous tally (tx 03 TALLY /
tx 04 CONNECTED) until Ctrl-C or `--timeout`; `--capture <path>.jsonl`
logs the raw wire frames. **SW-P-02 has no in-protocol keep-alive
command**, so the rotating rx 01 INTERROGATE / tx 03 TALLY round-trip *is*
the keep-alive — the provider answers each sweep frame, keeping the
consumer's "alive" bit set. There is no producer-side `--play` value
oscillator (a crosspoint matrix has no drifting analogue values to
oscillate, unlike ACP1 — N/A by protocol nature).

```
# producer side: serve a matrix
dhs producer probel-sw02p serve --tree matrix.json --port 2002

# consumer side: bootstrap sweep + live tallies (+ optional raw-frame capture)
dhs consumer probel-sw02p watch 127.0.0.1:2002 --dsts 4 --srcs 4 --capture run.jsonl
```

Real capture (stdout, loopback, 4-dst sweep over a matrix where dst 2 and
dst 3 were previously routed — five tally events: the 4-dst bootstrap
sweep plus one keep-alive ping during the run window):

```
$ dhs consumer probel-sw02p watch 127.0.0.1:12002 --dsts 4 --srcs 4 --timeout 4s
event  cmd=0x03 payload_len=3
event  cmd=0x03 payload_len=3
event  cmd=0x03 payload_len=3
event  cmd=0x03 payload_len=3
event  cmd=0x03 payload_len=3
```

The `--capture` JSONL is the per-frame walk record (real, first 8 frames —
note dst 2 tallies src 5 and dst 3 tallies src 6 from the §5 routes):

```json
{"proto":"probel-sw02p","dir":"tx","hex":"ff0100007f","len":5}
{"proto":"probel-sw02p","dir":"rx","hex":"ff0307007f77","len":6}
{"proto":"probel-sw02p","dir":"tx","hex":"ff0100017e","len":5}
{"proto":"probel-sw02p","dir":"rx","hex":"ff0307017f76","len":6}
{"proto":"probel-sw02p","dir":"tx","hex":"ff0100027d","len":5}
{"proto":"probel-sw02p","dir":"rx","hex":"ff0300020576","len":6}
{"proto":"probel-sw02p","dir":"tx","hex":"ff0100037c","len":5}
{"proto":"probel-sw02p","dir":"rx","hex":"ff0300030674","len":6}
```

Real session-metrics summary logged on Disconnect (stderr):

```
level=INFO msg="probel-sw02p session metrics" summary="uptime=4.001s cpu=0.00% mem=0B rx=5/30B tx=5/25B errs=decode:0 nak:0 to:0 retry:0 rec:0 lat_us p50~0 p95~0 p99~0"
```

## 9. ensure() — idempotent converge (ADR-0007)

`ensure` converges a crosspoint to a target source and reports whether it
changed; running it twice ⇒ the second run is a no-op. For SW-P-02 the
converged state is a route: `(matrix, level, dst) -> src`. The matrix is
**naturally idempotent** — re-issuing the same rx 02 CONNECT for a route
already in place changes nothing, and the provider re-broadcasts the
existing tx 04 CONNECTED state. The §5 read-back proves this directly:
after `connect --dst 2 --src 5`, an immediate `interrogate --dst 2`
returns `src=5`; a second identical `connect` produces the same tx 04
`ff 04 00 02 05 75` with no state change.

```
# intent: dst 2 on (matrix 0, level 0) is routed from src 5
#   first apply  -> changed=true   (route established, tx 04 CONNECTED dst=2 src=5)
#   second apply -> changed=false  (already routed; identical tx 04 re-broadcast)
```

A dedicated `ensure` consumer subcommand is not yet wired for SW-P-02; the
underlying converge semantic is the `connect` path's idempotency above.

## 10. Wireshark

Dissector:
[`../wireshark/dhs_probel_sw02p.lua`](../wireshark/dhs_probel_sw02p.lua) —
decodes the `SOM / COMMAND / MESSAGE / CHECKSUM` framing and every command
byte, with a per-frame Info column showing matrix/level/dst/src.

| OS | Plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

```
# copy the dissector, then filter on the dhs proto:
#   display filter:  dhs_probel_sw02p
#   port decode:     tcp.port == 2002
tshark -r capture.pcapng -O dhs_probel_sw02p -Y dhs_probel_sw02p
```

## 11. Ansible (the exclusive integration / deploy driver — no .ps1)

Inventory ([`../../../ansible/inventory/hosts.ini`](../../../ansible/inventory/hosts.ini))
and playbooks
([`../../../ansible/playbooks/probel-sw02p-integration.yml`](../../../ansible/playbooks/probel-sw02p-integration.yml)).
SW-P-02 has **no separate vendor emulator in-tree** — our own provider IS
the matrix emulator — so the integration play has two tiers: a loopback
tier (`go test -tags integration`, provider + consumer round-trip
in-process, always runs) and an external tier gated on
`PROBEL_SW02P_TEST_HOST` (the same Go body pointed at a live SW-P-02 matrix
on the lab network).

```ini
# inventory/hosts.ini
[producer]
dhs-ubuntu ansible_host=10.100.0.103 ansible_user=root
```

```yaml
# loopback (always) + optional live matrix (playbooks/probel-sw02p-integration.yml)
#   ansible-playbook -i inventory/hosts.ini playbooks/probel-sw02p-integration.yml
#   PROBEL_SW02P_TEST_HOST=10.100.0.42 ansible-playbook -i ... playbooks/probel-sw02p-integration.yml
- hosts: localhost
  connection: local
  tasks:
    - command:
        chdir: "{{ repo_root }}"
        argv: [go, test, -tags, integration, "./internal/probel-sw02p/integration/", -count=1]
      register: sw02p_loopback
      changed_when: false
    - assert: { that: ["'ok' in sw02p_loopback.stdout or 'PASS' in sw02p_loopback.stdout"] }
    - debug: { msg: "{{ sw02p_loopback.stdout_lines + sw02p_loopback.stderr_lines }}" }  # surface logs
```

dhs / go-test logs go to **stderr** → `register` the task and `debug` the
combined `stdout_lines + stderr_lines` to surface them; `changed_when:
false` makes the read-only/idempotent contract explicit (run-twice = same
result). Run `ansible-playbook -v` for Ansible's own echo of task output.

## 12. See also

- [`../CLAUDE.md`](../CLAUDE.md) — wire format, command catalogue, quirks, protect/lock authority ladder
- [`./README.md`](./README.md) · [`./consumer.md`](./consumer.md) · [`./provider.md`](./provider.md) · [`./runbook.md`](./runbook.md)
- [`../../../docs/adr/0025-per-connector-definition-of-done.md`](../../../docs/adr/0025-per-connector-definition-of-done.md)
