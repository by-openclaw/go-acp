# IS-04 — known caveats for this harness

Snapshot of the per-test posture for the AMWA NMOS Testing tool against
dhs running in this docker-compose. Update whenever the cause of a row
changes; the table is the source of truth for what to expect.

## IS-04-03 (Peer-to-Peer Node) — 2026-05-02 round 1

Peer-to-Peer mode harness setup: `dhs-registry` is **stopped** so the
Node enters Mode-D and advertises `_nmos-node._tcp` directly. The AMWA
tool browses for that service type and probes the Node API directly at
the discovered host:port.

### IS-04-03 round 1 results

| API ver | Pass | Fail | Warning | Disabled | Manual | N/A | Notes |
|---|---:|---:|---:|---:|---:|---:|---|
| **v1.0** | **16** | **0** | **0** | 7 | 1 | 3 | clean — `test_01` Pass; `test_02` Manual |
| **v1.1** | **16** | **0** | **0** | 7 | 1 | 3 | clean |
| **v1.2** | **16** | **0** | **0** | 7 | 1 | 3 | clean |
| **v1.3** | **17** | **0** | **0** | 7 | 1 | 3 | clean |

**65 Pass / 0 Fail / 0 Warning across all four AMWA-published IS-04 minors.**

`test_01` (Node advertises `_nmos-node._tcp` with `ver_*` TXT records
when no Registration API is reachable) passes on every minor. `test_02`
(counters increment on resource change) is always Manual — operator
confirms by mutating a Node resource and checking the counter bumped via
`avahi-browse -rpt _nmos-node._tcp`.

The 7 Disabled tests are `auto_node_17/18/19/20/21/22/23` (or one less
on v1.0/v1.1/v1.2): authorization probes that require `ENABLE_AUTH=true`
in the harness — out of scope for the unauthenticated profile we ship by
default.

### Closed gap

`_nmos-node._tcp` TXT records were missing the six IS-04 §3.1.1 `ver_*`
counter keys (`ver_slf`, `ver_dvc`, `ver_src`, `ver_flw`, `ver_snd`,
`ver_rcv`). The Mode-D peer probe + AMWA `test_01` both look at those
specifically. Fix:

- Six `atomic.Uint64` counters on `IS04NodeServer` (one per resource
  type), exposed via `BumpResourceVersion(rt)` for future IS-05
  call-sites to wire when staging activations promote into the live
  bundle.
- `Responder.Update(ctx, ins)` extension on the dnssd interface — the
  stdlib backend re-emits the announcement with the RFC 6762 §10.2
  cache-flush bit; the Avahi backend calls
  `EntryGroup.UpdateServiceTxt` via DBus. Both surface the new TXT to
  peers without tearing down the announcement.
- `buildNodeTXTLocked` rebuilds the full TXT map from current counter
  state at announce + republish time.

Result: every `_nmos-node._tcp` advertisement now carries the four
base TXT keys (`api_proto`, `api_ver`, `api_auth`, `pri`) + the six
`ver_*` counters, satisfying IS-04 §3.1.1 byte-by-byte.

### IS-04-03 reproduce one round

```
ssh root@10.100.0.105 'cd /root/amwa-test && DHS_API_VER=v1.3 docker compose up -d --build --force-recreate dhs'
sleep 8   # mDNS settle
ssh root@10.100.0.105 '/root/amwa-test/run-is0403.sh v1.3'
scp root@10.100.0.105:/root/amwa-test/results/is04-03-v1.3.json tests/integration/nmos/amwa/results/
```

To run all four minors back-to-back, repeat the loop with `DHS_API_VER`
set to v1.0 / v1.1 / v1.2 / v1.3. Each cycle takes ~15 s (8 s mDNS
settle + ~3 s harness run + container restart).

---

## IS-04-02 (Registry — Registration + Query API) — 2026-05-02 round 22

Single docker-compose serves both `dhs-node` (IS-04-01 NUT, port 18080)
and `dhs-registry` (IS-04-02 NUT, port 8235). Registry mounts every IS-04
codec registered with the binary in parallel under
`/x-nmos/{registration,query}/{v1.0,v1.1,v1.2,v1.3}/` and now
advertises the registration face on **both** service-type names so
v1.0/v1.1/v1.2 peers (which browse the legacy `_nmos-registration._tcp`)
discover us alongside v1.3+ peers (which browse the modern
`_nmos-register._tcp`).

