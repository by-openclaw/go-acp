# OSC — Open Sound Control 1.0 + 1.1

OSC is a **message / push** protocol, not a Tree/DM or matrix connector. Any
peer may send to any peer; there is no client/server handshake, no device tree
to walk, and no addressable object catalogue. The dhs docs set is therefore
adapted: the verbs are `watch` (consumer) and `send` / `fader` / `serve`
(producer), and the Tree/DM-only sections (`get`/`set`/`inc`/`dec`/`reset` by
OID, matrix crosspoints, tree walk, export/import) are marked **N/A** with the
reason throughout.

| Role | Doc | Status |
|---|---|---|
| **Verbs & config reference** | [verbs.md](verbs.md) | every verb + transport/pattern/wireshark/ansible/fixtures, with real captured samples |
| Operator runbook | [runbook.md](runbook.md) | ✓ shipping |
| Consumer | [consumer.md](consumer.md) | ✓ shipping — `watch` listener/monitor over UDP / TCP-LP / TCP-SLIP |
| Provider | [provider.md](provider.md) | ✓ shipping — `send` / `fader` / `serve` push sender |

## Registry entries (Pattern A — one entry per wire-incompatible version)

| Registry name | Wire version | Transport surface |
|---|---|---|
| `osc-v10` | OSC 1.0 | UDP (primary) + TCP int32 length-prefix (`tcp-len`) |
| `osc-v11` | OSC 1.1 | UDP + TCP SLIP (`tcp-slip`, RFC 1055 double-END); adds `T`/`F`/`N`/`I` + `[`/`]` array markers |

Registration: `consumer.Register(&Factory{version: V10})` + `V11` in
[`../consumer/plugin.go`](../consumer/plugin.go); the symmetric provider in
[`../provider/`](../provider/). Default port is **8000** (common OSC
convention — no port is officially mandated).

## Spec documents

| Document | Link | Description |
|---|---|---|
| OSC 1.0 spec | <https://opensoundcontrol.org/spec-1_0.html> | Authoritative 1.0 spec (CNMAT, UC Berkeley) |
| OSC 1.1 spec | <https://opensoundcontrol.org/spec-1_1.html> | OSC 1.1 paper (community-accepted) |
| Page index | <https://opensoundcontrol.org/page-list.html> | Index of all spec pages |
| Wire format (this repo) | [../CLAUDE.md](../CLAUDE.md) | Byte-exact wire spec + quirks |
| Wireshark dissector | [../wireshark/dhs_osc.lua](../wireshark/dhs_osc.lua) | Byte-exact reference — `Proto("dhs_osc", ...)`, filter `dhs_osc` |
| Replay fixtures | [../testdata/fixtures/README.md](../testdata/fixtures/README.md) | Golden codec-produced `*.bin` packets |

> **Citation rule.** Cite OSC from **opensoundcontrol.org** (the
> CNMAT (UC Berkeley) home of Open Sound Control). Do **not** cite `osc.org` —
> that is the Orlando Science Center, unrelated to Open Sound Control.

## Definition of done

This docs set is ADR-0025 deliverable 9 for OSC. See
[`../../../docs/adr/0025-per-connector-definition-of-done.md`](../../../docs/adr/0025-per-connector-definition-of-done.md).
</content>
</invoke>
