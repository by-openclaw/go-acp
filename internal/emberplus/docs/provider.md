# Ember+ Provider

> **Status: TODO (Part B)**
>
> The provider side of the Ember+ plugin is planned as the next
> milestone after the consumer (Part A) is merged. This file is a
> placeholder so cross-references resolve today.

## Planned scope

Per the original scope document:

- B1 — JSON tree loader (build the provider tree from a declarative
  file, spec-named keys mirroring the smh TypeScript reference)
- B2 — S101 server (listener, per-connection keep-alive, fragmentation
  buffer, subscriber set)
- B3 — Request handlers (GetDirectory / Subscribe / Unsubscribe /
  Invoke / SetValue / Matrix SetConnection)
- B4 — Announce engine (parameter value change, matrix tally, stream
  tick)
- B5 — Error taxonomy (reuses `internal/protocol/emberplus/errors.go`)
- B6 — Embedded test provider (router matrix + gain params + labels +
  one function) for CI end-to-end testing

No provider code exists yet — see [consumer.md](consumer.md) for the
current plugin surface.
