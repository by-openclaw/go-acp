# Cerebrum Northbound API v0.17 — the RouteMaster messages measured on 2.9.3 build 15692

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-22
**Server tested:** Cerebrum 2.9.3 build 15692, web client 2.9.3 build 2222 (staging VM, 10.6.250.5), standalone, no federation; one comparison on a Cerebrum 2.9.0 (10.6.239.121, Northbound only, same Neurons registered as routers)
**Document used:** *Cerebrum Northbound API* v0.17 (October 2026); previous document 0v16
**Interface used:** Northbound for every write and for every read-back (`OBTAIN` of `SRCE_MNE`, `DEST_MNE`, `LEVEL_MNE`, `RM_SRCE_TAGS`); RCP was used only to confirm that an object exists or carries a value that the Northbound reply did not return, and each such cell says so
**Method:** automated and repeatable; every case runs on a temporary source or destination that is deleted afterwards; the reply of each Northbound read-back is kept as the server sent it and quoted in section 5

## 1. Why we are writing

We received the Northbound API document v0.17. It adds the RouteMaster
messages we were missing on Northbound: adding and deleting sources and
destinations (§4.2.3–§4.2.5) and a `FEDERATION` attribute on the
association and tag actions (§4.1.7–§4.2.2). We implemented them in our
automation client and ran every one of them on our server. This note
lists what the server answered to each message, what the Northbound
reads of §5.1.5–§5.1.7 returned afterwards, and the points where a
short change on your side would complete the picture.

## 2. Result in one line

Every v0.17 add and delete is accepted by 2.9.3 and applied for local
and virtual sources and destinations within one second; the federation
variants are accepted and not applied on our standalone server; the
on 2.9.3 the IP association (`SRCE_ASSOC_IP` / `DEST_ASSOC_IP`) is
applied and returned once the level carries its device and the sender
is named `<node>|<sender>`, the form the user interface stores; the
installation of 2.9.3 left every level without device and did not
migrate the bindings 2.9.2 had stored, and the server acknowledges an
unresolvable name as it acknowledges a good one; the router association
(`SRCE_ASSOC`) is applied and returned in all cases; the Northbound
read of a tag returns no value;
alternate mnemonics by slot are proven once their names exist in the
user interface; the level messages are unchanged.

## 3. The messages on 2.9.3

