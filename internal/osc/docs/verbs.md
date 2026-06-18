# OSC — verbs & configuration reference

Per-connector verb reference, same 12-section order as the acp1 / probel-sw02p
docs so the set doesn't drift. **OSC is a message/push protocol** — it has no
Tree/DM, no addressable object catalogue, and no matrix — so the Tree/DM-only
sections (`get`/`set`/`inc`/`dec`/`reset` by OID, crosspoints, tree walk,
export/import, ensure) are marked **N/A** with the reason, not invented.

All samples below are **real**: either **live-captured** on this host with a
purpose-built binary over loopback on unique ports (UDP 19000, TCP-LP 19001,
TCP-SLIP 19002, plus 19005–19009), or **quoted from a committed codec-produced
fixture** under [`../testdata/fixtures/`](../testdata/fixtures/) and clearly
attributed. No wire bytes are hand-written.

Wire format lives in [`../CLAUDE.md`](../CLAUDE.md); this file is the operator
how-to. Verbs that exist: consumer `watch`; producer `send` `fader` `serve`.

Spec: OSC 1.0 <https://opensoundcontrol.org/spec-1_0.html> · OSC 1.1
<https://opensoundcontrol.org/spec-1_1.html> (not `osc.org`, which is
the Orlando Science Center).

---

## 1. Transport configs

OSC speaks three transports (see [`../CLAUDE.md`](../CLAUDE.md) "Wire layer"):

| Kind | Flag value | Default port | Version | Notes |
|---|---|---|---|---|
| UDP | `udp` | 8000 | both | one datagram = one packet; broadcast-friendly; SO_REUSEADDR for multi-listener |
| TCP length-prefix | `tcp-len` | 8000 | **osc-v10 only** | int32 BE size + packet (OSC 1.0) |
| TCP SLIP | `tcp-slip` | 8001 | **osc-v11 only** | RFC 1055 double-END framing (OSC 1.1) |

The version↔framing pairing is enforced at the CLI. Real captured guard errors:

```
$ dhs_osc.exe producer osc-v10 send --to 127.0.0.1:19002 --transport tcp-slip --address /x --types T
error: transport=tcp-slip requires --protocol osc-v11 (SLIP is OSC 1.1 only)

$ dhs_osc.exe producer osc-v11 send --to 127.0.0.1:19001 --transport tcp-len --address /x --types i 1
error: transport=tcp-len requires --protocol osc-v10 (length-prefix is OSC 1.0 only)
```

Consumer binds with `--listen transport:port`; producer sends with
`--transport KIND --to host:port` or binds a logger with `--bind transport:port`.

```
dhs consumer osc-v10 watch --listen udp:8000
dhs producer osc-v10 send  --to 127.0.0.1:8000 --transport udp --address /a --types i 1
dhs producer osc-v11 serve --bind tcp-slip:8001
```

## 2. Controllers & redundancy

OSC has **no client identity on the wire** and no session — it is symmetric and
connectionless in spirit. "Redundancy" is an app/deployment concern:

- **Single peer (default):** one producer `send`s, one consumer `watch`es.
- **Fan-out:** one producer `--to` each destination, or UDP broadcast/subnet
  destinations (the codec/provider sets SO_BROADCAST on the egress socket).
- **Multi-listener on one host:** UDP consumers set SO_REUSEADDR so several dhs
  instances share a port (matches the ACP1 / TSL multi-listener contract). On
  platforms where SO_REUSEPORT is not active (e.g. Windows) delivery to a shared
  UDP port may land on only one binder — the integration suite accepts partial
  reception (`TestV10_UDP_MultiInstance_SamePort`).

> **N/A — redundant controller pairing.** ACP1's "2+ producer instances, one per
> NIC/controller" model does not apply: OSC has no controller concept and no
> announce-vs-transaction split to enforce TCP on. Fan-out is the only model.

## 3. Logging & severity

Logs go to **stderr**, decoded data to **stdout** (so `2>` captures logs, `1>`
captures the decoded stream). The osc CLI wires a fixed `slog` text handler at
`info` level on stderr for `watch` / `send` / `serve`; `fader` discards logs and
prints only its perf summary to stderr. There is **no `--log-level` flag** on the
osc verbs today (unlike acp1).

Real captured stderr banner (UDP watch):

```
osc-v10 watching udp:19000 (pattern="") — Ctrl-C to stop
```

