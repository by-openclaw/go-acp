# Ember+ Provider

> **Status: shipping**
>
> Serves a canonical tree.json as an Ember+ provider over S101/TCP
> (default port 9000; Lawo deployments vary across 9000 / 9090 / 9092).
> Symmetric to the consumer plugin at
> [internal/emberplus/consumer/](../../../internal/emberplus/consumer/);
> reuses [internal/emberplus/codec/s101](../../../internal/emberplus/codec/s101)
> framing, [internal/emberplus/codec/ber](../../../internal/emberplus/codec/ber)
> TLV codec, and [internal/emberplus/codec/glow](../../../internal/emberplus/codec/glow)
> tag constants.
>
> CLI: `dhs producer emberplus serve --tree <path> --host 0.0.0.0 --port 9000`.
> See [runbook.md](runbook.md) and the runbook subpages
> [runbook/export-import.md](runbook/export-import.md) +
> [runbook/validate.md](runbook/validate.md) for the operator workflow.

## Landed scope (B1..B6)

- **B1** — JSON tree loader, declarative file with spec-named keys.
- **B2** — S101 server: listener, per-connection keep-alive, fragmentation
  buffer, subscriber set.
- **B3** — Request handlers: GetDirectory / Subscribe / Unsubscribe /
  Invoke / SetValue / Matrix SetConnection.
- **B4** — Announce engine: parameter value change, matrix tally,
  stream tick.
- **B5** — Error taxonomy reuses
  [internal/emberplus/consumer/errors.go](../../../internal/emberplus/consumer/errors.go).
- **B6** — Embedded test provider (router matrix + gain params +
  labels + one function) for CI end-to-end testing.

## Open work

- APP 24 Template encoder (`encodeTemplateInChild` in
  [internal/emberplus/provider/encoder.go](../../../internal/emberplus/provider/encoder.go))
  — pending byte-exact wire capture against real Lawo mc² / Powercore
  / DHD consoles to confirm child shape under Ember+ §1.5 templates.
