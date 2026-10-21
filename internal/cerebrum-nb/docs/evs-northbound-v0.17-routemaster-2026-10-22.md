# Cerebrum Northbound API v0.17 — the RouteMaster messages measured on 2.9.3 build 15692

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-22
**Server tested:** Cerebrum 2.9.3 build 15692, web client 2.9.3 build 2222 (staging VM, 10.6.250.5), standalone, no federation
**Document used:** *Cerebrum Northbound API* v0.17 (October 2026); previous document 0v16
**Interface used:** Northbound only, for every write and every read-back; RCP was used only to confirm that an object exists where Northbound has no read for it, and each such cell says so

## 1. Why we are writing

We received the Northbound API document v0.17. It adds the RouteMaster
messages we were missing on Northbound: adding and deleting sources and
destinations (§4.2.3–§4.2.5) and a `FEDERATION` attribute on the
association and tag actions (§4.1.7–§4.2.2). We implemented them in our
automation client and ran every one of them on our server, each on a
temporary source or destination that was deleted afterwards. This note
lists what the server answered, what Northbound read back, and the few
points where a short change on your side would complete the picture.

## 2. Result in one line

Every v0.17 message is accepted by 2.9.3 and applied for local and
virtual sources and destinations within one second; the federation
variants are accepted and not applied on our standalone server; the
level messages are unchanged.

## 3. The v0.17 messages on 2.9.3

| # | Message (section) | Request sent | Server answer | Northbound read-back (`OBTAIN SRCE_MNE` / `DEST_MNE`) | Status |
|---|---|---|---|---|---|
| 1 | `RM_SRCE_ADD` (§4.2.3) | `COUNT="1" VIRTUAL="0" FEDERATION="0"` | `<ROUTING TYPE="RM_SRCE_ADD" FEDERATION="0"><RESULT VALUE="ACCEPTED"/>` | one more source, id 16, named "Src 16" by the server | proven |
| 2 | `RM_SRCE_ADD` virtual (§4.2.3) | `COUNT="1" VIRTUAL="1" FEDERATION="0"` | `RESULT ACCEPTED` | one more source, id 17, "Virtual 1", capability levels 1;2;3;4;5 (every level of the server) — and one more destination, id 16, "Virtual 1", same levels: the message creates the pair | proven |
| 3 | `RM_DEST_ADD` (§4.2.3) | `COUNT="1" VIRTUAL="0" FEDERATION="0"` | `RESULT ACCEPTED` | one more destination, id 17, "Dest 17" | proven |
| 4 | `RM_SRCE_ADD` federation (§4.2.3) | `COUNT="1" VIRTUAL="0" FEDERATION="1"` | `RESULT ACCEPTED` | no new source on Northbound; the federation collection stays empty (confirmed over RCP) | accepted, not applied — standalone server |
| 5 | `SRCE_ASSOC_IP` federation (§4.1.9) | `LOGICAL_SRCE_ID="16" LOGICAL_LEVEL_ID="1" TARGET_DEVICE_NAME="NMOS" TARGET_SENDER_NAME="VTX-01" SUB_DEVICE="1" FEDERATION="1"` | `ACK` | Northbound has no read for a binding; the source's levels stay empty (confirmed over RCP) | accepted, not applied — standalone server |
| 6 | `RM_SRCE_TAGS` federation (§4.2.1) | `SRCE_ID="16" LEVEL_ID="1" TAGS="ZZ-PROBE" FEDERATION="1"` | `ACK` | Northbound has no read for tags | accepted — not verifiable on a standalone server |
| 7 | `RM_SRCE_DELETE` (§4.2.4) | `SRCE_ID="16,17" FEDERATION="0"` | `RESULT ACCEPTED` | sources back to 15; the virtual destination "Virtual 1" is gone with its source | proven: two ids in one message |
| 8 | `RM_DEST_DELETE` (§4.2.4) | `DEST_ID="16" FEDERATION="0"` | `RESULT ACCEPTED` | destinations back to 15 | proven |

The local bindings and tags of 0v16 (`SRCE_ASSOC_IP`, `DEST_ASSOC_IP`,
`RM_SRCE_TAGS` without `FEDERATION`) still work on 2.9.3: a source
bound to sender VTX-01 and a destination to receiver VRX-01 were read
back with their binding over RCP, since Northbound has no read for it.

## 4. Levels, unchanged in v0.17

| # | Operation | Request | Server answer | Northbound read-back (`OBTAIN LEVEL_MNE`) | Status |
|---|---|---|---|---|---|
| 1 | Create | `LEVEL_MNE` on the unused level 5, `MNEMONIC="ZZ-PROBE-L"` | `ACK` | 5 levels, the new one listed | proven |
| 2 | Rename | `LEVEL_MNE` level 5, `MNEMONIC="ZZ-PROBE-L2"` | `ACK` | renamed | proven |
| 3 | Delete | `LEVEL_MNE` level 5, `MNEMONIC=""` | `NACK 8 ONE_OR_MORE_ACTIONS_INVALID` | 5 levels, the test level stays | **no** — the level is deleted in the user interface |

## 5. Behaviour we observed

| # | Behaviour | Where |
|---|---|---|
| 1 | `RESULT VALUE="ACCEPTED"` means validated and queued (§4.2.5); on 2.9.3 the change was readable within one second every time | §4.2.5 |
| 2 | A create does not return the ids it created; the server names the IOs "Src N", "Dest N", "Virtual N" | §4.2.3 |
| 3 | `VIRTUAL="1"` creates a source and a destination of the same name; deleting either of them removes both | §4.2.3, §4.2.4 |
| 4 | Ids are positions: a delete renumbers the IOs after it, and a pair deletion renumbers both collections | §4.2.4 |
| 5 | The reply to an `RM_*` action is the `<ROUTING>` envelope of §4.2.5, where every other action answers `<ACK>` | §4.2.5 |
| 6 | `FEDERATION` is not on `SRCE_MNE` / `DEST_MNE` / `LEVEL_MNE`, nor on the lock actions | §4.1 |
| 7 | The login reply still reports `API_VER="0.1"`; the document version is not visible on the wire | §2.1 |
| 8 | `OBTAIN SRCE_MNE` / `DEST_MNE` return id, capability levels, mnemonic and alternates, but not the binding of a level, the tags, or whether the IO is virtual | §5.1 |

## 6. What we ask

| # | Request | Section | Why |
|---|---|---|---|
| 1 | The created ids in the `RESULT` of `RM_SRCE_ADD` / `RM_DEST_ADD` | §4.2.5 | safe automation with several clients |
| 2 | A read of a source or destination's bindings, tags and virtual flag over Northbound (an `OBTAIN` with the level detail) | §5.1 | a client that verifies its own writes without a second interface |
| 3 | A level delete, or a documented way to remove a level | §4.1.4 | levels provisioned and removed without the user interface |
| 4 | `FEDERATION` on `SRCE_MNE` / `DEST_MNE` | §4.1.5–§4.1.6 | the mnemonic of a federation IO over Northbound |
| 5 | A `FAILED` result, or a later signal, when a queued change is not applied | §4.2.5 | reliable reporting |
| 6 | The document version in the login reply | §2.1 | a client that knows which messages the server accepts |
| 7 | A note in §4.2.3–§4.2.4 that a virtual add creates the pair and that a delete of either id removes it | §4.2.3 | what we measured, written down |

Thank you for v0.17: it closes the create and delete gap we had on
Northbound. The measurement behind this note is automated and takes
a few minutes; we are glad to run it again on any build, and to share
every request and answer behind each line.
