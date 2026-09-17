## What

<!-- One sentence: what changed. -->

## Why

<!-- Motivation: what problem does this solve? Link to issue. -->

Closes #

## Scope

- [ ] acp1
- [ ] acp2
- [ ] emberplus
- [ ] probel-sw08p / probel-sw02p
- [ ] osc / tsl / cerebrum-nb / amwa
- [ ] transport
- [ ] export
- [ ] cli
- [ ] api
- [ ] core
- [ ] ci / chore

**Cross-protocol changes** (`cmd/dhs/*`, `internal/consumer/*`, or more than one `internal/<proto>/`): justify why the change can't be plugin-internal.

## Wire evidence (protocol changes only)

<!-- own-encoder ↔ own-decoder is NOT compliance.
     Paste tshark / dissector output or a reference-controller capture. -->

## Reference controller

- [ ] Cerebrum still happy after this change
- [ ] VSM Studio still happy after this change
- [ ] N/A (no protocol behaviour change)

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

- [ ] Fleet producer — which host per `docs/testbed.md` (dhs-debian .101 / dhs-ubuntu .102 / dhs-rocky .103 / win11 .105)
- [ ] Vendor emulator — Synapse ACP1 (office net 10.6.239.113) / TinyEmber+ (desk-03) / AMWA NMOS Testing tool (dhs-tools .104)
- [ ] Real device — Neuron (ACP2 / CCM) / Cerebrum staging (.5) / other (specify)
- [ ] No device needed (pure codec / doc change)

**Live-LXC command + observed output** (paste verbatim):

```text
dhs consumer <proto> <verb> <ip> [...]
# expected:
# ...
```

## Checklist

- [ ] `go test -count=1 ./...` passes
- [ ] `go vet ./...` clean
- [ ] `golangci-lint run ./...` clean
- [ ] No new external dependencies
- [ ] Integration tested on VM before PR

## Approval

@yboujraf — requesting review
