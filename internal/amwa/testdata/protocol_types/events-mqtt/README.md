# events-mqtt

IS-07 Events API, MQTT — CONNECT, then the retained connection-status and state PUBLISHes on `x-nmos/events/v1.0/…`.

- Captured on 2026-10-04 with the released dhs v0.36.2: our MQTT client (`internal/amwa/session/mqtt`) publishing to the lab's mosquitto broker (tooling host, port 1884); a standard subscriber read the retained state back.
- `capture.pcapng` — the conversation as it was on the wire.
- `tshark.tree` — that capture through `../../../wireshark/dhs_nmos.lua` (`tshark -O dhs_nmos,dhs_nmos_http`).
