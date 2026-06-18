# TSL UMD — verbs & configuration reference

Per-connector verb reference. Same 12-section order as the acp1 reference so
the docs don't drift, **adapted for a tally/UMD push protocol** — sections
that assume an addressable object model (get/set/inc/dec/reset by OID, walk
a tree, matrix crosspoints, idempotent converge) are marked **N/A** with the
reason, because TSL has no request/reply and no queryable state.

All samples below are **real captures** from a producer→consumer loopback run
on this host (Windows / PowerShell), captured 2026-06-18 with a binary built
`go build -o dhs_tsl.exe ./cmd/dhs`, on unique loopback ports (v3.1 UDP 14000,
v4.0 UDP 14004, v5.0 UDP 18901, v5.0 TCP 18902). Wire **hex** bytes are quoted
from the committed, codec-generated
[`../testdata/fixtures/golden_frames.jsonl`](../testdata/fixtures/golden_frames.jsonl)
(clearly attributed) — never hand-written. Wire format lives in
[`../CLAUDE.md`](../CLAUDE.md).

Verbs: consumer `listen`; producer `send`, `serve`. (No get/set/walk —
push protocol; see §4–§5, §9.)

---

## 1. Transport configs

TSL is push-only; the consumer binds a listener and the producer dials a
destination. Per version:

| Version | Transport | Default port | Consumer flag | Producer flag |
|---|---|---|---|---|
| v3.1 | UDP | 4000 | `--bind HOST:PORT` | `--dest HOST:PORT` |
| v4.0 | UDP | 4004 (testbed; spec 4000) | `--bind HOST:PORT` | `--dest HOST:PORT` |
| v5.0 | UDP (≤2048 B) | 8901 | `--bind HOST:PORT` | `--dest HOST:PORT` |
| v5.0 | TCP (DLE/STX wrapper) | 8902 (testbed; spec 8901) | `--bind HOST:PORT --tcp` | `--dest HOST:PORT --tcp` |

`--tcp` is **v5.0 only** — v3.1/v4.0 over TCP is off-spec (see
[`../CLAUDE.md`](../CLAUDE.md) "Out of scope"). Real validation of that
guard:

```
$ dhs consumer tsl-v31 listen --tcp
error: consumer tsl-v31: --tcp is only supported for tsl-v50
```

```
# UDP listener / sender (default)
dhs consumer tsl-v50 listen --bind 0.0.0.0:8901
dhs producer tsl-v50 send  --dest 10.0.0.5:8901 --screen 0 --index 2 --lh red --text "PGM"

# v5.0 TCP with DLE/STX wrapper
dhs consumer tsl-v50 listen --bind 0.0.0.0:8902 --tcp
dhs producer tsl-v50 send  --dest 10.0.0.5:8902 --tcp --screen 0 --index 2 --rh amber
```

The producer takes a **repeatable** `--dest` (push to several MVs at once)
and an optional `--bind` for the local egress source (default `0.0.0.0:0`
= ephemeral). For UDP at least one `--dest` is required:

```
$ dhs producer tsl-v40 send
error: producer tsl-v40 send: at least one --dest is required for UDP
```

## 2. Controllers & redundancy

**Default = one tally source pushing to one or more multiviewers.** TSL has
no client identity on the wire and no session — redundancy is an
**app/deployment** concern, not a protocol one:

- **Single source (default):** one producer ↔ one or more MV listeners
  (`--dest` repeatable).
- **Redundant sources:** run **2+ producer instances** (e.g. main + backup
  switcher), each pushing the same frames; the MV listener consumes whatever
  arrives. There is no arbitration on the wire — last frame wins per display.
- **Fan-out:** one producer, many `--dest` — every MV gets an identical copy.

```
# one source → three multiviewers
dhs producer tsl-v50 serve --dest mv-1:8901 --dest mv-2:8901 --dest mv-3:8901 --refresh 1s \
  --screen 0 --index 2 --lh red --text "PGM"
```

