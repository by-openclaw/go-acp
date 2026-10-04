# registration-heartbeat

IS-04 Registration API — heartbeats: `POST /health/nodes/{id}` and the health the registry answers.

- Captured on 2026-10-04 with the released dhs v0.36.2: an nmos-cpp Node and the nmos-cpp reference registry (10.6.250.104:8110).
- `capture.pcapng` — the conversation as it was on the wire.
- `tshark.tree` — that capture through `../../../wireshark/dhs_nmos.lua` (`tshark -O dhs_nmos,dhs_nmos_http`).