| # | Message (section) | Request sent | Server answer | Northbound read-back | Status |
|---|---|---|---|---|---|
| 1 | `RM_SRCE_ADD` (§4.2.3) | `COUNT="1" VIRTUAL="0" FEDERATION="0"` | `<ROUTING TYPE="RM_SRCE_ADD" FEDERATION="0"><RESULT VALUE="ACCEPTED"/></ROUTING>` | `OBTAIN SRCE_MNE SRCE_ID="16"` answers `MNEMONIC="Src 16"`, no `ASSOCIATIONS` element (RCP: sources 15 → 16) | proven |
| 2 | `RM_SRCE_ADD` virtual (§4.2.3) | `COUNT="1" VIRTUAL="1" FEDERATION="0"` | `RESULT VALUE="ACCEPTED"` | source 17 `"Virtual 1"` with `ASSOCIATION_1` … `ASSOCIATION_5`, one per level, `SRCE_ID="4294967293" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"`; and destination 16 `"Virtual 1"` with `ASSOCIATION_1` … `ASSOCIATION_5`, `DEST_ID="4026531839"`, same empty attributes: the message creates the pair (RCP: both `virtual: true`) | proven |
| 3 | `RM_DEST_ADD` (§4.2.3) | `COUNT="1" VIRTUAL="0" FEDERATION="0"` | `RESULT VALUE="ACCEPTED"` | `OBTAIN DEST_MNE DEST_ID="17"` answers `MNEMONIC="Dest 17"`, no `ASSOCIATIONS` element (RCP: destinations 15 → 17 after rows 2 and 3) | proven |
| 4 | `RM_SRCE_ADD` federation (§4.2.3) | `COUNT="1" VIRTUAL="0" FEDERATION="1"` | `<ROUTING TYPE="RM_SRCE_ADD" FEDERATION="1"><RESULT VALUE="ACCEPTED"/></ROUTING>` | `SRCE_MNE` has no `FEDERATION` attribute (§4.1.5), so Northbound cannot list the federation sources; RCP: the federation-sources collection stays at 0 | accepted, not applied — standalone server |
| 5 | `SRCE_ASSOC_IP` / `DEST_ASSOC_IP`, the IP association (§4.1.9–§4.2.0, as in 0v16) | `LOGICAL_SRCE_ID="16" LOGICAL_LEVEL_ID="1" TARGET_DEVICE_NAME="NMOS" SUB_DEVICE="1" TARGET_SENDER_NAME="VTX-01"` as in 0v16; then `TARGET_SENDER_NAME="lab-convert-01\|VTX-01"`, the sender named `<node>\|<sender>` as the user interface stores it | `ACK` every time, whatever the name | With the level carrying no device (the state the 2.9.3 installation left all five levels in): no `ASSOCIATIONS` element, "Level not configured" in the user interface, whatever the frame or the name. With the level given its device in the user interface (IP Router, "Multiple Devices"): `TARGET_SENDER_NAME="VTX-01"` is stored as `NMOS\|1\|VTX-01`, returned as an `ASSOCIATION` row and shown red (unresolved); `TARGET_SENDER_NAME="lab-convert-01\|VTX-01"` is stored as `NMOS\|1\|lab-convert-01\|VTX-01`, the form the user interface writes, returned with `RM_IP_UID="NMOS\|1\|lab-convert-01\|VTX-01"` and resolved. The bare names 2.9.2 stored for the whole plant were not migrated and show unresolved too | proven with the level configured and the sender named `<node>\|<sender>`; **silent `ACK`** on an unresolvable name; **2.9.2 bindings not migrated** |
| 5b | `SRCE_ASSOC`, the router association (§4.1.7) | `LOGICAL_SRCE_ID="19" LOGICAL_LEVEL_ID="1" TARGET_DEVICE_NAME="Neuron-convert-Hybrid" TARGET_DEVICE_TYPE="ROUTER" TARGET_LEVEL_ID="1" TARGET_SRCE_ID="1"` (the Neuron registered as a router, its input 1) | `ACK` | `OBTAIN SRCE_MNE SRCE_ID="19"` carries `<ASSOCIATIONS><ASSOCIATION_1 SRCE_ID="1" RM_DEVICE_NAME="Neuron-convert-Hybrid" RM_DEVICE_TYPE="ROUTER" RM_LEVEL_ID="1"/></ASSOCIATIONS>`; the user interface shows "1 - IP001-01" on level 1 | proven — the same reply on a 2.9.0 server |
| 6 | `RM_SRCE_TAGS` local (§4.2.1) | `SRCE_ID="16" LEVEL_ID="1" TAGS="ZZ-PROBE"` | `ACK` | `OBTAIN RM_SRCE_TAGS SRCE_ID="16" LEVEL_ID="1"` (§5.1.7) answers `<TAGS AVAILABLE="1" LIST=",,,,"/>`; with `LEVEL_ID="*"`, five rows, one per level with its `LEVEL_NAME`, all `LIST=",,,,"` (RCP reads `tags: ["ZZ-PROBE"]` on level 1) | partly — applied on the server (RCP reads it), **not returned by the Northbound read** |
| 7 | `SRCE_ASSOC_IP` federation (§4.1.9) | as row 5 plus `FEDERATION="1"` | `ACK` | no Northbound read of a federation binding; RCP: the local binding of row 5 unchanged, the federation collection empty | accepted — not verifiable on a standalone server |
| 8 | `RM_SRCE_TAGS` federation (§4.2.1) | as row 6 plus `FEDERATION="1"` | `ACK` | `OBTAIN RM_SRCE_TAGS SRCE_ID="16" LEVEL_ID="1"` answers `LIST=",,,,"`, as in row 6 | accepted — not verifiable on a standalone server |
| 9 | `RM_SRCE_DELETE` (§4.2.4) | `SRCE_ID="16,17" FEDERATION="0"` | `<ROUTING TYPE="RM_SRCE_DELETE" FEDERATION="0"><RESULT VALUE="ACCEPTED"/></ROUTING>` | `OBTAIN SRCE_MNE` wildcard counts 15 sources; the virtual destination `"Virtual 1"` is gone with its source and `"Dest 17"` now answers at id 16 (id re-read over RCP) | proven: two ids in one message |
| 10 | `RM_DEST_DELETE` (§4.2.4) | `DEST_ID="16" FEDERATION="0"` | `RESULT VALUE="ACCEPTED"` | `OBTAIN DEST_MNE` wildcard counts 15 destinations | proven |
| 11 | `SRCE_MNE` / `DEST_MNE` / `LEVEL_MNE` alternate mnemonic by slot (§4.1.4–§4.1.6 `ALT_MNE`), ten alternate mnemonic names defined in the user interface | `SRCE_ID="16" MNEMONIC="ZZ-ALT1" ALT_MNE="1"`, `ALT_MNE="10"`; on a virtual source `ALT_MNE="1"`; on its virtual destination `DEST_MNE ALT_MNE="2"`; on level 5 `LEVEL_MNE ALT_MNE="1"` | `ACK` each time | `OBTAIN SRCE_MNE` answers `<ALT_MNE_1 AVAILABLE="1" MNEMONIC="ZZ-ALT1"/>` and `<ALT_MNE_10 AVAILABLE="1" MNEMONIC="ZZ-ALT10"/>`; the virtual source its `ALT_MNE_1`, the virtual destination its `ALT_MNE_2` only (the pair shares the label, not the alternates); `OBTAIN LEVEL_MNE` lists the ten slots, `AVAILABLE="0"` or `"1"` | proven; the user interface shows the same values |
| 13 | `ROUTE` on one level (§4.1.1) | `SRCE_ID="1" DEST_ID="7" LEVEL_ID="1"`: destination VIEW 3 (receivers of lab-view-01, all disabled before) takes source CONVERT 1 | `ACK` | within 4 s the node's receiver VIDEO STREAM 1 reports CONVERT 1's sender over IS-05, and so does the level 2 audio receiver: a `ROUTE` on level 1 routed level 2 too; `OBTAIN ROUTE` shows destination 7 on levels 1;2 | proven |
| 14 | `ROUTE` on all levels (§4.1.1, no `LEVEL_ID`) | `SRCE_ID="1" DEST_ID="7"` | `ACK` | within 5 s the video and the audio receivers of VIEW 3 take CONVERT 1's senders; the two levels without device are left alone. On a FusioN destination (FUSION CH6 ← CONVERT 2) the video receiver took the new sender later than 6 s after the `ACK` | proven; the FusioN applies late |
| 12 | `SRCE_MNE` alternate mnemonic on a slot beyond the defined names | `SRCE_ID="16" MNEMONIC="ZZ-ALT11" ALT_MNE="11"` with ten names defined; earlier, with no name defined, `ALT_MNE="1"` and `ALT_MNE="2"` | `ACK` on slot 11 and on slot 1 without names; `NACK 8` on slot 2 without names | nothing stored, no `ALT_MNE_11` in the reply | **accepted, not applied** — a silent `ACK` |

