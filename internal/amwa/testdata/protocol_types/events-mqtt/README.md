# events-mqtt

IS-07 Events API, MQTT — CONNECT, then the retained connection-status and state PUBLISHes on `x-nmos/events/v1.0/…`.

- Captured on 2026-10-04 with the released dhs v0.36.2: our MQTT client (`internal/amwa/session/mqtt`) publishing to a CONNACK stub on the control node — the fleet has no MQTT broker, so this is our wire with no third party on the other end.
- `capture.pcapng` — the conversation as it was on the wire.
- `tshark.tree` — that capture through `../../../wireshark/dhs_nmos.lua` (`tshark -O dhs_nmos,dhs_nmos_http`).