UDP push is connectionless: a producer that loses its MV silently keeps
sending. v5.0 TCP adds OS-layer SO_KEEPALIVE (30 s, both ends) to detect a
dead socket — TSL v5 defines no app-layer heartbeat.

## 3. Logging & severity

Logs go to **stderr**, decoded frames to **stdout** (so `2>` captures logs,
`1>` captures the decode). Both sides log via `log/slog` at info level by
default. There is no per-invocation `--log-level` flag wired for TSL today
(unlike acp1) — severity is fixed at info; this is the one logging knob the
TSL CLI does **not** expose yet.

```
dhs consumer tsl-v50 listen --bind 0.0.0.0:8901 1>frames.txt 2>run.log   # frames→file, logs→file
```

## 4. info / walk

**N/A — push protocol.** TSL has no device-info request and no object tree to
walk: the consumer cannot query the producer, it can only listen for pushed
frames. The closest equivalent is `listen` (§8): every frame the producer
sends is decoded and printed with full field labels. There is no `info` or
`walk` verb.

## 5. get / set / inc / dec / reset

**N/A — push protocol.** TSL has no addressable objects and no
request/reply, so there is nothing to `get`, `set`, `inc`, `dec`, or
`reset`. The producer *encodes* tally/UMD state from flags and pushes it;
the consumer *decodes* it. State is expressed by the producer's `send` flags
(`--tally1..4`, `--lh/--text-tally/--rh`, `--brightness`, `--text`,
`--display-left/--display-right`), documented in §8.

## 6. export / import

**N/A — push protocol.** There is no walked tree or device snapshot to
export to json/yaml/csv, and nothing to import back. The replay analog of an
export is the committed
[`../testdata/fixtures/golden_frames.jsonl`](../testdata/fixtures/golden_frames.jsonl)
— the exact on-wire bytes per (version, transport), codec-generated. Quoted
real bytes (v3.1, UDP, from the committed golden fixture):

```json
{"version":"v3.1","transport":"udp","description":"addr=7 tally1+tally4 brightness=full text=\"PGM LIVE\"","hex":"873950474d204c4956452020202020202020"}
```

Decoding `87 39 50474d204c495645 2020202020202020`: HEADER `0x87` = addr 7
(`0x80 | 7`), CTRL `0x39` = tally1+tally4 + brightness full (bits
0,3,4,5), DATA = `"PGM LIVE"` space-padded to 16. This is exactly what the
live `listen` capture decoded in §8.

## 7. reports — tree ASCII & PlantUML mindmap

**N/A — push protocol.** A TSL frame is flat (a handful of tally bits + one
label, or a list of DMSGs) — there is no hierarchy to render as an ASCII
tree or PlantUML mindmap. The `listen` output (§8) already prints every
field per frame.

## 8. listen — the decode verb (consumer)

`listen` binds a UDP (or v5.0 TCP) socket and prints every decoded frame
until Ctrl-C. This is the consumer's only verb.

### v3.1 — 4 binary tallies + brightness, no colour

Live loopback capture (producer drove addr 7, tally1+tally4, full
brightness, `"PGM LIVE"` to UDP 14000):

```
$ dhs consumer tsl-v31 listen --bind 127.0.0.1:14000
tsl-v31 consumer listening on udp://127.0.0.1:14000 (Ctrl-C to stop)
v3.1  remote=127.0.0.1:52223  addr=7  T1=ON T2=off T3=off T4=ON  brightness=full  UMD="PGM LIVE        "
```
```
# producer that drove it:
dhs producer tsl-v31 send --dest 127.0.0.1:14000 --addr 7 --tally1 --tally4 --brightness 3 --text "PGM LIVE"
```

The decoded line matches the committed golden frame hex
`873950474d204c4956452020202020202020` (§6) byte-for-byte — the doc is real.

### v4.0 — v3.1 tallies + XDATA (L/R display × LH/Text/RH colour)

Live loopback capture (UDP 14004):

