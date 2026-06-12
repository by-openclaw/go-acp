# ACP1 — operational runbook

Quick-reference card for operators. For wire-format detail see
[CLAUDE.md](../CLAUDE.md); for in-depth consumer behaviour see
[consumer.md](consumer.md).

---

## Transport matrix

| Mode | Port | Notes |
|---|---:|---|
| UDP direct (Mode A) | 2071 | One datagram = one ACP1 message. Stateless. Simulator + most legacy fixtures. |
| TCP direct (Mode B) | 2071 | u32 BE MLEN prefix per message (MLEN > 8). |
| AN2/TCP (Mode C) | 2072 | AN2 dlen frame. Required for receiving announces (per-session `EnableProtocolEvents([1])`). |

The consumer auto-detects the transport with `--transport tcp` / `--transport udp` / `--transport an2`. Default: UDP. Receiving announces requires AN2.

## Verb reference

### Consumer

| Verb | What it does | Common flags | Cache effect |
|---|---|---|---|
| `dhs consumer acp1 info <ip>` | Read identity + slot count | `--transport`, `--port`, `--timeout` | none |
| `dhs consumer acp1 walk <ip>` | Discover one slot's full DM | `--slot N` or `--all`, `--path BOARD`, `--filter foo` | writes `.cache/devices/<ip>/slot_<n>.json` (IP-keyed; ACP1 has no identity probe) |
| `dhs consumer acp1 get <ip>` | Read one property | `--slot N --group control --id 4` (or `--label`) | reads cached labels for resolution |
| `dhs consumer acp1 set <ip>` | Write one property | same as `get` plus `--value …` | none (live device only) |
| `dhs consumer acp1 inc <ip>` | Increment by step (setIncValue) | same as `get` (no `--value`) | none (live device only) |
| `dhs consumer acp1 dec <ip>` | Decrement by step (setDecValue) | same as `get` (no `--value`) | none (live device only) |
| `dhs consumer acp1 reset <ip>` | Reset to default (setDefValue) | same as `get` (no `--value`) | none (live device only) |
| `dhs consumer acp1 watch <ip>` | Subscribe to announces | `--slot N`, `--group G`, `--label L`, `--id I`, `--no-walk`, `--auto-walk-on-plug` | reads `slot_<n>.json` for label resolution while live tree fills |
| `dhs consumer acp1 export <ip> --format json|yaml|csv` | Dump tree to a file | `--out path.json` | none |
| `dhs consumer acp1 import <file>` | Replay a captured tree | offline | none |

### Provider

| Verb | What it does | Required flags |
|---|---|---|
| `dhs producer acp1 serve --tree fixture.json --port 2071 --bind <ip>` | Serve UDP + TCP + AN2 from one fixture | `--tree`, `--port`, optional `--bind` (VIP) |

The provider listens on **all three transports simultaneously** on the same `--port` (UDP + TCP-direct) plus `--port + 1` for AN2/TCP — i.e. `--port 2071` exposes UDP/TCP at 2071 and AN2/TCP at 2072. Multi-client is inherent: each TCP/AN2 connection is an independent session.

## Provider — broadcasts gating (announce flag)

Two layers; both must be true for a consumer to receive an announce.

| Layer | Where it lives | How to toggle |
|---|---|---|
| **1. Provider broadcasts gate** | `Identity.Broadcasts` object on the device's tree (per spec p.20) | the customer flips it via `dhs consumer acp1 set <ip> --slot 0 --group identity --label Broadcasts --value 1` |
| **2. Per-AN2-session opt-in** | Per consumer connection | the consumer plugin sends `AN2 EnableProtocolEvents([1])` after connect; happens automatically on `watch` |

If the gate (layer 1) is OFF, the provider emits no announces to anyone. If layer 2 is missing, that one consumer sees nothing while others may still receive. UDP/TCP-direct consumers never receive announces by design — only AN2 sessions do.

## DM cache contract

ACP1 keeps **IP-keyed** caching:

```
.cache/devices/<ip>/slot_<n>.json
```

- `walk` writes the file on every successful walk (one per slot).
- `watch` reads the file at startup for instant label/unit resolution while the background walk completes.
- ACP1 has no identity probe (Card Name + Hardware Version aren't at fixed labels across products), so the cache cannot survive an IP change. Re-cabling means re-walking.
- The IP-keyed cache stores values stripped (`Value: zero`); only labels + types + ranges persist.

## Common flows

### First-time bring-up

```
dhs consumer acp1 info  10.6.239.113
dhs consumer acp1 walk  10.6.239.113 --all       # populates cache for every present slot
dhs consumer acp1 watch 10.6.239.113 --slot 0    # labels resolve from frame 1
```

### Continuous monitoring across reboot

```
dhs consumer acp1 watch 10.6.239.113 --slots all     # all present slots, labels from cache
```

### Bring up an emulator alongside production traffic

```
dhs producer acp1 serve --tree neuron-fixture.json --port 2071 --bind 10.6.239.200
```

`--bind <vip>` pins the listener AND the broadcast source IP, so multi-instance emulators on the same host appear as distinct From: addresses (ref #263).

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `watch` shows raw IDs, no labels | No prior walk for this IP | `walk --slot N` or `walk --all` first |
| `watch` shows nothing despite live activity on the device | Broadcasts gate OFF on the provider, OR consumer not on AN2 | flip Broadcasts via `set …Broadcasts 1`, or pass `--transport an2` |
| `watch --slots 0` also shows slot 1 announces | Multi-slot list filter is permissive (Subscribe accepts only one slot) | use `--slot 0` (singular) for strict slot filter |
| Cache stale after re-cabling | IP-keyed cache invalidated by Card Name mismatch on next walk | the validator detects this — `walk` again |

## Pointers

- Wire format: [CLAUDE.md](../CLAUDE.md)
- Deep-dive consumer doc: [consumer.md](consumer.md)
- Wireshark dissector: [../wireshark/dhs_acpv1.lua](../wireshark/dhs_acpv1.lua)
- Spec PDF: [../assets/AXON-ACP_v1_4.pdf](../assets/AXON-ACP_v1_4.pdf)
