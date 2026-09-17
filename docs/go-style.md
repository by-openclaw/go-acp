# Go house style

The writing-level standard for this repository — what `gofmt`, the
linters and the ADRs do **not** already enforce. Read it once, then
let `.golangci.yml` hold the machine-checkable half.

Scope note (ADR-0015): build rules live elsewhere and are only
referenced here — layering and dependency policy in
[`docs/dependencies.md`](../internal/amwa/docs/dependencies.md) and
ADR-0005/0006, naming and testing baselines in the root
[`CLAUDE.md`](../CLAUDE.md) "Coding conventions". This page is about
how the code and its commentary are *written*.

## Comments carry the WHY, and only the why

The house comment is a **rationale**, not a narration. It states the
constraint the code cannot show: the spec clause being satisfied, the
failure that taught us, the reason the obvious alternative is wrong.

```go
// The null entry is not padding: it is how IS-08 spells "this
// output may be left unrouted". Without it a controller must treat
// unrouting as forbidden.
out = append(out, nil)
```

Rules of thumb:

- **Never narrate.** `// increment the counter` is deleted on sight.
- **Cite the authority.** A behaviour pinned by a spec names the spec
  and section (`IS-05 §4.2`, `MQTT 3.1.1 §2.2.3`, `ADR-0013`); one
  pinned by a tool names the test (`IS-14-01 test_27`); one pinned by
  a real device names the device ("a real EVS Neuron ships…").
- **Record the scar.** When code exists because something broke, the
  comment says what broke and how it looked from the outside — that
  is what stops the next reader from "simplifying" it back.
- **Package headers are doctrine.** Every non-trivial package opens
  with a comment that says what the package IS, the shape of its
  surface, and the one or two invariants that make it correct.

## Errors are operator sentences

An error is read by someone at 2 a.m. with no source open.

- Lower-case, no trailing punctuation, prefixed with the failing
  subsystem the way the package does it (`nmos set:`, `registry/mirror:`,
  `settings:`).
- Say what was expected AND what to do:
  `--format: unknown value %q (want table|json|md)`.
- When a collection was searched, say what WAS there — the resolver
  that rejects a label lists the labels present.
- Wrap with `%w` when the caller may branch on it; `errors.Is/As` at
  call sites, never string matching (root CLAUDE.md).
- The device's own answer is the truth: report what the peer said
  (status code, body words), not what we hoped.

## The honesty doctrine

- **No silent fallbacks.** A skipped feature, an unresolvable value,
  an absorbed deviation — each is logged, counted, or fired as a
  compliance event. "It happens to work" is not a state this codebase
  recognises.
- **Warnings name consequences.** Not "leg 0 unset" but "this leg
  emits nothing".
- **Tests assert from the authority**, never from the code under
  test: wire bytes from the spec document, scores from the AMWA tool,
  behaviour from the real device. A test that would still pass after
  the bug is reintroduced is not a test.

## Shape

- One primary type per file; the file name says which (root CLAUDE.md
  naming).
- Constructors validate and return `(*T, error)`; half-built values
  do not escape.
- Hooks and cross-layer callbacks are explicit struct fields with a
  comment stating WHO wires them and what must not happen inside
  (blocking under a lock is the classic).
- Goroutines started by a type are owned by it: a `Close`/`Stop` that
  provably ends them (`done` channels, contexts), never "it will exit
  eventually".
- Table-driven tests name every case; the name states the behaviour,
  not the input (`"both refuses one destination"`, not `"case 4"`).

## Linting

`.golangci.yml` enforces the machine-checkable half: the standard
linter set plus `depguard` (layering) — and the pre-commit hook runs
`staticcheck`, so style debates end at the hook, not in review.
A linter is only enabled when the whole tree already passes it:
the config describes the code, not an aspiration.
