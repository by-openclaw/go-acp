# Cerebrum Northbound API 0.13 — wire-key catalogue

Authoritative, fact-only catalogue of every element name, attribute
name, and enum value on the Cerebrum Northbound v0.13 wire. Derived
from the EVS spec text; cross-referenced against a third-party
reference driver (NDA, never committed) and against live captures.

**Sources**

1. **Primary.** EVS *Cerebrum Northbound API 0.13* — full text extracted
   from [../assets/cerebrum_northbound_api_full_v0_13.docx](../assets/cerebrum_northbound_api_full_v0_13.docx).
   Also present: image-heavy PDF/DOCX originals at
   [../assets/Cerebrum Northbound API 0v13.pdf](../assets/Cerebrum%20Northbound%20API%200v13.pdf) and [../assets/Cerebrum Northbound API 0v13.docx](../assets/Cerebrum%20Northbound%20API%200v13.docx).
2. **Live wire captures.** pcaps from real Cerebrum servers — these
   override spec text where the two disagree (see "Wire-actual vs
   spec" below).
3. **Secondary (NDA, reference-only, never committed).** Third-party
   vendor reference driver. XML kept locally for fact-extraction;
   gitignored.

---

## Transport

| Fact | Value |
|---|---|
| Transport | WebSocket — `ws://` or `wss://` (TLS configurable) |
| URL path | **none** — `wss://host:port` only; no HTTP path |
| Default port | **40007** (configurable in the Cerebrum app) |
| Framing | One XML document per WebSocket text message |
| Encoding | **UTF-8** (unless specified differently) |
| Licensing | One northbound license per WebSocket connection |
| Pre-requisites | (1) northbound licence count sufficient; (2) API enabled in Cerebrum; (3) user account enabled for NB access with username + password |

Product aliases: EVS also brands the same API **"Neuron Bridge"** (appears
as page-header text throughout the spec).

---

## Element case on the wire (wire-actual, NOT spec)

Spec examples are **inconsistent** between sections:

| Section | Case observed |
|---|---|
| §2 Commands | lowercase roots: `<login>`, `<poll>`, `<action>`, `<subscribe>`, `<obtain>`, `<unsubscribe>`, `<unsubscribe_all>` |
| §2 attributes | lowercase: `username`, `password`, `mtid` |
| §4.1 Routing | UPPERCASE attrs: `TYPE`, `DEVICE_NAME`, `SRCE_NAME`, etc. |
| §4.2-4.4, §5.* | lowercase attrs: `type`, `category`, `group`, `device_name` |

**Live wire reality.** Production Cerebrum servers emit AND require
**UPPERCASE** elements + attributes. Verified 2026-04-26 via pcap
against a real Cerebrum: a lowercase `<login mtid="1"/>` was rejected
with `<NACK MTID="0" ERROR="MTID_ERROR" ERROR_CODE="1"/>` because the
server didn't recognise lowercase `mtid` as the MTID field. The
third-party reference driver also emits UPPERCASE throughout.

dhs therefore:

- **Emits UPPERCASE** on the wire (matches reality, matches every
  observed peer).
- **Accepts any case** on receive (case-folded AST), so a future
  spec-strict lowercase server still works without code changes.
- Treat **UPPERCASE** as the canonical wire form.

---

## Top-level commands (§2)

All requests carry `mtid` (Message Transaction ID — 32-bit unsigned,
wrapping, string-formatted on the wire).

| Root | TX body | RX | Notes |
|---|---|---|---|
| `<login>` | `username`, `password`, `mtid` | `<login_reply mtid api_ver>` | Must succeed before any other command (§1.1) |
| `<poll>` | `mtid` | `<poll_reply mtid CONNECTED_SERVER_ACTIVE PRIMARY_SERVER_STATE SECONDARY_SERVER_STATE>` | Keep-alive + redundancy probe |
| `<action>` | `mtid`, one child action element (§4) | `<ack>` / `<nack>` / `<busy>` | Mutations |
| `<subscribe>` | `mtid`, one or more subscription children (§5) | `<ack>` / `<nack>` / `<busy>` | Register for events |
| `<obtain>` | `mtid`, one or more obtain children (§5) | `<ack>` / `<nack>` / `<busy>` + event(s) | One-shot read; children same shape as subscribe |
| `<unsubscribe>` | `mtid`, children matching prior subscribe | `<ack>` / `<nack>` / `<busy>` | Remove specific subscriptions |
| `<unsubscribe_all>` | `mtid` | `<ack>` / `<nack>` / `<busy>` | Remove every active subscription |

**TX/RX symmetry rule (§5 preamble):** `subscribe` / `obtain` /
`unsubscribe` bodies and the resulting event payloads share the same
XML element shape — the server echoes the element structure back on
change.

### `<poll_reply>` attributes

| Attr | Meaning |
|---|---|
| `mtid` | echo of the request mtid |
| `CONNECTED_SERVER_ACTIVE` | `1` if this server is active, `0` if inactive |
| `PRIMARY_SERVER_STATE` | `0` / `1` |
| `SECONDARY_SERVER_STATE` | `0` / `1` |

Redundancy (§1.5): Cerebrum deployments usually have Primary +
Secondary; exactly one is active at any time. `poll` surfaces this.

---

## Standard responses (§1.4)

| Root | Meaning |
|---|---|
| `<ack>` | Success |
| `<nack>` | Failure — carries an error attribute (see §6 table) |
| `<busy>` | Temporary — retry expected |

All three echo the original `mtid`.

---

## Definitions (§3)

### DEVICE_TYPE (§3.1)

Three device classes:

| Value | Meaning |
|---|---|
| `Router` | Routing-capable device (includes ROUTER route-master) |
| `SNMP` | SNMP-managed device |
| `Device` | Generic non-router, non-SNMP device |

### LOCK (§3.2)

Valid values for the `LOCK` / `lock` attribute on routing
actions/events. Observed from spec examples:

| Value | Meaning |
|---|---|
| `PROTECT` | Soft reservation |
| `RELEASE` | Clear an existing lock/protect |

`DURATION` attribute (in ms) accompanies timed locks (e.g.
`lock='PROTECT' duration='1000'`).

### ITEM_TYPE (§3.3)

Value enum for category items. Confirmed from driver — **spec §3.3 is
image-based** so these come from `Api/Definitions/ItemType.cs`:

| Wire value | Meaning |
|---|---|
| `BLANK` | Empty slot |
| `SRCE` | Source (short form) |
| `SOURCE` | Source (long form) |
| `DEST` | Destination |
| `CATEGORY` | Nested category |
| `SALVO` | Salvo instance |
| `INHERIT` | Inherit from parent category |
| `TEXT` | Text entry |
| `FILE` | File reference |
| `CUSTOM` | Custom entry |

---

## Actions (§4)

Always wrapped in `<action mtid='…'>…</action>`. Body is one of:

### §4.1 Routing — `<routing TYPE='…' …/>`

| TYPE | Key attrs (from spec examples) |
|---|---|
| `ROUTE` | `DEVICE_NAME`, `DEVICE_TYPE`, `SRCE_NAME` / `SRCE_ID`, `DEST_NAME` / `DEST_ID`, `SRCE_LEVEL_NAME` / `SRCE_LEVEL_ID`, `DEST_LEVEL_NAME` / `DEST_LEVEL_ID`, optional `USE_TAGS` |
| `SRCE_LOCK` | `DEVICE_NAME`, `DEVICE_TYPE`, `SRCE_NAME` / `SRCE_ID`, `LEVEL_NAME` / `LEVEL_ID`, `LOCK`, optional `DURATION` |
| `DEST_LOCK` | `DEVICE_NAME`, `DEVICE_TYPE`, `DEST_NAME` / `DEST_ID`, `LEVEL_NAME` / `LEVEL_ID`, `LOCK`, optional `DURATION` |
| `LEVEL_MNE` | `DEVICE_NAME`, `DEVICE_TYPE`, `LEVEL_ID`, `MNEMONIC`, optional `ALT_MNE`, optional `ON_DEVICE` |
| `SRCE_MNE` | `DEVICE_NAME`, `DEVICE_TYPE`, `SRCE_ID`, `MNEMONIC`, optional `ALT_MNE`, optional `ON_DEVICE` |
| `DEST_MNE` | `DEVICE_NAME`, `DEVICE_TYPE`, `DEST_ID`, `MNEMONIC`, optional `ALT_MNE`, optional `ON_DEVICE` |
| `SRCE_ASSOC` | `LOGICAL_SRCE_ID`, `LOGICAL_LEVEL_ID`, optional `TARGET_DEVICE_NAME`, optional `TARGET_DEVICE_TYPE`, optional `TARGET_LEVEL_ID`, `TARGET_SRCE_ID` |
| `DEST_ASSOC` | `LOGICAL_DEST_ID`, `LOGICAL_LEVEL_ID`, optional `TARGET_DEVICE_NAME`, optional `TARGET_DEVICE_TYPE`, optional `TARGET_LEVEL_ID`, `TARGET_DEST_ID` |
| `SRCE_ASSOC_IP` | `LOGICAL_SRCE_ID`, `LOGICAL_LEVEL_ID`, `TARGET_DEVICE_NAME`, `TARGET_SENDER_NAME`, `SUB_DEVICE` |
| `DEST_ASSOC_IP` | `LOGICAL_DEST_ID`, `LOGICAL_LEVEL_ID`, `TARGET_DEVICE_NAME`, `TARGET_RECEIVER_NAME`, `SUB_DEVICE` |
| `RM_SRCE_TAGS` | `SRCE_ID`, `LEVEL_ID`, `TAGS` |
| `RM_DEST_TAGS` | `DEST_ID`, `LEVEL_ID`, `TAGS` |

Devices may be addressed by either **IP address** or **name** (§1.7);
routes may be expressed by either **ID** or **name** (§1.8). Use `*` for
wildcard subscriptions (§1.6).

### §4.2 Categories — `<category type='…' …/>`

| type | Attrs |
|---|---|
| `MODIFY_ITEM` | `category`, `index`, `item_type`, `value` |
| `MODIFY_ALL` | `category`, `item_type`, `value` (comma-separated list) |
| `MODIFY_DESC` | `category`, `description` |
| `CREATE` | `name`, `label`, `inherits` (parent category), `description` |
| `DELETE` | `category` |
| `DELETE_ITEM` | `category`, `index` |

### §4.3 Salvos — `<salvo type='…' …/>`

| type | Attrs |
|---|---|
| `RUN` | `group`, `instance` |
| `SAVE` | `group`, `instance` |
| `RENAME` | `group`, `instance`, `new_name` |
| `DESCRIPTION` | `group`, `instance`, `description` |
| `DELETE` | `group`, `instance` |

### §4.4 Device — `<device type='…' …/>`

| type | Attrs |
|---|---|
| `SET_VALUE` | `device_name`, `sub_device`, `object` (dotted path, e.g. `Status.Connected`), `value` |

---

## Subscriptions / Obtains / Events (§5)

TX children sent inside `<subscribe>` / `<obtain>` / `<unsubscribe>`;
RX events carry the same element shape back.

### §5.1 Routing — `<routing_change type='…' …/>`

| type | Addressing attrs |
|---|---|
| `ROUTE` | `device_name`, `device_type`, `dest_name` / `dest_id`, `level_name` / `level_id` |
| `SRCE_LOCK` | `device_name`, `device_type`, `srce_name` / `srce_id`, `level_name` / `level_id` |
| `DEST_LOCK` | `device_name`, `device_type`, `dest_name` / `dest_id`, `level_name` / `level_id` |
| `LEVEL_MNE` | `device_name`, `device_type`, `level_id` |
| `SRCE_MNE` | `device_name`, `device_type`, `srce_name`, `level_id` |
| `DEST_MNE` | `device_name`, `device_type`, `dest_name`, `level_id` |
| `RM_SRCE_TAGS` | `srce_id`, `level_id` |
| `RM_DEST_TAGS` | `dest_id`, `level_id` |

### §5.2 Categories — `<category_change type='…' …/>`

| type | Attrs |
|---|---|
| `CATEGORY_LIST` | none |
| `CATEGORY_DETAILS` | `category` |

### §5.3 Salvos — `<salvo_change type='…' …/>`

| type | Attrs |
|---|---|
| `GROUP_LIST` | none |
| `INSTANCE_LIST` | `group` |
| `INSTANCE_DETAILS` | `group`, `instance` |

### §5.4 Devices — `<device_change type='…' …/>`

| type | Attrs |
|---|---|
| `LIST` | none |
| `DETAILS` | `ip_address`, `device_type` |
| `VALUE` | `device_name`, `sub_device`, `object` |

### §5.5 Data Stores — `<datastore_change …/>`

Subscription/obtain by file path inside a Cerebrum data store; events
echo the element back with a `type` attribute.

| Role | Wire |
|---|---|
| TX | `<datastore_change name='Global Data Files\Index.xml'/>` |
| RX | `<datastore_change name='Global Data Files\Index.xml' type='ATTRIBUTE'/>` |

---

## Error codes (§6, `<NACK>` payload)

Wire form (verified 2026-04-26 against a real Cerebrum):

```
<NACK MTID="N" ERROR="<CODE>" ERROR_CODE="<ID>"/>
```

Note: spec §6 documents attribute names `id` / `code` / `description`,
but **live Cerebrum uses `ERROR_CODE` / `ERROR` and no description**.
Decoder accepts either form; encoder emits the wire-actual UPPERCASE
form.

| ID | ERROR (Code) | Description |
|---:|---|---|
| 0 | `INVALID_USER_OR_PASS` | specified username or password is invalid |
| 1 | `MTID_ERROR` | a message type identifier was not specified |
| 2 | `UNKNOWN_COMMAND` | an unknown command has been specified |
| 3 | `INVALID_XML` | the message XML from a client cannot be loaded |
| 4 | `SERVER_INACTIVE` | the connected server is inactive |
| 5 | `UNKNOWN_CONNECTION` | the connection is unrecognized — a new one must be established |
| 6 | `NOT_LOGGED_IN` | a successful login is required |
| 7 | `COMMAND_MISSING_PARAMETERS` | both a username and password must be specified |
| 8 | `ONE_OR_MORE_ACTIONS_INVALID` | the specified action failed to complete |
| 9 | `ONE_OR_MORE_EVENTS_INVALID` | the specified event subscription failed to complete |
| 10 | `ONE_OR_MORE_OBTAINS_INVALID` | the specified obtain failed to complete |
| 11 | `RESPONSE_TOO_LARGE` | the XML message is too large to be sent to a client |
| 12 | `NO_LICENCE_AVAILABLE` | the Cerebrum server has no licences available (British spelling) |
| 13 | `OK` | the request completed successfully |

> Every NACK code becomes a `compliance.Event` named `cerebrum_nack_<code>`.

---

## Change history (§7)

| Ver | Date | Change |
|---|---|---|
| 0.01 | 14/09/2021 | First release (V2.2.14538) |
| 0.02 | 08/12/2021 | Correct attribute names (DEST, LEVEL) in §5.1.1 / §5.1.4 / §5.1.6 |
| 0.03 | 18/02/2022 | Added change/obtain/subscribe for generic device values |
| 0.04 | 18/03/2022 | Added missing *Delete Category Item* |
| 0.05 | 05/04/2022 | Source/destination protect/lock now allows "all level" |
| 0.06 | 24/05/2022 | Source mnemonic obtain/subscribe includes `RM_TAGS` |
| 0.07 | 30/05/2022 | Destination mnemonic obtain/subscribe includes `RM_TAGS` |
| 0.08 | 31/05/2022 | Semantic mistakes / examples corrected |
| 0.09 | 15/02/2023 | Data Stores added |
| 0.10 | 15/11/2023 | Text-encoding method documented in intro |
| 0.11 | 02/08/2024 | `SRCE_ASSOC_IP` + `DEST_ASSOC_IP` added |
| 0.12 | 13/02/2025 | Error-code list added |
| 0.13 | 18/02/2025 | Error-code IDs added |

---

## Compliance events to surface

Every deviation fires a named compliance event rather than being
silently absorbed. Planned names:

- `cerebrum_case_normalized` — peer sent a non-lowercase element/attribute
- `cerebrum_nack_<code>` — one per error code in §6
- `cerebrum_busy_received` — server returned BUSY
- `cerebrum_unknown_notification` — notification root / TYPE we don't recognise
- `cerebrum_mtid_reused` — the same mtid arrived on two different in-flight requests
- `cerebrum_server_inactive` — `CONNECTED_SERVER_ACTIVE='0'` seen on poll_reply