## 4. Levels, unchanged in v0.17

| # | Operation | Request | Server answer | Northbound read-back (`OBTAIN LEVEL_MNE`) | Status |
|---|---|---|---|---|---|
| 1 | Create | `LEVEL_MNE` on the unused level 5, `MNEMONIC="ZZ-PROBE-L"` (run of 2026-10-21 on this build, when level 5 did not exist) | `ACK` | 5 levels, the new one listed | proven |
| 2 | Rename | `LEVEL_MNE` level 5, `MNEMONIC="ZZ-PROBE-L"` then `"ZZ-PROBE-L2"` | `ACK` | renamed each time, read back over Northbound and RCP | proven |
| 3 | Delete | `LEVEL_MNE` level 5, `MNEMONIC=""` | `NACK 8 ONE_OR_MORE_ACTIONS_INVALID` | 5 levels, the test level stays | **no** — the level is deleted in the user interface |
| 4 | Read the level's default device (§5.1.4 `RM_DEFAULT_DEVICE`, "present for Routemaster levels; the target device and level of the associations on this level") | `OBTAIN LEVEL_MNE LEVEL_ID="*"` | 6 rows | no `RM_DEFAULT_DEVICE` element on any level, the two levels configured in the user interface with the IP Router as default device included; `MNE` and `ALT_MNE_N` only | **missing in the reply** |
| 5 | Set the level's default device, "Multiple Devices", IP filter | no message; a level created by `LEVEL_MNE` (levels 5 and 6) has none of them | — | the user interface is the only place; the same over RCP, where every write path answers 404, 400 or drops the fields | **no** |

