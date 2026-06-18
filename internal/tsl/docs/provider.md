# TSL UMD Provider (tally / UMD source)

> **Status: shipping**
>
> The TSL provider IS the tally source — the VSM/Kaleido-equivalent that
> *pushes* tally + UMD frames at one or more multiviewers. Symmetric to the
> consumer at [internal/tsl/consumer/](../consumer/); both sides share the
> stdlib-only codec at [internal/tsl/codec/](../codec/).
>
> CLI: `dhs producer tsl-vNN send|serve --dest HOST:PORT [version flags]`.
> See [runbook.md](runbook.md) for the operator workflow and [verbs.md](verbs.md)
> for the full flag reference with real captures.

---

## Role

Unlike a Tree/DM provider (acp1 serves a queryable canonical tree) or a
matrix provider (probel answers crosspoint interrogations), the TSL provider
is **fire-and-forget push**. It encodes one frame from its flags and sends
it to every `--dest`; `serve` re-emits the same frame on a `--refresh` loop.
There is no inbound request to answer, no tree to host, no state the
consumer can read back.

---

## Verbs

| Verb | What it does | Required flags |
|---|---|---|
| `send` | encode one frame from the flags and push **once** to every `--dest` | `--dest` (UDP); `--dest --tcp` (v5.0 TCP) |
| `serve` | encode + push, then re-emit every `--refresh DURATION` until Ctrl-C | `--dest`, `--refresh` |

Both build the frame identically; `serve` just adds the refresh ticker. With
`--refresh 0` (the default), `serve` behaves like `send` (emit once, return).

Real `send` confirmation (stderr):

```
$ dhs producer tsl-v31 send --dest 127.0.0.1:14000 --addr 7 --tally1 --tally4 --brightness 3 --text "PGM LIVE"
tsl-v31 send emitted to 1 destination(s)
```

Real `serve` (refresh loop, stderr — one emit line, then re-emits silently
every tick until Ctrl-C):

```
$ dhs producer tsl-v31 serve --dest 127.0.0.1:14000 --refresh 500ms --addr 3 --tally1 --text "REFRESH"
tsl-v31 serve emitted to 1 destination(s)
```

---

## Frame-build flags per version

### v3.1 / v4.0 (binary tallies + address)

| Flag | Meaning |
|---|---|
| `--addr N` | display address 0..126 |
| `--tally1..4` | binary tally bits (CTRL bits 0-3) |
| `--brightness 0..3` | 0=off 1=1/7 2=1/2 3=full |
| `--text "STR"` | UMD label (≤16 ASCII; space-padded to 16) |
| `--text-pad spaces` | DATA padding — spec is spaces; `nul` is rejected on tx (rx-tolerated only) |

### v4.0 extra (XDATA colour)

| Flag | Meaning |
|---|---|
| `--display-left lh:text:rh` | XDATA Xbyte L (off\|red\|green\|amber) |
| `--display-right lh:text:rh` | XDATA Xbyte R |

### v5.0 (DMSG colour + screen)

| Flag | Meaning |
|---|---|
| `--screen N` | screen 0..65534 |
| `--broadcast` | override screen with 0xFFFF (all screens) |
| `--index N` | display index 0..65534 |
| `--lh / --text-tally / --rh` | per-position colour (off\|red\|green\|amber) |
| `--utf16` | encode TEXT as UTF-16LE (FLAGS bit 0) |
| `--text "STR"` | UMD label |
| `--dmsg "index=N,lh=...,text-tally=...,rh=...,brightness=N,umd=STR"` | repeatable composite DMSG; **overrides** the singular flags |
| `--tcp` | push via TCP DLE/STX wrapper instead of UDP |
| `--keepalive DUR` | TCP SO_KEEPALIVE period (default 30 s; `--tcp` only) |

`--dmsg` is the v5.0 multi-display path (Miranda "Group display messages") —
one packet carries N DMSGs. Real 3-DMSG capture is in [verbs.md](verbs.md) §8.

---

## Destinations & fan-out

`--dest HOST:PORT` is **repeatable** — one `send`/`serve` pushes an identical
frame to every destination. The producer binds a local UDP egress
(`--bind`, default `0.0.0.0:0` ephemeral) once and reuses it for all UDP
destinations. v5.0 TCP dials each `--dest` per emit.

```
# one source → three multiviewers, refreshed every second
dhs producer tsl-v50 serve --dest mv-1:8901 --dest mv-2:8901 --dest mv-3:8901 \
  --refresh 1s --screen 0 --index 2 --lh red --text "PGM"
```

For UDP, at least one `--dest` is required (real guard):

```
$ dhs producer tsl-v40 send
error: producer tsl-v40 send: at least one --dest is required for UDP
```

---

## Validation (client-side, pre-send)

The producer validates every flag before encoding — bad input fails fast
with exit code != 0 and a stderr message, nothing is sent. Real captures:

```
$ dhs producer tsl-v31 send --dest 127.0.0.1:14000 --addr 1 --text-pad nul
error: --text-pad nul is reserved for off-spec emission and is not allowed on tx (spec is spaces); rx tolerates and fires tsl_v31_null_pad

$ dhs producer tsl-v50 send --dest 127.0.0.1:18901 --dmsg "lh=red"
error: --dmsg #1 "lh=red": missing index=N
```

Ranges: `--addr 0..126`, `--screen 0..65534`, `--index 0..65534`,
`--brightness 0..3`, tally colours `off|red|green|amber`. Out-of-range
values are rejected pre-send (e.g. `--brightness 5` → out of range 0..3).

---

## Spec-conformant emission (what the provider will NOT do)

- **Never** pads v3.1 DATA with 0x00 — space-pad (0x20) per spec. `--text-pad nul` is refused on tx.
- **Never** clears the wrong CTRL bits — brightness is CTRL bits 4-5 (not 5-6, the TallyArbiter bug).
- **Never** emits v5.0 over TCP without the DLE/STX wrapper — `--tcp` always wraps and byte-stuffs 0xFE.
- v3.1/v4.0 over TCP is not offered at all (off-spec).

---

## Pointers

- Full flag reference + real captures: [verbs.md](verbs.md)
- Operator runbook: [runbook.md](runbook.md)
- Wire format (byte-exact): [../CLAUDE.md](../CLAUDE.md)
- Dispatcher: [cmd/dhs/cmd_tsl_producer.go](../../../cmd/dhs/cmd_tsl_producer.go)
- Source: [internal/tsl/provider/](../provider/) · codec [internal/tsl/codec/](../codec/)
