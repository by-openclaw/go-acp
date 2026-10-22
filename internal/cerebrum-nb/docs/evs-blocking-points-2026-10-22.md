# Cerebrum 2.9.3 build 15692 — three points that block RouteMaster provisioning over the APIs

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-22
**Server tested:** Cerebrum 2.9.3 build 15692, web client 2.9.3 build 2222 (staging VM, 10.6.250.5), standalone
**Documents used:** *Cerebrum Northbound API* v0.17 (October 2026); *EVS Cerebrum RCP* OpenAPI 2.6.1 (the server answers `GET /v2/api` → 2.5.3)
**Method:** every frame below is quoted as sent and received on 2026-10-22; "user interface" means the RouteMaster pages of the Cerebrum client. The complete measurements are in our two notes of the same day, one per interface.

We provision the RouteMaster from a plan: levels, sources and destinations with their bindings, virtual pairs, categories, alternate labels, routes. Three points keep the user interface in every run. Federation is out of scope until our licence is installed.

## 1. A level cannot be created with its attributes, and its attributes cannot be read

A level's default device, its "Multiple Devices" setting and its IP filter decide whether any IP binding on that level resolves. Neither interface carries them.

**Northbound.** §4.1.4 `LEVEL_MNE` has the attributes `TYPE`, `DEVICE_NAME` or `IP_ADDRESS`, `DEVICE_TYPE`, `LEVEL_NAME` or `LEVEL_ID`, `MNEMONIC`, `ALT_MNE`, `ON_DEVICE`: the name only. §5.1.4 documents `RM_DEFAULT_DEVICE` in the reply.

| | Frame |
|---|---|
| TX, create level 6 | `<ACTION MTID="3"><ROUTING TYPE="LEVEL_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" LEVEL_ID="6" MNEMONIC="ZZ-L6"/></ACTION>` |
| RX | `<ACK MTID="3"/>` |
| TX, read level 6 | `<OBTAIN MTID="2"><ROUTING_CHANGE TYPE="LEVEL_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" LEVEL_ID="6"/></OBTAIN>` |
| RX | `<ROUTING_CHANGE TYPE="LEVEL_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" LEVEL_ID="6"><MNE AVAILABLE="1" MNEMONIC="ZZ-L6" ORIGINAL_MNEMONIC="ZZ-L6"/><ALT_MNE_1 AVAILABLE="0"/><ALT_MNE_2 AVAILABLE="0"/><ALT_MNE_3 AVAILABLE="0"/><ALT_MNE_4 AVAILABLE="0"/><ALT_MNE_5 AVAILABLE="0"/><ALT_MNE_6 AVAILABLE="0"/><ALT_MNE_7 AVAILABLE="0"/><ALT_MNE_8 AVAILABLE="0"/><ALT_MNE_9 AVAILABLE="0"/><ALT_MNE_10 AVAILABLE="0"/></ROUTING_CHANGE>` |
| TX, read level 1, configured in the user interface (default device = the IP Router, "Multiple Devices" ticked, IP filter "video") | `<OBTAIN MTID="2"><ROUTING_CHANGE TYPE="LEVEL_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" LEVEL_ID="1"/></OBTAIN>` |
| RX | `<ROUTING_CHANGE TYPE="LEVEL_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" LEVEL_ID="1"><MNE AVAILABLE="1" MNEMONIC="Video" ORIGINAL_MNEMONIC="Video"/><ALT_MNE_1 AVAILABLE="0"/><ALT_MNE_2 AVAILABLE="0"/><ALT_MNE_3 AVAILABLE="0"/><ALT_MNE_4 AVAILABLE="0"/><ALT_MNE_5 AVAILABLE="0"/><ALT_MNE_6 AVAILABLE="0"/><ALT_MNE_7 AVAILABLE="0"/><ALT_MNE_8 AVAILABLE="0"/><ALT_MNE_9 AVAILABLE="0"/><ALT_MNE_10 AVAILABLE="0"/></ROUTING_CHANGE>` |

Level 6 appears in the Levels tab with no default device, "Multiple Devices" unticked and no IP filter. No `RM_DEFAULT_DEVICE` element in either reply.

**RCP.** The 2.6.1 document has `GET /routemaster/levels` and `GET /routemaster/levels/{levelId}` only.

| Request | Answer |
|---|---|
| `GET /v2/routemaster/levels/6` | `404` `{"reqid":7,"error":{"code":1,"message":"Unknown RouteMaster collection. Supported collections are [sources, destinations, federation-sources, federation-destinations]"}}` |
| `PATCH /v2/routemaster/levels/6` body `{"multipleDevices": true, "device": {"name": "Cerebrum", "typeId": 1, "deviceLevel": 1}, "ipFilter": "video"}` | `404`, the same message |
| `POST /v2/routemaster/levels` body `{"id": 7, "mnemonic": "ZZ-L7", "multipleDevices": true, "device": {"name": "Cerebrum", "deviceLevel": 1}, "ipFilter": "video"}` | `404`, the same message |
| `PUT /v2/routemaster/levels/6` body `{"multipleDevices": true, "ipFilter": "video"}` | `404`, the same message |
| `PATCH /v2/devices/Cerebrum/0/routers/levels/6` body `{"multipleDevices": true, "ipFilter": "video"}` | `400` `{"reqid":7,"error":{"code":2,"message":"Invalid request type: levels"}}` |
| `PATCH /v2/devices/Cerebrum/0/routers/level-mnemonics/6` body `{"level_6": {"originalMnemonic": "ZZ-L6", "multipleDevices": true, "device": {"name": "Cerebrum", "deviceLevel": 1}, "ipFilter": "video"}}` | `200` `{"reqid":7}`; the following `GET` returns the mnemonics only |

