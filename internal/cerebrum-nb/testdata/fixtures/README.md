# Cerebrum NB test fixtures (codec-generated 0v16 wire frames)

Replay fixtures for the EVS **Cerebrum Northbound API 0v16** connector, per
ADR-0025 deliverable 8 (replay fixtures). Sibling of
[`internal/probel-sw02p/testdata/fixtures/`](../../../probel-sw02p/testdata/fixtures/README.md)
— same layout, same role: committed, real, minimal wire artifacts that let
the codec / docs / dissector be cross-checked without a live matrix.

## Provenance — read this first

Every `.xml` file here is **produced by the 0v16 codec**, never hand-typed
and never lifted from a live capture. There is **no live Cerebrum peer**
(the Northbound API licence is currently missing on the lab Cerebrum) and
**no emulator**, so the codec is the only authority for real 0v16 wire
bytes. A live capture against a licensed Cerebrum is pending and will be
diffed against these fixtures when available.

- **TX frames** (`tx_*.xml`) are the raw output of a `codec.Encode*` call.
  The encoder uses an ordered `AttrsBuilder`, so the bytes are
  deterministic and UPPERCASE (the wire-actual canonical case verified
  against a live Cerebrum 2026-04-26 — see [`../../CLAUDE.md`](../../CLAUDE.md)
  "Wire layer").
- **RX frames** (`rx_*.xml`) are produced by feeding the page-cited 0v16
  worked example (the same string already pinned byte-for-byte in
  [`../../codec/codec_0v16_test.go`](../../codec/codec_0v16_test.go))
  through `codec.Decode` and re-serialising the resulting case-folded
  `codec.Element` AST with sorted attribute keys. Every element name,
  attribute name, attribute value and nesting therefore comes from the
  codec's real RX decode path; only the attribute *ordering* is normalised
  (sorted) so the output is byte-deterministic. RX frames are lowercase
  because the decoder case-folds the AST.

`(*codec.Element).String()` is deliberately **not** used for RX rendering:
it iterates the attribute map in Go's randomised order and is not
byte-stable, which would defeat the drift-guard.

## Drift-guard

[`fixtures_test.go`](fixtures_test.go) `TestFixtures` regenerates every
fixture from the codec and asserts the committed file is byte-identical —
the same pattern as probel-sw02p's `TestMatrixTreeExportFixture`. It runs
under the normal (non-integration) test build, so CI catches any drift
between the committed bytes and the codec.

Regenerate after an intentional codec change (PowerShell on this host):

```powershell
$env:DHS_REGEN_FIXTURES=1
go test ./internal/cerebrum-nb/testdata/fixtures/ -run TestFixtures -count=1
Remove-Item Env:DHS_REGEN_FIXTURES
go test ./internal/cerebrum-nb/testdata/fixtures/ -run TestFixtures -count=1   # must be green
```

## Fixture catalogue

| File | Dir | Codec entry-point | Spec |
|---|:--:|---|---|
| `tx_login.xml` | TX | `EncodeLogin` | §2.1 LOGIN |
| `tx_device_config_add_generic.xml` | TX | `EncodeDeviceConfiguration` (GENERIC ADD) | §4.5.1.1 p20 |
| `tx_action_route.xml` | TX | `EncodeAction(&RoutingAction{ROUTE})` | §4.1.1 |
| `tx_action_salvo_run.xml` | TX | `EncodeAction(&SalvoAction{RUN})` | §4.3 |
| `tx_action_category_create.xml` | TX | `EncodeAction(&CategoryAction{CREATE})` | §4.2 |
| `tx_obtain_datastore.xml` | TX | `EncodeObtain(&DatastoreChange{Path})` | §5.5.1 p30 |
| `rx_login_reply_datastores.xml` | RX | `Decode` LOGIN_REPLY + `<DATASTORES>` | §2.1 p7 |
| `rx_device_config_result_accepted.xml` | RX | `Decode` DEVICE_CONFIGURATION `<RESULT>` | §4.5.1.1 p20 |
| `rx_continue.xml` | RX | `Decode` `<CONTINUE/>` | §1.4 p5 |
| `rx_wildcard_complete.xml` | RX | `Decode` `<WILDCARD_COMPLETE>` | §1.6 p6 |
| `rx_routing_change_route.xml` | RX | `Decode` ROUTING_CHANGE TYPE=ROUTE | §5.1.1 |
| `rx_device_change_value.xml` | RX | `Decode` DEVICE_CHANGE TYPE=VALUE | §5.4.3 p29 |
| `rx_salvo_change_instance_details.xml` | RX | `Decode` SALVO_CHANGE INSTANCE_DETAILS | §5.3.3 p28 |
| `rx_category_change_details.xml` | RX | `Decode` CATEGORY_CHANGE CATEGORY_DETAILS | §5.2.2 p28 |
| `rx_datastore_change_data.xml` | RX | `Decode` DATASTORE_CHANGE FILE + `<DATA>` | §5.5.1 p30 |

Covers the headline 0v16 additions over the 0v13 baseline: the
`<DATASTORES>` login body, `<DEVICE_CONFIGURATION>` CRUD + `<RESULT>`, the
`<CONTINUE>` / `<WILDCARD_COMPLETE>` flow-control frames, and the enriched
`*_change` events plus a datastore DATA body.

## `../exports/`

Reserved for canonical-tree JSON exports (probel-sw02p ships
`matrix_tree.json` there). Cerebrum NB is **consumer-only** (see
[`../../docs/provider.md`](../../docs/provider.md)); it serves no tree of
its own, so there is no provider-export fixture to commit. A `.gitkeep`
holds the directory.

## Pairing with `captures/`

Live captures (when an NB licence is available) land LOCAL-ONLY under
`captures/cerebrum-nb/<scenario>/` (gitignored per ADR-0021). Promote a
trimmed frame into a committed fixture only after confirming it matches the
codec — these codec-generated fixtures stay the reference until then.

## Authoritative spec

[`../../assets/Cerebrum Northbound API 0v16.pdf`](../../assets/Cerebrum%20Northbound%20API%200v16.pdf)
— 0v16 is the authoritative version; 0v13 is the historical baseline.