> **N/A — `--log-level` / `--log-format json`.** Not wired on osc verbs; severity
> is fixed. Documented as absent rather than invented.

## 4. info / walk

> **N/A — `info` and `walk` do not exist for OSC.** OSC has no device-info reply
> and no walkable object tree: a peer never advertises its address space, and
> there is no `getObject`/root-count mechanism. Discovery is **passive** — bind a
> port and observe which addresses arrive. The equivalent is `watch` (§8).

## 5. get / set / inc / dec / reset

> **N/A — there is no addressable object model.** OSC carries a free-form address
> string + typed args per message; there is no OID, no value-read request, no
> confirmed-echo, and no step/default semantics. A producer *pushes* a value with
> `send` (§ producer.md); it never "reads" one. ACP1's `get/set/inc/dec/reset`
> (setValue/setIncValue/setDecValue/setDefValue) have no OSC analogue.

## 6. export / import

> **N/A — nothing to export.** OSC has no tree/DM snapshot to serialize to
> json/yaml/csv and no importable object set. The committed artifacts are golden
> **wire packets** (`../testdata/fixtures/*.bin`), not a canonical tree — see §11
> and [`../testdata/fixtures/README.md`](../testdata/fixtures/README.md).

## 7. reports — tree ASCII & PlantUML mindmap

> **N/A — no tree to render.** The `tree` verb requires a walkable object
> hierarchy; OSC has none. The closest report is the per-message decoded line
> emitted by `watch` (§8), which matches the Wireshark Info column field-for-field.

## 8. watch (the only consumer verb)

`watch` binds a port and prints every received packet as
`[transport] /address ,tags arg1 arg2 …` — the same shape as the
`dhs_osc.lua` dissector Info column, so terminal and Wireshark compare
line-for-line. `--pattern` filters by OSC address pattern (default: all).

**Live-captured (UDP, port 19000, osc-v10):**

```
# consumer
$ dhs_osc.exe consumer osc-v10 watch --listen udp:19000
# producer drove two messages:
#   send --to 127.0.0.1:19000 --transport udp --address /mixer/fader1 --types fsi 0.75 PGM 42
#   send --to 127.0.0.1:19000 --transport udp --address /color       --types r FF8800FF
[udp      ] /mixer/fader1  ,fsi  0.75 "PGM" 42
[udp      ] /color  ,r  #ff8800ff
```

**Live-captured (TCP length-prefix, port 19001, osc-v10):**

```
[tcp-len  ] /ch/1/gain  ,if  1 -6
[tcp-len  ] /q/go  ,s  "START"
```

**Live-captured (TCP SLIP, port 19002, osc-v11 — payload-less tags + array):**

```
# send --transport tcp-slip --address /q/go   --types iTFNI 7
# send --transport tcp-slip --address /array  --types "i[ii]" 1 10 20
[tcp-slip ] /q/go  ,iTFNI  7 T F N I
[tcp-slip ] /array  ,i[ii]  1 [ 10 20 ]
```

The `T F N I` print as zero-byte payload-less args and `[ … ]` are array markers
— the OSC 1.1 additions, captured live, never fabricated.

### Address-pattern filtering (real OSC wildcard semantics)

`--pattern` uses OSC 1.0 wildcard syntax: `*` `?` `[abc]` `{a,b}`. Per spec, `*`
matches a run **within one address part** (it does **not** cross `/`). Captured:

```
# watch --listen udp:19006 --pattern "/mixer/*/gain"
# driven with /mixer/ch3/gain (matches) and /other/skip (dropped):
[udp      ] /mixer/ch3/gain  ,f  3.5
```

`/other/skip` produced no line — the pattern correctly filtered it out.

## 9. ensure() — idempotent converge

> **N/A — no converge target.** `ensure` reads current value, compares to
> `--value`, and writes if different. OSC cannot read a value back (no
> get/reply), so there is no idempotent converge. The idempotency that **does**
> apply to OSC is socket-level: each integration test binds an ephemeral socket
> and tears it down, so the Ansible play's run-twice = same-result (§11). Pushing
> the same message twice simply sends two identical datagrams.

## 10. Wireshark

