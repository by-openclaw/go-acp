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
- Viewer quirks (#68) around matrix reflection are **parked**.

## What NOT to do

- Do NOT trust tag numbers by position — always read APPLICATION tags.
- Do NOT assume matrix targets/sources are contiguous (holes are legal).
- Do NOT silently swallow formula errors — route through `compliance.Profile`.
- Do NOT import `acp/internal/*` from `codec/` subpackages (stdlib-only rule).
