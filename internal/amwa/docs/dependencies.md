# NMOS — strict-dependency architecture

Layered architecture with **enforced one-way dependency flow**. Every
new file lands in exactly one layer; layer N may import layer < N
only. Cross-protocol imports are forbidden outside neutral
infrastructure (`internal/protocol/`, `internal/provider/`,
`internal/registry/`, `internal/protocol/compliance/`,
`internal/storage/`, `internal/metrics/`, `internal/transport/`).

This file is normative. The `depguard` golangci-lint rule + a
`go list -deps` test in CI enforce it; reviewers reject any PR that
introduces a back-arrow.

---

## Layer stack (top = most knowledge of the world; bottom = stdlib only)

```
┌────────────────────────────────────────────────────────────────────────────┐
│  LAYER 4 — CLI                                                             │
│  cmd/dhs/cmd_nmos.go                                                       │
│                                                                            │
│  Allowed:  acp/internal/amwa/consumer    (blank import + verb dispatch)     │
│            acp/internal/amwa/provider    (blank import)                    │
│            acp/internal/amwa/registry    (blank import)                    │
│            acp/internal/protocol         (interface, registry lookup)       │
│            acp/internal/provider         (interface, registry lookup)       │
│            acp/internal/registry         (interface, registry lookup)       │
│  Forbidden: anything under internal/amwa/codec/* directly                   │
│             anything under internal/amwa/session/* directly                 │
│             any other internal/<proto>/* (cross-protocol leak)              │
└──────────────────────────────────┬─────────────────────────────────────────┘
                                   │
┌──────────────────────────────────▼─────────────────────────────────────────┐
│  LAYER 3 — PLUGIN                                                          │
│  internal/amwa/consumer/  (Controller)                                     │
│  internal/amwa/provider/  (Node)                                           │
│  internal/amwa/registry/  (Registry — dual-face middleware)                │
│                                                                            │
│  Allowed:  acp/internal/amwa/session/*                                      │
│            acp/internal/amwa/codec/*                                        │
│            acp/internal/protocol           (interface only)                 │
│            acp/internal/provider           (interface only)                 │
│            acp/internal/registry           (interface only — NEW slot)      │
│            acp/internal/protocol/compliance                                │
│            acp/internal/storage            (portable data dir)              │
│            acp/internal/metrics            (connector + Prom)               │
│  Forbidden: any other internal/<proto>/*                                    │
│             cmd/*                                                          │
│             cross-imports between consumer / provider / registry            │
└──────────────────────────────────┬─────────────────────────────────────────┘
                                   │
┌──────────────────────────────────▼─────────────────────────────────────────┐
│  LAYER 2 — SESSION                                                         │
│  internal/amwa/session/dnssd/                                              │
│  internal/amwa/session/registration/   (Node-side registration client)     │
│  internal/amwa/session/registry_core/  (Registry catalogue + GC + WS subs) │
│  internal/amwa/session/query/          (Controller-side Query API client)  │
│  internal/amwa/session/connection/     (IS-05 stage/activate orchestration)│
│  internal/amwa/session/events/         (IS-07 WS publisher + subscriber)   │
│  internal/amwa/session/control/        (IS-12 + MS-05-02 model server +    │
│                                          client)                           │
│  internal/amwa/session/bootstrap/      (IS-09 fetch on Node boot)          │
│                                                                            │
│  Allowed:  acp/internal/amwa/codec/*                                        │
│            acp/internal/transport       (HTTP/WS capture)                   │
│            acp/internal/protocol/compliance                                │
│            acp/internal/metrics                                            │
│  Forbidden: acp/internal/amwa/{consumer,provider,registry}                  │
│             cmd/*                                                          │
│             any other internal/<proto>/*                                    │
└──────────────────────────────────┬─────────────────────────────────────────┘
                                   │
┌──────────────────────────────────▼─────────────────────────────────────────┐
│  LAYER 1 — CODEC                                                            │
│  internal/amwa/codec/dnssd/        (mDNS + unicast SRV/TXT)                 │
│  internal/amwa/codec/jsonschema/   (Schema compiler — BCP-002/004/006/007)  │
│  internal/amwa/codec/rql/          (Query API filter syntax)                │
│  internal/amwa/codec/sdp/          (RFC 4566 SDP encode + decode)           │
│  internal/amwa/codec/is04/         (Node/Device/Source/Flow/Sender/Receiver)│
│  internal/amwa/codec/is05/         (staged/active/transportfile envelopes)  │
│  internal/amwa/codec/is07/         (state/health/reboot/shutdown envelopes) │
│  internal/amwa/codec/is08/         (channel mapping schemas)                │
│  internal/amwa/codec/is09/         (Global config schema)                   │
│  internal/amwa/codec/ms05/         (NcObject root + class + datatype reg)   │
│  internal/amwa/codec/is12/         (JSON envelope: messageType + handle)    │
│                                                                            │
│  Allowed:  Go stdlib                                                       │
│            sibling internal/amwa/codec/* (per the inter-codec graph below) │
│  Forbidden: ANY acp/* path outside internal/amwa/codec/                     │
│             ANY third-party module                                         │
│             (this layer must be lift-to-own-repo ready — same rule as       │
│              internal/<proto>/codec/ for every other protocol)             │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## Inter-codec dependency graph (Layer 1 only)

Inside Layer 1, codec sub-packages may import each other but only along
the directed graph below. New cross-edges require an architecture
review.

```
   ┌──────────────┐         ┌──────────────┐
   │  jsonschema  │         │    dnssd     │      ◄── independent of all others
   │ (validator)  │         │ (mDNS + SRV) │
   └──────┬───────┘         └──────────────┘
          │
          │  (compiled validators baked into resource encoders)
          │
          ▼
   ┌──────────────┐
   │     is04     │  ◄── BCP-002 + BCP-004 schemas live here as resource-shape rules
   │   resource   │
   │    graph     │
   └──────┬───────┘
          │  (UUIDs, controls URN list)
          │
          ├─────────────────┬────────────────┐
          │                 │                │
          ▼                 ▼                ▼
   ┌──────────┐       ┌──────────┐     ┌──────────┐
   │   is05   │       │   is07   │     │   is08   │
   └─────┬────┘       └──────────┘     └──────────┘
         │
         │ (transportfile body)
         ▼
   ┌──────────┐
   │   sdp    │
   └──────────┘

   ┌──────────────┐
   │     ms05     │  ◄── BCP-008-01 + BCP-008-02 register feature-set classes here
   │   class +    │       (NcReceiverMonitor / NcSenderMonitor) — no separate pkg
   │   datatype   │
   │   registry   │
   └──────┬───────┘
          │
          ▼
   ┌──────────────┐
   │     is12     │  ◄── wire envelope ONLY; depends on ms05 for marshalling
   └──────────────┘

   ┌──────────────┐
   │     is09     │  ◄── independent (only used at Node bootstrap)
   └──────────────┘

   ┌──────────────┐
   │     rql      │  ◄── used by is04 Query API only; no reverse import
   └──────────────┘
