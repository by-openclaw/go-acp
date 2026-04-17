## What

<!-- One sentence: what changed. -->

## Why

<!-- Motivation: what problem does this solve? Link to issue. -->

Closes #

## Scope

- [ ] acp1
- [ ] acp2
- [ ] transport
- [ ] export
- [ ] cli
- [ ] api
- [ ] core
- [ ] ci / chore

## Type

- [ ] feat — new feature
- [ ] fix — bug fix
- [ ] docs — documentation only
- [ ] chore — maintenance, refactor, CI
- [ ] security — security fix or hardening

## Files changed

| File | New / Modified | Description |
|------|---------------|-------------|
| `path/to/file.go` | new | short description |

## Test results

| Suite | Passed | Failed |
|-------|--------|--------|
| `go test ./...` | 0 | 0 |
| `go vet ./...` | clean | — |
| `golangci-lint run` | clean | — |

## Device tested

- [ ] ACP1 emulator 10.6.239.113
- [ ] ACP2 real device 10.41.40.195
- [ ] Other (specify)
- [ ] No device needed (pure codec / doc change)

## Checklist

- [ ] `go test -count=1 ./...` passes
- [ ] `go vet ./...` clean
- [ ] `golangci-lint run ./...` clean
- [ ] No new external dependencies
- [ ] Integration tested on VM before PR

## Approval

@yboujraf — requesting review
