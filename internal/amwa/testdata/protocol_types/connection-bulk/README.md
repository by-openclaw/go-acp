# connection-bulk

IS-05 Connection API — a salvo: `POST /bulk/receivers`.

- Captured on 2026-10-04 with the released dhs v0.36.2: `dhs consumer nmos connect --route … --route …` against the nmos-cpp reference node (10.6.250.104:8120).
- `capture.pcapng` — the conversation as it was on the wire.
- `tshark.tree` — that capture through `../../../wireshark/dhs_nmos.lua` (`tshark -O dhs_nmos,dhs_nmos_http`).
