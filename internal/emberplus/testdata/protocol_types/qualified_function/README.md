# QualifiedFunction

APP tag **20**. A Function addressed by its absolute path, so a consumer can
reach it without descending through every ancestor.

## Spec

Ember+ Documentation v2.50, p. 91.

```
QualifiedFunction ::= [APPLICATION 20] IMPLICIT SEQUENCE {
    path     [0] RELATIVE-OID,
    contents [1] FunctionContents OPTIONAL,
    children [2] ElementCollection OPTIONAL
}

FunctionContents ::= SET {
    identifier        [0] EmberString      OPTIONAL,
    description       [1] EmberString      OPTIONAL,
    arguments         [2] TupleDescription OPTIONAL,
    result            [3] TupleDescription OPTIONAL,
    templateReference [4] RELATIVE-OID     OPTIONAL
}
```

The frame carries two of the TinyEmber+ router's functions, each as a
QualifiedFunction: `add` — arguments `arg1`, `arg2` and result `sum`, each a
TupleItemDescription (APP 21) — and `doNothing`, which takes none.

## Source

- Vendor emulator: **TinyEmberPlusRouter** (Lawo), port 9092.
- Frame: line 15 (0-based index 14) of
  [`../../fixtures/tiny-ember-router/raw.s101.jsonl`](../../fixtures/tiny-ember-router/raw.s101.jsonl),
  a provider → consumer reply, one complete S101 packet (flags `C0`).
- Pcap: [`capture.pcapng`](capture.pcapng) — that frame wrapped in IPv4/TCP
  (source port 9092) by Wireshark's `text2pcap`.
- Frozen tree: [`tshark.tree`](tshark.tree) — `tshark -V` with the repo's
  `internal/emberplus/wireshark/dhs_emberplus.lua`, time fields frozen.

## CLI equivalent

```bash
dhs consumer emberplus walk 10.6.239.113:9092
```
