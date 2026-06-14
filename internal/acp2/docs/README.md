# ACP2 — Axon Control Protocol v2

| Role | Doc | Status |
|---|---|---|
| **Verbs & config reference** | [verbs.md](verbs.md) | every verb + transport/redundancy/logging/export/import/tree/ensure/wireshark/ansible, with real samples |
| Operator runbook | [runbook.md](runbook.md) | ✓ shipping |
| Consumer | [consumer.md](consumer.md) | ✓ shipping |
| Provider | [provider.md](provider.md) | ✓ shipping — serves canonical tree as ACP2 device over AN2/TCP (port 2072) |

## Spec documents

| Document | Path | Description |
|---|---|---|
| ACP2 Protocol | [acp2_protocol.pdf](../../../internal/acp2/assets/acp2_protocol.pdf) | Full specification |
| AN2 Transport | [an2_protocol.pdf](../../../internal/acp2/assets/an2_protocol.pdf) | Transport layer |
| Wireshark dissector | [dhs_acpv2.lua](../../../internal/acp2/wireshark/dhs_acpv2.lua) | Byte-exact reference |
