# configuration-bulk

IS-14 Configuration API — a backup and a validated restore: `GET` / `PATCH …/bulkProperties/`.

- Captured on 2026-10-04 with the released dhs v0.36.2: `dhs consumer nmos config --backup / --restore --validate-only` against the nmos-cpp reference node (10.6.250.104:8120).
- `capture.pcapng` — the conversation as it was on the wire.
- `tshark.tree` — that capture through `../../../wireshark/dhs_nmos.lua` (`tshark -O dhs_nmos,dhs_nmos_http`).
