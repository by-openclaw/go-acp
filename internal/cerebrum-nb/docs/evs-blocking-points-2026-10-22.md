# Cerebrum 2.9.3 build 15692 — four points that block RouteMaster provisioning over the APIs

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-22
**Server tested:** Cerebrum 2.9.3 build 15692, web client 2.9.3 build 2222 (staging VM, 10.6.250.5), standalone
**Documents used:** *Cerebrum Northbound API* v0.17 (October 2026); *EVS Cerebrum RCP* OpenAPI 2.6.1 (the server answers `GET /v2/api` → 2.5.3)
**Method:** every frame and every answer below is quoted as sent and received on 2026-10-22. "User interface" means the RouteMaster pages of the Cerebrum client.

We provision the RouteMaster from a plan: levels, sources and destinations with their bindings, virtual pairs, categories, alternate labels, routes. Four points keep the user interface in every run or leave an error unseen. Federation is out of scope until our licence is installed.

## 1. A level's default device, "Multiple Devices" and IP filter cannot be set or read

These three settings decide whether an IP binding on the level resolves. We set them in the user interface only.

**Northbound.** §4.1.4 `LEVEL_MNE` has the attributes `TYPE`, `DEVICE_NAME` or `IP_ADDRESS`, `DEVICE_TYPE`, `LEVEL_NAME` or `LEVEL_ID`, `MNEMONIC`, `ALT_MNE`, `ON_DEVICE`. §5.1.4 documents `RM_DEFAULT_DEVICE` in the reply.

| | Frame |
|---|---|
| TX, create level 6 | `<ACTION MTID="3"><ROUTING TYPE="LEVEL_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" LEVEL_ID="6" MNEMONIC="ZZ-L6"/></ACTION>` |
| RX | `<ACK MTID="3"/>` |
| TX, read level 6 | `<OBTAIN MTID="2"><ROUTING_CHANGE TYPE="LEVEL_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" LEVEL_ID="6"/></OBTAIN>` |
| RX | `<ROUTING_CHANGE TYPE="LEVEL_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" LEVEL_ID="6"><MNE AVAILABLE="1" MNEMONIC="ZZ-L6" ORIGINAL_MNEMONIC="ZZ-L6"/><ALT_MNE_1 AVAILABLE="0"/><ALT_MNE_2 AVAILABLE="0"/><ALT_MNE_3 AVAILABLE="0"/><ALT_MNE_4 AVAILABLE="0"/><ALT_MNE_5 AVAILABLE="0"/><ALT_MNE_6 AVAILABLE="0"/><ALT_MNE_7 AVAILABLE="0"/><ALT_MNE_8 AVAILABLE="0"/><ALT_MNE_9 AVAILABLE="0"/><ALT_MNE_10 AVAILABLE="0"/></ROUTING_CHANGE>` |
| TX, read level 1, configured in the user interface (default device = the IP Router, "Multiple Devices" ticked, IP filter "video") | `<OBTAIN MTID="2"><ROUTING_CHANGE TYPE="LEVEL_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" LEVEL_ID="1"/></OBTAIN>` |
| RX | `<ROUTING_CHANGE TYPE="LEVEL_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" LEVEL_ID="1"><MNE AVAILABLE="1" MNEMONIC="Video" ORIGINAL_MNEMONIC="Video"/><ALT_MNE_1 AVAILABLE="0"/><ALT_MNE_2 AVAILABLE="0"/><ALT_MNE_3 AVAILABLE="0"/><ALT_MNE_4 AVAILABLE="0"/><ALT_MNE_5 AVAILABLE="0"/><ALT_MNE_6 AVAILABLE="0"/><ALT_MNE_7 AVAILABLE="0"/><ALT_MNE_8 AVAILABLE="0"/><ALT_MNE_9 AVAILABLE="0"/><ALT_MNE_10 AVAILABLE="0"/></ROUTING_CHANGE>` |

Level 6 appears in the Levels tab with no default device, "Multiple Devices" unticked and no IP filter. No `RM_DEFAULT_DEVICE` element in either reply.