```
$ dhs consumer tsl-v40 listen --bind 127.0.0.1:14004
tsl-v40 consumer listening on udp://127.0.0.1:14004 (Ctrl-C to stop)
v4.0  remote=127.0.0.1:49309  addr=11  T1=off T2=off T3=off T4=off  brightness=full  UMD="CAM 1 ISO       "
      DisplayL  LH=red Text=green RH=amber
      DisplayR  LH=green Text=red RH=off
```
```
dhs producer tsl-v40 send --dest 127.0.0.1:14004 --addr 11 --text "CAM 1 ISO" --brightness 3 \
  --display-left red:green:amber --display-right green:red:off
```

Matches committed golden frame `8b3543414d20312049534f...33021b24` (addr 11,
displayL red/green/amber, displayR green/red/off, `"CAM 1 ISO"`).

### v5.0 — single-DMSG (LH/Text/RH colour + brightness, per display)

Live loopback capture (UDP 18901):

```
$ dhs consumer tsl-v50 listen --bind 127.0.0.1:18901
tsl-v50 consumer listening on udp://127.0.0.1:18901 (Ctrl-C to stop)
v5.0  remote=127.0.0.1:59614  screen=1  charset=ASCII  dmsgs=1
      display=11  LH=red  Text=green  RH=amber  brightness=full  UMD="CAM 1"
```
```
dhs producer tsl-v50 send --dest 127.0.0.1:18901 --screen 1 --index 11 \
  --lh red --text-tally green --rh amber --text "CAM 1"
```

Matches committed golden frame `0f00000001000b00db00050043414d2031`
(PBC=15, ver 0, flags 0, screen 1, DMSG index 11, CONTROL `0x00db`,
len 5, `"CAM 1"`).

### v5.0 — multi-DMSG group (Miranda "Group display messages")

Live loopback capture — one packet, three DMSGs:

```
v5.0  remote=127.0.0.1:61443  screen=0  charset=ASCII  dmsgs=3
      display=2  LH=red  Text=off  RH=off  brightness=full  UMD="PGM"
      display=3  LH=off  Text=green  RH=off  brightness=full  UMD="PVW"
      display=4  LH=off  Text=off  RH=amber  brightness=full  UMD="ISO"
```
```
dhs producer tsl-v50 send --dest 127.0.0.1:18901 --screen 0 \
  --dmsg "index=2,lh=red,rh=off,brightness=3,umd=PGM" \
  --dmsg "index=3,text-tally=green,umd=PVW" \
  --dmsg "index=4,rh=amber,umd=ISO"
```

`--dmsg` is repeatable and **overrides** the singular `--index/--lh/...`
flags when present.

### v5.0 — UTF-16LE label + broadcast screen

Live loopback captures showing the `charset=UTF-16LE` flag (FLAGS bit 0) and
the broadcast `screen=65535` (0xFFFF):

```
v5.0  remote=127.0.0.1:63281  screen=0  charset=UTF-16LE  dmsgs=1
      display=5  LH=red  Text=off  RH=off  brightness=full  UMD="CAMERA"
v5.0  remote=127.0.0.1:63282  screen=65535  charset=ASCII  dmsgs=1
      display=0  LH=off  Text=off  RH=green  brightness=full  UMD="ALL"
```
```
dhs producer tsl-v50 send --dest 127.0.0.1:18901 --screen 0 --utf16 --index 5 --lh red --text "CAMERA"
dhs producer tsl-v50 send --dest 127.0.0.1:18901 --broadcast --index 0 --rh green --text "ALL"
```

### v5.0 — TCP with DLE/STX wrapper

Live loopback capture (TCP 18902):

```
$ dhs consumer tsl-v50 listen --bind 127.0.0.1:18902 --tcp
tsl-v50 consumer listening on tcp://127.0.0.1:18902 (Ctrl-C to stop)
v5.0  remote=127.0.0.1:51532  screen=0  charset=ASCII  dmsgs=1
      display=2  LH=off  Text=off  RH=amber  brightness=full  UMD="ISO 2"
```
```
dhs producer tsl-v50 send --dest 127.0.0.1:18902 --tcp --screen 0 --index 2 --rh amber --text "ISO 2"
```

