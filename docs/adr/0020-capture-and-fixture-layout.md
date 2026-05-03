# ADR-0020 — capture and fixture file layout

Status: accepted

## Context

`dhs` already produces and consumes wire captures in three distinct
contexts (codec test fixtures, Wireshark dissector cross-check, live
captures from the `--capture` flag) and the layout drifted: per-type
fixtures sit under `internal/<proto>/testdata/`, multi-frame scenario
captures landed under `tests/fixtures/<proto>/`, per-product DM library
spec lives only in `docs/fixtures-products.md`, and live captures
mixed binary build outputs and runtime data inside `bin/devices/`.

Without a single rule, every new fixture is placed wherever it was
authored and tests + tooling have to grow per-protocol special cases
to find them. The line schema inside `*.jsonl` capture files is
covered separately in ADR-0021; this ADR is about WHERE files live.

## Decision

Four buckets. Each capture artifact belongs to exactly one.

### Bucket 1 — Per-type fixtures (codec + dissector regression)

Lives with the connector code. One folder per wire type or command
verb (`get_property`, `set_property`, `command_subscribe`, `parameter`,
`error_no_access`, ...).

```text
internal/<proto>/testdata/protocol_types/<name>/
├── frames.jsonl       wire-trace per ADR-0021 (drives encode/decode tests)
├── capture.pcapng     OS-socket capture (drives Wireshark Lua dissector check)
├── tshark.tree        frozen dissector output (regression diff target)
└── README.md          scenario + spec citation + dhs commit at capture time
```

Loaded by `internal/<proto>/codec/*_test.go`. Cross-checked against
the dissector via `tshark -X lua_script:<dissector>.lua` on
`capture.pcapng`.

### Bucket 2 — Scenario fixtures (multi-frame replay)

End-to-end scenario captures that exercise more than one wire type or
verb in sequence. Same connector subtree, different folder.

```text
internal/<proto>/testdata/scenarios/<name>/
├── frames.jsonl       wire-trace per ADR-0021
├── capture.pcapng     optional OS-socket capture
└── README.md          scenario context, endpoint IPs, dhs commit
```

Replayed via the `replay` verb (per ADR-0002) — see ADR-0021 for
replay semantics.

### Bucket 3 — Per-product DM library

Vendor-firmware fingerprinting. One folder per product release. Lives
under `tests/fixtures/products/` because it's a cross-cutting library
indexed by manufacturer/product, not by protocol.

```text
tests/fixtures/products/<manufacturer>/<product>/<proto>/<role>/<version>/
├── meta.json          provenance + capture context (schema below)
├── wire.jsonl         wire-trace per ADR-0021
├── tree.json          canonical tree
└── capture.pcapng     optional, only if it adds dissector coverage
```

`<role>` ∈ `{consumer, producer, registry}` per ADR-0001.
`<version>` is the firmware / software version string.
`tests/fixtures/products/CHANGELOG.md` per
`<manufacturer>/<product>/<proto>/<role>/` summarises DM evolution
across versions.

### Bucket 4 — Live captures (gitignored)

Output of the `--capture` flag during dev runs. Two sub-trees by
lifecycle.

```text
.cache/devices/<ip>/slot_N.json                            CLI tree cache (auto-written, regenerable)
captures/<proto>/<ip>/[<slot>/]<scenario>/                 manual replay archive
├── frames.jsonl                                           wire-trace per ADR-0021
├── tree.json                                              canonical (post-walk)
├── glow.json                                              optional, Ember+ specific
└── capture.pcapng                                         optional OS-socket capture
```

Both `.cache/` and `captures/` at repo root, both gitignored. The
sub-keying after `<proto>/<ip>/` is per-protocol — `<slot>/` for
ACP1/ACP2, `<api-ver>/` for NMOS, just `<scenario>/` for slot-less
protocols (Ember+, OSC, TSL, Probel SW-P-08/02). Each connector's
`internal/<proto>/CLAUDE.md` declares its sub-keying.

## `meta.json` schema (Bucket 3)

