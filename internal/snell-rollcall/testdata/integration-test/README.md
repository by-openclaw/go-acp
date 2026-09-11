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

## `iq-frame-12.json` — the IQ frame, card for card

The IQ modular frame at `10.6.255.113` as its own port list describes it: five
IQDBE00 Nodal cards on the odd ports `01` to `09` and three IQMUX42 AES cards on
`0B`, `0C` and `0D`. Both DMs were walked off that frame (`IQMUX42@8.5.cs17` on
2026-09-11), so a client of this manifest finds real cards at the addresses a
client of the real frame uses.

    dhs producer rollcall serve --manifest manifest/iq-frame-12.json         --cache-dir internal/snell-rollcall/testdata/integration-test         --generation 16 --unit 12

`--generation 16` because the real frame advertises no long strings, and
`--unit 12` because it is unit `0x0C`. `iqframe_test.go` serves it this way and
asserts every card answers at the real address with the real type.

What it does not reproduce, and why:

- **The gateway** is ours. The IQH3UM4-S serves a paged menu — seven page
  entries over 619 commands — which this connector does not walk yet.
- **Instance names** such as `EMB.06 (Nodal)` are per-card data, which ADR-0022
  routes through a manifest `defaults` block that is not built yet. Cards are
  named after their model.
- **Services**: our cards advertise Display, which the real ones do not. A DM
  does not record services.

