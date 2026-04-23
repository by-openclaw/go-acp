# Changelog

All notable changes documented here. Format follows [Keep a Changelog](https://keepachangelog.com/).

**Version source of truth: git tags.** The Makefile reads `git describe --tags`
and injects it into the binary via `-ldflags`. No hardcoded version strings
anywhere. Workflow:

```
1. Work on main branch
2. Update this file with the new version section
3. git tag -a vX.Y.Z -m "vX.Y.Z"
4. git push origin vX.Y.Z
5. make build-all / make package
```

---

## Unreleased

### Added

* **fixtures(acp2):** per-type README + capture + frozen tree for every
  ACP2 wire element — all 6 object types (node/preset/enum/number/ipv4/
  string), all 4 functions (get_version/get_object/get_property/
  set_property), announce (type=2), and all 6 error codes
  (protocol/invalid_obj_id/invalid_idx/invalid_pid/no_access/
  invalid_value). Captures are self-driven via `dhs producer` +
  `dhs consumer` + tshark on loopback — no external hardware. Regenerate
  with `make fixtures-acp2`. Closes #64.
* **consumer(acp2):** `--idx N` and `--pid N` flags on
  `dhs consumer acp2 get`, letting operators read arbitrary pids and
  preset idx values (defaults preserve the historical pid=8 / idx=0
  behaviour).
* **provider(acp2):** preset (obj_type 1) encoder — pid 7 preset_depth +
  depth-indexed pids 8/9/10/11. Driven by a `preset` token in the
  canonical `Parameter.format` string alongside a numeric wire type and
  optional `depth=N`.
* **consumer(acp2):** `diag` now probes an unknown func code (0xFF) to
  exercise the provider's stat=0 protocol-error path.

### Fixed

* **provider(acp2):** handleGetProperty rejects `idx != 0` on non-preset
  objects with stat=2 `invalid_idx`, per spec §4. Previously silently
  ignored.

## [0.3.1](https://github.com/by-openclaw/go-acp/compare/v0.3.0...v0.3.1) (2026-04-23)


### Bug Fixes

* **emberplus/provider:** coerce Connect/Disconnect to Absolute on oneToN / oneToOne ([#98](https://github.com/by-openclaw/go-acp/issues/98)) ([d1c342c](https://github.com/by-openclaw/go-acp/commit/d1c342ce442fae64cc5c8ad41633c538d3a97006))
* **emberplus/provider:** matrix Connect/Disconnect semantics. ([d1c342c](https://github.com/by-openclaw/go-acp/commit/d1c342ce442fae64cc5c8ad41633c538d3a97006))
* **emberplus/wireshark:** descend SET wrapper in Parameter/NodeContents. ([d926a24](https://github.com/by-openclaw/go-acp/commit/d926a244118bf73df1dd941720b46b2a3a9adce9))
* **emberplus/wireshark:** descend universal SET wrapper in ParameterContents / NodeContents ([#99](https://github.com/by-openclaw/go-acp/issues/99)) ([d926a24](https://github.com/by-openclaw/go-acp/commit/d926a244118bf73df1dd941720b46b2a3a9adce9))

## [0.3.0](https://github.com/by-openclaw/go-acp/compare/v0.2.0...v0.3.0) (2026-04-23)


### Features

* **diff:** acp diff — semantic tree.json comparison + CHANGELOG generator ([#50](https://github.com/by-openclaw/go-acp/issues/50)) ([54fd9c6](https://github.com/by-openclaw/go-acp/commit/54fd9c6934bd640807d88e811eab970d15c72775)), closes [#49](https://github.com/by-openclaw/go-acp/issues/49)
* **emberplus:** Wireshark dissector + unified install docs ([#36](https://github.com/by-openclaw/go-acp/issues/36).e) ([#55](https://github.com/by-openclaw/go-acp/issues/55)) ([1833a9d](https://github.com/by-openclaw/go-acp/commit/1833a9d57e601bbf71638f271149b7235c38800a))
* **export:** CSV lossless round-trip with oid + path columns ([#39](https://github.com/by-openclaw/go-acp/issues/39)) ([39b88d5](https://github.com/by-openclaw/go-acp/commit/39b88d5f02246615f73d3ef4d15a26e2b832c3ab)), closes [#38](https://github.com/by-openclaw/go-acp/issues/38)
* **extract:** acp extract — per-product DM triple into fixture layout ([#48](https://github.com/by-openclaw/go-acp/issues/48)) ([1eb8e83](https://github.com/by-openclaw/go-acp/commit/1eb8e83af32e76f5e772a432af176e3fc901c3a2))
* **fixtures:** ACP1 per-type README + capture + frozen tree ([#63](https://github.com/by-openclaw/go-acp/issues/63)) ([559b67b](https://github.com/by-openclaw/go-acp/commit/559b67bc3f2169684849ce35b5624b8ae7cf01ee))
* **fixtures:** ACP1 per-type README + capture + frozen tree ([#63](https://github.com/by-openclaw/go-acp/issues/63)) ([#65](https://github.com/by-openclaw/go-acp/issues/65)) ([559b67b](https://github.com/by-openclaw/go-acp/commit/559b67bc3f2169684849ce35b5624b8ae7cf01ee))
* **fixtures:** Ember+ per-type README + capture + frozen tree ([#60](https://github.com/by-openclaw/go-acp/issues/60)) ([1140ca6](https://github.com/by-openclaw/go-acp/commit/1140ca66e7f1055d7ab9780cd0b4549f54343177))
* **fixtures:** Ember+ per-type README + capture + frozen tree ([#60](https://github.com/by-openclaw/go-acp/issues/60)) ([#61](https://github.com/by-openclaw/go-acp/issues/61)) ([1140ca6](https://github.com/by-openclaw/go-acp/commit/1140ca66e7f1055d7ab9780cd0b4549f54343177))
* **import:** selective --id / --path filters (mutually exclusive) ([#46](https://github.com/by-openclaw/go-acp/issues/46)) ([7bfc8ab](https://github.com/by-openclaw/go-acp/commit/7bfc8ab59413f3deea6a1c4946e0d098bf67fe69)), closes [#45](https://github.com/by-openclaw/go-acp/issues/45)
* **probel:** SW-P-08 end-to-end — scaffold + 11 commands ([#83](https://github.com/by-openclaw/go-acp/issues/83)–89) ([#84](https://github.com/by-openclaw/go-acp/issues/84)) ([326a21e](https://github.com/by-openclaw/go-acp/commit/326a21ee9303fc3fdba43dc84933b5d5e0ae6da9))
* **provider:** ACP1 announce-demo ticker + datagram + broadcast logs ([#81](https://github.com/by-openclaw/go-acp/issues/81)) ([36b9eb2](https://github.com/by-openclaw/go-acp/commit/36b9eb274b65777771afd4109688c86404e0b4b8))
* **provider:** ACP1 provider plugin — UDP:2071 MVP ([#74](https://github.com/by-openclaw/go-acp/issues/74)) ([144c4fe](https://github.com/by-openclaw/go-acp/commit/144c4fecd3f20b9049d4b18b4e827d69f43a22f6))
* **provider:** ACP2 provider plugin — AN2/TCP:2072 MVP ([#75](https://github.com/by-openclaw/go-acp/issues/75)) ([#76](https://github.com/by-openclaw/go-acp/issues/76)) ([94dcba5](https://github.com/by-openclaw/go-acp/commit/94dcba596b33b326d78b21ccbea9a90f6ce8d1ff))
* **provider:** Ember+ provider plugin — MVP serve tree.json on :9010 ([#66](https://github.com/by-openclaw/go-acp/issues/66)) ([#67](https://github.com/by-openclaw/go-acp/issues/67)) ([1f331e5](https://github.com/by-openclaw/go-acp/commit/1f331e5e8f35c5fe218a78da3bf817a402a038a7))
* **provider:** Matrix + Function + Labels + Streams + Lock (spec-compliant) ([#72](https://github.com/by-openclaw/go-acp/issues/72)) ([44e9757](https://github.com/by-openclaw/go-acp/commit/44e97570119738c5f95c42d31986aac87dfbf0e4))
* **scenario:** declarative error-path test harness (replay-only MVP) ([#52](https://github.com/by-openclaw/go-acp/issues/52)) ([40654be](https://github.com/by-openclaw/go-acp/commit/40654bed998a18e6e05d818c00a80dedd0894db1)), closes [#51](https://github.com/by-openclaw/go-acp/issues/51)
* **wireshark:** ACP1 + ACP2 dissector Info column — richer details ([#80](https://github.com/by-openclaw/go-acp/issues/80)) ([aa1d627](https://github.com/by-openclaw/go-acp/commit/aa1d627b9ddf9c64f36d3b6242daa2a16e1eaffa))


### Bug Fixes

* **capture:** name raw frame file after the wire transport per protocol ([#42](https://github.com/by-openclaw/go-acp/issues/42)) ([7db5149](https://github.com/by-openclaw/go-acp/commit/7db5149c65b62255a0d0764132321166bf494ccc)), closes [#41](https://github.com/by-openclaw/go-acp/issues/41)

## [0.2.0](https://github.com/by-openclaw/go-acp/compare/v0.1.1...v0.2.0) (2026-04-19)


### Features

* **acp1:** canonical export + compliance profile — align with Ember+ architecture ([#33](https://github.com/by-openclaw/go-acp/issues/33)) ([9c5448e](https://github.com/by-openclaw/go-acp/commit/9c5448e976a011c297cbe092dcf27c67f9afa20f))
* **acp2:** add diag command for protocol probing ([5c5f515](https://github.com/by-openclaw/go-acp/commit/5c5f5150a82e841972757f2cbe9ec41738c64142))
* **acp2:** background walk for watch command ([4effe4d](https://github.com/by-openclaw/go-acp/commit/4effe4ddf279b895627a0cde087f3c2eaf52fc84))
* **acp2:** canonical alignment — closes Part A ([#37](https://github.com/by-openclaw/go-acp/issues/37)) ([b6343e3](https://github.com/by-openclaw/go-acp/commit/b6343e34154fdd8e1c4038a52ea4565729514249))
* **acp2:** complete ACP2 protocol plugin (AN2/TCP) ([0ba290b](https://github.com/by-openclaw/go-acp/commit/0ba290b4a14ce2ff1da0c79a9bac39416d16e484))
* **acp2:** complete ACP2 protocol plugin (AN2/TCP) ([9da152c](https://github.com/by-openclaw/go-acp/commit/9da152c7c6a9be3a6332e944a85e9242d126049a))
* **acp2:** fast GetValue by ID without full walk ([3e669fc](https://github.com/by-openclaw/go-acp/commit/3e669fca3396c151d2b5ee3b9563d598f85b60fa))
* **acp2:** SetValue with fetchObjectMeta fallback ([ffa86b9](https://github.com/by-openclaw/go-acp/commit/ffa86b9f2036ffe1b57b5fa51097deea046ebc03))
* **acp2:** streaming walk output, no timeout on tree traversal ([213ec46](https://github.com/by-openclaw/go-acp/commit/213ec4690e426f18758b7a85b65e85243b6cfa00))
* add --slot filter to export command, remove walk timeout ([ffce194](https://github.com/by-openclaw/go-acp/commit/ffce19452d9229d8bba152be902e2c15cd58ce14))
* add traffic capture, Wireshark dissectors, CLI help overhaul ([95cfa4e](https://github.com/by-openclaw/go-acp/commit/95cfa4e2ba4886a1db3f66d929879b277901e05c))
* **cli:** add --path flag for subtree walk and export ([484096d](https://github.com/by-openclaw/go-acp/commit/484096d7468d6dd9f0aefb260b515fa7112a6d90))
* **cli:** add --path flag for subtree walk and export ([bad08c9](https://github.com/by-openclaw/go-acp/commit/bad08c9ecd72d728c55b785e591226354eab8957)), closes [#9](https://github.com/by-openclaw/go-acp/issues/9)
* connector architecture + logging foundation + unified export ([b60f7ab](https://github.com/by-openclaw/go-acp/commit/b60f7abce57b2a7c75ee4d0b57b6d78189325fbe))
* connector architecture doc + logging foundation + unified export ([9c69d57](https://github.com/by-openclaw/go-acp/commit/9c69d5754f9f29172e5833946042062a8b97192f)), closes [#14](https://github.com/by-openclaw/go-acp/issues/14)
* disk cache label resolution + ACP1 exports + storage tests ([8f6f78a](https://github.com/by-openclaw/go-acp/commit/8f6f78a7da73bd294bbfe0dc88b44fb50b86afe9))
* **emberplus:** consumer feature-complete per spec v2.50 — canonical export + resolver + runtime + tests ([#29](https://github.com/by-openclaw/go-acp/issues/29)) ([171f32a](https://github.com/by-openclaw/go-acp/commit/171f32a3b12bf94dc515952bd63f41a9b341aa25))
* **emberplus:** Ember+ consumer protocol plugin ([9117b2e](https://github.com/by-openclaw/go-acp/commit/9117b2ee5a1a5569e4a4cc85e6eb5efd643ecf0b))
* **emberplus:** Ember+ consumer protocol plugin (BER + S101 + Glow) ([bf51e6d](https://github.com/by-openclaw/go-acp/commit/bf51e6d1c91c6352b0e64ab8987bf31331f65353))
* **emberplus:** function invoke, invoke result decoder, SET unwrap fixes ([9310b68](https://github.com/by-openclaw/go-acp/commit/9310b6803ecc3c46a63ae41fc38fa8a9a067a984))
* **emberplus:** wire-tested Ember+ consumer — set, matrix, subscribe, path addressing ([81b522d](https://github.com/by-openclaw/go-acp/commit/81b522d45f94e8dc2f31752f119fde9c75ea1dfb))
* **export:** hierarchical tree export/import for ACP2 ([7218040](https://github.com/by-openclaw/go-acp/commit/72180403cbe4a722e4b204b75e05bcf10d2ad04b))
* **export:** hierarchical tree export/import for ACP2 ([7ac720d](https://github.com/by-openclaw/go-acp/commit/7ac720d2e588ced492607cbee0b5dab68221365a)), closes [#5](https://github.com/by-openclaw/go-acp/issues/5)
* file-backed tree store with hierarchical JSON cache ([cd8389c](https://github.com/by-openclaw/go-acp/commit/cd8389c404051a425209a137683577967d3d999c))
* file-backed tree store with hierarchical JSON cache ([5abb399](https://github.com/by-openclaw/go-acp/commit/5abb3993d7d255dc103f65b13ea9ba73fe41add4)), closes [#11](https://github.com/by-openclaw/go-acp/issues/11)
* get/set resolve labels from disk cache + regenerate ACP1 exports + storage tests ([4454885](https://github.com/by-openclaw/go-acp/commit/44548855252d6b4bf06787012066f9671ad8a99c)), closes [#16](https://github.com/by-openclaw/go-acp/issues/16)
* **watch:** instant labels from disk cache + unit display ([7701919](https://github.com/by-openclaw/go-acp/commit/77019198b026e6ac30952a7f9772452e35f2d2af))
* **watch:** instant labels from disk cache + unit display + source tag ([91b2c11](https://github.com/by-openclaw/go-acp/commit/91b2c11e92dc65dba04083451f711df256c71ab5))


### Bug Fixes

* ACP2 export groups objects by path (BOARD, PSU, etc.) ([41c0ae5](https://github.com/by-openclaw/go-acp/commit/41c0ae550a697fc6d84b766ff1644dd78ed1af44))
* **acp2:** announce decode, watch command, walk --filter ([ee4b925](https://github.com/by-openclaw/go-acp/commit/ee4b9258b11ace6aaa4f2bd207e7974161a6d612))
* **acp2:** enum decode in GetValue, default resolution, walk --filter, asset reorg ([0213588](https://github.com/by-openclaw/go-acp/commit/02135886fb2a0930e9bd37e371f6c419f9ae308c))
* **acp2:** fix off-by-one in AN2 reply parsing — skip func echo byte ([3d23953](https://github.com/by-openclaw/go-acp/commit/3d239539ffd9462c4397a24352af04cb12f13a05))
* **acp2:** IPv4 set from string, nil conn panic on disconnect ([7de6a54](https://github.com/by-openclaw/go-acp/commit/7de6a54f3837d23a88d5e171618293f594bf45c7))
* **acp2:** parse enum options variable-length format, use full u32 index ([deaf9ad](https://github.com/by-openclaw/go-acp/commit/deaf9ad3e533f4cf461ed65995a9b679af2cf35d))
* **acp2:** remove idx from get_object request, add payload hex dump ([a493971](https://github.com/by-openclaw/go-acp/commit/a493971e991182bbf339a8d65bafbb7c5d0452c0))
* **acp2:** revert get_object to include idx (confirmed by dissector), add full hex dumps ([e4979eb](https://github.com/by-openclaw/go-acp/commit/e4979eb8dbcf4813ed4ef2dc52dab3034dab8b2d))
* **acp2:** suppress announce log flooding (ACP1 pattern) ([c8f8ded](https://github.com/by-openclaw/go-acp/commit/c8f8dedb184cdb209381feebff816343a964ee1c))
* **acp2:** watch pre-walks slot for labels + typed announce values ([b255527](https://github.com/by-openclaw/go-acp/commit/b2555272a0285a62a15bdc8e90eb41d5977ee215))
* **ci:** enable Git LFS in checkout for replay test captures ([cfbd1ca](https://github.com/by-openclaw/go-acp/commit/cfbd1ca344fce767bd104fbe7b1d51028cfb141a))
* **ci:** handle LFS pointer files in replay tests + install git-lfs on RHEL ([3457b84](https://github.com/by-openclaw/go-acp/commit/3457b84633d6a3d7d6d07aff119665e441e30d70))
* **cli:** walks ignore --timeout; default --timeout 1s ([#35](https://github.com/by-openclaw/go-acp/issues/35)) ([a48838e](https://github.com/by-openclaw/go-acp/commit/a48838ef8ba951785854ea26cb28a9e33b500f53))
* **export:** remove unused orderedMap type (lint) ([8f466a3](https://github.com/by-openclaw/go-acp/commit/8f466a3390be56a027938f7aa5d771ebd47c7ad1))
* remove timeout on walk and watch pre-walk ([6b0aca9](https://github.com/by-openclaw/go-acp/commit/6b0aca99b2787e50d0fbd58f95b443f88d8eac82))
* resolve all golangci-lint errors for CI ([c11cc01](https://github.com/by-openclaw/go-acp/commit/c11cc0197c716c371195b1dbe3b80c54ea4fe11b))
* restore ACP1 walk output (regression from streaming walk) ([a952dde](https://github.com/by-openclaw/go-acp/commit/a952ddefda369b3cf465dde5ce3db915aaf57b46))

## [0.1.1](https://github.com/by-openclaw/go-acp/compare/v0.1.0...v0.1.1) (2026-04-16)


### Bug Fixes

* **ci:** resolve 6 golangci-lint errcheck/staticcheck findings ([67dbf3e](https://github.com/by-openclaw/go-acp/commit/67dbf3ec4772a8a42723651631b22c01fe3a3e5e))
* **ci:** resolve VCS status error in RHEL/Rocky containers ([c8e3478](https://github.com/by-openclaw/go-acp/commit/c8e3478d7836d1521fb61d07035c842958d9a61c))

## [Unreleased]

_Changes on main not yet tagged._

### Fixed

- **probel-sw08p (provider):** salvo commit (rx 121 op=Set) now emits one
  tx 04 Crosspoint Connected per applied slot to every session (originator
  via `streamToSender`, others via `tallies` fan-out), fires
  `probel_salvo_emitted_connected` per slot. Follows §3.2.3 literally over
  §3.2.30's unreachable "listeners use cmd 122+123" advice — no shipping
  SW-P-08 controller (Commie, Lawo VSM, and by inference real XD/ECLIPSE
  matrices they were built against) implements that path. Live-verified:
  both Commie (originator) and VSM (second session) tally UIs refresh on
  salvo commit. Regression guards: `TestSalvoSetEmitsConnectedPerSlot`
  (unit) + `TestIntegrationSalvoBroadcastsConnectedToAllSessions`
  (two real TCP sessions). Closes #92.
- **probel-sw08p (provider):** rx 121 op=Clear on an empty salvo slot now
  replies `tx 123 status=0x02 SalvoDoneNone` per §3.3.25 (spec-strict).
  Interim diagnostic coerce to status=0x01 `SalvoDoneCleared` reverted.

---

## [0.1.0] — 2026-04-16

Initial release. ACP1 protocol fully implemented.

### Added

- ACP1 plugin: full codec for all 11 object types (root, int, ipaddr, float, enum, string, frame, alarm, file, long, byte)
- ACP1 walker with LRU+TTL cache and live event updates
- ACP1 announcement listener (UDP broadcast, SO_REUSEADDR multi-instance)
- ACP1 typed value codec: encode/decode for read/write with step-based precision
- UDP direct transport (port 2071)
- TCP direct transport (ACP v1.4, port 2071)
- Protocol registry with compile-time plugin model
- CLI commands: `info`, `walk`, `walk --all`, `get`, `set`, `watch`, `discover`
- CLI commands: `export` (JSON, YAML, CSV), `import` (JSON, YAML, CSV)
- CLI commands: `list-protocols`, `help` (with per-command help pages)
- CLI flag: `--transport udp|tcp`
- Label-based addressing for all CLI operations
- Sub-group marker detection (both enum-space and leading-whitespace string conventions)
- Frame-status decoding with human-readable slot status symbols
- Cross-compile targets: Linux amd64/arm64, macOS amd64/arm64, Windows amd64
- Cross-platform verified: Windows 11 + Ubuntu 24
- Export/import lossless round-trip for all three formats (49/62/0 parity)

### Not yet implemented

- ACP1 AN2 transport
- ACP2 protocol
- REST API server (`acp-srv`)
- Persistence (`devices.yaml`)

---

Copyright (c) 2026 BY-SYSTEMS SRL — https://www.by-systems.be — MIT License
