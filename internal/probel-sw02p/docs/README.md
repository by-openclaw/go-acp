# Probel SW-P-02 — General Remote Control Protocol (Issue 26)

| Role | Doc | Status |
|---|---|---|
| **Verbs & config reference** | [verbs.md](verbs.md) | every verb + transport/redundancy/interrogate/connect/protect/lock/reports/ensure/wireshark/ansible, with real captures |
| Operator runbook | [runbook.md](runbook.md) | ✓ shipping |
| Consumer | [consumer.md](consumer.md) | ✓ shipping — full matrix verb set (interrogate / connect / connect-on-go / go / protect-* / dual-status / lock-status / status / router-config / watch) over TCP |
| Provider | [provider.md](provider.md) | ✓ shipping — serves a canonical matrix as an SW-P-02 device over TCP (port 2002) |

## Spec documents

| Document | Path | Description |
|---|---|---|
| SW-P-02 Issue 26 | [SW-P-02 issue 26.doc](../../../internal/probel-sw02p/assets/probel-sw02/SW-P-02%20issue%2026.doc) | Full specification (original Word document) |
| SW-P-02 Issue 26 (text) | [SW-P-02_issue_26.txt](../../../internal/probel-sw02p/assets/probel-sw02/SW-P-02_issue_26.txt) | antiword-extracted plain text |
| Wireshark dissector | [dhs_probel_sw02p.lua](../../../internal/probel-sw02p/wireshark/dhs_probel_sw02p.lua) | Byte-exact reference |
