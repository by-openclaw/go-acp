# captures/

Local manual wire-trace archive. Gitignored.

Per **ADR-0020 Bucket 4**, this folder holds hand-triggered captures
kept around for offline replay and for diagnosing device-specific
quirks without needing the live device. Path shape:

```text
captures/<proto>/<ip>/[<slot>/]<scenario>/
├── frames.jsonl       wire-trace per ADR-0021
├── tree.json          canonical (post-walk)
├── glow.json          optional, Ember+ specific
├── capture.pcapng     optional OS-socket capture
└── README.md          scenario context + dhs commit at capture time
```

Sub-keying after `<proto>/<ip>/` is per-protocol — declared in each
connector's `internal/<proto>/CLAUDE.md`:

| Connector kind | Sub-keying |
|---|---|
| Slot-based (ACP1, ACP2) | `<slot>/` then `<scenario>/` |
| Slot-less (Ember+, OSC, TSL, Probel SW-P-08/02, Cerebrum) | `<scenario>/` directly |
| NMOS-shaped | `<api-ver>/` then `<scenario>/` |

## Companion path

CLI tree cache (`<ip>/slot_N.json` files written automatically on each
walk) lives under `.cache/devices/`. Same gitignore rule, different
lifecycle: cache regenerates, captures are kept.

## Naming

- One folder per physical device, keyed by IP. Use the device's real
  IP, not a descriptive name.
- Numeric prefix for ordered sequences (`01_`, `02_`...). Sequences
  belong in their own subfolder (`set_roundtrip/`, `errors/`) to keep
  the slot/scenario root scannable.
- `frames.jsonl` is the canonical wire-trace name (per ADR-0021); the
  CLI's `--capture` flag emits this format.
- `capture.pcapng` is the canonical OS-socket capture name (per
  ADR-0020).

## Promoting a capture to a committed fixture

A capture in `captures/` moves into a committed location when:

1. It is **small** (< 100 KB — committed as a regular blob; LFS is
   capped).
2. It exercises an **edge case a unit test needs to replay** (error-path
   decoding, specific object-type quirk, announce flood scenario).
3. It is **stable** — re-running against the device produces
   byte-identical output, so the golden stays valid.

Destinations per ADR-0020:

| Use | Destination |
|---|---|
| Per-type / per-verb codec test fixture | `internal/<proto>/testdata/protocol_types/<name>/` |
| Multi-frame scenario fixture | `internal/<proto>/testdata/scenarios/<name>/` |
| Per-product DM library entry | `tests/fixtures/products/<manufacturer>/<product>/<proto>/<role>/<version>/` |
