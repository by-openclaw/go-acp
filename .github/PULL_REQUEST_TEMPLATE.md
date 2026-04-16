## Summary

<!-- One sentence: what changed and why. -->

Closes #

## Type

- [ ] feat — new feature
- [ ] fix — bug fix
- [ ] docs — documentation only
- [ ] chore — maintenance, refactor, CI
- [ ] security — security fix or hardening

## Protocol / area

- [ ] acp1
- [ ] acp2
- [ ] transport
- [ ] export
- [ ] cli
- [ ] api
- [ ] core

## Files changed

| File | Type | Change |
|------|------|--------|
| `internal/protocol/acp1/example.go` | new | ExampleFunc (N lines) |
| `tests/unit/acp1/example_test.go` | new | N tests |

## Test results

| Suite | Scope | File | Passed | Failed |
|-------|-------|------|--------|--------|
| Unit (black-box) | acp1 | `tests/unit/acp1/` | 0 | 0 |
| Unit (white-box) | acp1 | `internal/protocol/acp1/` | 0 | 0 |
| Integration | acp1 | `tests/integration/acp1/` | 0 | 0 |
| **Full suite** | **All** | `go test ./...` | **0** | **0** |
| Lint | golangci-lint | `./...` | clean | — |
| Vet | go vet | `./...` | clean | — |

## Device tested

<!-- Which device / emulator did you verify against? -->

- [ ] Synapse Simulator 10.6.239.113 (UDP)
- [ ] Real ACP1 device (specify IP + firmware)
- [ ] Real ACP2 device (specify IP + firmware)
- [ ] No device needed (pure codec / doc change)

## Checklist

- [ ] `go test -count=1 ./...` passes
- [ ] `go vet ./...` clean
- [ ] `golangci-lint run ./...` clean
- [ ] No new external dependencies (or justified + approved)
- [ ] CHANGELOG.md updated
- [ ] Per-protocol docs updated if behavior changed
- [ ] Export example files regenerated if format changed
