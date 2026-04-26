# CLAUDE.md — Cerebrum NB (EVS Cerebrum Northbound API / "Neuron Bridge")

Atomic per-protocol context for the EVS Cerebrum Northbound API plugin
(branded **Neuron Bridge** in some EVS docs). Read the root `CLAUDE.md`
first for cross-cutting rules (registry, compliance, error hierarchy, Go
idioms); this file holds Cerebrum-specific wire spec + quirks.

The full element / attribute / enum catalogue lives in
[docs/keys.md](docs/keys.md). This file is the wire-layer + quirk index;
it points at `keys.md` rather than duplicating its tables.

---

## Folder layout (this package)

```
internal/cerebrum-nb/
├── CLAUDE.md        ← this file
├── codec/
│   ├── ws/          stdlib-only RFC 6455 WebSocket client (lift-ready)
│   └── xml/         stdlib-only encoder/decoder for every §2/§4/§5 element
├── consumer/        package cerebrum_nb — implements protocol.Protocol
├── provider/        package cerebrum_nb — implements provider.Provider (later PR)
├── wireshark/       dhs_cerebrum_nb.lua — full WS-frame + XML-payload
│                    dissector covering every command + event TYPE with
│                    per-message Info column
├── docs/
│   ├── keys.md      authoritative element / attribute / enum catalogue
│   ├── consumer.md  CLI walkthrough + portable-Windows recipe
│   └── README.md    one-page overview
└── assets/          spec PDFs + DOCX + OCR (NDA driver XML gitignored)
```

## Authoritative spec

- **EVS Cerebrum Northbound API v0.13** (2025-02-18) — at
  [assets/Cerebrum Northbound API 0v13.pdf](assets/Cerebrum%20Northbound%20API%200v13.pdf)
  / [assets/Cerebrum Northbound API 0v13.docx](assets/Cerebrum%20Northbound%20API%200v13.docx).
- Full text extracted at
  [assets/cerebrum_northbound_api_full_v0_13.docx](assets/cerebrum_northbound_api_full_v0_13.docx).
  OCR companion at [assets/cerebrum-nb-api-ocr.txt](assets/cerebrum-nb-api-ocr.txt).
- Third-party vendor reference driver — held under NDA, gitignored,
  cited only as a secondary cross-check.

When the spec, the device, and the codec disagree, the dissector breaks
the tie first. Do NOT rely on the OCR text for byte-exact parsing — it
is image-derived and lossy; always verify against the DOCX or the live
WebSocket stream.

---

## Plugin scope (locked 2026-04-26)

| Aspect | Value |
|---|---|
| Registry name | `cerebrum-nb` |
| Default port | **40007** (configurable in the Cerebrum app) |
| Transport | WebSocket — `ws://host:40007` (TLS via `wss://`) |
| URL path | **none** — connect to `wss://host:port` only; no HTTP path |
| Framing | One XML document per WebSocket text message (UTF-8) |
| Licensing | One northbound licence per WebSocket connection |
| Pre-requisites | (1) sufficient northbound licences; (2) API enabled in Cerebrum; (3) NB-enabled user account |

Consumer-first. Provider lands in a follow-up PR (no Cerebrum emulator
yet; we don't dogfood until we can talk to a real device or the EVS
training simulator).

---

## Wire layer

### WebSocket (RFC 6455) — hand-rolled

Implemented in [codec/ws/](codec/ws/). Stdlib-only, no `acp/*` imports —
lift-ready per `feedback_codec_isolation`.

| Concern | Behaviour |
|---|---|
| Handshake | Standard `Sec-WebSocket-Key`/`Sec-WebSocket-Accept` per §1.3 |
| URL path | request `/` (Cerebrum ignores path; spec says no path) |
| Sub-protocol | none — Cerebrum does not use `Sec-WebSocket-Protocol` |
| Client-to-server frames | Always masked (RFC 6455 §5.3) |
| Server-to-client frames | Never masked |
| Opcode used | `0x1` Text (XML payload) — never Binary |
| Fragmentation | Accepted on RX; on TX we always send a single non-fragmented frame (the largest XML payload we emit fits comfortably) |
| Ping/Pong | Reply Pong on RX Ping; emit Ping every 30 s as fallback keep-alive |
| Close | Send `0x8` Close with code 1000 on graceful Disconnect |
| Max payload | 16 MiB cap on RX (config-driven; rejecting larger fires `cerebrum_nack_response_too_large`) |

### XML element catalogue

Every element / attribute / enum lives in [docs/keys.md](docs/keys.md).
Quick index:

| Spec section | Catalogue heading |
|---|---|
| §2 Commands | "Top-level commands" |
| §3 Definitions | "Definitions (§3)" — DEVICE_TYPE, LOCK, ITEM_TYPE |
| §4 Actions | "Actions (§4)" — Routing / Categories / Salvos / Device |
| §5 Subscriptions / Events | "Subscriptions / Obtains / Events (§5)" |
| §6 Error codes | "Error codes (§6, `<nack>` payload)" |

### Element-name case quirk

