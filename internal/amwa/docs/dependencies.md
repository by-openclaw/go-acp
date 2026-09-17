# NMOS — strict-dependency architecture

Layered architecture with **enforced one-way dependency flow**. Every
new file lands in exactly one layer; layer N may import layer < N
only. Cross-protocol imports are forbidden outside neutral
infrastructure (`internal/consumer/`, `internal/provider/`,
`internal/registry/`, `internal/consumer/compliance/`,
`internal/datastore/`, `internal/metrics/`, `internal/transport/`).

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
│  Allowed:  dhs/internal/amwa/consumer    (blank import + verb dispatch)     │
│            dhs/internal/amwa/provider    (blank import)                    │
│            dhs/internal/amwa/registry    (blank import)                    │
│            dhs/internal/consumer         (interface, registry lookup)       │
│            dhs/internal/provider         (interface, registry lookup)       │
│            dhs/internal/registry         (interface, registry lookup)       │
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
│  internal/amwa/facade/    (AMWA-tool facade — drives the consumer)         │
│                                                                            │
│  Allowed:  dhs/internal/amwa/session/*                                      │
│            dhs/internal/amwa/codec/*                                        │
│            dhs/internal/consumer           (interface only)                 │
│            dhs/internal/provider           (interface only)                 │
│            dhs/internal/registry           (interface only)                 │
│            dhs/internal/consumer/compliance                                │
│            dhs/internal/datastore            (portable data dir)              │
│  Forbidden: any other internal/<proto>/*                                    │
│             cmd/*                                                          │
│             cross-imports between consumer / provider / registry            │
└──────────────────────────────────┬─────────────────────────────────────────┘
                                   │
┌──────────────────────────────────▼─────────────────────────────────────────┐
│  LAYER 2 — SESSION                                                         │
│  internal/amwa/session/dnssd/       (mDNS + unicast browse + peer list)    │
│  internal/amwa/session/auth/        (IS-10 token client + validation)      │
│  internal/amwa/session/certmgr/     (BCP-003-03 EST certificate manager)   │
│  internal/amwa/session/connection/  (IS-05 stage/activate orchestration)   │
│  internal/amwa/session/events/      (IS-07 WS publisher + subscriber)      │
│  internal/amwa/session/http/        (shared HTTP server + auth gate)       │
│  internal/amwa/session/mqtt/        (IS-07 MQTT client)                    │
│  internal/amwa/session/query/       (Controller-side Query API client)     │
│  internal/amwa/session/system/      (IS-09 fetch on Node boot)             │
│                                                                            │
│  Allowed:  dhs/internal/amwa/codec/*                                        │
│            dhs/internal/transport       (HTTP/WS capture)                   │
│            dhs/internal/consumer/compliance                                │
│  Forbidden: dhs/internal/amwa/{consumer,provider,registry}                  │
│             cmd/*                                                          │
│             any other internal/<proto>/*                                    │
└──────────────────────────────────┬─────────────────────────────────────────┘
                                   │
┌──────────────────────────────────▼─────────────────────────────────────────┐
│  LAYER 1 — CODEC                                                            │
│  internal/amwa/codec/spec/         (NMOS-wide base: Versioned, Registry[T]) │
│  internal/amwa/codec/dnssd/        (mDNS + unicast SRV/TXT)                 │
│  internal/amwa/codec/jsonschema/   (draft-04 schema compiler)               │
│  internal/amwa/codec/is04/         (Node/Device/Source/Flow/Sender/Receiver)│
│  internal/amwa/codec/is05/         (staged/active/transportfile envelopes)  │
│  internal/amwa/codec/is07/         (state/health/reboot/shutdown envelopes) │
│  internal/amwa/codec/is08/         (channel mapping schemas)                │
│  internal/amwa/codec/is09/         (Global config schema)                   │
│  internal/amwa/codec/is10/         (authorization: JWT + access rules)      │
│  internal/amwa/codec/is11/         (stream compatibility types)             │
│  internal/amwa/codec/is12/         (JSON envelope: messageType + handle)    │
│  internal/amwa/codec/is14/         (device configuration types)             │
│  internal/amwa/codec/ms05/         (NcObject root + class + datatype reg)   │
│  internal/amwa/codec/edid/         (BCP-005-01 EDID parsing)                │
│  internal/amwa/codec/est/          (BCP-003-03 EST + PKCS#7 bytes)          │
│  internal/amwa/codec/bcp/          (BCP JSON-shape validator packages)      │
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

The graph has THREE tiers within Layer 1:

1. **`spec/`** — NMOS-wide base (`Versioned` interface, generic
   `Registry[T]`, `SelectHighestMutual`, `ComplianceEvent + Reporter`).
   Stdlib-only. No sibling imports. Every spec depends on it.
2. **Per-spec packages** (`is04/`, `is05/`, …) — canonical structs +
   per-spec `Codec` interface (extends `spec.Versioned`). May import
   `spec/` and select sibling specs as the directed graph below allows.
3. **Per-minor packages** (`is04/v11/`, `is05/v10/`, …) — Strategy
   impls, one per wire minor. Import their host spec package +
   `spec/`. Never import sibling minors (`v12/` ≠ `v11/`) or other
   specs' minors. Blank-imported from `cmd/dhs/main.go` to wire init().
4. **BCP validator packages** (`bcp/bcp00201/`, `bcp/bcp00401/`, …) —
   register into their host spec at init() via
   `<host>.RegisterValidator(...)`. Implement `bcp.Validator` (which
   itself extends `spec.Versioned`).

```
   ┌──────────────┐
   │     spec     │  ◄── leaf base; no sibling imports
   │ Versioned +  │      Versioned interface, generic Registry[T],
   │ Registry[T] +│      SelectHighestMutual, ComplianceEvent + Reporter
   │  Reporter    │
   └──────┬───────┘
          │  (every per-spec package imports spec)
          ▼
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
   └──────────┘       └──────────┘     └──────────┘

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

   ┌──────────────────────────────────┐
   │  is10 · is11 · is14 · edid · est │  ◄── independent — import spec/ only
   └──────────────────────────────────┘
```

RQL filter parsing lives in `internal/amwa/registry/query.go`
(Layer 3) and SDP encode/decode in
`internal/amwa/provider/connection_sdp*.go` (Layer 3) — neither is a
codec package.

### Forbidden edges

- `is04` MUST NOT import `is05` / `is07` / `is08` / `is12` / `ms05`.
  IS-04 owns the resource graph; everything else points back into it
  via `controls` URN entries which are pure data, not code.
- `ms05` MUST NOT import `is12` (wire is the marshaller; model is the
  domain).
- `dnssd` and `jsonschema` MUST NOT import any sibling codec package.
- `is09` MUST NOT import `is04` (System config is bootstrap-only).
- `spec/` (the NMOS-wide codec base) MUST NOT import any sibling
  codec package. It is the leaf — every spec depends on it; it
  depends on no spec.
- Per-minor packages (`is04/v11/`, `is05/v10/`, …) MUST NOT be
  imported by Layer 2 / 3 / 4. Only their host spec package may import
  them, and only at `init()` time for `Register` calls. Plugin code
  goes through the host spec's `Codec` interface via the
  `spec.Registry[T]`.
- `is04/v12/` MUST NOT import `is04/v11/` or `is04/v13/`. Per-minor
  packages are siblings — they share the canonical structs in their
  parent (`is04/`), not each other.
- BCP validator packages (`bcp/bcp00201/`, `bcp/bcp00401/`, …)
  register into their host spec via `<host>.RegisterValidator(...)` at
  init() time and are blank-imported from `cmd/dhs/main.go`. They MUST
  NOT be imported anywhere else.

---

## New Tier-1 registry slot — `internal/registry/`

NMOS Registry doesn't fit `internal/consumer/` (consumer plugins) nor
`internal/provider/` (provider plugins). It is a dual-face middleware:
left face consumes registrations, right face provides catalogue. Same
process, two faces.

The **Tier-1 plugin slot**:

```
internal/registry/
├── registry.go           neutral interface every Registry plugin implements
└── factory.go            Factory + Register() + Lookup() — same shape as
                          internal/consumer/ + internal/provider/
```

```go
// internal/registry/registry.go
package registry

type Registry interface {
    Serve(ctx context.Context, opts ServeOptions) error
    Stop() error
    Stats() Stats
}

type Meta struct {
    Name        string // e.g. "nmos"
    Description string
    DefaultPort int
}

type Factory interface {
    Meta() Meta
    New(logger *slog.Logger) Registry
}

// internal/registry/factory.go — keyed on f.Meta().Name
func Register(f Factory) { ... }
func Lookup(name string) (Factory, bool) { ... }
```

`internal/amwa/registry/` (NMOS Registry plugin) registers via
`func init() { registry.Register(&Factory{}) }` and `cmd/dhs/main.go`
blank-imports it just like consumer + provider plugins today.

CLI dispatch (Layer 4):

```go
// cmd/dhs/cmd_nmos.go — runNMOSRegistry / runNMOSRegistryServe
f, ok := registryslot.Lookup("nmos")
if !ok { ... }
r := f.New(logger)
r.Serve(ctx, opts)
```

The slot is introduced for NMOS. Whether other protocols will use it
is undecided and out of scope for this PR.

---

## Enforcement

Three independent gates — all CI-fail on violation:

### 1. `depguard` golangci-lint rule

`.golangci.yml` (version `"2"` layout — rules live under
`linters.settings.depguard.rules`) carries three rules:

```yaml
linters:
  enable:
    - depguard
  settings:
    depguard:
      rules:
        # Layer 1 — codec stays stdlib-only.
        nmos-codec-stdlib-only:
          list-mode: lax
          files:
            - "**/internal/amwa/codec/**"
          deny:
            - pkg: github.com          # third-party
            - pkg: dhs/cmd
            - pkg: dhs/internal/amwa/session
            - pkg: dhs/internal/amwa/consumer
            - pkg: dhs/internal/amwa/provider
            - pkg: dhs/internal/amwa/registry

        # Layer 2 — session never reaches back into plugins or cmd/.
        nmos-session-no-plugin-imports:
          list-mode: lax
          files:
            - "**/internal/amwa/session/**"
          deny:
            - pkg: dhs/internal/amwa/consumer
            - pkg: dhs/internal/amwa/provider
            - pkg: dhs/internal/amwa/registry
            - pkg: dhs/cmd

        # Layer 3 — one files-glob covers all three plugins, so this
        # rule can only deny cmd/ (denying a sibling plugin here would
        # also deny the plugin's own package).
        nmos-plugin-no-cross-plugin:
          list-mode: lax
          files:
            - "**/internal/amwa/consumer/**"
            - "**/internal/amwa/provider/**"
            - "**/internal/amwa/registry/**"
          deny:
            - pkg: dhs/cmd
```

Cross-plugin isolation (consumer ↛ provider ↛ registry) is therefore
enforced by the Go test (`TestNoCrossPluginImports` in
`internal/amwa/dependencies_test.go`), not by depguard.

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
    pkg, err := build.Import("dhs/internal/amwa/codec/...", "", 0)
    // walk every codec package, fail if any import starts with "dhs/"
    // (excluding sibling dhs/internal/amwa/codec/*)
}

func TestSessionDoesNotImportPlugins(t *testing.T) {
    // walk every session package, fail if it imports
    // dhs/internal/amwa/{consumer,provider,registry}
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
| `dhs/internal/datastore` | Portable data dir + atomic file writes | 2, 3 |
| `dhs/internal/transport` | HTTP/WS capture (`--capture` flag) | 2 only |
| `dhs/internal/consumer/compliance` | Compliance.Profile + event types | 2, 3 |
| `dhs/internal/consumer` | Consumer interface + registry | 3, 4 |
| `dhs/internal/provider` | Provider interface + registry | 3, 4 |
| `dhs/internal/registry` *(NEW)* | Registry interface + registry | 3, 4 |

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
- ADR-0006 — codec stdlib-only rule applied to every protocol.
- This NMOS-specific layering extends those rules with explicit
  layer numbering, an inter-codec graph, and CI enforcement.