## 5. The replies behind section 3, as the server sent them

| Read-back | Reply |
|---|---|
| row 1 — `OBTAIN SRCE_MNE SRCE_ID="16"` after the local add | `<ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="16" LEVEL_ID="*"><MNE AVAILABLE="1" MNEMONIC="Src 16" ORIGINAL_MNEMONIC="Src 16"/></ROUTING_CHANGE>` |
| row 2 — `OBTAIN SRCE_MNE SRCE_ID="17"` after the virtual add | `<ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="17" LEVEL_ID="*"><MNE AVAILABLE="1" MNEMONIC="Virtual 1" ORIGINAL_MNEMONIC="Virtual 1"/><ASSOCIATIONS><ASSOCIATION_1 SRCE_ID="4294967293" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/><ASSOCIATION_2 SRCE_ID="4294967293" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/><ASSOCIATION_3 SRCE_ID="4294967293" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/><ASSOCIATION_4 SRCE_ID="4294967293" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/><ASSOCIATION_5 SRCE_ID="4294967293" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/></ASSOCIATIONS></ROUTING_CHANGE>` |
| row 2 — `OBTAIN DEST_MNE DEST_ID="16"` after the virtual add | `<ROUTING_CHANGE TYPE="DEST_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" DEST_ID="16" LEVEL_ID="*"><MNE AVAILABLE="1" MNEMONIC="Virtual 1" ORIGINAL_MNEMONIC="Virtual 1"/><ASSOCIATIONS><ASSOCIATION_1 DEST_ID="4026531839" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/><ASSOCIATION_2 DEST_ID="4026531839" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/><ASSOCIATION_3 DEST_ID="4026531839" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/><ASSOCIATION_4 DEST_ID="4026531839" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/><ASSOCIATION_5 DEST_ID="4026531839" RM_DEVICE_NAME="" RM_DEVICE_TYPE="" RM_LEVEL_ID="0"/></ASSOCIATIONS></ROUTING_CHANGE>` |
| row 3 — `OBTAIN DEST_MNE DEST_ID="17"` after the local add | `<ROUTING_CHANGE TYPE="DEST_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" DEST_ID="17" LEVEL_ID="*"><MNE AVAILABLE="1" MNEMONIC="Dest 17" ORIGINAL_MNEMONIC="Dest 17"/></ROUTING_CHANGE>` |
| row 5 — `OBTAIN SRCE_MNE SRCE_ID="16" LEVEL_ID="1"` after the binding | `<ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="16" LEVEL_ID="1"><MNE AVAILABLE="1" MNEMONIC="Src 16" ORIGINAL_MNEMONIC="Src 16"/></ROUTING_CHANGE>` |
| row 5b — `OBTAIN SRCE_MNE SRCE_ID="19"` after the router association | `<ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="19" LEVEL_ID="*"><MNE AVAILABLE="1" MNEMONIC="ZZ-NB-S3" ORIGINAL_MNEMONIC="ZZ-NB-S3"/><ASSOCIATIONS><ASSOCIATION_1 SRCE_ID="1" RM_DEVICE_NAME="Neuron-convert-Hybrid" RM_DEVICE_TYPE="ROUTER" RM_LEVEL_ID="1"/></ASSOCIATIONS></ROUTING_CHANGE>` |
| row 6 — `OBTAIN RM_SRCE_TAGS SRCE_ID="16" LEVEL_ID="1"` after the tag | `<ROUTING_CHANGE TYPE="RM_SRCE_TAGS" SRCE_ID="16" LEVEL_ID="1"><TAGS AVAILABLE="1" LIST=",,,,"/></ROUTING_CHANGE>` |
| row 6 — `OBTAIN RM_SRCE_TAGS SRCE_ID="16" LEVEL_ID="*"`, first of the five rows | `<ROUTING_CHANGE TYPE="RM_SRCE_TAGS" SRCE_ID="16" LEVEL_ID="1" LEVEL_NAME="Video"><TAGS AVAILABLE="1" LIST=",,,,"/></ROUTING_CHANGE>` |
| row 11 — `OBTAIN SRCE_MNE SRCE_ID="16"` after `ALT_MNE="1"` | `<ROUTING_CHANGE TYPE="SRCE_MNE" IP_ADDRESS="0.0.0.0" DEVICE_TYPE="ROUTER" SRCE_ID="16" LEVEL_ID="*"><MNE AVAILABLE="1" MNEMONIC="Src 16" ORIGINAL_MNEMONIC="Src 16"/></ROUTING_CHANGE>` |

