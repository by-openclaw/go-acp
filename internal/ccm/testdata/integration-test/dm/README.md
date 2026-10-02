# CCM device models — generated, not committed

ADR-0025 deliverable 4 allows a DM fixture to be committed **or
generated**. For CCM it is generated, for two reasons.

1. **The model is the device's own.** A CCM DM is built from the
   `api.yml` the device serves plus a walk of the resources it declares
   (operator rule, 2026-10-01: never hand-written, never hard-coded). A
   committed copy would be a second source that goes stale with the
   next firmware.
2. **Size.** The Neuron Shuffle's DM is 292 714 objects — 118 MB of
   JSON. The FusioN6 fixture that *is* committed is 7 203 objects.

## The generator

`internal/ccm/integration/shuffle_test.go`,
`TestTheDMIsCollectedOnceAndTheNextWatchStartsFromIt`:

- the first `dhs consumer ccm watch <host> --slot 0` finds no DM, shows
  values at once from the event channel and collects the model in the
  background (about 4 minutes on the Shuffle), writing
  `.cache/dm/ccm/<productName>@<productVersion>.json`;
- the test checks the file is a tree snapshot with the model in it;
- the second watch prints `DM cache hit "<identity>" — seeded slot 0
  with N objects` and reads nothing from the device to get it.

Nothing here is skipped for a missing fixture: the test makes its own.

## What IS committed

| what | where |
|---|---|
| The model contract, per API version | `internal/ccm/codec/testdata/SHUFFLE@2.0.0-openapi.yml`, `BRIDGE@7.0.3-api.yml` |
| The device manifests the suite targets | `../manifest/lab-shuffle-01.json`, `../manifest/lab-convert-01.json` |

The Shuffle at 10.6.255.103 runs product `6.0.0-7e4a27e` and serves API
document version `2.0.0`: the same document as the committed
`SHUFFLE@2.0.0-openapi.yml`, byte for byte once line endings are
normalised (compared 2026-10-02). The DM key is the product version;
the spec file name is the API version.
