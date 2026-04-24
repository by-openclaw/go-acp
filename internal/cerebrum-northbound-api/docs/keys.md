# Cerebrum Northbound API 0.13 — wire-key catalogue

Scope: every XML **element name**, **attribute name**, and **enum value**
we know is used on the wire, grouped by role. Fact-only; no verbatim
third-party material. When the OCR'd spec and the Skyline DataMiner
driver disagree, the spec wins here — the driver is listed alongside as
independent corroboration.

**Sources** (in priority order):

1. **Primary.** EVS *Cerebrum Northbound API 0.13* PDF — see
   [../assets/Cerebrum Northbound API 0v13.pdf](../assets/Cerebrum Northbound API 0v13.pdf) and the matching
   [../assets/Cerebrum Northbound API 0v13.docx](../assets/Cerebrum Northbound API 0v13.docx).
2. **Secondary (NDA-gated, reference only).** Skyline Communications
   DataMiner driver DMS-DRV-7371 *EVS Cerebrum* v1.1.0.2. Held under
   Skyline licence. **Not committed** to this repo — see
   `.gitignore` (`/tmp/`). Driver XML at
   `tmp/cerebrum-docx/cerebrum.xml` when present locally. This page
   lists the **facts** derived from that driver (names, shapes, enum
   values) — never verbatim XML.
3. **Tertiary.** OCR extract at
   [../assets/cerebrum-nb-api-ocr.txt](../assets/cerebrum-nb-api-ocr.txt) — useful for
   prose paragraphs the driver omits.

---

## Transport envelope

| Fact | Value |
|---|---|
| Transport | XML over WebSocket (`ws://` / `wss://`) |
| Framing | One XML document per WebSocket text message |
| Concurrency | Each WebSocket connection is an independent session |
| Character set | UTF-8 |
| Declaration | Commands serialise as an `XElement.ToString()` — no XML prolog required on the wire |

---

## Top-level messages

Requests — sent from consumer → Cerebrum:

| Root element | Purpose |
|---|---|
| `LOGIN` | Open a session, authenticate |
| `SUBSCRIBE` | Register one or more subscription filters (children describe what to subscribe to) |
| `ACTION` | Perform a mutation — routing change, salvo run, category edit, etc. Body carries the per-topic element. |

Responses + notifications — sent from Cerebrum → consumer:

| Root element | Purpose |
|---|---|
| `LOGIN_REPLY` | Result of LOGIN. Carries `API_VER`. |
| `ACK` | Generic success ack for a request |
| `NACK` | Generic failure ack; carries `ERROR` attribute |
| `BUSY` | Temporary-failure ack; retry expected |
| `WILDCARD_COMPLETE` | End-of-stream marker when a `*`-wildcard subscription has finished its initial fan-out |
| `CATEGORY_CHANGE` | Category subscription update; discriminated by `TYPE` |
| `DEVICE_CHANGE` | Device subscription update; discriminated by `TYPE` |
| `ROUTING_CHANGE` | Routing subscription update; discriminated by `TYPE` |
| `SALVO_CHANGE` | Salvo subscription update; discriminated by `TYPE` |

> **Not exercised by the Skyline driver** but present in the PDF spec:
> `LOGOUT`, `POLL`, `OBTAIN`, `UNSUBSCRIBE`, `UNSUBSCRIBE_ALL`. Treat
> these as spec-defined but needing fixture-level confirmation before
> we implement them.

---

## Universal attributes