```json
{
  "schema_version": 1,
  "protocol": "acp2",
  "manufacturer": "Vendor",
  "product": "ProductName",
  "direction": "consumer",
  "version": "2.4",
  "version_kind": "firmware",
  "discovered_at": "2026-05-04T10:00:00Z",
  "description": "free text from device identity",
  "dm_fingerprint": "sha256:<hex>",
  "object_count": 214,
  "capture_tool": {
    "name": "dhs",
    "version": "0.7.1",
    "git_tag": "v0.7.1",
    "git_commit": "0cbde05"
  },
  "notes": ""
}
```

| Field | Required | Meaning |
|---|---|---|
| `schema_version` | yes | starts at 1 |
| `protocol` | yes | matches the directory segment |
| `manufacturer` | yes | matches the directory segment (lowercase slug) |
| `product` | yes | matches the directory segment |
| `direction` | yes | `consumer` / `producer` / `registry` |
| `version` | yes | matches the directory segment |
| `version_kind` | yes | `firmware` for hardware, `software` for soft providers, `release` for named releases |
| `discovered_at` | yes | UTC ISO-8601 with second precision |
| `description` | no | free text from device identity |
| `dm_fingerprint` | yes | `sha256:` + hex of canonical `tree.json` |
| `object_count` | yes | number of leaves in `tree.json` |
| `capture_tool` | yes | structured object — name, version, git_tag, git_commit |
| `notes` | no | free text |

Instance-level facts (slot, device_ip, serial, MAC, rack_id) are
deliberately excluded — those are deployment details, not DM-intrinsic.
Two physical instances of the same firmware produce identical
fingerprints.

## Naming rules

| Rule | Why |
|---|---|
| One folder per wire-type-or-verb under `protocol_types/` | Codec test discovery is `for d in protocol_types/*/`, no manifest |
| One folder per scenario under `scenarios/` | Same discovery pattern |
| `frames.jsonl` is the canonical name in Buckets 1 + 2 + 4 | Same name, same shape (per ADR-0021), no per-bucket dialect |
| `wire.jsonl` is the canonical name in Bucket 3 | Aligns with `meta.json` + `tree.json` triple per existing product-fixture spec |
| `capture.pcapng` is the canonical name in every bucket | Single name across the project; reviewers know what to open |
| Every committed `.pcapng` MUST have a sibling `README.md` | A binary alone is unreadable in 6 months |
| Per-type fixtures MUST also ship `tshark.tree` | Reviewers diff dissector output without launching Wireshark |
| `.pcapng` ≥ 100 KB → git-LFS; if LFS quota exhausted → keep under `captures/` (local-only) and reference in `README.md` | LFS quota is a hard constraint |

## Forbidden

- Putting per-protocol fixtures under `tests/fixtures/<proto>/`. Those
  belong under `internal/<proto>/testdata/` (Buckets 1 + 2). The
  `tests/fixtures/` tree is reserved for the cross-cutting product
  library (Bucket 3).
- Mixing CLI tree cache and manual captures in the same path. Cache
  is throwaway (`.cache/devices/`), captures are kept
  (`captures/`).
- Putting any committed file under `bin/`. `bin/` is for build
  artefacts only.
- Committing a `.pcapng` without its sibling `README.md`.
- Inventing a per-bucket variant of `frames.jsonl` (e.g.
  `decoded.jsonl`, `wire.json`, `events.json`). One name, one shape.

## Consequences

- Codec tests find every per-type fixture with `filepath.Glob` —
  no per-protocol manifest, no "registered fixture" code path.
- Wireshark dissector verification re-runs `tshark` against every
  `capture.pcapng` and diffs the result against the frozen
  `tshark.tree` — a `make verify-dissectors` target becomes
  mechanical.
- The `replay` verb (ADR-0002 + ADR-0021) consumes any
  `frames.jsonl` from any bucket without conditional code paths.
- Per-repo split (ADR-0001) carries Buckets 1 + 2 + the cache /
  capture tooling with the connector. Bucket 3 becomes a separate
  shared product-library repo or stays in the spec module.
