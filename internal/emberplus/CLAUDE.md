# CLAUDE.md — Ember+ (Lawo)

Atomic per-protocol context for the Ember+ plugin. Read the root `CLAUDE.md`
first for cross-cutting rules; this file holds Ember+-specific wire + glow
semantics.

Authoritative refs:
- `internal/emberplus/assets/Ember+ Documentation.pdf`
- `internal/emberplus/assets/Ember+ Formulas.pdf`

Wireshark dissector: `./wireshark/dhs_emberplus.lua`.

Testbed emulator: `internal/emberplus/assets/smh/` (BY-RESEARCH TS emulator, port 9000/9090/9092);
convention — targets labeled `1`, sources labeled `2`.

---

## Folder layout (this package)

```
internal/emberplus/
├── CLAUDE.md    ← this file
├── codec/       stdlib-only wire codec packages
│   ├── ber/     BER (ASN.1 encoding primitives)
│   ├── glow/    GlowDTD tagged structures
│   ├── s101/    S101 framing (keep-alive, CRC16, escape)
│   └── matrix/  matrix/target/source encoder helpers
├── consumer/    package emberplus — implements protocol.Protocol
├── provider/    package emberplus — implements provider.Provider
├── wireshark/   dhs_emberplus.lua
├── docs/        consumer.md / provider.md / README.md
└── assets/      Ember+ PDFs + TinyEmberPlus/EmberPlusView tools + smh/ TS lib
```

- Packages under `codec/` are stdlib-only (lift-ready).
- `consumer/` and `provider/` both use `package emberplus`; they are imported
  with aliases where needed.

---

## Transport

- TCP, typically port 9000 (Lawo boxes vary: 9000, 9090, 9092).
- S101 framing layer:
  - `0xFE` start, `0xFF` end, `0xFD` escape (XOR next byte with `0x20`).
  - CRC-16/CCITT over the unescaped inner bytes.
  - Multi-packet messages (MPM) flagged in S101 header.

## Stack

```
TCP
└── S101 (framing + CRC + keep-alive)
    └── GlowDTD (ASN.1 BER APPLICATION tags for Ember+ semantic types)
        └── Glow trees: Node / Parameter / Matrix / Function
```

## GlowDTD tag numbers (APPLICATION class)

```
1=Parameter  2=Command  3=Node   4=ElementCollection
5=StreamEntry 6=QualifiedParameter  7=QualifiedNode
8=RootElementCollection  9=StreamCollection
10=ElementCollection (legacy)
11=Invocation 12=InvocationResult
13=Template   14=QualifiedTemplate
15=Function   16=QualifiedFunction
17=Matrix     18=QualifiedMatrix
19=TargetCollection 20=SourceCollection
21=ConnectionCollection 22=Connection
```

## Paths

Use **dot-separated** OIDs everywhere (`1.2.3.4`), never slash. This matches
the Ember+ OID convention and lines up with ACP1/ACP2 label-path conventions
in the repo.

## Commands

| code | name              |
|-----:|-------------------|
|   30 | Subscribe         |
|   31 | Unsubscribe       |
|   32 | GetDirectory      |
|   33 | Invoke            |

## Matrix

- `<Matrix>` has `targetCount`, `sourceCount`, `targets`, `sources`,
  `connections`. Targets and sources identified by numeric index; labels live
  in their respective `targets[]` / `sources[]` sub-elements.
- `Connection` carries `target`, `sources[]`, `operation` (0=absolute set,
  1=connect, 2=disconnect), `disposition`.

### Known deviations from spec (matrix subscription)

§p.88 reads: "As soon as a consumer issues a GetDirectory command on a
matrix object, it implicitly subscribes to matrix connection changes."
Strict reading: only sessions that walked the matrix receive
connection-change announcements. Our provider broadcasts matrix
connection changes to **every connected session**, mirroring
libember-cpp / TinyEmber+ / Lawo provider stacks — most viewers walk
matrix contents from a parent node reply rather than direct
GetDirectory(matrix), so strict subscription gating leaves crosspoint
tallies stranded at the consumer. The same broadcast-to-all rule
applies to plain Parameter value-change announcements (Subscribe(30)
exists in spec for streams; no shipping provider gates plain-param
emission on it). Stream parameters stay subscription-gated in
`provider/streamer.go`.

## Stream values

StreamEntry pushes a raw value per `streamIdentifier`. Map to the parameter
that declared `streamIdentifier` in its full DTD. Wildcard subscribers must
diff-merge stream payload into their cached tree.

## Functions

Invoke by path; carries typed arg tuples. Results come back as
`InvocationResult` with a matching `invocationId`. The formulas PDF catalogues
the well-known builtin functions.

## Keep-alive

S101 carries keep-alive request/response frames. No glow payload; enforced at
S101 layer. Respond to every request promptly.

## Quirks / landmines (from PR #67+#72)

- Some providers emit `offlineElement` markers when walking — treat as tombstone,
  do NOT cache the element.
- Connection-snapshot diff needs to honor `disposition=modified` (partial).
- `formula` evaluation (#70) is **parked** — consumer ignores formulas and
  surfaces raw values. Do not lazily implement — requires full Formulas PDF.
- BER REAL mantissa convention (#68 fix 2026-04-26): every Ember+ stack
  (libember, EmberViewer, Lawo VSM) reads `N` as a normalised fraction with
  binary point implicit after the leading 1 bit, not as the literal X.690
  §8.5.7 unsigned integer. `EncodeReal` + `DecodeReal` in `codec/ber/value.go`
  bias the wire exponent by `bits.Len64(N)-1` to match. Pinned by
  `TestReal_EcosystemBytes` (50.0 → `80 05 19`, 100.0 → `80 06 19`,
  0.1 → `80 fc 0c cc cc cc cc cc cd`). Verified live against EmberViewer
  v2.40.0.35 + Lawo VSM Studio.
- S101 reader resyncs on a second BOF mid-frame (`codec/s101/reader.go`).
  Spec mandates 0xFE escape-stuffing; Lawo VSM-as-consumer emits a 15-byte
  non-S101 preamble before its first real frame on every reconnect, and
  resyncing on the second BOF drops the junk rather than failing CRC over
  the concatenation.

## What NOT to do

- Do NOT trust tag numbers by position — always read APPLICATION tags.
- Do NOT assume matrix targets/sources are contiguous (holes are legal).
- Do NOT silently swallow formula errors — route through `compliance.Profile`.
- Do NOT import `acp/internal/*` from `codec/` subpackages (stdlib-only rule).
