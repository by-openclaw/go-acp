# TupleItemDescription

APP tag **21**. One typed, named slot in a Function's argument or result
list — how a provider tells a consumer what `invoke` takes and returns.

## Spec

Ember+ Documentation v2.50, p. 91.

```
TupleItemDescription ::= [APPLICATION 21] IMPLICIT SEQUENCE {
    type [0] ParameterType,
    name [1] EmberString OPTIONAL
}

TupleDescription ::= SEQUENCE OF [0] TupleItemDescription
```

The frame is a real device's Function (APP 19) whose `arguments` carry
TupleItemDescriptions: the Powercore function `ParamLoopBack` (under its
`licensing` node) takes one argument, `type 4` (boolean) named
`"Boolean Par."`.

## Source

- Real device: **Lawo Power Core**, port 9000.
- Frame: line 48 (0-based index 47) of
  [`../../fixtures/powercore/raw.s101.jsonl`](../../fixtures/powercore/raw.s101.jsonl)
  (captured 2026-06-24 with `dhs consumer emberplus walk --capture`), a
  provider → consumer reply, one complete S101 packet (flags `C0`).
- Pcap: [`capture.pcapng`](capture.pcapng) — that frame wrapped in IPv4/TCP
  (source port 9000) by Wireshark's `text2pcap`.
- Frozen tree: [`tshark.tree`](tshark.tree) — `tshark -V` with the repo's
  `internal/emberplus/wireshark/dhs_emberplus.lua`, time fields frozen.

## CLI equivalent

```bash
dhs consumer emberplus walk <powercore-ip>:9000
```