### IS-04-02 round 22 results

| API ver | Pass | Fail | CNT | Disabled | NA | Manual | Notes |
|---|---:|---:|---:|---:|---:|---:|---|
| **v1.0** | **47** | **0** | 8 | 16 | 20 | 1 | clean — `test_01` Pass after legacy advertise |
| **v1.1** | **62** | **0** | 9 | 16 | 4 | 1 | clean |
| **v1.2** | **68** | **0** | 2 | 16 | 4 | 1 | clean — +7 Pass vs round 21 (legacy advertise un-blocked Mock-Node-driven cascade) |
| **v1.3** | **65** | **0** | 11 | 16 | 3 | 1 | clean (no regression) |

**242 Pass / 0 Fail / 0 Warning across all four AMWA-published IS-04 minors.**

The 3 round-21 Fails (`test_01` on v1.0/v1.1/v1.2) all closed in
round 22. v1.2 also jumped +7 Pass because the AMWA harness's internal
Mock Node uses the same legacy-name browse logic as `test_01`; once we
advertised on `_nmos-registration._tcp`, the Mock Node could register
against us on v1.2 rounds, which un-blocked the rest of the suite from
its CNT cascade.

### What `test_01` was actually checking — round-21 root cause

Earlier (round 21) I labelled the 3 `test_01` Fails as a
`python-zeroconf` cache flake bounded to the AMWA harness side. That
was wrong. Reading
[`IS0402Test.py`](https://raw.githubusercontent.com/AMWA-TV/nmos-testing/master/nmostesting/suites/IS0402Test.py)
proves it:

```python
def test_01(self, test):
    service_type = "_nmos-registration._tcp.local."
    if self.is04_reg_utils.compare_api_version(api["version"], "v1.3") >= 0:
        service_type = "_nmos-register._tcp.local."
    return self.do_dns_sd_advertisement_check(test, api, service_type)
```

The harness chooses the **legacy** service-type for v1.0/v1.1/v1.2 and
the **modern** service-type only for v1.3+. Our registry advertised
*only* on `_nmos-register._tcp`, so the v1.0/v1.1/v1.2 browse for
`_nmos-registration._tcp` returned nothing — "No matching mDNS
announcement found". Spec-strict bug on our side, not a harness flake.

The previous "harness cache" hypothesis fit the v1.3-passes /
others-fail pattern coincidentally because v1.3 is the only minor that
uses the modern service-type. Lesson logged via
`feedback_blame_third_party_last`: any failure = our wire bytes until
proven otherwise; comparing against the upstream test source first
would have rooted this in minutes, not rounds.

### Closed gap (round 22)

`internal/amwa/registry/registry.go` advertises on
`_nmos-registration._tcp` (the legacy name) **in addition to**
`_nmos-register._tcp` whenever `apiVers` contains any of v1.0/v1.1/v1.2,
mirroring the existing consumer-side `RegistryWatcher` browse fix
(#193). The decision is encapsulated in
`pickRegistryServices(apiVers []string) []string` (`helpers.go`) with a
7-case unit test pinning the behaviour. Distinct instance names per
service-type (`dhs-nmos-registry` vs `dhs-nmos-registry-legacy`) avoid
EntryGroup collisions when both names resolve to the same host:port.

Wire confirmation from the LXC docker bridge:

```
$ avahi-browse -rpt _nmos-register._tcp
=  br-… IPv4 dhs-nmos-registry         _nmos-register._tcp     dhs-registry.local 172.18.0.4:8235 \
       "api_proto=http" "pri=0" "api_ver=v1.0,v1.1,v1.2,v1.3" "api_auth=false"

$ avahi-browse -rpt _nmos-registration._tcp
=  br-… IPv4 dhs-nmos-registry-legacy  _nmos-registration._tcp dhs-registry.local 172.18.0.4:8235 \
       "api_proto=http" "pri=0" "api_ver=v1.0,v1.1,v1.2,v1.3" "api_auth=false"
```

Cerebrum (production peer on the rig) advertises both service-types
the same way — independent confirmation that this is the spec-strict
posture, not a dhs-specific quirk.

### Open caveats (round 22)

| Test | State | Note |
|---|---|---|
| `auto_query_5..19`, `auto_registration_4/5/6` | Could Not Test | by-id endpoint probes that need pre-registered fixtures the harness doesn't auto-populate. Same status `nmos-cpp` gets on a fresh registry per the public AMWA dashboard. |
| `test_25` (v1.2) | Not Implemented | Query API ancestry filter (`query.ancestry_id` / `query.ancestry_type`) returns 501 — test accepts as OPTIONAL per spec. |
| `test_22` family | Manual | Spec-mandated reboot-persistence checks; harness can't auto-verify against an ephemeral container. |

### Closed gaps (sub-issues, all real spec bugs that affect any v1.X peer, not just AMWA)

| Gap | Effect |
|---|---|
| Location header on POST/PUT /resource | Closed test_03/15/21*/23/24/27-31 (cascade ~22 tests) |
| /x-nmos, /x-nmos/{api}, /x-nmos/{api}/ root listings | Closed auto_query_1/2 + auto_registration_1/2 |
| Real X-Paging-* pagination — since/until anchors, limit=0 echo, Link header with raw `:` | Closed test_21_1..test_21_9 + test_21_1_1 |
| Presence-vs-empty validation per api_ver (v1.0 = id+version+label; v1.1+ adds description+tags) | Closed test_04 + 6 do_400_check siblings |
| Subscriptions: per-id GET, query.rql honored, query.downgrade lifted out of params, filter-edge grain semantics, SYNC pre==post | Closed test_23_1, test_24_1, test_29, test_31 (v1.3) |
| Per-resource api_ver tracking + 409 Conflict + URL-version-isolated query | Closed test_22, test_22_2, test_32 |
| Per-version codec strip of Flow.components / Node Endpoint+Service authorization / Device.controls.authorization / Receiver.subscription.active | Closed test_31 cross-resource bytes-equality on v1.0/v1.1/v1.2 |
| Cascade-via-source for v1.0 Flow (no device_id) + Flow.source_id parent check | Closed test_26/27/28 v1.0 |
| Node.Validate skips api/clocks/interfaces when absent (v1.0 wire shape) | Closed v1.0 test_03 cluster |
| Flow.Validate per-format guards only when format-specific fields present | Closed v1.0 test_09/18 |
| query.ancestry_id / query.ancestry_type returns 501 (test accepts as OPTIONAL) | Closed test_25 v1.2 |

### IS-04-02 reproduce one round

```
cd tests/integration/nmos/amwa
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../../../bin/dhs.linux ../../../cmd/dhs
scp ../../../bin/dhs.linux root@10.100.0.105:/root/amwa-test/dhs
ssh root@10.100.0.105 'cd /root/amwa-test && docker compose up -d --build --force-recreate dhs-registry'
sleep 8
for v in v1.0 v1.1 v1.2 v1.3 ; do
    ssh root@10.100.0.105 "/root/amwa-test/run-is0402.sh $v" | head -3
    scp root@10.100.0.105:/root/amwa-test/results/is04-02-$v.json results/
done
```

---

## IS-04-01 (Node) — round 25, 2026-05-02

Snapshot of the per-test posture for the AMWA NMOS Testing tool
against `dhs-node` (port 18080) running in the same docker-compose.

## Multi-version conformance status (2026-05-02, post-#191/#192/#193)

`feat/nmos-is04-amwa-conformance` runs all four AMWA-published IS-04
minors per the strict-all-versions rule
(`internal/amwa/CLAUDE.md` Versioning, memory
`feedback_amwa_strict_all_versions`). Each `.env` row of the harness
sets `DHS_API_VER` and the suite's `version` parameter; result JSONs
land in `results/is04-01-v<X>.json`. Run between rounds with a full
`docker compose down --remove-orphans` to reset AMWA Mock state.

### Headline numbers

### Final round (2026-05-02, post-LXC-rig + post-test_12_01-fix) — full conformance

| API ver | Pass | Fail | Warning | Manual | Notes |
|---|---:|---:|---:|---:|---|
| **v1.0** | **53** | **0** | **0** | 1 | clean — all gaps closed |
| **v1.1** | **56** | **0** | **0** | 1 | clean — test_13 closed by v1.1 sender validator fix |
| **v1.2** | **50** | **0** | **0** | 1 | clean |
| **v1.3** | **59** | **0** | **0** | 1 | clean — test_12_01 closed by full api_ver TXT list + suspend-on-register |

**218 Pass / 0 Fail / 0 Warning across all four AMWA-published IS-04 minors.**

Test rig: AMWA Testing tool `master-902dd5d` running on dhs-tools LXC (10.100.0.105) via docker-compose; dhs-node container built fresh from cross-compiled binary each run. Native Linux Docker bridge → no Docker-Desktop-Windows multicast bug.

### Closed sub-issues

| Issue | Commit | Effect |
|---|---|---|
| #190 v1.0 codec | `4bb7f6f` | v10 codec package + 7 v1.0.3 schemas + tests; codec.AllCodecs now lists v1.0/v1.1/v1.2/v1.3 |
| #193 watcher api_ver filter | `1b9dd33` | Browse both `_nmos-register._tcp` AND `_nmos-registration._tcp`; v1.0/v1.1 mocks now discoverable |
| #192 provider downcast | `fd26b4b` | Per-version codec dispatch on every Node-API GET / PUT-target body; auto_node_11/12 now Pass on v1.2 |
| #191 v12 codec gating | `bb0cffe` | Strip `Node.interfaces[].attached_network_device` + `Receiver.caps.{constraint_sets,version}` for v1.2 wire |
| nmos-cpp arch alignment | `0c6ea9a` | cascade always heartbeat-first; Browser fan-out subscriptions; one shared Browser per process |
| #194 DNS-SD daemon delegation Phase A — Avahi/Linux | `eb55fb2` | system DNS-SD daemon path via Avahi DBus (pure-Go); sub-ms cascade timing; v1.0 Pass count: **30 → 43** |
| LXC test rig + multi-Node mDNS instance name | `943ed36` | unique mDNS instance per Node label (RFC 6763 §4.1.1); enables service-per-device multi-Node deploy |
| Watcher dedupe by URL | `644f643` | RegistryWatcher.Best() collapses dual-name advertised same Cerebrum URL → no register/deregister flap on the wire |
| Per-version registration codec | `5907793` | EncodeRegistrationVersioned dispatches via per-api_ver Codec; closes test_04/07/08/09/10/11 on v1.0/v1.1/v1.2 |
| v10 keeps tags + description | `a81d681` | v1.0.3 schemas allow these as additionalProperties; closes test_28 |
| /transportfile + manifest_href | `1b46ff9` | RFC 4566 SDP body served per Sender; rewriteManifestHrefs points URL at this Node; closes auto_node_11/12 v1.1+v1.2 + cascade flake on v1.0 |
| v1.1 sender validator | `7c7509c` | DecodeSender uses v1.1 required-field set (no interface_bindings/caps/subscription); closes test_13 |
| api_ver TXT comma-list + suspend-on-register | `534694d` | full versions array in mDNS TXT; mDNS responder torn down on register, re-announced on lose-registration; closes test_12_01 |

### Open sub-issues (multi-OS coverage to preserve)

Phase B + C of #194. The dhs DNS-SD layer is interface-driven so each OS path is isolated by build tag — adding any of these does NOT touch the Linux Avahi or stdlib paths.

| Issue | Phase | What |
|---|---|---|
| #195 | B-windows | Bonjour via `dnssd.dll` (CGo); Bonjour Service install required |
| #196 | B-macos | Bonjour via `libSystem` (CGo); daemon always present on macOS |
| #197 | C | LXC multi-distro interop test (Debian/Ubuntu/RHEL/Rocky) |

**Stdlib path is the floor — keep working on every OS regardless of which daemon backend is active.**

How to reproduce any single round:

```
cd tests/integration/nmos/amwa
DHS_API_VER=v1.3 docker compose up -d --build
# wait ~10 s for both containers to be healthy
curl -s -X POST http://127.0.0.1:5000/api \
  -H 'Content-Type: application/json' \
  -d '{"suite":"IS-04-01","host":["dhs-node"],"port":[18080],"version":["v1.3"],"output":"json"}' \
  > results/is04-01-v1.3.json
docker compose stop dhs
DHS_API_VER=v1.2 docker compose up -d dhs
# repeat the curl with version=v1.2, etc.
```

## Known gaps blocking v1.0 / v1.1 / v1.2 from matching v1.3 baseline

These are *missing implementations*, not deferred work — every minor in
the Versioning table must reach v1.3-equivalent conformance. Each gap is
a tracked sub-issue closed by its own commit on this protocol branch.

| Gap | Affects | Sub-issue | Cause |
|---|---|---|---|
| **v12 codec field-gating** | v1.2 round | TODO | `internal/amwa/codec/is04/v12/codec.go` claims "v1.2 = v1.3" but v1.3 added `interfaces[].attached_network_device` (Node), `caps.constraint_sets` + `caps.version` (Receiver). v12 should strip these on encode and reject on decode. |
| **v11 codec field-gating completeness** | v1.1 round | TODO | `nodeV12PlusFields = ["interfaces"]` only; `clocks` was wrong key earlier — needs full audit against v1.1.3 schema, especially Source/Flow per-format fields. |
| **Registry watcher api_ver match logic** | v1.0/v1.1/v1.2 rounds | TODO | When running with `--api-ver=v1.X` (X<3), the watcher logs no Registry-found activity even though AMWA mocks advertise the matching version. Need to verbose-log `pickAPIVer` decisions to confirm whether mocks' `api_ver` TXT is read correctly. |
| **Producer URL routing for non-default api_ver** | v1.0/v1.1/v1.2 Node API responses | TODO | `auto_node_11/12` failures are "Response schema validation error" — JSON shape returned at `/x-nmos/node/v1.X/` is canonical-shape (v1.3) when it should be downcast to the requested minor. Provider needs to look up the codec by URL path version and use that codec's Encode methods, not the canonical struct's. |
| **Node fixture `api.versions` array** | all rounds | resolved 2026-05-02 | `amwa-test-node.json` had `["v1.3"]` hardcoded; updated to `["v1.0","v1.1","v1.2","v1.3"]`. Follow-up: provider should auto-derive from `is04.SupportedVersions()` instead of trusting the fixture. |

## Tests that need human attention

| Test | State | Note |
|---|---|---|
| `test_22` Node resource IDs persist over a reboot | Manual | Spec-mandated manual check. **In this harness `dhs` is rebuilt + the container recreated every iteration**, so IDs are intentionally ephemeral; the AMWA testing tool can never auto-verify this against an ephemeral container. To verify for real: deploy `dhs` outside Docker (or with a writable persistent volume that holds the bundle JSON), reboot the host, and confirm via the Node API that every UUID is identical pre-/post-reboot. |
| `test_16` Failover heartbeat-only (v1.3) | Fail (timing) | Docker Desktop Windows: the AMWA cascade kill loop disables mocks every `HEARTBEAT_INTERVAL+1=6 s`; under that schedule our 5 s heartbeat occasionally hits a freshly-disabled mock and the cascade re-pick triggers a stray POST /resource. Same code on Linux Docker (true host networking) clears it. Track the platform-portability gap, do not patch by lengthening heartbeats — that breaks `test_05`. |
| `test_16_01` Timeout-mock heartbeat (v1.3) | Warning | Same root cause as `test_16` — cascade runs out of clock before reaching registry 5 (5106). |

## Tests that look "Not Implemented" but are intentional

| Test | State | Note |
|---|---|---|
| `auto_node_17..23` (7 rows) | Test Disabled | We advertise `api_auth=false`; IS-10 / TLS is **in scope per the strict-all-versions rule** but tracked under separate Track A composes (BCP-003-01 TLS + BCP-003-02 Auth). Tool correctly skips when ENABLE_AUTH=false. |
| `test_02`, `test_02_01` | Test Disabled | Tool's `DNS_SD_MODE=multicast`; these are unicast variants — covered by the planned `prod-dnssd` deployment shape in the SOW. |
| `test_12` (v1.3 round only) | Test Disabled | Tool says "disabled for Nodes >= v1.3"; the v1.0/v1.1/v1.2 sibling `test_12_01` covers the v1.3 case. |

## Web UI

`http://127.0.0.1:5000` → drop-down `IS-04-01` → Host `dhs-node`,
Port `18080`, Version `v1.0` / `v1.1` / `v1.2` / `v1.3` → Test.
