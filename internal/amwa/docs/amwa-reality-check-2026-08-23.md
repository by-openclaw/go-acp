# AMWA reality check — 2026-08-23

Verified directly against `AMWA-TV/nmos-testing` (suite inventory +
docs index) on 2026-08-23, owner-prompted ("bcp008 is supported and
mxl too"). This is the delta between AMWA's CURRENT published state
and this repo's version tables (CLAUDE.md / reference.md) — the
strict-every-published-version rule means every row here is WORK, not
commentary. Fold into reference.md when epic #146 planning resumes.

## 1. Test automation — the certification instrument is headless

`nmos-testing` officially supports unattended runs:

| Doc | Capability |
|---|---|
| 2.5 Usage — Non-Interactive Mode | CLI suite runs, JSON/JUnit output — no UI |
| 2.6 Usage — Using the API | the tool exposes its own API to launch/poll suites |
| 2.9 Fully Automated Testing of Controllers | automates even the controller-side question flow |
| 2.7 Testing IS-07 MQTT · 2.2 BCP-003-01 TLS · 2.3 IS-10 Auth | dedicated modes our harness must wire |

Owner requirement (recorded on #173): certification runs fully
automated; only genuinely-manual tests (IS-04-01 test_22 reboot
persistence, #198) stay human.

## 2. Suite inventory vs our tables (delta only)

Suites present in nmos-testing that our version tables do NOT yet
track as implemented targets:

| Suite file(s) | Spec | Status in our tables |
|---|---|---|
| BCP0080101Test, BCP0080201Test, BCP008Test | BCP-008-01/-02 receiver/sender status | listed as targets (#171) — suites CONFIRMED available |
| BCP00301Test | BCP-003-01 secure transport (TLS) | not in our table — published, must be added |
| BCP0050101Test | BCP-005-01 (EDID / IS-11 companion) | not in our table |
| BCP0060101/02Test, BCP00604Test | BCP-006-01 (two parts), -04 | partially tracked (#170) |
| BCP0070301/02Test | BCP-007-03 | not in our table (BCP-007-01 NDI was "WIP" — the -03 track has published suites) |
| IS0601Test | IS-06 network control | not in our table at all |
| IS1001Test (+ 2.3 auth mode) | IS-10 authorization | our CLAUDE.md says "Auth is IS-10, out of scope for v1" — AMWA ships a suite; certification scope decision needed |
| IS1101Test | IS-11 stream compatibility | our tables list nothing — published |
| IS1401Test | IS-14 device configuration | not in our table — published |

Per the repo rule ("no minor AMWA has published is ever deferred"),
IS-06 / IS-11 / IS-14 / BCP-003-01 / BCP-005-01 / BCP-007-03 move
from absent to MISSING-IMPLEMENTATION status; reference.md version
table needs the corresponding rows with exact minors (pull from each
spec repo when epic #146 planning resumes).

## 3. MXL (Media eXchange Layer)

- Repo: `dmf-mxl/mxl` (Linux Foundation / EBU Dynamic Media Facility
  effort; ffmpeg integration guidance already public via cbcrc).
- MXL is NOT an NMOS spec and has no nmos-testing suite; it is the
  in-memory/ST 2110-adjacent media exchange layer that NMOS-controlled
  facilities are converging toward. Relevance to dhs: objective-B
  plants will meet MXL-based media functions whose CONTROL plane is
  still NMOS — track as ecosystem awareness, not as a connector
  target today. Revisit when AMWA/EBU publish a control-plane
  binding.

## 4. Actions this doc implies (gated, epic #146)

1. reference.md version-table refresh with the specs above.
2. #173 harness: non-interactive + API mode, JSON/JUnit artifacts.
3. Scope decision (owner): IS-10 auth + IS-06 + IS-11 + IS-14 in the
   certification target set — "full certified" per the owner's
   2026-08-23 statement implies yes for anything with a published
   suite.

## Addendum 2026-09-01

The historical findings above are kept as written; the deltas have
since been closed in code:

- **IS-10 / BCP-003-02** authorization: implemented (`codec/is10`,
  `session/auth`; `--auth-url` on node + registry). CLAUDE.md's
  "out of scope for v1" wording is gone — its version table now
  tracks it as shipped.
- **IS-11, IS-14, BCP-003-01, BCP-003-03, BCP-005-01, BCP-007-03
  MXL**: implemented (`codec/is11` + `provider/streamcompat.go`,
  `codec/is14` + `provider/configuration.go`, TLS serving,
  `codec/est` + `session/certmgr`, `codec/edid`,
  `urn:x-nmos:transport:mxl` in the IS-04 + IS-05 codecs).
- **BCP-008-01 / BCP-008-02**: ship (ms05 monitor classes over
  IS-12).
- **IS-06** remains the only genuinely absent spec.