```

### Forbidden edges

- `is04` MUST NOT import `is05` / `is07` / `is08` / `is12` / `ms05`.
  IS-04 owns the resource graph; everything else points back into it
  via `controls` URN entries which are pure data, not code.
- `ms05` MUST NOT import `is12` (wire is the marshaller; model is the
  domain).
- `dnssd` and `jsonschema` MUST NOT import any sibling codec package.
- `sdp` MUST NOT import `is04` / `is05` (it's pure RFC 4566).
- `is09` MUST NOT import `is04` (System config is bootstrap-only).
- `rql` MUST NOT import `is04` (it's pure filter-syntax; resource
  type-checking happens in the IS-04 layer).
- `bcp-008-*` is NOT a separate package; it's MS-05-02 classes.
  Likewise `bcp-002`, `bcp-004`, `bcp-006`, `bcp-007` are JSON-Schema
  files compiled into the relevant codec, NOT separate code paths.

---

## New Tier-1 registry slot — `internal/registry/`

NMOS Registry doesn't fit `internal/protocol/` (consumer plugins) nor
`internal/provider/` (provider plugins). It is a dual-face middleware:
left face consumes registrations, right face provides catalogue. Same
process, two faces.

A **new Tier-1 plugin slot** lands in this branch:

```
internal/registry/
├── registry.go           neutral interface every Registry plugin implements
├── factory.go            Factory + Register() + Lookup() — same shape as
│                         internal/protocol/ + internal/provider/
└── compliance/           OPTIONAL — registry-side compliance events
```

```go
// internal/registry/registry.go
package registry

type Registry interface {
    Serve(ctx context.Context, opts ServeOptions) error
    Stop() error
    Stats() Stats
}

type Factory interface {
    Name() string
    DefaultPort() int
    NewRegistry() Registry
}

func Register(f Factory) { ... }
func Lookup(name string) (Factory, bool) { ... }
```

`internal/amwa/registry/` (NMOS Registry plugin) registers via
`func init() { registry.Register(&Factory{}) }` and `cmd/dhs/main.go`
blank-imports it just like consumer + provider plugins today.

CLI dispatch (Layer 4):

```go
// cmd/dhs/cmd_registry.go
case "registry":
    f, ok := registry.Lookup(args[1])  // "nmos"
    if !ok { ... }
    f.NewRegistry().Serve(ctx, opts)