Spec examples are **inconsistent** between lowercase (§2, §4.2-4.4, §5)
and UPPERCASE (§4.1). **Live wire reality (verified 2026-04-26 against
a real Cerebrum):** the server emits AND requires UPPERCASE
elements + attributes. A lowercase `<login mtid="1"/>` is rejected
with `MTID_ERROR` because the server doesn't recognise lowercase
`mtid` as the MTID field. We:

- **Emit UPPERCASE** on the wire (matches reality).
- **Accept any case** on RX (case-folded AST); fire
  `cerebrum_case_normalized` only if non-UPPERCASE seen (rare —
  defensive against future spec-strict lowercase peers).

Treat **UPPERCASE** as the canonical form. See `keys.md` "Element
case on the wire" for the live evidence.

### Message Transaction ID (mtid)

| Fact | Value |
|---|---|
| Range | unsigned 32-bit; wraps at `2^32 - 1` |
| Encoding | decimal string on the wire (e.g. `mtid="42"`) |
| Required | every TX command |
| Echoed | every RX `<ack>` / `<nack>` / `<busy>` and every direct event reply |
| Reuse | Every in-flight request MUST have a unique mtid; reuse fires `cerebrum_mtid_reused` |

Allocation: `atomic.Uint32` counter starting at 1 (reserve 0 as
"unset"); allocate next, lookup pending request by mtid, free on
ack/nack/busy. Asynchronous events (no request-side mtid) carry the
mtid of the originating subscribe.

### Subscribe vs Obtain

Both share identical body shape (§5 preamble: "TX/RX symmetry rule"):

| Verb | Behaviour |
|---|---|
| `<subscribe>` | Register for live events; server keeps the subscription until `<unsubscribe>` or socket close |
| `<obtain>` | One-shot snapshot; server replies with current state once and forgets |
| `<unsubscribe>` | Remove a specific subscription (body must match the original `<subscribe>`) |
| `<unsubscribe_all>` | Remove every active subscription (mtid only, no body) |

Use `obtain` for `Walk` (one-shot tree dump). Use `subscribe` for
`Watch` (continuous events).

### Wildcards (§1.6)

`*` is a wildcard for the addressing attributes of `<subscribe>` /
`<obtain>` children. Example: subscribe to all routing events with
`<routing_change type='ROUTE' device_name='*' device_type='*'/>`.

### Address-by-name vs by-IP (§1.7)

Devices may be addressed by either `device_name='RTR-A'` or
`ip_address='10.0.0.5'`. Per spec §1.7 these are equivalent; the
server resolves both. We emit whichever the caller passes; we do not
proactively rewrite.

### Routes by ID vs by name (§1.8)

Routes carry `_id` and `_name` variants in parallel (e.g. `srce_id`
+ `srce_name`). Either or both may be supplied on TX; events always
carry **both** on RX. On RX we keep both fields populated in the
canonical model; on TX prefer `_id` when known (more compact) and fall
back to `_name`.

---

## Portable Windows layout (locked 2026-04-26)

When `dhs.exe` runs on Windows from a non-installed location (i.e. not
under `Program Files`), the working directory for state is **the .exe
directory**, not `%APPDATA%\dhs\`. This is a deliberate divergence from
root `CLAUDE.md` Storage rules — it makes Cerebrum deployment a single
copy-paste of the binary onto the Cerebrum host with everything
self-contained.

Rule:

- If `--data-dir` is set → use it.
- Else if `os.Executable()` resolves and the binary is on Windows → use
  `<exe-dir>` as `--data-dir`.
- Else → fall back to the root `CLAUDE.md` per-OS defaults.

Layout next to the binary:

```
<exe-dir>/
├── dhs.exe
├── config.yaml          (optional; read on startup)
├── logs/
│   └── dhs.log          (rotated by date; keep 7)
└── captures/
    ├── pcap/            (when --capture is set)
    └── xml/             (raw RX/TX XML when --debug-xml)
```

The same rule applies to other protocols on Windows; cerebrum-nb is
the trigger but the implementation lives in
[`internal/storage/`](../storage/) so every plugin benefits.

Documented in [docs/consumer.md](docs/consumer.md) "Install on a
Cerebrum host".

---

## CLI verbs (consumer)

```
dhs consumer cerebrum-nb connect      <host>          # login + ping loop only
dhs consumer cerebrum-nb listen       <host>          # subscribe to ALL *_CHANGE
dhs consumer cerebrum-nb list-devices <host>          # one-shot device list
dhs consumer cerebrum-nb list-routers <host>          # one-shot router list (DEVICE_TYPE='Router')
dhs consumer cerebrum-nb walk         <host>          # full obtain across all §5 types
```

Common flags: `--port 40007` `--user <u>` `--pass <p>` `--tls` (use
`wss://`) `--insecure-skip-verify` `--debug-xml` (write RX+TX XML to
`captures/xml/`) `--data-dir <path>` (override portable layout).

---

## Compliance events

Every spec deviation fires a named compliance event. Every NACK code
becomes a `cerebrum_nack_<code>` event. Full list in
[docs/keys.md](docs/keys.md) "Compliance events to surface".

