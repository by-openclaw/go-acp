# Ember+ Protocol Analysis — Consumer Implementation

Research for future integration into the BY-SYSTEMS control platform.

## Three-Layer Architecture

| Layer | Name | Purpose |
|-------|------|---------|
| 1 | **S101** | TCP framing — frame markers (0xFE/0xFF), byte escaping, CRC-CCITT16, keep-alive |
| 2 | **EmBER** | ASN.1 BER encoding — Tag-Length-Value serialization of Glow types |
| 3 | **Glow** | Data schema — Node, Parameter, Matrix, Function, Template |

## Transport

- **TCP** (standard sockets, typically port 9000–9100, configurable)
- **S101 framing**: BOF (0xFE) + header(4 bytes) + BER payload + CRC + EOF (0xFF)
- **Keep-alive**: built-in request/response with configurable timeout
- **No discovery protocol** — consumer must know the provider's IP:port

## Data Model

Tree structure similar to ACP2:

| Ember+ Type | ACP2 Equivalent | Notes |
|-------------|-----------------|-------|
| Node | node (type 0) | Container with children |
| Parameter | number/enum/string | Typed value with min/max/access |
| Matrix | (none) | Signal routing — unique to Ember+ |
| Function | (none) | Callable RPC — unique to Ember+ |
| Template | (none) | Reusable structure definitions |

**Parameter types**: Integer, Real, String, Boolean, Trigger, Enum, Octets

**Commands**: GetDirectory, Subscribe, Unsubscribe, Invoke, KeepAlive

## Comparison with ACP

| Feature | ACP1/ACP2 | Ember+ |
|---------|-----------|--------|
| Transport | UDP/TCP custom | TCP + S101 framing |
| Encoding | Custom binary BE | ASN.1 BER (TLV) |
| Data model | Tree (ACP2) / flat groups (ACP1) | Tree |
| Subscriptions | UDP broadcast (ACP1) / AN2 events (ACP2) | Explicit subscribe per parameter |
| Discovery | UDP broadcast (ACP1) / AN2 init (ACP2) | None — must know IP |
| Matrix routing | No | Yes |
| RPC functions | No | Yes |
| Strings | ASCII (ACP1) / UTF-8 (ACP2) | UTF-8 |

**Key similarity**: both use tree-based parameter models with typed values, min/max constraints, and access levels. `protocol.Object` can represent Ember+ parameters with minor additions (Matrix and Function would need new `ValueKind` entries).

## Existing Go Library

**Repo**: https://github.com/dufourgilles/emberlib

| Aspect | Assessment |
|--------|------------|
| Scope | Consumer + Provider |
| Origin | Port from Node.js (dufourgilles/node-emberplus) |
| Maturity | "initial version", zero issues/PRs |
| Tests | None visible |
| License | Not stated (check repo) |
| Dependencies | Minimal (pure Go) |

## Implementation Options

### Option A — Use existing lib as-is
- Fast start, but risk: unmaintained, no tests, Node.js translation quirks
- Not recommended for production

### Option B — From scratch, spec-first (recommended)
- Same approach as ACP1/ACP2 — spec is authoritative
- Use existing lib as cross-reference only (same role C# played for ACP1)
- Estimated ~2000–3700 LOC

| Component | Complexity | Est. LOC |
|-----------|-----------|----------|
| BER encoder/decoder | moderate | 500–1000 |
| S101 framing | low | 200–400 |
| Glow data model | low-moderate | 300–600 |
| Consumer | low-moderate | 400–700 |
| **Total** | **moderate** | **~2000** |

### Option C — Hybrid
- Prototype with existing lib, then rewrite for production
- Best if we need to demo quickly

**Note**: Go's stdlib `encoding/asn1` is DER-only (strict subset of BER) and **cannot** parse Ember+ BER data as-is. A BER codec must be written or sourced from a third-party package.

## Key BER Challenge

ASN.1 BER is the biggest implementation cost. Unlike ACP1/ACP2's simple fixed-byte layouts, BER uses variable-length tags and lengths with recursive nesting. Options:

1. **Hand-write for Ember+ subset** — only implement the ~15 tag types Glow actually uses (not full ASN.1)
2. **Use `github.com/Logicalis/asn1`** — third-party BER package (adds a dependency)
3. **Use `codello.dev/asn1/ber`** — newer alternative

For the "no external deps" rule, option 1 is the path. The Glow subset of BER is finite and well-documented.

## References

- [Ember+ Official Repo](https://github.com/Lawo/ember-plus)
- [Go Library](https://github.com/dufourgilles/emberlib)
- [C# Reference](https://github.com/Lawo/ember-plus-sharp)
- Third-party S101 protocol notes (public web reference; vendor name omitted by policy)
- [Wireshark Dissector](https://lists.wireshark.org/archives/wireshark-commits/201806/msg00161.html)

---

Copyright (c) 2026 BY-SYSTEMS SRL — MIT License