**Ask:** a Northbound message and an RCP resource that set a level's default device, "Multiple Devices" and IP filter, and read them; `RM_DEFAULT_DEVICE` sent in the `LEVEL_MNE` reply as §5.1.4 documents it.

## 2. The alternate mnemonic names cannot be created or listed

Writing an alternate label on a source, a destination or a level works on both interfaces once the names (`Name 1` … `Name 10` on our server) exist. The names themselves exist only in the user interface.

**Northbound.** §4.1.4–§4.1.6: `ALT_MNE` is "the index of the alternate mnemonic being targeted"; §5.1.4–§5.1.6 reply with `ALT_MNE_n`. No message names, creates or lists the alternates.

| | Frame |
|---|---|
| TX, slot 1 on source 16 (ten names defined) | `<ACTION MTID="3"><ROUTING TYPE="SRCE_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" SRCE_ID="16" MNEMONIC="ZZ-ALT1" ALT_MNE="1"/></ACTION>` |
| RX | `<ACK MTID="3"/>` |
| TX, slot 11 on source 16 (beyond the ten names) | `<ACTION MTID="3"><ROUTING TYPE="SRCE_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" SRCE_ID="16" MNEMONIC="ZZ-ALT11" ALT_MNE="11"/></ACTION>` |
| RX | `<ACK MTID="3"/>` |
| TX, read source 16 | `<OBTAIN MTID="2"><ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="16" LEVEL_ID="*"/></OBTAIN>` |
| RX | `<ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="16" LEVEL_ID="*"><MNE AVAILABLE="1" MNEMONIC="ZZ-NB-TAG" ORIGINAL_MNEMONIC="ZZ-NB-TAG"/><ALT_MNE_1 AVAILABLE="1" MNEMONIC="ZZ-ALT1"/><ALT_MNE_10 AVAILABLE="1" MNEMONIC="ZZ-ALT10"/></ROUTING_CHANGE>` |
| TX, slot 2 on source 16 earlier the same day, no name defined yet | `<ACTION MTID="3"><ROUTING TYPE="SRCE_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" SRCE_ID="16" MNEMONIC="ZZ-ALT2" ALT_MNE="2"/></ACTION>` |
| RX | `<NACK MTID="3" ERROR="ONE_OR_MORE_ACTIONS_INVALID" ERROR_CODE="8"/>` |

Slot 1 and slot 10 are stored and read back; slot 11 is acknowledged and nothing is stored; before the names existed, slot 2 was refused.

**RCP.** 2.6.1: `alternateMnemonics` is "a map of alternative mnemonics indexed by the Alternative Mnemonic name"; no resource for the names.

| Request | Answer |
|---|---|
| `PATCH /v2/routemaster/sources/16` body `{"alternateMnemonics": {"Eng": "ZZ-PROBE"}}`, no name "Eng" defined | `400`, error code 2, message `Unknown alternate mnemonic name in 'alternateMnemonics'` |

**Ask:** a message and a resource to create, list and rename the alternate mnemonic names; a `NACK` from Northbound for a slot beyond them.

## 3. A write that is not applied is acknowledged, and a create returns no id

**Northbound.**

| | Frame |
|---|---|
| TX, add one source | `<ACTION MTID="2"><ROUTING TYPE="RM_SRCE_ADD" COUNT="1" VIRTUAL="0" FEDERATION="0"/></ACTION>` |
| RX | `<ROUTING TYPE="RM_SRCE_ADD" FEDERATION="0"><RESULT VALUE="ACCEPTED"/></ROUTING>` then `<ACK MTID="2"/>` — no id; the new source was found by reading the whole list before and after |
| TX, bind source 17 on level 1, the level having no default device at that time | `<ACTION MTID="2"><ROUTING TYPE="SRCE_ASSOC_IP" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" LOGICAL_SRCE_ID="17" LOGICAL_LEVEL_ID="1" TARGET_DEVICE_NAME="NMOS" TARGET_SENDER_NAME="VTX-01" SUB_DEVICE="1"/></ACTION>` |
| RX | `<ACK MTID="2"/>` — the user interface shows "Level not configured" for source 17, level 1 |
| TX, bind source 18 on level 1, the level configured, with a sender name the server does not resolve | `<ACTION MTID="2"><ROUTING TYPE="SRCE_ASSOC_IP" LOGICAL_SRCE_ID="18" LOGICAL_LEVEL_ID="1" TARGET_DEVICE_NAME="NMOS" TARGET_SENDER_NAME="VTX-01" SUB_DEVICE="1"/></ACTION>` |
| RX | `<ACK MTID="2"/>` — the user interface shows the binding in red; with `TARGET_SENDER_NAME="lab-convert-01\|VTX-01"` the same action is resolved |

**RCP.**

| Request | Answer |
|---|---|
| `POST /v2/routemaster/sources` body `{"count": 1, "mnemonic": "ZZ-PROBE-S"}` | `202 Accepted`, no id in the body |
| `PATCH /v2/routemaster/sources/17` body `{"levels": {"level_1": {"device": {"senderReceiver": "VTX-01"}}}}` | `202 Accepted`; the `GET` returns the string as sent; the user interface shows it in red; `"NMOS\|1\|lab-convert-01\|VTX-01"` resolves |

**Ask:** the created ids in the `RESULT` of `RM_SRCE_ADD` / `RM_DEST_ADD` and in the `202` body of `POST`; a `NACK` with a reason, and an error instead of `202`, when a binding cannot be applied (level without device, name that does not resolve); the form `<node>|<sender>` written in the documents for `TARGET_SENDER_NAME`, `TARGET_RECEIVER_NAME` and `senderReceiver`.

Thank you. We re-run every line above on any build on request.