Headline events:

```
cerebrum_case_normalized           peer sent non-lowercase element/attribute
cerebrum_busy_received             server returned <busy>; retry after backoff
cerebrum_unknown_notification      RX root or TYPE not in keys.md
cerebrum_mtid_reused               same mtid on two in-flight requests
cerebrum_server_inactive           poll_reply CONNECTED_SERVER_ACTIVE='0'
cerebrum_response_too_large        RX frame exceeded the 16 MiB cap
cerebrum_nack_<code>               one per §6 error-code entry
```

Every event is registered in `consumer/compliance_events.go` and
exposed via the standard `compliance.Profile` accessor.

---

## What NOT to do

- Never connect to a Cerebrum without first verifying licence
  availability — login fails with `nack code='NO_LICENCE_AVAILABLE'`;
  treat as fatal, do not retry in a tight loop.
- Never reuse an in-flight `mtid` — server may correlate the second
  request with the first response and drop yours silently.
- Never assume `<ack>` means "applied". Routing actions arrive as
  `<ack>` first, then a separate `<routing_change>` event when the
  matrix actually crosses the point. Two-stage commit; treat the event
  as the truth.
- Never lowercase elements on TX. Spec §2 / §4.2-4.4 / §5 examples
  show lowercase but real Cerebrum servers reject it (verified
  2026-04-26 — lowercase `<login mtid=...>` returns
  `MTID_ERROR`). Wire-actual is UPPERCASE everywhere.
- Never include the binary in `Program Files` for Cerebrum deploys —
  portable layout requires write access to the .exe directory; UAC will
  block writes under `Program Files`. Drop into `C:\dhs\` or similar.
- Never persist NB credentials to `config.yaml` in plaintext on the
  Cerebrum host — environment variables only (`DHS_CEREBRUM_USER` /
  `DHS_CEREBRUM_PASS`) until we wire DPAPI.
- Never silently swallow `<nack>`. Always fire the matching compliance
  event before bubbling the error up to the caller.
- Never trust the spec PDF for byte-exact parsing — it has image-only
  tables in §3.3 (ITEM_TYPE) and §6 (Error codes); use the DOCX text
  + the third-party reference driver cross-check (per `keys.md`
  source notes) + a live pcap.

---

## Known deviations from spec

None observed yet — first real-device interop will surface them. Each
will be documented here with the byte-level evidence (Wireshark
capture path, frame number, expected vs observed XML) and a compliance
event ID.

---

## Quirks + landmines

1. **No HTTP path.** Cerebrum's WebSocket endpoint is just `ws://host:40007`
   — no `/api`, no `/nb`. Hitting any path returns 404 from the embedded
   server.

2. **No sub-protocol.** Cerebrum does not negotiate via
   `Sec-WebSocket-Protocol`. Don't send one; spec doesn't define one.

3. **Element case is UPPERCASE on the wire (not lowercase).** Spec
   examples show mixed case but live Cerebrum requires UPPERCASE
   everywhere. Lowercase TX is rejected with `MTID_ERROR`. Emit
   UPPERCASE, accept any.

4. **mtid is decimal-as-string.** Not hex, not binary; serialise via
   `strconv.FormatUint`.

5. **`<obtain>` and `<subscribe>` share their body grammar.** Same XML
   children, different verb. Build a shared encoder; switch the root.

6. **Wildcard `*` only in `<subscribe>` / `<obtain>`.** Actions never
   accept wildcards — sending `device_name='*'` to `<action>` fires
   `cerebrum_nack_one_or_more_actions_invalid`.

7. **`device_type='Router'` covers Routers AND the route-master ROUTER
   device.** §3.1 explicitly lumps them. If you want only the
   route-master, post-filter on `device_name`.

8. **Redundancy is in the keep-alive.** `<poll>` returns
   `CONNECTED_SERVER_ACTIVE`. If `0`, the server we're connected to is
   the standby — every `<action>` will return
   `nack code='SERVER_INACTIVE'` until the active server is reachable.
   Surface this as `cerebrum_server_inactive` and offer a CLI flag
   `--auto-failover` (later) that reconnects to the named secondary.

9. **British spelling: `NO_LICENCE_AVAILABLE`.** Not `LICENSE`. Spec
   §6, ID 12. Match exactly when string-comparing.

10. **`OK` (ID 13) is a NACK code with success semantics.** Never
    actually emitted by live Cerebrum servers we've observed; listed
    in spec §6 as a placeholder. Treat any `<NACK ERROR='OK'>` as a
    bug-report event (`cerebrum_nack_ok`) — should never reach us.

11. **Asynchronous events do not echo a request mtid.** Events are
    "free-standing" XML at the root; the mtid attribute is the
    subscribe's mtid, not a per-event one. Don't try to correlate
    events back to specific subscribes — track by element shape +
    addressing.

12. **One licence per WebSocket.** Multiple `dhs` instances against the
    same Cerebrum each consume a licence; cap your fanout. The
    consumer plugin reuses one connection across all subscribes.
