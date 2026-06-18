# TSL UMD — operational runbook

Quick-reference card for operators. For wire-format detail see
[CLAUDE.md](../CLAUDE.md); for deep consumer/producer behaviour see
[consumer.md](consumer.md) / [provider.md](provider.md); for the full verb
reference with real captures see [verbs.md](verbs.md).

TSL is a **push protocol**: a tally source (producer) pushes tally + UMD
state at multiviewer listeners (consumers). No request/reply, no tree, no
matrix.

---

## Transport matrix

| Version | Transport | Default port | Notes |
|---|---:|---|
| v3.1 | UDP | 4000 | 18-byte frame. 4 binary tallies + brightness. No colour. |
| v4.0 | UDP | 4004 (spec 4000) | v3.1 + CHKSUM + VBC + XDATA colour (L/R display). |
| v5.0 | UDP | 8901 | PBC/VER/FLAGS/SCREEN + DMSG(s). ≤2048 B. Kaleido default. |
| v5.0 | TCP | 8902 (spec 8901) | DLE/STX wrapper + 0xFE byte-stuffing. Use `--tcp`. |

Testbed port offsets (v4.0→4004, v5.0 TCP→8902) exist only so all versions
coexist on one host; the spec uses 4000 / 8901.

## Verb reference

### Consumer (multiviewer receiver)

| Verb | What it does | Common flags |
|---|---|---|
| `dhs consumer tsl-v31 listen` | bind UDP, decode + print every v3.1 frame | `--bind HOST:PORT` |
| `dhs consumer tsl-v40 listen` | same, v4.0 | `--bind HOST:PORT` |
| `dhs consumer tsl-v50 listen` | same, v5.0 UDP (or TCP with `--tcp`) | `--bind HOST:PORT`, `--tcp`, `--keepalive DUR` |

There is no `info` / `walk` / `get` / `set` / `export` (push protocol — see
verbs.md §4-§7, §9 for the N/A rationale). `listen` is the only consumer verb.

### Producer (tally source)

| Verb | What it does | Required flags |
|---|---|---|
| `dhs producer tsl-vNN send --dest HOST:PORT [flags]` | encode one frame, push once | `--dest` (UDP) |
| `dhs producer tsl-vNN serve --dest HOST:PORT --refresh DUR [flags]` | push + re-emit every DUR | `--dest`, `--refresh` |

`--dest` is repeatable (fan-out to many MVs). `--tcp` is v5.0 only.

## Common flows

### Bring up a v3.1 tally feed to a multiviewer

```
# MV side (listener):
dhs consumer tsl-v31 listen --bind 0.0.0.0:4000

# tally-source side:
dhs producer tsl-v31 serve --dest 10.0.0.5:4000 --refresh 1s --addr 7 --tally1 --text "PGM LIVE"
```

Real decoded frame on the MV side (loopback proof):

```
v3.1  remote=127.0.0.1:52223  addr=7  T1=ON T2=off T3=off T4=ON  brightness=full  UMD="PGM LIVE        "
```

### v5.0 Kaleido feed with a multi-display group

```
dhs producer tsl-v50 serve --dest kaleido:8901 --refresh 1s --screen 0 \
  --dmsg "index=2,lh=red,umd=PGM" \
  --dmsg "index=3,text-tally=green,umd=PVW" \
  --dmsg "index=4,rh=amber,umd=ISO"
```

### v5.0 over TCP (DLE/STX) to a routable MV

```
dhs consumer tsl-v50 listen --bind 0.0.0.0:8902 --tcp
dhs producer tsl-v50 send  --dest 10.0.0.5:8902 --tcp --screen 0 --index 2 --rh amber --text "ISO 2"
```

### Local loopback smoke test (no external gear)

```
# terminal 1:
dhs consumer tsl-v50 listen --bind 127.0.0.1:18901
# terminal 2:
dhs producer tsl-v50 send --dest 127.0.0.1:18901 --screen 1 --index 11 --lh red --text-tally green --rh amber --text "CAM 1"
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `listen` shows nothing | no source pushing, wrong port, or firewall blocking UDP | confirm the producer's `--dest` matches the consumer's `--bind` port |
| `--tcp` rejected on v3.1/v4.0 | TCP is v5.0-only (off-spec for v3.1/v4.0) | drop `--tcp`, or use v5.0 |
| `at least one --dest is required` | producer run with no `--dest` (UDP) | add `--dest HOST:PORT` |
| `--text-pad nul ... not allowed on tx` | tried off-spec NUL padding | use `--text-pad spaces` (the default) |
| v4.0 consumer logs `tsl_checksum_fail` | source emitted a bad 2's-complement checksum | source bug — the frame is still absorbed + decoded |
| UTF-16 label shows mojibake on a v5 consumer that expected ASCII | source set FLAGS bit 0 | the consumer transcodes automatically + fires `tsl_charset_transcode`; check the source's charset flag |
| v5.0 TCP socket goes silent, no error | dead peer; TSL has no app heartbeat | SO_KEEPALIVE (30 s) detects it; tune with `--keepalive` |

## Idempotency note

`serve --refresh` re-emits the **same** frame unconditionally — it is a
keep-alive refresh, not an idempotent converge. TSL has no read-back, so
there is no `ensure`-style "run twice = no-op" at the protocol level. The
Ansible loopback integration is idempotent in the test sense (run-twice =
same PASS, sockets torn down each run) — see verbs.md §11.

## Pointers

- Wire format: [CLAUDE.md](../CLAUDE.md)
- Verb reference + real captures: [verbs.md](verbs.md)
- Consumer deep-dive: [consumer.md](consumer.md) · Provider: [provider.md](provider.md)
- Wireshark dissector: [../wireshark/dhs_tsl.lua](../wireshark/dhs_tsl.lua)
- Spec: [../assets/tsl-umd-protocol.pdf](../assets/tsl-umd-protocol.pdf)
- Integration playbook: [../../../ansible/playbooks/tsl-integration.yml](../../../ansible/playbooks/tsl-integration.yml)