The committed golden TCP frame
`fe020f000000fefefefe01fefed00005005354554646` (from the fixture)
exercises the harder wire detail: `FE 02` DLE/STX opener, SCREEN `0xFEFE`
byte-stuffed to `FE FE FE FE`, INDEX `0xFE01` → `FE FE 01`.

## 9. ensure() — idempotent converge

**N/A — push protocol.** `ensure` (ADR-0007) converges an addressable
object to a target value and reports whether it changed — TSL has no
addressable object and no read-back, so there is nothing to converge or
diff against. The producer's `serve --refresh` re-emits the *same* frame
on a loop (a keep-alive refresh, not a converge); it is unconditional, not
idempotent-by-comparison. See §8 (serve) and the runbook.

## 10. Wireshark

Dissector: [`../wireshark/dhs_tsl.lua`](../wireshark/dhs_tsl.lua) — decodes
v3.1 / v4.0 over UDP, v5.0 over UDP, and v5.0 over TCP (DLE/STX wrapper +
0xFE byte-unstuffing), with a per-frame Info column and an expert warning on
reserved-bit / unknown-version deviations.

| OS | Plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

Default ports decoded (overridable in the dissector preferences):
v3.1 UDP 4000, v4.0 UDP 4004, v5.0 UDP 8901, v5.0 TCP 8902.

```
# copy the dissector, then filter on the dhs proto:
#   display filter:  dhs_tsl
#   port decode:     udp.port in {4000 4004 8901} || tcp.port == 8902
tshark -r capture.pcapng -O dhs_tsl -Y dhs_tsl
```

## 11. Ansible (the exclusive integration / deploy driver — no .ps1)

Integration is driven by Ansible:
[`../../../ansible/playbooks/tsl-integration.yml`](../../../ansible/playbooks/tsl-integration.yml).
Because TSL is push-only and the Go integration body
([`../integration/`](../integration/)) is **loopback-only** today (our own
provider pushes to our own consumer in-process across all three versions),
the play has two tiers:

1. **Loopback (always):** runs `go test -tags integration ./internal/tsl/integration/`
   — provider+consumer round-trip for v3.1/v4.0/v5.0 plus the codec
   golden-frame drift-guard. `changed_when: false` ⇒ run-twice = same
   result (ADR-0025 idempotency).
2. **External (gated on `TSL_TEST_HOST`):** **skipped by design** today —
   the Go tests have no external dial path, so the play only prints a
   "not yet supported" notice when `TSL_TEST_HOST` is set, rather than
   silently re-running loopback. The upgrade path (Miranda IP Emulator /
   Lawo VSM on the lab network) is documented inline in the playbook.

```
# loopback only (the supported path):
ansible-playbook -i inventory/hosts.ini playbooks/tsl-integration.yml

# with TSL_TEST_HOST set — prints the not-yet-supported notice:
TSL_TEST_HOST=10.100.0.42 ansible-playbook -i inventory/hosts.ini playbooks/tsl-integration.yml
```

dhs / go-test logs go to **stderr** → the play `register`s the task and
surfaces `stdout_lines + stderr_lines` via `debug` so a failing run shows
the test output inline under `ansible-playbook -v`.

## 12. See also

- [`../CLAUDE.md`](../CLAUDE.md) — wire format (byte-exact), compliance events, quirks
- [`./README.md`](./README.md) · [`./consumer.md`](./consumer.md) · [`./provider.md`](./provider.md) · [`./runbook.md`](./runbook.md)
- [`../testdata/fixtures/golden_frames.jsonl`](../testdata/fixtures/golden_frames.jsonl) — real wire bytes per (version, transport)
- [`../../../docs/adr/0025-per-connector-definition-of-done.md`](../../../docs/adr/0025-per-connector-definition-of-done.md)
