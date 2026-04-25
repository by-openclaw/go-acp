# OSC test harness (osc.js reference peer)

Byte-level oracle for validating [../..](../..) (the dhs OSC codec) against
an independent reference implementation. Test-only — not linked into the
Go build. Runs inside the devcontainer so Node stays out of host tooling.

## Install

Done automatically by `.devcontainer/post-create.sh` on container build.
To install manually:

```bash
cd internal/osc/assets/test-harness
npm install
```

## Usage

Two modes: `encode` turns a JSON spec into OSC bytes, `decode` turns bytes
back into a JSON spec. Both use osc.js metadata form, so every type tag
(including OSC 1.1 `[`/`]` array markers) round-trips.

```bash
# encode a message to hex
echo '{"address":"/foo","args":[{"type":"i","value":42},{"type":"f","value":3.14}]}' \
  | node harness.js encode --hex
# -> 2f666f6f00000000 2c696600 0000002a 4048f5c3

# decode hex bytes back to JSON
echo '2f666f6f00000000 2c696600 0000002a 4048f5c3' \
  | tr -d ' ' | node harness.js decode --hex

# encode to a file for byte-exact comparison against dhs producer output
node harness.js encode --spec spec.json --out reference.bin

# decode a file captured from dhs producer
node harness.js decode --in captured.bin
```

## Spec JSON shapes

Message — a list of typed args:

```json
{
  "address": "/mixer/1/gain",
  "args": [
    { "type": "i", "value": 42 },
    { "type": "f", "value": 3.14 },
    { "type": "s", "value": "hello" },
    { "type": "T" }
  ]
}
```

Bundle — `timeTag` + nested packets (messages or bundles):

```json
{
  "timeTag": { "raw": [0, 1] },
  "packets": [
    { "address": "/a", "args": [{ "type": "i", "value": 1 }] },
    { "address": "/b", "args": [{ "type": "i", "value": 2 }] }
  ]
}
```

OSC 1.1 array markers — emit `[` and `]` as type-tag sentinels:

```json
{
  "address": "/array",
  "args": [
    { "type": "i", "value": 1 },
    { "type": "[" },
      { "type": "i", "value": 10 },
      { "type": "i", "value": 20 },
    { "type": "]" }
  ]
}
```

## Why this harness

No off-the-shelf Go OSC library covers OSC 1.1 arrays, and none exercise
TCP SLIP framing. osc.js does both. We drive it from Go tests via
`exec.Command("node", "harness.js", ...)` in an integration-tagged test
to confirm byte-exact parity between dhs and the reference.

## Notes

- Transport framing (UDP / TCP length-prefix / TCP SLIP) is the dhs side's
  responsibility — this harness deals with raw OSC packet bytes only.
  For framed-transport round-trips, let dhs do the framing and compare
  inner packets.
- osc.js does not implement OSC address-pattern matching on dispatch;
  that too lives in the dhs consumer.
