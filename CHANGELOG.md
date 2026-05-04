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

## [0.7.0](https://github.com/by-openclaw/go-acp/compare/v0.6.0...v0.7.0) (2026-05-04)


### Features

* **emberplus:** implement Plugin.Validate() per ADR-0021 (closes [#218](https://github.com/by-openclaw/go-acp/issues/218)) ([#219](https://github.com/by-openclaw/go-acp/issues/219)) ([a4ddc71](https://github.com/by-openclaw/go-acp/commit/a4ddc71f8adce106c9ab439ba1bec63b3631ec12))
* **nmos/bcp:** validators for BCP-002/004/006/008 (Steps 10-13, closes [#168](https://github.com/by-openclaw/go-acp/issues/168) [#169](https://github.com/by-openclaw/go-acp/issues/169) [#170](https://github.com/by-openclaw/go-acp/issues/170) [#171](https://github.com/by-openclaw/go-acp/issues/171)) ([#181](https://github.com/by-openclaw/go-acp/issues/181)) ([d6ed87f](https://github.com/by-openclaw/go-acp/commit/d6ed87f48e24e42bbc45970c9ca1d43f07a7186f))
* **nmos/is-04:** IS-04-01 + IS-04-02 + IS-04-03 conformance — 516 Pass / 3 Fail (all AMWA upstream MdnsListener bug) ([#189](https://github.com/by-openclaw/go-acp/issues/189)) ([b395f1a](https://github.com/by-openclaw/go-acp/commit/b395f1ad4088fbe2205f4bbf0e486b99dac02f5d))
* **nmos/is05:** codec base + v1.0.2 + v1.1.2 Strategy impls (Step 5, closes [#163](https://github.com/by-openclaw/go-acp/issues/163)) ([#174](https://github.com/by-openclaw/go-acp/issues/174)) ([b2a17bf](https://github.com/by-openclaw/go-acp/commit/b2a17bf39ae4c7f01463e19ef92f901607ab9dd3))
* **nmos/is07:** codec base + v1.0.1 Strategy impl (Step 6) ([#176](https://github.com/by-openclaw/go-acp/issues/176)) ([f7224b3](https://github.com/by-openclaw/go-acp/commit/f7224b355cc723d8e1042d67ccb275d8fd44d075))
* **nmos/is07:** WS publisher + subscriber + CLI verbs (Step 6, closes [#164](https://github.com/by-openclaw/go-acp/issues/164)) ([#177](https://github.com/by-openclaw/go-acp/issues/177)) ([21bba98](https://github.com/by-openclaw/go-acp/commit/21bba9897271843ec55145abe715fa1d5d4abe71))
* **nmos/is08:** codec base + v1.0.1 Strategy impl (Step 7, closes [#165](https://github.com/by-openclaw/go-acp/issues/165)) ([#178](https://github.com/by-openclaw/go-acp/issues/178)) ([3c16936](https://github.com/by-openclaw/go-acp/commit/3c16936135a3f3fe86ef88d092ee2ef983d88471))
* **nmos/is09:** retrofit onto spec.Codec pattern (Step 3) ([#161](https://github.com/by-openclaw/go-acp/issues/161)) ([af3ca66](https://github.com/by-openclaw/go-acp/commit/af3ca66725a45be2cac446ba22a3ed2e04858469))
* **nmos/is12:** codec base + v1.0.1 Strategy impl (Step 8, refs [#166](https://github.com/by-openclaw/go-acp/issues/166)) ([#179](https://github.com/by-openclaw/go-acp/issues/179)) ([3adb035](https://github.com/by-openclaw/go-acp/commit/3adb035c86eee11fa73d6d482a960341dc3703d7))
* **nmos/ms05:** codec base + v1.0.0 Strategy impl (Step 9, refs [#167](https://github.com/by-openclaw/go-acp/issues/167)) ([#180](https://github.com/by-openclaw/go-acp/issues/180)) ([eaaa660](https://github.com/by-openclaw/go-acp/commit/eaaa660fc9694b33a9194d9755275d447f6a4b65))
* **nmos/wireshark:** HTTP + WebSocket layers in dhs_nmos.lua (Step 14, closes [#172](https://github.com/by-openclaw/go-acp/issues/172)) ([#182](https://github.com/by-openclaw/go-acp/issues/182)) ([a1dae55](https://github.com/by-openclaw/go-acp/commit/a1dae5541df74dbf03be851f4b4edc38234f4a9a))
* **nmos:** codec base — spec.Versioned + Registry[T] + Reporter ([#159](https://github.com/by-openclaw/go-acp/issues/159)) ([c370911](https://github.com/by-openclaw/go-acp/commit/c3709113797b720c6ebd56e3d5918b95a42e35a8))
* **nmos:** IS-04 Controller consumer (Step 4) ([#162](https://github.com/by-openclaw/go-acp/issues/162)) ([26bca70](https://github.com/by-openclaw/go-acp/commit/26bca7085040a0535226f22fbd52a43c8e4cb3b9))
* **nmos:** IS-04 multi-version codec — v1.1.3 + v1.2.2 + v1.3.3 in parallel ([#4](https://github.com/by-openclaw/go-acp/issues/4)b) ([#160](https://github.com/by-openclaw/go-acp/issues/160)) ([db4f73b](https://github.com/by-openclaw/go-acp/commit/db4f73b5f7841d1101e8ca257d87ac95766714b3))
* **nmos:** IS-04 Registry — Registration + Query + WS subscriptions + GC ([#157](https://github.com/by-openclaw/go-acp/issues/157)) ([7813f38](https://github.com/by-openclaw/go-acp/commit/7813f388001b5706af0936fe918a067bbac35e0f))
* **nmos:** IS-04 v1.3 Node API — provider + Registration client ([#155](https://github.com/by-openclaw/go-acp/issues/155)) ([8293d4f](https://github.com/by-openclaw/go-acp/commit/8293d4f956f52185f17666961ef1b5886110fb6b))
* **nmos:** IS-09 System API — server + client + selection rule ([#153](https://github.com/by-openclaw/go-acp/issues/153)) ([6f89db8](https://github.com/by-openclaw/go-acp/commit/6f89db8aca78be2b6135891b54538212859c4f98))
* **nmos:** Phase 1 step [#1](https://github.com/by-openclaw/go-acp/issues/1) — DNS-SD codec + 3 modes + plugin slot + dep gates + harness ([#149](https://github.com/by-openclaw/go-acp/issues/149)) ([7e474ba](https://github.com/by-openclaw/go-acp/commit/7e474ba66c32362daad7595b4b7588bcf84ef380))
* **probel-sw02p:** Validate() + Canonicalize() + testdata/protocol_types/ (closes [#222](https://github.com/by-openclaw/go-acp/issues/222)) ([#223](https://github.com/by-openclaw/go-acp/issues/223)) ([e5062b7](https://github.com/by-openclaw/go-acp/commit/e5062b7ca68cc8025ecf3996c3beeb9041b7bc2f))
* **probel-sw08p:** Validate() + Canonicalize() + testdata/protocol_types/ (closes [#220](https://github.com/by-openclaw/go-acp/issues/220)) ([#221](https://github.com/by-openclaw/go-acp/issues/221)) ([e4915ee](https://github.com/by-openclaw/go-acp/commit/e4915eeab58d90a3c60b0d71fea2ceddd468dd80))
* **probel-sw08p:** wire names-family extended-form (rx 228/229/230/231 + tx 234/235) + boundary tests (advances [#227](https://github.com/by-openclaw/go-acp/issues/227)) ([#229](https://github.com/by-openclaw/go-acp/issues/229)) ([f6941b7](https://github.com/by-openclaw/go-acp/commit/f6941b77ae2a07e4683bc4d6336cbb1d233cf897))
* **probel-sw08p:** wire salvo extended-form (rx 248 / tx 250 / rx 252 / tx 253) + boundary tests (advances [#227](https://github.com/by-openclaw/go-acp/issues/227)) ([#228](https://github.com/by-openclaw/go-acp/issues/228)) ([8ac312b](https://github.com/by-openclaw/go-acp/commit/8ac312bb0c908cb05ef5cd4db9438c2fcaeaee72))
* **tsl:** compliance_events + Validate() + Canonicalize() + testdata/protocol_types/ (closes [#224](https://github.com/by-openclaw/go-acp/issues/224), [#212](https://github.com/by-openclaw/go-acp/issues/212)) ([#225](https://github.com/by-openclaw/go-acp/issues/225)) ([66b6aa2](https://github.com/by-openclaw/go-acp/commit/66b6aa235f9f2ea89542c5c983e904208575779f))
* **validate:** canonical validate verb + Trame wire-trace contract (closes [#212](https://github.com/by-openclaw/go-acp/issues/212) partially) ([#213](https://github.com/by-openclaw/go-acp/issues/213)) ([a3e42da](https://github.com/by-openclaw/go-acp/commit/a3e42da4bb84c1e1c411372c3991073a821f56af))


### Bug Fixes

* **probel-sw02p:** TestEmitExtendedProtectTallyDumpFanout port-collision flake on rhel9 / rocky9 ([#233](https://github.com/by-openclaw/go-acp/issues/233)) ([cbf2c05](https://github.com/by-openclaw/go-acp/commit/cbf2c0574d63044ae829aa2f8a8da54419bb177c))
* **probel-sw08p:** tally-dump byte→word→ext-word promotion ladder + boundary tests (advances [#227](https://github.com/by-openclaw/go-acp/issues/227)) ([#230](https://github.com/by-openclaw/go-acp/issues/230)) ([260f4f4](https://github.com/by-openclaw/go-acp/commit/260f4f4929b11e85153bd335ed061a2859d94649))
* **probel-sw08p:** wire keepalive auto-responder before reader goroutine starts (closes [#234](https://github.com/by-openclaw/go-acp/issues/234)) ([#235](https://github.com/by-openclaw/go-acp/issues/235)) ([8dfd860](https://github.com/by-openclaw/go-acp/commit/8dfd8600c1ca74b7054d19c759665cf8c38ecab5))

## [0.6.0](https://github.com/by-openclaw/go-acp/compare/v0.5.0...v0.6.0) (2026-04-30)


### Features

* **cerebrum-nb,cli:** device-details verb (OBTAIN DEVICE_CHANGE TYPE=DETAILS) ([e2dbab3](https://github.com/by-openclaw/go-acp/commit/e2dbab38f33499cd3d013dba6ec00c75c541a8e7))
* **cerebrum-nb,cli:** one-OBTAIN-per-verb collection set (seq pcap workflow) ([21f2f0f](https://github.com/by-openclaw/go-acp/commit/21f2f0fbb50f2a42ff6b321ff3d2e037d4864bb5))
* **cerebrum-nb/codec:** decode RX child elements for routing_change ([098d6d5](https://github.com/by-openclaw/go-acp/commit/098d6d5f5675529b665057ea7243260f8684ee2d))
* **cerebrum-nb:** consumer plugin (XML over WebSocket:40007) + portable Windows binary ([36fa45f](https://github.com/by-openclaw/go-acp/commit/36fa45f4fa2c7467cfe475c307bfb433c8773879))
* **cerebrum-nb:** decode 4 more live wire shapes (VALUE / CATEGORY_DETAILS / INSTANCE_LIST / INSTANCE_DETAILS) ([3490dda](https://github.com/by-openclaw/go-acp/commit/3490dda72a2ce6d000a5de5933d394ce302a8bcc))
* **cerebrum-nb:** decode CATEGORY_DETAILS &lt;items&gt; positional grid ([667052c](https://github.com/by-openclaw/go-acp/commit/667052cb33b6a31a5f7e828ae31f796c76cb249f))
* **cerebrum-nb:** decode DEVICE_CHANGE TYPE=DETAILS sub-tree ([b08fa50](https://github.com/by-openclaw/go-acp/commit/b08fa50261471a3e5aa17041643fa7b10b63f0b0))
* **cerebrum-nb:** route action verb; resolve ROUTE source in listen; drop matrix-dm ([c0a4b38](https://github.com/by-openclaw/go-acp/commit/c0a4b38af29a30c7d764b0583b1d5bc5111da34a))


### Bug Fixes

* **cerebrum-nb,cli:** clarify list-devices empty-result message ([4615b4f](https://github.com/by-openclaw/go-acp/commit/4615b4f7f6deec62682248e5236a4802c90ebfa9))
* **cerebrum-nb,cli:** list-devices --device-type filter + drop DEVICE_NAME column ([6866463](https://github.com/by-openclaw/go-acp/commit/6866463cc6c889a5c1493b6cece03b3ccc82941c)), closes [#144](https://github.com/by-openclaw/go-acp/issues/144)
* **cerebrum-nb,cli:** print full api_ver, not (unknown) for v0.x ([11aabf3](https://github.com/by-openclaw/go-acp/commit/11aabf35ed8361290e5807f0d87e775a8ece83c0))
* **cerebrum-nb:** list-routers shows route-master + wire ROUTER-class entries ([fd7d939](https://github.com/by-openclaw/go-acp/commit/fd7d9396d499a08f96b2fe106ea08eeefebc6484))
* **cerebrum-nb:** live wire shapes for DEVICE/CATEGORY/SALVO list + split listen subscribes ([4531c60](https://github.com/by-openclaw/go-acp/commit/4531c60856dfe25b526e943ed09bd1472643a88a))
* **cerebrum-nb:** parse all &lt;INSTANCE&gt; children per &lt;DEVICE&gt; (was dropping ROUTER/SNMP classes) ([644c140](https://github.com/by-openclaw/go-acp/commit/644c1407df0edb92df42f5b8a300eefa1ea77c02))
* **cerebrum-nb:** TCP keep-alive persistence + conditional LOGIN + dispatch fix ([1754cac](https://github.com/by-openclaw/go-acp/commit/1754cac0f6321a5dcc6096fa715a27c3d31102fa)), closes [#144](https://github.com/by-openclaw/go-acp/issues/144)

## [0.5.0](https://github.com/by-openclaw/go-acp/compare/v0.4.0...v0.5.0) (2026-04-26)


### Features

* **cli:** list-commands + help-cmd across all 5 protocols ([#133](https://github.com/by-openclaw/go-acp/issues/133)) ([932e321](https://github.com/by-openclaw/go-acp/commit/932e3218732dcd2b855e95e2887b3b4d235f2811))
* **probel-sw02p:** implement salvo commands §3.2.7/8/14/15/36-39/53/54 ([#106](https://github.com/by-openclaw/go-acp/issues/106)) ([cadebe6](https://github.com/by-openclaw/go-acp/commit/cadebe61b8ad13ef6259451573ce8c60d17247f5))
* **probel,osc:** consumer matrix-config flags + bootstrap rx 01 sweep + keep-alive ping + TCP SO_KEEPALIVE ([#132](https://github.com/by-openclaw/go-acp/issues/132)) ([a7f0e9e](https://github.com/by-openclaw/go-acp/commit/a7f0e9e673fe325aa2242f4aea2ecafbb7c496f0))
* **tsl:** TSL UMD v3.1/v4.0/v5.0 plugin + Wireshark dissector + CLI ([#134](https://github.com/by-openclaw/go-acp/issues/134)) ([7a2ea2b](https://github.com/by-openclaw/go-acp/commit/7a2ea2b995ede3773d97ea85cb48be10d5c0e7e2))


### Bug Fixes

* **emberplus:** BER REAL ecosystem mantissa bias + S101 BoF resync ([a057f9a](https://github.com/by-openclaw/go-acp/commit/a057f9a4065d78efced6ce9db0dc0b688530b3eb))
* **emberplus:** BER REAL ecosystem mantissa bias + S101 BoF resync ([#68](https://github.com/by-openclaw/go-acp/issues/68)) ([a244e17](https://github.com/by-openclaw/go-acp/commit/a244e170d0184da69cd5aa0b554e93feb93974f7))
* **emberplus:** broadcast value-change announcements to all active sessions ([f2edbff](https://github.com/by-openclaw/go-acp/commit/f2edbfff96426b19392ecb215a2de27e3c6f1f0a))
* **emberplus:** broadcast value-change announcements to all active sessions ([b679ecb](https://github.com/by-openclaw/go-acp/commit/b679ecb704fb22d6912d4a6d1b694db87fb80447))

## [0.4.0](https://github.com/by-openclaw/go-acp/compare/v0.3.1...v0.4.0) (2026-04-25)


### Features

* **acp2:** close remaining 5 per-type fixture gaps (full spec coverage, [#64](https://github.com/by-openclaw/go-acp/issues/64)) ([77a8a1c](https://github.com/by-openclaw/go-acp/commit/77a8a1c1e891ccb7b6e8025f4f668d352dc2d5fe))
* **fixtures/acp2:** per-type capture + frozen tree library ([#64](https://github.com/by-openclaw/go-acp/issues/64)) ([daa18a4](https://github.com/by-openclaw/go-acp/commit/daa18a452ae05707ec95ce76eed613b8bc77e729))
* **fixtures/acp2:** per-type capture + frozen tree library (partial [#64](https://github.com/by-openclaw/go-acp/issues/64)) ([90a58bb](https://github.com/by-openclaw/go-acp/commit/90a58bb5a2034f004e939555631b088c9789abe9))
* **osc/cli:** full OSC consumer + producer verbs with help ([4954016](https://github.com/by-openclaw/go-acp/commit/49540166c26a547c205d1757049a41980c680624))
* **osc/wireshark:** full from-scratch dissector — UDP + TCP-LP + TCP-SLIP ([c5f2e35](https://github.com/by-openclaw/go-acp/commit/c5f2e35f2a196acf4e89d9716482c6f1d24a50aa))
* **osc:** full OSC 1.0 + 1.1 plugin — codec + UDP/TCP-LP/TCP-SLIP + dissector + CLI ([de45286](https://github.com/by-openclaw/go-acp/commit/de4528603ff4259a8d0c1927375810e200f66f97))
* **osc:** full OSC 1.0 address-pattern matcher + array constructors + per-tag tests ([58b1e56](https://github.com/by-openclaw/go-acp/commit/58b1e56efb57f50e160eced93c7255631486eb4c))
* **osc:** richer Wireshark SLIP dissector — address + type-tag + arg count in Info column ([64536b2](https://github.com/by-openclaw/go-acp/commit/64536b28fe3dcc6fd672fbe97e2ef550d2724eb5))
* **osc:** scaffold plugin — consumer + provider stubs + CLAUDE.md ([7f13e34](https://github.com/by-openclaw/go-acp/commit/7f13e34240091c764827578d2e0a4b37baf80ff1))
* **osc:** show typed arg values in Wireshark Info col + CLI watch output ([25cbac7](https://github.com/by-openclaw/go-acp/commit/25cbac787dba61f62cb6f260aef003a2c2f25286))
* **osc:** TCP — length-prefix (v1.0) + SLIP double-END (v1.1) ([bdf1ff2](https://github.com/by-openclaw/go-acp/commit/bdf1ff2bddd7bc2b7e8c8043bef3c5f9512327a2))
* **osc:** v1.0 codec + UDP consumer + provider + integration ([2785e5b](https://github.com/by-openclaw/go-acp/commit/2785e5ba7eb45ec84d8a37c0a48b90205c814ee2))
* **osc:** Wireshark — SLIP unstuffer that delegates to built-in OSC dissector ([9d78124](https://github.com/by-openclaw/go-acp/commit/9d78124c9115e4f1ac098a7c8d5cc453dddd723d))


### Bug Fixes

* **osc/wireshark:** correct string/blob alignment check off-by-one ([30537ca](https://github.com/by-openclaw/go-acp/commit/30537ca87a60c16d4e50786975c9849e14a8d9f8))
* **osc/wireshark:** use add_proto_expert_info for ProtoExpert objects ([71b6349](https://github.com/by-openclaw/go-acp/commit/71b6349cdad24d4b612d9cb6f7314ea9175ce224))
* **probel-sw08p/provider:** emit tx 04 Connected on salvo Set ([#92](https://github.com/by-openclaw/go-acp/issues/92)) ([077c265](https://github.com/by-openclaw/go-acp/commit/077c2651ceb780b97a78b6d47e71cce154e37b3c))
* **probel-sw08p/provider:** emit tx 04 Connected on salvo Set ([#92](https://github.com/by-openclaw/go-acp/issues/92)) ([e4f6579](https://github.com/by-openclaw/go-acp/commit/e4f657941cc8b7775184282d952f0a86667e04ec))

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