## 6. Behaviour we observed

| # | Behaviour | Where |
|---|---|---|
| 1 | `RESULT VALUE="ACCEPTED"` means validated and queued (§4.2.5); on 2.9.3 the change was readable within one second every time | §4.2.5 |
| 2 | A create does not return the ids it created; the server names the IOs "Src N", "Dest N", "Virtual N" | §4.2.3 |
| 3 | `VIRTUAL="1"` creates a source and a destination of the same name; renaming the source renames the destination; deleting either removes both (source side this run, destination side on 2026-10-21 on this build) | §4.2.3, §4.2.4 |
| 4 | Ids are positions: a delete renumbers the IOs after it, and a pair deletion renumbers both collections | §4.2.4 |
| 5 | The reply to an `RM_*` action is the `<ROUTING>` envelope of §4.2.5, where every other action answers `<ACK>` | §4.2.5 |
| 6 | Two conditions for an IP association on 2.9.3, neither of which Northbound can read or set: the RouteMaster level must carry its device (Levels tab: Default Device = the IP Router, "Multiple Devices" when a level mixes IP senders and router inputs), and the sender or receiver must be named `<node>\|<sender>`, stored as `NMOS\|<slot>\|<node>\|<name>`. The 2.9.3 installation left the five levels without device and the plant's bindings in the bare form 2.9.2 stored, so every association of the plant was unresolved until the levels were configured again and the bindings rewritten. The router association (`SRCE_ASSOC`) is applied and returned in every case, on 2.9.3 as on 2.9.0. Otherwise the `ASSOCIATIONS` element of §5.1.5–§5.1.6 is sent for a virtual source or destination only: one `ASSOCIATION_N` per level, `RM_DEVICE_NAME` and `RM_DEVICE_TYPE` empty, `RM_LEVEL_ID="0"`, and without the `RM_REVERSED`, `RM_TAGS` and `RM_IP_UID` attributes of the document. A source bound to an IP sender by `SRCE_ASSOC_IP` carries no `ASSOCIATIONS` element, which §5.1.5 says "should be assumed to indicate no association" | §5.1.5 |
| 7 | `OBTAIN RM_SRCE_TAGS` answers `LIST=",,,,"` for every level, the level that carries the tag included, where §5.1.7 shows `LIST="Tag1\|Tag2"` with the tags of the level asked | §5.1.7 |
| 8 | `FEDERATION` is not on `SRCE_MNE` / `DEST_MNE` / `LEVEL_MNE`, nor on the lock actions | §4.1 |
| 9 | The login reply still reports `API_VER="0.1"`; the document version is not visible on the wire | §2.1 |
| 9b | A `ROUTE` with `LEVEL_ID="1"` routed levels 1 and 2 of the destination (the levels the source is bound on); the crosspoint table (`OBTAIN ROUTE`) can lag the device by seconds, and a FusioN receiver applied the take later than 6 s after the `ACK` where the Neuron receivers took within 4 s | §4.1.1 |
| 10 | `ALT_MNE` targets one of the alternate mnemonic names defined in the Cerebrum user interface (ten on our server); a slot beyond them is acknowledged and dropped; with no name defined, slot 1 was acknowledged and dropped and slot 2 refused. No message lists, creates or renames the names; only the `LEVEL_MNE` reply, which carries every slot with `AVAILABLE`, reveals how many exist | §4.1.4–§4.1.6, §5.1.4 |