Dissector: [`../wireshark/dhs_osc.lua`](../wireshark/dhs_osc.lua) —
`Proto("dhs_osc", "OSC (dhs — Open Sound Control 1.0 + 1.1)")`. Decodes UDP +
TCP length-prefix (1.0) + TCP SLIP (1.1), every type tag including 1.1
payload-less + array markers, recursive bundle decode, with a per-message Info
column of `address + type-tag + arg count`.

| OS | Plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

```
# display filter:  dhs_osc
# field filters:   dhs_osc.address, dhs_osc.type_tag, dhs_osc.arg_count
tshark -r capture.pcapng -O dhs_osc -Y dhs_osc
```

Dissector-replay regression
([`../integration/dissector_replay_test.go`](../integration/dissector_replay_test.go))
runs `tshark -X lua_script:internal/osc/wireshark/dhs_osc.lua` over the committed
multi-frame capture [`../testdata/scenarios/battery/capture.pcapng`](../testdata/scenarios/battery/capture.pcapng).

## 11. Fixtures, Ansible & integration

### Golden packet fixtures (real codec output)

[`../testdata/fixtures/*.bin`](../testdata/fixtures/) are produced by the repo's
own codec encode path (`codec.Message.Encode` / `codec.Bundle.Encode`) — never
hand-written. `TestOSCFixtures` re-encodes each and asserts byte-identity, so a
fixture can never drift from the codec. Real bytes, dumped from the committed
files on this host:

```
# message_v11_payloadless.bin (20 bytes) — quoted from the committed fixture
2f712f676f0000002c54464e4969000000000007
#  └/q/go\0\0\0─┘ └,TFNIi\0\0──┘ └int32 7─┘
#  address (8B)   type-tag (8B)   arg payload: only the 'i' has bytes;
#                                  T/F/N/I are payload-less (OSC 1.1)

# bundle_timetag.bin (68 bytes) — quoted from the committed fixture
2362756e646c65000000000100000000...  ("#bundle\0" + NTP timetag 0x0000000100000000 + elements)

# message_all_tags.bin (92 bytes) — quoted from the committed fixture
2f6d697865722f6661646572310000002c696673626864745363726d00000000...
#  /mixer/fader1 + type-tag string ",ifsbhdtScrm"
```

`message_v11_payloadless.bin` proves the 1.1 contract end-to-end: the type-tag
string `,TFNIi` is encoded but **only the trailing `i` contributes argument
bytes** (`00000007`) — `T`/`F`/`N`/`I` are zero-payload. This is the same packet
the live `watch` capture in §8 decoded as `,iTFNI  7 T F N I`.

### Ansible (exclusive integration driver — no .ps1)

[`../../../ansible/playbooks/osc-integration.yml`](../../../ansible/playbooks/osc-integration.yml)
has two tiers:

```
# loopback only (CI / agent, no node required):
ansible-playbook -i inventory/hosts.ini playbooks/osc-integration.yml

# plus the osc.js cross-impl oracle (install node + osc.js first):
ansible-playbook -i inventory/hosts.ini playbooks/osc-integration.yml -e osc_run_oscjs=true
```

The oracle-per-tier rule (never validate our consumer with our own provider) is
satisfied by a **cross-implementation oracle** — the osc.js reference peer in
[`../assets/test-harness`](../assets/test-harness) — not by a lab host. The
loopback tier runs our provider ↔ our consumer over real localhost UDP / TCP-LP /
TCP-SLIP sockets (`-run TestV10|TestV11|TestMultiPeer`); each test binds an
ephemeral socket and tears it down, so run-twice = same result (ADR-0025
idempotency, `changed_when: false`).

> **Note — `OSC_TEST_HOST`.** No osc integration test reads `OSC_TEST_HOST` /
> `OSC_TEST_PORT` today. They are reserved for a future live-peer test (QLab /
> X32 / Companion) and passed through to the external task environment, but are
> deliberately **not** used to gate anything yet.

## 12. See also

- [`../CLAUDE.md`](../CLAUDE.md) — wire format, type tags, framing, quirks
- [`./README.md`](./README.md) · [`./consumer.md`](./consumer.md) · [`./provider.md`](./provider.md) · [`./runbook.md`](./runbook.md)
- [`../../../docs/adr/0025-per-connector-definition-of-done.md`](../../../docs/adr/0025-per-connector-definition-of-done.md)
- OSC 1.0 / 1.1 specs: <https://opensoundcontrol.org/spec-1_0.html> · <https://opensoundcontrol.org/spec-1_1.html>
</content>
