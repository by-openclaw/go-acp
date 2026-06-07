# ACP2 — operational runbook

Quick-reference card for operators. For wire-format detail see
[CLAUDE.md](../CLAUDE.md); for in-depth consumer behaviour see
[consumer.md](consumer.md).

---

## Transport matrix

| Mode | Port | Notes |
|---|---:|---|
| AN2/TCP | 2072 | **Only transport.** ACP2 has no UDP / TCP-direct. AN2 proto byte = 2. AN2 framer carries every request, reply, and announce. |

The consumer always uses AN2/TCP; no `--transport` selection. Default `--port 2072`.

## Verb reference

### Consumer

| Verb | What it does | Common flags | Cache effect |
|---|---|---|---|
| `dhs consumer acp2 info <ip>` | Read AN2 + ACP2 versions, slot count | `--port`, `--timeout` | none |
| `dhs consumer acp2 walk <ip>` | Discover one slot's full DM | `--slot N` or `--all`, `--path BOARD.Stream`, `--filter foo`, `--capture out.jsonl` | writes `.cache/dm/<CardName>@<HardwareVersion>.json` (multi-slot, identity-keyed) |
| `dhs consumer acp2 get <ip>` | Read one property | `--slot N --id 70232`, `--capture out.jsonl` | reads identity-keyed cache for label resolution |
| `dhs consumer acp2 set <ip>` | Write one property | same as `get` plus `--value …` | none |
| `dhs consumer acp2 watch <ip>` | Subscribe to announces | `--slot N`, `--id I`, `--no-walk` | hot-loads `.cache/dm/<identity>.json` before Subscribe → labels from frame 1 |

### Provider

| Verb | What it does | Required flags |
|---|---|---|
| `dhs producer acp2 serve --tree fixture.json --port 2072 --bind <ip>` | Serve AN2/TCP from one fixture | `--tree`, `--port`, optional `--bind` |

Multi-client is inherent: each AN2 session is independent. Announces are fanned to every session that has called `EnableProtocolEvents([2])`.

## Provider — broadcasts gating (announce flag)

| Layer | Where it lives | How to toggle |
|---|---|---|
| **1. Provider broadcasts gate** | tree-level on/off (mirrored from ACP1 §p.20) | flip via `dhs consumer acp2 set <ip> --slot 0 --label Broadcasts --value 1` |
| **2. Per-AN2-session opt-in** | per consumer connection | sent automatically by the consumer plugin on `watch`: `AN2 EnableProtocolEvents([2])` |

Both layers must be true for a consumer to see announces. The provider drops announces silently when the gate is OFF.

## DM cache contract

ACP2 uses **identity-keyed** caching exclusively (DHS 2016 MasterView model — see internal/acp2/CLAUDE.md):

```
.cache/dm/<CardName>@<HardwareVersion>.json   ← single file per device, multi-slot
```

- Identity = labels `Card Name` + `Hardware Version` on slot 0 (sub-second probe at watch start). Real Neuron 10.41.40.4 → `SHPRM1@0.7`.
- Walking any slot of the frame **accumulates** into the same file via merge — slot 0 + slot 1 + slot N share one MasterView.
- The cache survives re-cabling and IP changes; only a Card swap or firmware bump (Hardware Version change) invalidates it.
- File format: flat JSON via stdlib `json.Encoder` (NOT `export.WriteJSON` — that hierarchical writer drops `Object.Meta` which is load-bearing for type info on round-trip).
- `watch` start: identity probe → `LoadByIdentity` → seed the in-memory `WalkedTree` for every cached slot **before** Subscribe fires → enum labels resolve from the very first announce.

## Common flows

### First-time bring-up against the producer emulator

```
dhs consumer acp2 info  10.100.0.103 --port 2072
dhs consumer acp2 walk  10.100.0.103 --port 2072 --slot 0   # primes slot 0 in dm/<id>.json
dhs consumer acp2 walk  10.100.0.103 --port 2072 --slot 1   # merges slot 1 into the same file
dhs consumer acp2 watch 10.100.0.103 --port 2072 --slot 1   # hot-loads, labels frame 1
```

After both walks, the file contains every slot you walked. Restart `watch` any time — labels resolve immediately.

### Inspect a single object

```
dhs consumer acp2 get 10.100.0.103 --port 2072 --slot 1 --id 70232 --capture out.jsonl
# → value = "Stream 16" (enum idx 8)
# → out.jsonl contains the raw AN2 frames for replay / spec audit
```

### Bring up an emulator on a VIP alongside production traffic

```
dhs producer acp2 serve --tree neuron-fixture.json --port 2072 --bind 10.100.0.200
```

### Verify cache state

```
type .cache\dm\SHPRM1@0.7.json | findstr "\"slot\":"
# expect "slot": 0 AND "slot": 1
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `watch` shows `idx N` for enums, no labels | DM cache file missing the watched slot, OR identity probe failed | run `walk --slot N` once; then check `.cache/dm/<identity>.json` exists with that slot |
| `loaded N labels from cache` line still appears | running an old binary (pre-#355) — that path is disabled for ACP2 | rebuild: `go build -o bin/dhs.exe ./cmd/dhs` |
| `walk --slot 1` overwrites `walk --slot 0`'s data | running an old binary (pre-#356 round-trip fix) | rebuild + delete the corrupt file: `del .cache\dm\<id>.json` |
| Producer emits no announces to any consumer | Broadcasts gate is OFF on the tree | `set --label Broadcasts --value 1` |
| One consumer sees no announces while another does | that consumer didn't send `EnableProtocolEvents([2])` | it's automatic on `watch`; check the binary version |
| `watch --slots 0` leaks slot 1 announces | multi-slot lists don't filter (Subscribe takes a single slot) | use `--slot 0` (singular) for strict filter |
| Cache says `<Card>@<HwVer>` for a different device than expected | producer fixture identity differs from real device | each device has its own file by design — no cross-pollination |

## Pointers

- Wire format + spec audit: [CLAUDE.md](../CLAUDE.md)
- Deep-dive consumer doc: [consumer.md](consumer.md)
- Wireshark dissector: [../wireshark/dhs_acpv2.lua](../wireshark/dhs_acpv2.lua)
- Spec docx: [../assets/acp2_protocol.docx](../assets/acp2_protocol.docx)
- AN2 transport spec: [../assets/an2_protocol.pdf](../assets/an2_protocol.pdf)
