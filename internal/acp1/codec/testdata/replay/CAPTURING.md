# Capturing a new device into the replay corpus

The replay corpus grows by capturing real wire frames from a device (or
vendor emulator) and committing them under `testdata/replay/<device>/`. The
codec and provider replay tests pick the new file up automatically.

## 1. Capture a walk

Point `dhs` at the device and walk one or more slots with `--capture`. The
`--capture <dir>` flag writes raw frames to `<dir>/raw.acp1.jsonl`:

```sh
# One slot:
dhs consumer acp1 walk <device-ip> --slot 0 --transport udp --capture /tmp/cap0

# Several slots into separate dirs, then concatenate:
for s in 0 1 2 3 4 5; do
  dhs consumer acp1 walk <device-ip> --slot $s --transport udp --capture /tmp/cap$s
done
cat /tmp/cap*/raw.acp1.jsonl > /tmp/device.jsonl
```

Use `--transport tcp` or `--transport an2` to capture the other wire modes
(Mode B / Mode C) — the codec must decode all three identically.

## 2. Trim to the wire fields (optional but tidy)

Captures carry `ts`/`proto`/`len` metadata the tests ignore. Keep the file
small and stable by reducing each line to `{"dir","hex"}` (any tool works;
the tests tolerate extra fields, so this step is cosmetic).

## 3. Commit under a device directory

```
internal/acp1/codec/testdata/replay/<vendor>-<model>@<firmware>/walk.jsonl
```

e.g. `axon-rrs18@1601/walk.jsonl`. Use the emulator name (`synapse-sim`) for
emulator captures. Then add a row to `INDEX.md`.

The `.jsonl` ignore rule is overridden for `testdata/replay/**` (see the repo
`.gitignore`), so committed corpus files are tracked while ad-hoc `--capture`
output elsewhere stays local (ADR-0021).

## 4. Verify

```sh
go test ./internal/acp1/codec/ -run TestReplay_RealFrames -v
go test ./internal/acp1/provider/ -run TestReplayFidelity -v
```

Both should report your new device as a passing subtest. If decode fails, the
device exposed a wire shape the codec doesn't handle yet — that's a real
finding: fix the codec (spec-first), don't trim the frame away.
