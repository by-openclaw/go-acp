# ADR-0021 — wire-trace JSONL contract

Status: accepted

## Context

The same wire-byte trace serves three uses across the project:

1. **Live captures** — every connector's `--capture` flag emits a
   line per frame so dev runs can be replayed offline.
2. **Codec test fixtures** — encoder + decoder round-trip tests load
   committed `frames.jsonl` files (per ADR-0020 Buckets 1, 2, 3).
3. **Replay** — the `replay` verb (ADR-0002) feeds the same lines back
   onto the wire (or into the codec) to reproduce a session against a
   real or fake peer.

If the line schema differs across uses, every consumer grows
per-source dialect handling and every codec change risks silently
breaking one of the three. The format must be one binding contract,
checked at the input boundary.

## Decision

One JSON object per line, one frame per line, UTF-8 + LF, no trailing
whitespace. Same shape regardless of which of the three uses produced
or consumes the file.

### Required fields

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | int | starts at `1`; bumps on a breaking change to required fields or to the meaning of an existing field |
| `dir` | string | `"tx"` (sent by `dhs`) or `"rx"` (received from peer) |
| `hex` | string | raw wire bytes the codec encodes / decodes — lowercase, no spaces, no `0x` prefix |

### Optional fields

| Field | Type | When recommended |
|---|---|---|
| `ts` | RFC3339Nano string | live captures: real timestamp; fixtures: frozen value or omitted |
| `proto` | string | `"acp1"` / `"acp2"` / `"emberplus"` / ... — redundant with folder location, but lets a single `frames.jsonl` be self-describing if extracted |
| `transport` | string | `"udp"` / `"tcp"` / `"tcp-slip"` / `"an2"` / `"s101"` / `"ws"` / ... — REQUIRED when the same protocol runs over multiple transports (Ember+ over S101, OSC over UDP / TCP-LP / TCP-SLIP, ...) |
| `peer` | string | `"<ip>:<port>"` of the remote endpoint — live captures only; omitted in committed fixtures (deployment detail, not codec-intrinsic) |
| `session` | string | UUID per `--capture` session — lets a multi-session live capture be split |
| `dhs_version` | string | `buildinfo.Version` (per ADR-0018) — provenance for replay |
| `note` | string | scenario marker (e.g. `"set_pre"`, `"announce_after_revert"`) — lets `replay --stop-at <note>` fail fast |

### Reader / writer rules

| Rule | Why |
|---|---|
| Every writer MUST set `schema_version` (no implicit default) | Forward-compat readers refuse unversioned input |
| A reader MUST tolerate unknown optional fields | Forward-compat: optional fields can be added without bumping schema_version |
| A reader MUST reject lines whose `schema_version` is HIGHER than it knows | Loud break if a producer outpaces a consumer |
| One JSON object per line — no pretty-printing, no array wrapper | Diffable in PRs; line-streaming consumers don't buffer |
| `hex` MUST be the bytes that hit the socket (post-framing for stream protocols, full datagram for UDP) | Codec round-trip is well-defined |
| Adding an optional field is **non-breaking** if `schema_version` stays at 1 | Standard JSON-schema additive evolution |
| Removing or retyping a required field bumps `schema_version` | Loud break, never silent |

### Example

```json
{"schema_version":1,"ts":"2026-04-19T10:51:58.726337Z","proto":"acp2","transport":"an2","dir":"tx","hex":"c63500000100000100","peer":"10.41.40.195:2072","session":"a4e0a7b0-5c4f-11f0-8b9d-0242ac110002","dhs_version":"v0.7.1"}
{"schema_version":1,"ts":"2026-04-19T10:51:58.729482Z","proto":"acp2","transport":"an2","dir":"rx","hex":"c635000001010003000001","peer":"10.41.40.195:2072","session":"a4e0a7b0-5c4f-11f0-8b9d-0242ac110002","dhs_version":"v0.7.1","note":"version_reply"}
```

A committed fixture has the same shape but typically omits `peer` /
`session` / `dhs_version` and freezes `ts`:

```json
{"schema_version":1,"dir":"tx","hex":"c635000001000001 00","proto":"acp2","transport":"an2"}
{"schema_version":1,"dir":"rx","hex":"c635000001010003000001","proto":"acp2","transport":"an2","note":"version_reply"}
```

## Replay semantics

The `replay` verb (per ADR-0002) consumes a `frames.jsonl` and
operates in one of three modes.

### Modes

| Mode flag | Interpretation of `dir` | Use case |
|---|---|---|
| `--as-client` (default) | `tx` → send to peer; `rx` → expect from peer (assert match within tolerance) | Reproduce a bug against a real device without scripting |
| `--as-server` | `rx` → wait for peer to send (assert match); `tx` → send to peer | Replace a missing device — stand up a fake matrix from a captured session |
| `--validate-only` | `dir` ignored — decode every line, assert codec succeeds | Codec smoke test, no peer required |

### Timing

| Flag | Behaviour |
|---|---|
| (default) | as-fast-as-possible — back-to-back |
| `--realtime` | honor `ts` deltas between consecutive frames |
| `--delay D` | constant delay D between frames |

### Mismatch handling

| Flag | Behaviour |
|---|---|
| (default) | abort on first mismatch (`expected hex vs got hex`) and exit non-zero |
| `--continue-on-mismatch` | record every mismatch, replay to end, exit non-zero with summary |
| `--stop-at <note>` | abort after reaching the frame whose `note` equals `<note>`, regardless of mismatch |

### Required by replayer

The replayer needs `transport` to wrap bytes with the correct framer
(UDP datagram vs TCP-length-prefix vs SLIP vs S101 vs AN2). Without
`transport`, `replay` MUST refuse to run for stream-based protocols
and warn for datagram-based ones.

## Forbidden

- Reporting a frame without `schema_version` / `dir` / `hex`.
- Pretty-printing across multiple lines.
- Wrapping the file in a JSON array (`[ {...}, {...} ]`).
- Adding required fields without bumping `schema_version`.
- Renaming an existing field (it's a remove + add — bump
  `schema_version`).
- Inventing a per-bucket dialect (e.g. `decoded.jsonl` with a
  different shape, `events.json` for live captures only).

## Consequences

- A single library function (`internal/wiretrace/`) reads, writes,
  and validates `frames.jsonl` files. Codec tests, live capture,
  replay, and dissector cross-check all use that one library.
- A captured `frames.jsonl` is portable — copy it from any bucket to
  any other, attach to a bug report, replay locally — without
  conversion.
- A new optional field (e.g. `latency_ns`) lands without breaking any
  reader.
- A breaking schema change is loud: readers refuse to parse
  `schema_version=2` files, contributors see the bump in PR review.
