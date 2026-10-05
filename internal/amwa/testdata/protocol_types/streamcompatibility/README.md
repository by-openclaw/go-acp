# streamcompatibility

IS-11 Stream Compatibility Management API — a Sender constrained: `PUT …/constraints/active`, then its status and its active constraints read back.

- Captured on 2026-10-05 with the released dhs v0.38.0: `dhs consumer nmos compat --sender … --constraints …` against the NMOS-Reference Node (github.com/alabou/NMOS-Reference at 02b5a564), on the private bridge of `ansible/playbooks/amwa-interop-is11.yml`.
- `capture.pcapng` — the conversation as it was on the wire.
- `tshark.tree` — that capture through `../../../wireshark/dhs_nmos.lua` (`tshark -O dhs_nmos,dhs_nmos_http`).
