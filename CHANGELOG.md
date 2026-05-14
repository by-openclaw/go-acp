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

## [0.11.0](https://github.com/by-openclaw/go-acp/compare/v0.10.0...v0.11.0) (2026-05-14)


### Features

* **acp1:** per-object meta fetch + ValueValidator (refs [#421](https://github.com/by-openclaw/go-acp/issues/421) [#422](https://github.com/by-openclaw/go-acp/issues/422)) ([#426](https://github.com/by-openclaw/go-acp/issues/426)) ([baf9d19](https://github.com/by-openclaw/go-acp/commit/baf9d19a6d8fa18b35be4942df5ff121def29015))
* manifest reader + producer DM consumer (acp1 + acp2) (refs [#432](https://github.com/by-openclaw/go-acp/issues/432) [#433](https://github.com/by-openclaw/go-acp/issues/433)) ([#434](https://github.com/by-openclaw/go-acp/issues/434)) ([a4af138](https://github.com/by-openclaw/go-acp/commit/a4af13881c5f12b2c44eeecb2ea8873f0fdceec5))


### Bug Fixes

* **cli/import:** drop ACP1 walk gate (refs [#423](https://github.com/by-openclaw/go-acp/issues/423)) ([#427](https://github.com/by-openclaw/go-acp/issues/427)) ([0eb2557](https://github.com/by-openclaw/go-acp/commit/0eb25571121f0faa36073f265a0523b70c00d6fa))
* **storage:** emit slot-agnostic DM (refs [#430](https://github.com/by-openclaw/go-acp/issues/430)) ([#431](https://github.com/by-openclaw/go-acp/issues/431)) ([e20c369](https://github.com/by-openclaw/go-acp/commit/e20c36923524e2a8ab110914c8c82af67634ffce))

## [0.10.0](https://github.com/by-openclaw/go-acp/compare/v0.9.0...v0.10.0) (2026-05-12)


### Features

* **cli:** ascii-tree rendering for walk verb ([#409](https://github.com/by-openclaw/go-acp/issues/409)) ([#411](https://github.com/by-openclaw/go-acp/issues/411)) ([e8af673](https://github.com/by-openclaw/go-acp/commit/e8af6732b1a7f1764a725c90ac53539b182169b8))


### Bug Fixes

* **cli/import:** add ValueValidator + catch id/enum/type mismatches before send (refs [#417](https://github.com/by-openclaw/go-acp/issues/417)) ([#418](https://github.com/by-openclaw/go-acp/issues/418)) ([48d37d5](https://github.com/by-openclaw/go-acp/commit/48d37d53aedb54c26f2777f6c7196cd22cbe982d))
* **cli/import:** dry-run skips full-slot walk (closes [#413](https://github.com/by-openclaw/go-acp/issues/413)) ([#414](https://github.com/by-openclaw/go-acp/issues/414)) ([35d2277](https://github.com/by-openclaw/go-acp/commit/35d22775c3ce79a3b377b3b35c8a1fab8e066cec))
* **cli/import:** skip walk on apply for ACP2/EmberPlus (refs [#415](https://github.com/by-openclaw/go-acp/issues/415)) ([#416](https://github.com/by-openclaw/go-acp/issues/416)) ([32041cd](https://github.com/by-openclaw/go-acp/commit/32041cd470a6f43421f97f4ba2bf89214eee7b32))
* **export/csv:** unify path separator to '.' (refs [#419](https://github.com/by-openclaw/go-acp/issues/419)) ([#420](https://github.com/by-openclaw/go-acp/issues/420)) ([99ef16d](https://github.com/by-openclaw/go-acp/commit/99ef16dba92604249592338c49e459a7cadad1b0))

## [0.9.0](https://github.com/by-openclaw/go-acp/compare/v0.8.0...v0.9.0) (2026-05-10)


### Features

* **acp2,cli:** auto-reconnect on session loss + surface IsOnline (closes [#367](https://github.com/by-openclaw/go-acp/issues/367)) ([#371](https://github.com/by-openclaw/go-acp/issues/371)) ([1cdba73](https://github.com/by-openclaw/go-acp/commit/1cdba734ae9265fe589be331d01ff900c7b112d1))
* **acp2:** service-layer keep-alive + populate SlotInfo.IsOnline (closes [#365](https://github.com/by-openclaw/go-acp/issues/365)) ([#366](https://github.com/by-openclaw/go-acp/issues/366)) ([55f7f81](https://github.com/by-openclaw/go-acp/commit/55f7f8183a19bf01a82f66fcf846d95218b7cbe2))

## [0.8.0](https://github.com/by-openclaw/go-acp/compare/v0.7.0...v0.8.0) (2026-05-09)


### Features

* **acp1-consumer:** --transport auto, TCP-first with UDP fallback ([6bdf914](https://github.com/by-openclaw/go-acp/commit/6bdf914e6421179ecd2c531583ca8249bee4e500))
* **acp1-consumer:** cross-protocol keep-alive + connection-aware freshness (advances [#298](https://github.com/by-openclaw/go-acp/issues/298)) ([58ce33d](https://github.com/by-openclaw/go-acp/commit/58ce33dfc788ec37b310203bc47f24c57657c718))
* **acp1-provider:** --preload + --play, slot-0 lift, controller-interop iteration ([ee6f946](https://github.com/by-openclaw/go-acp/commit/ee6f94625b16678699198bcac1e52e2aba3c199f))
* **acp1-provider:** slot.load/unload swap served tree (closes [#289](https://github.com/by-openclaw/go-acp/issues/289)) ([06410e7](https://github.com/by-openclaw/go-acp/commit/06410e75281c29db2a2cb367922985a500f94fc7))
* **acp1:** cross-protocol keep-alive + connection-aware freshness (advances [#298](https://github.com/by-openclaw/go-acp/issues/298)) ([a0bb725](https://github.com/by-openclaw/go-acp/commit/a0bb7254ccd679e898b817d25c3576ce807b3d28))
* **acp1:** full Phase-1 epic — provider + consumer + 3-LXC live rig ([ec77c9a](https://github.com/by-openclaw/go-acp/commit/ec77c9add6a1af1ad2711403d5c5c714010b1b17))
* **acp1:** Plugin.GetIdentity (advances [#251](https://github.com/by-openclaw/go-acp/issues/251)) ([c4cc68a](https://github.com/by-openclaw/go-acp/commit/c4cc68ae93d54fbb2344aeebbc76b21bef0696cb))
* **acp1:** Plugin.SeedFromDM (advances [#252](https://github.com/by-openclaw/go-acp/issues/252)) ([38f684c](https://github.com/by-openclaw/go-acp/commit/38f684c9152e23fed552610c12f0f733148a9a29))
* **acp1:** Plugin.SessionHealth implementation (advances [#266](https://github.com/by-openclaw/go-acp/issues/266)) ([cacdc3c](https://github.com/by-openclaw/go-acp/commit/cacdc3c7a69077e189dc8099b0eb85b0398b8122))
* **acp1:** provider admin CLI surface (advances [#258](https://github.com/by-openclaw/go-acp/issues/258)) ([9fc9f97](https://github.com/by-openclaw/go-acp/commit/9fc9f97e8c114592d72d22b230f5ba8854ad2530))
* **acp1:** provider AN2/TCP (Mode C) listener (advances [#256](https://github.com/by-openclaw/go-acp/issues/256)) ([21fdf87](https://github.com/by-openclaw/go-acp/commit/21fdf875170a686126dcc1467a5b3b90a21693f6))
* **acp1:** provider Broadcasts gate (advances [#257](https://github.com/by-openclaw/go-acp/issues/257)) ([863d23c](https://github.com/by-openclaw/go-acp/commit/863d23c8b20f579bb1284c56f8edb1538d492297))
* **acp1:** provider DM-library loader (advances [#260](https://github.com/by-openclaw/go-acp/issues/260)) ([9012811](https://github.com/by-openclaw/go-acp/commit/90128119a2cb15aecbfc06646c359b661fad782b))
* **acp1:** provider fuzz verb (advances [#262](https://github.com/by-openclaw/go-acp/issues/262)) ([aed7dd1](https://github.com/by-openclaw/go-acp/commit/aed7dd10e1fe0b6d75f84f7a07654f384d6ac91e))
* **acp1:** provider slot state machine + realistic timings (advances [#259](https://github.com/by-openclaw/go-acp/issues/259)) ([f3e8c2e](https://github.com/by-openclaw/go-acp/commit/f3e8c2efe007e3246644860479b88a8d18aef91b))
* **acp1:** provider TCP direct (Mode B) listener (advances [#255](https://github.com/by-openclaw/go-acp/issues/255)) ([40bb35c](https://github.com/by-openclaw/go-acp/commit/40bb35cf3cc13b0e5e62ff9ecbe153a4a7dad111))
* **acp1:** provider VIP-aware broadcast source (advances [#263](https://github.com/by-openclaw/go-acp/issues/263)) ([16f7335](https://github.com/by-openclaw/go-acp/commit/16f7335155dd37763f06951e6cf0659d86d0708e))
* **acp1:** validate --out-tree wired (advances [#264](https://github.com/by-openclaw/go-acp/issues/264)) ([29dc12e](https://github.com/by-openclaw/go-acp/commit/29dc12eb416f37af8410088b708dd9a95081b8b2))
* **acp2-consumer:** identity-keyed DM cache + hot-load on watch start (closes [#353](https://github.com/by-openclaw/go-acp/issues/353)) ([1602496](https://github.com/by-openclaw/go-acp/commit/16024965f939ab2855007998e40f88505c2dc105))
* **acp2-provider:** filter disabled objects per spec section Requirements ([#341](https://github.com/by-openclaw/go-acp/issues/341)) ([6cb2202](https://github.com/by-openclaw/go-acp/commit/6cb220210c119bdfe877c9c13f6b35ea5c6358de)), closes [#319](https://github.com/by-openclaw/go-acp/issues/319)
* **acp2-provider:** sanitize labels to spec charset per spec section Versions ([cd7af27](https://github.com/by-openclaw/go-acp/commit/cd7af27e871958d6e9104db669dae7592b8de985)), closes [#318](https://github.com/by-openclaw/go-acp/issues/318)
* **acp2:** identity-keyed DM is the only cache (closes [#355](https://github.com/by-openclaw/go-acp/issues/355)) ([7325a92](https://github.com/by-openclaw/go-acp/commit/7325a92896e9f765f0cd73f3a29277d771a53fa6))
* **acp2:** strict-spec close-out — full §5.1 + transport conformance + DM cache + 25 sub-PRs ([f2ca37e](https://github.com/by-openclaw/go-acp/commit/f2ca37ebbae95d1626a00e3d5d06e0e358719ca1))
* **cli:** acp1 watch emits per-slot frame-status delta (closes [#239](https://github.com/by-openclaw/go-acp/issues/239)) ([21ca0b4](https://github.com/by-openclaw/go-acp/commit/21ca0b42b499dae98999a9f5a08ac2d2fd026ac8))
* **cli:** acp1 watch shows per-slot frame-status delta, not full digest (closes [#239](https://github.com/by-openclaw/go-acp/issues/239)) ([c807937](https://github.com/by-openclaw/go-acp/commit/c807937b6d75831109fee57d0134622d6008c373))
* **cli:** consumer health verb (advances [#250](https://github.com/by-openclaw/go-acp/issues/250)) ([8c6b20e](https://github.com/by-openclaw/go-acp/commit/8c6b20e4706dfb67a58e847f5c37b7adb8154953))
* **cli:** hot-plug enrichment in watch (advances [#254](https://github.com/by-openclaw/go-acp/issues/254)) ([cdb133b](https://github.com/by-openclaw/go-acp/commit/cdb133b903069f02cf110d0f3b99c466142ad13a))
* **cli:** per-slot fingerprint cache + card-swap / fw diagnostic rows (advances [#254](https://github.com/by-openclaw/go-acp/issues/254)) ([fecefc1](https://github.com/by-openclaw/go-acp/commit/fecefc1055e14f37d0f5581322accd64079fa634))
* **cli:** watch discovery-scope flags (advances [#253](https://github.com/by-openclaw/go-acp/issues/253)) ([90c4ba6](https://github.com/by-openclaw/go-acp/commit/90c4ba6f1fd34197dbcc4362ed7c14d2d0a107c6))
* **cli:** widen hot-plug enrichment trigger (advances [#254](https://github.com/by-openclaw/go-acp/issues/254)) ([8b3caa2](https://github.com/by-openclaw/go-acp/commit/8b3caa230ea8b71190d43650fe4f1a1d47c7d382))
* **dm-cache:** per-card DM, one file per CardName@HwVer — ACP1 + ACP2 (closes [#363](https://github.com/by-openclaw/go-acp/issues/363)) ([#364](https://github.com/by-openclaw/go-acp/issues/364)) ([9720c08](https://github.com/by-openclaw/go-acp/commit/9720c08f24d257191c557f1e19ce2e4ddfc80025))
* **dmlib:** product.yaml metadata + identity_probe (advances [#246](https://github.com/by-openclaw/go-acp/issues/246)) ([97fe720](https://github.com/by-openclaw/go-acp/commit/97fe720992d2932f54c3f88c0eea3429293bdfeb))
* **dmlib:** runtime resolver for DM library (advances [#245](https://github.com/by-openclaw/go-acp/issues/245)) ([57f4bcf](https://github.com/by-openclaw/go-acp/commit/57f4bcfb60c345c8d57a6827fe7329656c0b56c3))
* **identity:** sanitiser helpers for path/yaml/json sinks (advances [#247](https://github.com/by-openclaw/go-acp/issues/247)) ([8e5bcd1](https://github.com/by-openclaw/go-acp/commit/8e5bcd12beb4bebac4d3b1a8193ba332f8505ff3))
* **protocol:** SessionHealth struct + HealthChecker interface (advances [#248](https://github.com/by-openclaw/go-acp/issues/248)) ([45a84a5](https://github.com/by-openclaw/go-acp/commit/45a84a598505e4981cbaad4c73fae53402641eba))
* **protocol:** SlotInfo extended with State enum + LiveAt + IsOnline (advances [#249](https://github.com/by-openclaw/go-acp/issues/249)) ([224587b](https://github.com/by-openclaw/go-acp/commit/224587b2dd269ab65d4bcce5be3a32a317672a43))
* **watch:** surface property unit on announces (closes [#359](https://github.com/by-openclaw/go-acp/issues/359)) ([#360](https://github.com/by-openclaw/go-acp/issues/360)) ([57a14e2](https://github.com/by-openclaw/go-acp/commit/57a14e27350f2b7fd47f8bc65a8c60e76403b152))


### Bug Fixes

* **acp1-provider:** enable SO_REUSEADDR on UDP listener (closes [#291](https://github.com/by-openclaw/go-acp/issues/291)) ([d708eb4](https://github.com/by-openclaw/go-acp/commit/d708eb48cd8681add2f0effed31444106e659b7d))
* **acp1-provider:** TCP framing + announce semantics + schema bounds + step snap ([9f499f8](https://github.com/by-openclaw/go-acp/commit/9f499f8c268269b77adf525a32ecc820288be546))
* **acp1-provider:** two more races caught by CI -race ([b10069d](https://github.com/by-openclaw/go-acp/commit/b10069d0d1dff2c706fcb018d5d63b053900aece))
* **acp1:** validate --out-tree round-trip now byte-equal to live export ([3150522](https://github.com/by-openclaw/go-acp/commit/3150522177e7ca9fecd127daf9e1ac08ebf022fa))
* **acp2-cache:** SaveByIdentity drops Object.Meta -- decode as raw(N) (closes [#361](https://github.com/by-openclaw/go-acp/issues/361)) ([#362](https://github.com/by-openclaw/go-acp/issues/362)) ([21e5005](https://github.com/by-openclaw/go-acp/commit/21e50052f5162bd7dd6a442d9e82af79fa06e7ba))
* **acp2-cli:** set --path resolves via cache, never full-walks slot ([#335](https://github.com/by-openclaw/go-acp/issues/335)) ([d9511f7](https://github.com/by-openclaw/go-acp/commit/d9511f7ed02fa0d348f2f9b565d92bff2dbe0470)), closes [#322](https://github.com/by-openclaw/go-acp/issues/322)
* **acp2-cli:** watch persists tree types so announces decode immediately ([#339](https://github.com/by-openclaw/go-acp/issues/339)) ([3a1ae5f](https://github.com/by-openclaw/go-acp/commit/3a1ae5f66c93d6c3dd8e51cf9753cef90305be68)), closes [#323](https://github.com/by-openclaw/go-acp/issues/323)
* **acp2-codec:** error decoder MUST NOT read body per spec section Error ([#333](https://github.com/by-openclaw/go-acp/issues/333)) ([f967be9](https://github.com/by-openclaw/go-acp/commit/f967be9b3243719ae8901154409037d2b582941f)), closes [#316](https://github.com/by-openclaw/go-acp/issues/316)
* **acp2-codec:** get_property request body = obj-id + idx only per spec ([#331](https://github.com/by-openclaw/go-acp/issues/331)) ([4ac6c07](https://github.com/by-openclaw/go-acp/commit/4ac6c07be5a11e73f703c863d3c2df35166dbf42)), closes [#314](https://github.com/by-openclaw/go-acp/issues/314)
* **acp2-consumer:** announce fallback decodes Enum/IPv4/String typed (closes [#351](https://github.com/by-openclaw/go-acp/issues/351)) ([#352](https://github.com/by-openclaw/go-acp/issues/352)) ([1afe903](https://github.com/by-openclaw/go-acp/commit/1afe9038d4ea1ff7890975e14465c18da61b4511))
* **acp2-consumer:** mtid pool MUST never reuse in-flight + ctx-cancellable ([#337](https://github.com/by-openclaw/go-acp/issues/337)) ([4730d25](https://github.com/by-openclaw/go-acp/commit/4730d2555f7096c0120b44a4e507a619a35d0c1d)), closes [#321](https://github.com/by-openclaw/go-acp/issues/321)
* **acp2-consumer:** preserve AN2 major+minor version, log as "1.0" (closes [#344](https://github.com/by-openclaw/go-acp/issues/344)) ([#345](https://github.com/by-openclaw/go-acp/issues/345)) ([2333400](https://github.com/by-openclaw/go-acp/commit/2333400ee4ff74b8cdea0e2bd461773d821db5ba))
* **acp2-consumer:** walker decodes pid=4/7/18/20 (currently silently dropped) ([#332](https://github.com/by-openclaw/go-acp/issues/332)) ([923a1d8](https://github.com/by-openclaw/go-acp/commit/923a1d8c017219b1ef66c2a24c7ec98efa1b22ea)), closes [#315](https://github.com/by-openclaw/go-acp/issues/315)
* **acp2-provider:** always emit pid=9/10/11 on Number/Enum/Preset ([#327](https://github.com/by-openclaw/go-acp/issues/327)) ([b2365b1](https://github.com/by-openclaw/go-acp/commit/b2365b173c8e8289baefaa1f3ee45b0624ea3ec0)), closes [#310](https://github.com/by-openclaw/go-acp/issues/310)
* **acp2-provider:** AN2 transport replies match spec §3.3 + real Neuron wire ([#303](https://github.com/by-openclaw/go-acp/issues/303)) ([0d9cc96](https://github.com/by-openclaw/go-acp/commit/0d9cc9600ea5faa92f5631d9bbd1a8034c4b3073))
* **acp2-provider:** applySetEnum validates against EnumMap.Value, not positional length (closes [#346](https://github.com/by-openclaw/go-acp/issues/346)) ([#347](https://github.com/by-openclaw/go-acp/issues/347)) ([15812b6](https://github.com/by-openclaw/go-acp/commit/15812b68d26416c976d3cd6dce09c76b121ab901))
* **acp2-provider:** enforce single-request-at-a-time per spec line 313 ([#338](https://github.com/by-openclaw/go-acp/issues/338)) ([bf57cf7](https://github.com/by-openclaw/go-acp/commit/bf57cf7f124f6120edbacb433f32543af3d4c933)), closes [#320](https://github.com/by-openclaw/go-acp/issues/320)
* **acp2-provider:** error reply body MUST be empty per spec section Error ([#330](https://github.com/by-openclaw/go-acp/issues/330)) ([f3235ae](https://github.com/by-openclaw/go-acp/commit/f3235aec2f6f73c9eda9d62df6375e7f27067c21)), closes [#313](https://github.com/by-openclaw/go-acp/issues/313)
* **acp2-provider:** omit pid=3 access on Nodes per spec property matrix ([#325](https://github.com/by-openclaw/go-acp/issues/325)) ([97a1247](https://github.com/by-openclaw/go-acp/commit/97a12470834715086214f3a39f5759a9517cfa35)), closes [#306](https://github.com/by-openclaw/go-acp/issues/306)
* **acp2-provider:** pid=7 (preset_depth) idx values from canonical, not 0..N-1 ([#340](https://github.com/by-openclaw/go-acp/issues/340)) ([714c2ea](https://github.com/by-openclaw/go-acp/commit/714c2ea013aae8d61926f92741d3efa0a9145a64)), closes [#311](https://github.com/by-openclaw/go-acp/issues/311)
* **acp2:** full §5.1 pid emission compliance + protocol versions matching real-Neuron wire (closes [#349](https://github.com/by-openclaw/go-acp/issues/349)) ([18856d0](https://github.com/by-openclaw/go-acp/commit/18856d03cbdeaf059ce9b6dd87681b0acbd54ec5))
* **acp2:** pid=15 options variable-length per real-device convention (closes [#312](https://github.com/by-openclaw/go-acp/issues/312)) ([#343](https://github.com/by-openclaw/go-acp/issues/343)) ([d421031](https://github.com/by-openclaw/go-acp/commit/d421031446cfd46317232573a190a8be849c0aa1))
* **cli:** acp1 watch label-cache key collision when groups share IDs (closes [#236](https://github.com/by-openclaw/go-acp/issues/236)) ([#237](https://github.com/by-openclaw/go-acp/issues/237)) ([11c2d61](https://github.com/by-openclaw/go-acp/commit/11c2d61be5ff604e9401af2602a5212f1e5fb612))
* **cli:** hot-plug enricher race on output writer ([c1e0195](https://github.com/by-openclaw/go-acp/commit/c1e0195256c9d8297cd1284aa2175411bed64df7))
* **cli:** standardize ACP2 verb flag taxonomy (--object alias, --direction io) ([#336](https://github.com/by-openclaw/go-acp/issues/336)) ([c6e85cf](https://github.com/by-openclaw/go-acp/commit/c6e85cfa1416439592e3721f92704ed17b2854af)), closes [#324](https://github.com/by-openclaw/go-acp/issues/324)
* **dmlib:** Diff is valid only between same Model+Proto (advances [#245](https://github.com/by-openclaw/go-acp/issues/245)) ([a46afa7](https://github.com/by-openclaw/go-acp/commit/a46afa761b77355b460fd32fe543f18a6c51a6dc))
* **export:** deterministic ReadJSON readback (closes flake on TestValidateOutTree_WritesSnapshot) ([#357](https://github.com/by-openclaw/go-acp/issues/357)) ([ee0d589](https://github.com/by-openclaw/go-acp/commit/ee0d589b284d22f65852820715148a3a30daccc2))

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
