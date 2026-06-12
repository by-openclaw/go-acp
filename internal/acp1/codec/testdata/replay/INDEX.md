# ACP1 replay corpus — real captured wire frames

Each subdirectory holds real ACP1 wire frames captured from one device or
firmware revision. They are the **independent oracle** for the codec
integration test (`codec.TestReplay_RealFrames`) and the provider fidelity
test (`provider.TestReplayFidelity_*`): captured device bytes, not output of
our own encoder, so a shared bug cannot make the test falsely pass.

One JSON object per line: `{"dir":"tx|rx","hex":"<frame bytes>"}`. Comment
lines start with `#`. `dir` is the capture direction (`tx` = sent by us,
`rx` = received from the device).

## How the corpus is used

| Test | Package | What it proves |
|------|---------|----------------|
| `TestReplay_RealFrames` | `codec` | every real frame decodes; non-error frames re-encode **byte-exact**; every `getObject` reply decodes into a typed object. |
| `TestReplayFidelity_GetObject` | `provider` | the provider's encoder, fed an object decoded from a real `getObject` reply, re-serves bytes that decode back to the **same object** — i.e. our provider serves like a real device. |

Both tests auto-discover `testdata/replay/*/*.jsonl`, so **adding a device is
just dropping a file in** — no code change. See `CAPTURING.md`.

## Devices in the corpus

| Directory | Device / firmware | Transport | Source | Frames | Captured |
|-----------|-------------------|-----------|--------|-------:|----------|
| `synapse-sim/` | Axon Synapse Simulator (emulator) | UDP (Mode A) | slots 0–5 walk | 600 | 2026-06-10 |

> Add a row per device as captures land. Prefer real Axon hardware
> (`<vendor>-<model>@<firmware>/`) so the corpus reflects field behaviour,
> not just the emulator.