| Attribute | Used on | Meaning |
|---|---|---|
| `MTID` | Every request + reply root | Message Transaction ID — unsigned 32-bit integer, correlates request and reply |
| `TYPE` | `ACTION`, `CATEGORY_CHANGE`, `DEVICE_CHANGE`, `ROUTING_CHANGE`, `SALVO_CHANGE` | Discriminator for the child-action / child-notification variant |
| `ERROR` | `NACK` | Error code — string, see [Error codes](#error-codes) |
| `API_VER` | `LOGIN_REPLY` | Server-reported API version string |

---

## LOGIN

| Attribute | Direction | Notes |
|---|---|---|
| `MTID` | req | transaction id |
| `USERNAME` | req | credential |
| `PASSWORD` | req | credential |
| `API_VER` | reply | version advertised by the server |

Login errors (in `NACK ERROR` on a LOGIN request):

- `INVALID_USER_OR_PASS`
- `NOT_LOGGED_IN`
- `NO_LICENCE_AVAILABLE`  (note British spelling — spec wire form)

---

## SUBSCRIBE

Body contains one child element per subscription. Child elements come
from the subscription catalogues below:

- Category subscriptions → filter `CATEGORY_CHANGE` fan-out
- Device subscriptions → filter `DEVICE_CHANGE` fan-out
- Routing subscriptions → filter `ROUTING_CHANGE` fan-out
- Salvo subscriptions → filter `SALVO_CHANGE` fan-out

Common addressing idiom across subscriptions: `"*"` as a wildcard on
any `*_ID` or `LEVEL_ID` attribute; server responds with matching
elements followed by a `WILDCARD_COMPLETE` terminator.

---

## ACTION

`ACTION` carries `MTID` + one child element whose name is the action
verb and whose attributes are the action parameters. Known action
verbs observed in the driver:

### Category actions

| TYPE value (on child) | Attributes |
|---|---|
| `MODIFY_ITEM` | `CATEGORY` (or `ParentCategory` field → `CATEGORY`), `INDEX`, `ITEM_TYPE`, `VALUE` |
| `CREATE` | `NAME`, `LABEL`, `INHERITS`, `DESCRIPTION` |
| `DELETE` | `CATEGORY` |
| `DELETE_ITEM` | `CATEGORY`, `INDEX` |
| `MODIFY_DESC` | `CATEGORY`, `DESCRIPTION` |

### Routing actions

| TYPE value | Attributes |
|---|---|
| `DEST_ASSOC` | `LOGICAL_DEST_ID`, `LOGICAL_LEVEL_ID`, optional `TARGET_DEVICE_NAME`, optional `TARGET_DEVICE_TYPE`, optional `TARGET_LEVEL_ID`, `TARGET_DEST_ID` |
| `SRCE_ASSOC` | `LOGICAL_SRCE_ID`, `LOGICAL_LEVEL_ID`, optional `TARGET_DEVICE_NAME`, optional `TARGET_DEVICE_TYPE`, optional `TARGET_LEVEL_ID`, `TARGET_SRCE_ID` |
| `DEST_MNE` | `IP_ADDRESS`, `DEVICE_TYPE`, `DEST_ID`, `MNEMONIC`, optional `ALT_MNE`, optional `ON_DEVICE` |
| `LEVEL_MNE` | `IP_ADDRESS`, `DEVICE_TYPE`, `LEVEL_ID`, `MNEMONIC`, optional `ALT_MNE`, optional `ON_DEVICE` |
| `SRCE_MNE` | `IP_ADDRESS`, `DEVICE_TYPE`, `SRCE_ID`, `MNEMONIC`, optional `ALT_MNE`, optional `ON_DEVICE` |
| `DEST_LOCK` | `IP_ADDRESS`, `DEST_ID`, `LEVEL_ID`, `LOCK` |
| `SRCE_LOCK` | `IP_ADDRESS`, `SRCE_ID`, `LEVEL_ID`, `LOCK` |
| `ROUTE` | `IP_ADDRESS`, `DEVICE_TYPE`; variable: `SRCE_ID` / `SRCE_NAME`, `DEST_ID` / `DEST_NAME`, `SRCE_LEVEL_ID` / `SRCE_LEVEL_NAME`, `DEST_LEVEL_ID` / `DEST_LEVEL_NAME`, optional `USE_TAGS` |
| `RM_DEST_TAGS` | `DEST_ID`, `LEVEL_ID`, `TAGS` |
| `RM_SRCE_TAGS` | `SRCE_ID`, `LEVEL_ID`, `TAGS` |

### Salvo actions

Salvo actions share a `GROUP` attribute and (except for the group-level
`SAVE`) an `INSTANCE`:

| TYPE value | Attributes |
|---|---|
| `DELETE`   | `GROUP`, `INSTANCE` |
| `RENAME`   | `GROUP`, `INSTANCE`, `NEW_NAME` |
| `RUN`      | `GROUP`, `INSTANCE` |
| `SAVE`     | `GROUP`, optional `INSTANCE` |
| `DESCRIPTION` | `GROUP`, `INSTANCE`, `DESCRIPTION` |

---

## Subscription child elements (sent inside SUBSCRIBE body)

| Subscription kind | Child `TYPE` values |
|---|---|
| Category | `CATEGORY_DETAILS` (with `CATEGORY`), `CATEGORY_LIST` |
| Device | `DETAILS` (with `IP_ADDRESS`, `DEVICE_TYPE`), `LIST` |
| Routing | `DEST_MNE`, `DEST_LOCK`, `LEVEL_MNE`, `ROUTE`, `RM_DEST_TAGS`, `RM_SRCE_TAGS`, `SRCE_MNE`, `SRCE_LOCK` — each with `IP_ADDRESS` + `DEVICE_TYPE` + the relevant `*_ID` / `LEVEL_ID` (any of which may be `"*"`) |
| Salvo | `GROUP_LIST`, `INSTANCE_DETAILS` (with `GROUP`, `INSTANCE`), `INSTANCE_LIST` (with `GROUP`) |

---

## Notification discriminators

Every `*_CHANGE` root reuses the same `TYPE` values as its
subscription counterpart:

| Root | TYPE values |
|---|---|
| `CATEGORY_CHANGE` | `CATEGORY_DETAILS`, `CATEGORY_LIST` |
| `DEVICE_CHANGE`   | `DETAILS`, `LIST` |
| `ROUTING_CHANGE`  | `DEST_MNE`, `DEST_LOCK`, `LEVEL_MNE`, `ROUTE`, `SRCE_MNE`, `SRCE_LOCK` |
| `SALVO_CHANGE`    | `GROUP_LIST`, `INSTANCE_DETAILS`, `INSTANCE_LIST` |

---

## Enumerations

### `ITEM_TYPE`

| Wire value | Meaning |
|---|---|
| `BLANK`    | unused / placeholder |
| `SRCE`     | source (short form) |
| `SOURCE`   | source (long form) |
| `DEST`     | destination |
| `CATEGORY` | category |
| `SALVO`    | salvo |
| `INHERIT`  | inherit parent |
| `TEXT`     | text entry |
| `FILE`     | file reference |
| `CUSTOM`   | custom entry |

### `LOCK`

Options observed: `AVAILABLE`, `LOCKED`, plus a server-reported
`UNLOCK_TIME` attribute when locked.

### Route-master constants

Special "any / router" addresses used on routing actions:

- `IP_ADDRESS = "0.0.0.0"` → route-master
- `DEVICE_TYPE = "ROUTER"` → route-master

### Common device elements inside `DETAILS`

Nested `<SERVICE IP=...>` and `<INSTANCE DEVICE_ID NAME TYPE>` children
are returned inside `DEVICE_CHANGE` `TYPE="DETAILS"`. Enumerated
device states (from OCR): `None`, `Up`, `Active`.

---

## Error codes (`NACK ERROR`)

Confirmed from the driver:

- `INVALID_USER_OR_PASS`
- `NOT_LOGGED_IN`
- `NO_LICENCE_AVAILABLE` (note spec British spelling)

The spec PDF §6 lists the full catalogue — when those pages are OCR'd
completely, extend this table. Every error code lands as
`compliance.Event` with a fixed event name `cerebrum_nack_<code>`.

---

## What we still need from the spec PDF

Items the driver does not cover but the OCR mentions:

1. `LOGOUT`, `POLL`, `OBTAIN`, `UNSUBSCRIBE`, `UNSUBSCRIBE_ALL` — full
   attribute list + reply shape
2. Keep-alive / heartbeat cadence (if any — the WebSocket layer may
   handle this)
3. Reconnect / session-resume semantics
4. TLS requirement + default port (driver defaults are data points on
   `ElementConnections`, not wire-level)
5. Redundancy wire details — the driver exposes rich primary /
   secondary server telemetry but the wire flow that delivers it
   needs a dedicated pcap
6. Full error-code catalogue from §6