**RCP.** The 2.6.1 document defines `GET /routemaster/levels` and `GET /routemaster/levels/{levelId}`, returning a level with `id`, `mnemonic`, `multipleDevices`, `device` (`name`, `typeId`, `deviceLevel`), `ipFilter`, `sourceTags`, `destinationTags`, and `PATCH /devices/{deviceName}/{deviceIndex}/routers/level-mnemonics/{levelId}` with `originalMnemonic`, `alternateMnemonics`, `IOMnemonics`.

| Request | Answer |
|---|---|
| `GET /v2/routemaster/levels/6` | `404` `{"reqid":7,"error":{"code":1,"message":"Unknown RouteMaster collection. Supported collections are [sources, destinations, federation-sources, federation-destinations]"}}` |
| `PATCH /v2/devices/Cerebrum/0/routers/level-mnemonics/5` body `{"level_5": {"originalMnemonic": "ZZ-PROBE-L3"}}` | `202`; the `GET` returns the new name |

**Questions.** Is there a Northbound message or an RCP request that sets a level's default device, "Multiple Devices" and IP filter? Why is `RM_DEFAULT_DEVICE` absent from the `LEVEL_MNE` reply? On which build does `GET /routemaster/levels` answer?

## 2. The alternate mnemonic names cannot be created or listed

Writing an alternate label on a source, a destination or a level works on both interfaces once the names (`Name 1` … `Name 10` on our server) exist. We create the names in the user interface only.

**Northbound.** §4.1.4–§4.1.6: `ALT_MNE` is "the index of the alternate mnemonic being targeted"; §5.1.4–§5.1.6 reply with `ALT_MNE_n`.

| | Frame |
|---|---|
| TX, slot 1 on source 16, ten names defined | `<ACTION MTID="3"><ROUTING TYPE="SRCE_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" SRCE_ID="16" MNEMONIC="ZZ-ALT1" ALT_MNE="1"/></ACTION>` |
| RX | `<ACK MTID="3"/>` |
| TX, read source 16 | `<OBTAIN MTID="2"><ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="16" LEVEL_ID="*"/></OBTAIN>` |
| RX | `<ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="16" LEVEL_ID="*"><MNE AVAILABLE="1" MNEMONIC="ZZ-NB-TAG" ORIGINAL_MNEMONIC="ZZ-NB-TAG"/><ALT_MNE_1 AVAILABLE="1" MNEMONIC="ZZ-ALT1"/><ALT_MNE_10 AVAILABLE="1" MNEMONIC="ZZ-ALT10"/></ROUTING_CHANGE>` |
| TX, slot 2 on source 16 earlier the same day, no name defined yet | `<ACTION MTID="3"><ROUTING TYPE="SRCE_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" SRCE_ID="16" MNEMONIC="ZZ-ALT2" ALT_MNE="2"/></ACTION>` |
| RX | `<NACK MTID="3" ERROR="ONE_OR_MORE_ACTIONS_INVALID" ERROR_CODE="8"/>` |

**RCP.** 2.6.1: `alternateMnemonics` is "a map of alternative mnemonics indexed by the Alternative Mnemonic name".

| Request | Answer |
|---|---|
| `PATCH /v2/routemaster/sources/16` body `{"alternateMnemonics": {"Eng": "ZZ-PROBE"}}`, no name "Eng" defined | `400`, error code 2, message `Unknown alternate mnemonic name in 'alternateMnemonics'` |

**Questions.** Is there a Northbound message or an RCP request that creates, lists or renames the alternate mnemonic names?

## 3. A create returns no id, and a write that is not applied is acknowledged

To find what it created, our client reads the whole collection before and after. To know whether a binding is applied, it reads it back and we look at the user interface.

**Northbound.**