```

Future protocols with similar dual-face middleware (none today) can
register here without touching protocol/ or provider/.

---

## Enforcement

Three independent gates land in the Phase 1 PR (step #1) — all CI-fail
on violation:

### 1. `depguard` golangci-lint rule

`.golangci.yml` adds:

```yaml
linters:
  enable:
    - depguard

linters-settings:
  depguard:
    rules:
      nmos-codec-stdlib-only:
        list-mode: lax
        files:
          - "**/internal/amwa/codec/**"
        deny:
          - pkg: "acp/"
            desc: "codec layer must be stdlib-only (lift-to-own-repo ready)"
          - pkg: "github.com/"
            desc: "codec layer must be stdlib-only"

      nmos-session-no-plugin-imports:
        list-mode: lax
        files:
          - "**/internal/amwa/session/**"
        deny:
          - pkg: "acp/internal/amwa/consumer"
            desc: "session must not import plugin layer (back-arrow)"
          - pkg: "acp/internal/amwa/provider"
          - pkg: "acp/internal/amwa/registry"
          - pkg: "acp/cmd/"

      nmos-plugin-no-cross-plugin:
        list-mode: lax
        files:
          - "**/internal/amwa/consumer/**"
        deny:
          - pkg: "acp/internal/amwa/provider"
            desc: "consumer must not import provider (cross-plugin leak)"
          - pkg: "acp/internal/amwa/registry"
      # ... mirror rules for provider/ and registry/
```

### 2. `go list -deps` import audit test

`internal/amwa/dependencies_test.go` (lives at the package root,
build-tag-free):

```go
package amwa_test

import (
    "go/build"
    "strings"
    "testing"
)

func TestCodecHasNoAcpImports(t *testing.T) {
    pkg, err := build.Import("acp/internal/amwa/codec/...", "", 0)
    // walk every codec package, fail if any import starts with "acp/"
    // (excluding sibling acp/internal/amwa/codec/*)
}

func TestSessionHasNoPluginImports(t *testing.T) {
    // walk every session package, fail if it imports
    // acp/internal/amwa/{consumer,provider,registry}
}

// ... etc
```

This runs on every `go test ./internal/amwa/...` and catches what
depguard might miss in dynamic build configs.

### 3. Architecture review checklist (PR review gate)

Every NMOS PR description includes a tickbox:

```
- [ ] No new edges added to the inter-codec graph in dependencies.md
      (or graph updated explicitly with rationale).
- [ ] Layer N package imports only layers < N.
- [ ] No cross-plugin imports (consumer ↛ provider ↛ registry).
- [ ] Codec packages remain stdlib-only.
```

Reviewers reject PRs that don't tick all four.

---

## Cross-cutting infrastructure (allowed everywhere)

These neutral packages already exist and are imported throughout
without breaking the layering:

| Package | Purpose | Layers allowed |
|---|---|---|
| `acp/internal/storage` | Portable data dir + atomic file writes | 2, 3 |
| `acp/internal/metrics` | Connector counters + Prom registry | 2, 3 |
| `acp/internal/transport` | HTTP/WS capture (`--capture` flag) | 2 only |
| `acp/internal/protocol/compliance` | Compliance.Profile + event types | 2, 3 |
| `acp/internal/protocol` | Consumer interface + registry | 3, 4 |
| `acp/internal/provider` | Provider interface + registry | 3, 4 |
| `acp/internal/registry` *(NEW)* | Registry interface + registry | 3, 4 |

---

## What this rules out

- A codec package importing the metrics library directly. **Why:** the
  codec must lift to its own repo without dragging metrics deps.
  Metrics surface live at the session layer.
- A session package calling into a plugin's high-level constructor.
  **Why:** plugins inject session components, not the reverse.
- The Controller plugin (`internal/amwa/consumer`) importing the
  Registry plugin (`internal/amwa/registry`). **Why:** consumer talks
  to a remote Registry over HTTP, not via in-process function calls.
  The two never share state in a single dhs binary at runtime.
- A new top-level `internal/nmos-shared/` "utility" package shared
  between consumer and provider. **Why:** that's a back-channel for
  layer-3 cross-plugin coupling. If two plugins genuinely need shared
  code, lift it to Layer 2 (session/) or Layer 1 (codec/).

---

## Cross-reference

- Top-level `CLAUDE.md` "Architecture principles" — encapsulation, DI,
  SoC, library independence, no hidden state.
- `feedback_codec_isolation.md` — codec stdlib-only rule applied to
  every protocol (Probel, Ember+, OSC, TSL, etc.).
- `feedback_architecture_principles.md` — OOP encapsulation, DI via
  ctor, SoC.
- This NMOS-specific layering extends those rules with explicit
  layer numbering, an inter-codec graph, and CI enforcement.