## 7. What we ask

| # | Request | Section | Why |
|---|---|---|---|
| 1 | The created ids in the `RESULT` of `RM_SRCE_ADD` / `RM_DEST_ADD` | §4.2.5 | safe automation with several clients |
| 2 | A `NACK` to `SRCE_ASSOC_IP` / `DEST_ASSOC_IP` when the level has no device or the sender name does not resolve; the migration of the bindings 2.9.2 stored (bare sender names) to the 2.9.3 form at installation; and the `<node>\|<sender>` naming of `TARGET_SENDER_NAME` / `TARGET_RECEIVER_NAME` written in §4.1.9–§4.2.0 | §4.1.9–§4.2.0 | no silent `ACK`; an upgrade that keeps the plant's IP routing |
| 2b | `RM_DEFAULT_DEVICE` sent in the `LEVEL_MNE` reply as §5.1.4 documents it, and a message to set a level's default device, "Multiple Devices" and IP filter | §4.1.4, §5.1.4 | levels provisioned and verified without the user interface, which today decide whether any IP association works |
| 3 | `OBTAIN RM_SRCE_TAGS` / `RM_DEST_TAGS` returning the tags of the level, `LIST="Tag1\|Tag2"` as documented | §5.1.7–§5.1.8 | a client that verifies its own tags on Northbound |
| 4 | A level delete, or a documented way to remove a level | §4.1.4 | levels provisioned and removed without the user interface |
| 5 | `FEDERATION` on `SRCE_MNE` / `DEST_MNE` | §4.1.5–§4.1.6 | the mnemonic of a federation IO over Northbound |
| 6 | A `FAILED` result, or a later signal, when a queued change is not applied | §4.2.5 | reliable reporting |
| 7 | The document version in the login reply | §2.1 | a client that knows which messages the server accepts |
| 8 | A note in §4.2.3–§4.2.4 that a virtual add creates the pair and that a delete of either id removes it | §4.2.3 | what we measured, written down |
| 9 | A `NACK` when `ALT_MNE` names a slot beyond the defined names, and a way to list the names | §4.1.4–§4.1.6 | no silent `ACK`; a client that knows which slots exist |

Thank you for v0.17: it closes the create and delete gap we had on
Northbound. The measurement behind this note is automated and takes a
few minutes; we are glad to run it again on any build, and to share
every request and answer behind each line.