| | Frame |
|---|---|
| TX, add one source | `<ACTION MTID="2"><ROUTING TYPE="RM_SRCE_ADD" COUNT="1" VIRTUAL="0" FEDERATION="0"/></ACTION>` |
| RX | `<ROUTING TYPE="RM_SRCE_ADD" FEDERATION="0"><RESULT VALUE="ACCEPTED"/></ROUTING>` then `<ACK MTID="2"/>`: no id |
| TX, bind source 17 on level 1, the level having no default device at that time | `<ACTION MTID="2"><ROUTING TYPE="SRCE_ASSOC_IP" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" LOGICAL_SRCE_ID="17" LOGICAL_LEVEL_ID="1" TARGET_DEVICE_NAME="NMOS" TARGET_SENDER_NAME="VTX-01" SUB_DEVICE="1"/></ACTION>` |
| RX | `<ACK MTID="2"/>`: the user interface shows "Level not configured" for source 17, level 1 |
| TX, bind source 18 on level 1, the level configured, with a sender name the server does not resolve | `<ACTION MTID="2"><ROUTING TYPE="SRCE_ASSOC_IP" LOGICAL_SRCE_ID="18" LOGICAL_LEVEL_ID="1" TARGET_DEVICE_NAME="NMOS" TARGET_SENDER_NAME="VTX-01" SUB_DEVICE="1"/></ACTION>` |
| RX | `<ACK MTID="2"/>`: the user interface shows the binding in red. The same action with `TARGET_SENDER_NAME="lab-convert-01\|VTX-01"` is resolved |

**RCP.**

| Request | Answer |
|---|---|
| `POST /v2/routemaster/sources` body `{"count": 1, "mnemonic": "ZZ-PROBE-S"}` | `202 Accepted`, no id in the body |
| `PATCH /v2/routemaster/sources/17` body `{"levels": {"level_1": {"device": {"senderReceiver": "VTX-01"}}}}` | `202 Accepted`; the `GET` returns the string as sent; the user interface shows it in red. `"NMOS\|1\|lab-convert-01\|VTX-01"` resolves |

**Questions.** Can the id of a created source or destination be returned, in the `RESULT` of `RM_SRCE_ADD` / `RM_DEST_ADD` and in the body of the `202`? Can a binding on a level without device, or with a name that does not resolve, be refused instead of acknowledged? Which form of `TARGET_SENDER_NAME`, `TARGET_RECEIVER_NAME` and `senderReceiver` does 2.9.3 expect?

## 4. The answer to an unsupported request does not say what was wrong

**Northbound.**

| | Frame |
|---|---|
| TX, slot 11 on source 16, ten names defined | `<ACTION MTID="3"><ROUTING TYPE="SRCE_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" SRCE_ID="16" MNEMONIC="ZZ-ALT11" ALT_MNE="11"/></ACTION>` |
| RX | `<ACK MTID="3"/>`: nothing stored, no `ALT_MNE_11` in the `OBTAIN` reply quoted in point 2 |
| TX, slot 2 on source 16, no name defined | `<ACTION MTID="3"><ROUTING TYPE="SRCE_MNE" DEVICE_TYPE="ROUTER" IP_ADDRESS="0.0.0.0" SRCE_ID="16" MNEMONIC="ZZ-ALT2" ALT_MNE="2"/></ACTION>` |
| RX | `<NACK MTID="3" ERROR="ONE_OR_MORE_ACTIONS_INVALID" ERROR_CODE="8"/>` |

**RCP.**

| Request | Answer |
|---|---|
| `GET /v2/routemaster/levels/6` | `404` `{"reqid":7,"error":{"code":1,"message":"Unknown RouteMaster collection. Supported collections are [sources, destinations, federation-sources, federation-destinations]"}}` |
| `PATCH /v2/devices/Cerebrum/0/routers/source-mnemonics/16` body `{"src_16": {"alternateMnemonics": {"Eng": "ZZ"}}}`, no name "Eng" defined | `400`, error code 2, message `Error parsing request` |

**Questions.** Why is `ALT_MNE="11"` acknowledged? Which attribute does `ERROR_CODE="8"` point to? Why does the documented `GET /routemaster/levels` answer "Unknown RouteMaster collection"? Why does the same unknown alternate name answer `Error parsing request` here and `Unknown alternate mnemonic name` on `/v2/routemaster/sources/16`?

Thank you. We re-run every line above on any build on request.
