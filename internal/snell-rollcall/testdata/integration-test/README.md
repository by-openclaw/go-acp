# The frame this connector serves without a device

A provider that needs hardware to start cannot be tested, so this is the frame
it starts from instead: one real card, committed, and the manifest that puts it
in a chassis.

```
dhs producer rollcall serve \
  --manifest internal/snell-rollcall/testdata/integration-test/manifest/rollcall-integration.json \
  --cache-dir internal/snell-rollcall/testdata/integration-test
```

## Where it came from

`dm/rollcall/IQDBE00@5.0.cs5.json` is an IQDBE00 nodal card, walked once off the
IQ modular frame `IQH3UM4-S` at 10.6.255.113 — 168 elements, the card's whole
menu rather than a trimmed sample of it. The name is the card's own answer to a
GETID, not a label anybody chose: type 5.0, command set 5.

The frames that walk produced are beside it in
`../fixtures/iq-frame-IQDBE00/wire.jsonl`, and are replayed through the codec by
`TestTheCommittedCaptureStillDecodes`. So the same walk is kept twice, on
purpose: once as what the device said, once as what we made of it.

## Why two slots

Both name the same DM. A device model describes a product and not a position,
and a frame with two of the same card is two slots pointing at one file — which
is also the cheapest way to notice a provider that has quietly started serving
a DM per slot.

## Keeping it honest

`TestManifestBuildsAFrameFromTheRepositoryAlone` starts a provider from these
files, walks it with our own consumer, and checks the card is there and both
slots agree. It runs under `-tags integration` and needs nothing but the
repository.
