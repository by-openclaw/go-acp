# CLAUDE.md — CCM (EVS Neuron REST API)

Atomic per-protocol context for the CCM connector — the REST API EVS
positions as the acp2 successor on Neuron (#706). Read the root
`CLAUDE.md` first.

**STATUS: BUILDING (owner go 2026-09-03).** Spec review done; the
device's own OpenAPI (`/api/v1/docs/api.yml`) is captured. Unit 1
shipped: stdlib UUID-keyed codec + `dhs consumer ccm walk` + `ccm
export` (stores api.yml schema + walked tree DM + extract, keyed by
productName@productVersion for firmware diff). Unit 2 shipped:
**recursive full-DM walk** — `codec.ClassifyBody` (branch = JSON array
of child names; resource = object / array-of-objects) + consumer
`WalkTree` follows the device's own shape (no wildcard; GET each node,
recurse only listed children) seeded from the API root OR explicit node
paths (`--start`, for the "root doesn't exist / know the path" case).
`ccm walk --tree` and `ccm export` (dm-tree.json) now capture the WHOLE
model (live BRIDGE@7.0.2: 41 resources / 13 nodes — io/ip, io/madi,
io/sdi, all of matrix, misc, processing, self), not just the six io/ip
stream paths the old Walk hardcoded. The acp2 connector stays
regardless: this bridge runs acp2 + REST/CCM + NMOS at once
(mixed-firmware, multi-protocol box).

## REST only — do not re-probe the WebSocket

**Decided by the codeowner: CCM is a REST connector.** Not "blocked on
firmware", not "pending EVS" — a decision. `watch` polls, the way
SNMP's does. Nothing in this tree should spend time on the WS again,
and this section exists so the next reader does not go looking.

The evidence behind the decision, so nobody has to re-gather it: the WS
is not served. Verified 2026-09-03 on 7.0.2 by packet capture and
re-verified 2026-09-24 on **7.0.3** — 443 negotiates http/1.1 only, the
WS would be a plain HTTP/1.1-Upgrade-over-TLS on 443 (no separate port,
nmap-clean), and every candidate path 404s through a
confirmed-correct handshake (`/api/v1/ws`, `/ws`,
`/api/v1/subscriptions`, `/api/v1/events`, ~25 more). **EVS's own
Cerebrum 2.9.0 CCM driver fails the same way** — the 1 MB persistent
443 connection in a Cerebrum-side capture is the REST poll, not a live
WS. The message protocol is documented (PDF §13) and would be a small
unit if EVS ever ships it; that is a decision for then, not a gap now.

**Unit 2 (PR #1065): the PROVIDER.** `dhs producer ccm serve` replays a
captured device model (a dm-tree: resource path → resource JSON, the
shape `ccm export` captures) so Cerebrum drives dhs as a CCM device —
emulate before hardware. Compliance boundary, enforced by construction:
`/api/v1` is the CCM protocol 100% to the device's own `api.yml`
(self-describing tree, the OpenAPI at `/api/v1/docs/api.yml`, writes
ONLY where and how that document declares them — the shipped document
has 35 PUT answering 200 + the resource, no PATCH — §12
`{code,message}`, `uuid`/`id` immutable). THE `api.yml` IS THE CONTRACT,
not the 0v1 PDF (a proposal: PATCH, empty 202, `/state`); where they
differ the document wins, and the emulator infers nothing beyond it; every dhs addition (landing, rendered README,
capabilities) lives under `/x-dhs` via the one `HandleExtension` entry
point and can never change what a controller observes. Matrix (§17) is
**in scope** (owner reversed the 2026-08-22 exclusion on 2026-09-09 —
it was made unaware matrix is in the protocol): 16 `/v1/matrix/...`
paths, per-essence matrices, UUID-addressed; `main`/`backup` are two
independent routing levels (main and backup source per destination —
the FE has main and backup inputs and outputs; NOT ST 2022-7, which is
the IP stream `legs` concept), `current` is the read-only effective
route whose failover rule the spec does not define (ask EVS). LIVE-
CONFIRMED (2026-09-09): state ids are NOT uuids — each info group
`{template,type,path,children[]{id,subIds}}` renders `template` with
`{idx}`/`{subIdsIdx}` to the state key (`IP000-05`), which resolves to
`children[idx].id` at `path/{id}` (uuid, or integer for Delay Bank —
schema deviation); `info` has no `levels` array (deviation; levels are
endpoints). The emulator serves the matrix exactly as the document
declares: `info`/`current` GET, `main`/`backup` GET+PUT (`MatrixState`:
object of strings, stored and returned, 200) on the matrices that have
them; no `current` recomputation (the document defines none). The
`/ws` change stream is a later unit. Operate it per `docs/runbook.md`.

**Firmware reality (BRIDGE 6.7.4, verified live on 10.6.255.102):**
this build serves the CCM resource MODEL (UUID-addressed REST, `/self`,
recursive `{uuid}` paths) but a SUBSET of the CCM 0v1 PROTOCOL — it has
GET+PUT only (no PATCH-maps per §11.2) and **no `/ws`** (verified with
a real Upgrade handshake — every candidate path 404s, not a connection
error). So `watch` over `/ws` is not available on this firmware; the
owner is upgrading to a CCM-enabled build where it is. Deviations are
absorbed + reported, never worked around (spec-strict posture). The
api.yml is stored versioned so the upgrade's diff shows what CCM adds.

---

## Two products, one connector — what differs on the wire

EVS ships more than one Neuron, and they do not describe themselves the
same way. Everything here is measured from the devices, not supposed.

| what | BRIDGE (video) | SHUFFLE (audio) |
|---|---|---|
| verified on | 10.6.255.102, fw 7.0.3 | 10.44.72.27, fw 2.0.0, 2026-09-25 |
| API base | `/api/v1` | `/api` — **no version segment** |
| spec document | `/docs/api.yml` | `/docs/openapi.yml` |
| OpenAPI | 3.1.2 | 3.1.1 |
| matrix info | `/matrix/video/path/info` | `/matrices/audio/info` |
| state maps | beside the info (`…/path/main`) | a level below (`…/state/main`) |
| axis naming | `template` + ordered `children` | UUIDs, no children, no template |
| providers per axis | 1 | **4 destinations, 5 sources** |
| crosspoints | 11 071 | **17 728 × 3 maps = 53 184** |
| model size | ~24 k objects | **170 060 objects** |

Neither is the "normal" one. Both are read from the device's own
document at connect time, which is why one connector serves both.

### `slots: "channels"` — a crosspoint key is not always a member

The shuffler's matrix providers carry `"slots": "channels"`. It means
the crosspoint keys are **not** the UUIDs of the streams under the
provider's `path` — they are the UUIDs of the channels INSIDE those
streams. The resource a crosspoint names is one level deeper:

```text
/io/ip/receivers/audio/<streamUuid>/channels/<channelUuid>
```

Nothing in the info body says which stream owns which channel. Only the
members do, in their own `channels` array. So the connector asks the
device, builds the index, and hands it to the codec
(`MatrixInfo.SetIndex`). Without it not one of the 53 184 crosspoints
resolves to anything, because with several providers on an axis there is
no honest way to guess which one a bare UUID belongs to.

### A collection may list a SUMMARY of its members

`/io/ip/receivers/audio` answers `[{"uuid":"…"}]` — the uuid and nothing
else — while the member itself carries the name, the SDP and the
`channels` list. The BRIDGE does the opposite: its collections answer
with every member in full.

Two rules follow, and both are load-bearing:

1. A listing may stand in for reading a member only once it has been
   PROVEN equal to that member (one probe per collection — see
   `read` in `consumer/model.go`). Assuming it would build a model out
   of summaries, silently missing fields no reader could know were
   missing.
2. Where a sub-collection is not served as a resource at all
   (`…/{uuid}/channels` 404s here), its ids come from the parent's
   field of that name. Otherwise every per-channel resource — 34 816 of
   them on this device — stays out of the model.

### Reading the routing: the canonical matrix file-set

The crosspoints are in the walked model, but a 53 184-row generic
export is not how anyone reads routing. The matrix gets its own files,
in the same grammar as the levelled protocols (#461, ADR-0023) — so a
Neuron reads like a Probel tally dump:

```text
dhs consumer ccm export <ip> --out-dir ./mx --prefix bridge
```

No `--path` means EVERY matrix — video, audio and data, main, backup
and current alike — each in its own directory named after it. The
BRIDGE has 11; an operator should not have to know that, nor run the
command 11 times, nor discover that an audio-only box has no
`matrix.video.path.main`. Naming one with `--path` writes that one
flat, which is the shape `import --xpoint` reads.

| file | what it holds |
|---|---|
| `<prefix>-matrix.csv` | the matrix entity: behaviour, target/source counts |
| `<prefix>-xpoint.csv` | `dest,srce,levels` — the tally dump itself |
| `<prefix>-dst.csv` | each destination → the resource it is, its channel, its kind |
| `<prefix>-src.csv` | the same for the feeding side |

The mapping lives in `-dst.csv` / `-src.csv`, one row per endpoint, not
repeated on every crosspoint row.

### And back in again

An export nobody can put back is a report, not a backup:

```text
dhs consumer ccm import <ip> --xpoint ./mx/<matrix>/bridge-xpoint.csv \
    --matrix ./mx/<matrix>/bridge-matrix.csv [--check] [--output json]
```

ADR-0007 semantics: `--check` reads the live state, reports
`would_change` and sends nothing. Only the crosspoints that differ are
written, so converging a matrix that already matches sends nothing at
all.

The write is a batch. This API has no PATCH, so one crosspoint is a
read-modify-write of the whole state map — doing that per crosspoint
would ship a 17 728-entry document 17 728 times, each one able to undo
the last. `SetValues` groups every change to one resource into a single
GET and a single PUT. See `internal/ccm/consumer/write.go`.

Nothing in that path is CCM-specific: it is driven by the link metadata
any connector's walk attaches, so a protocol that resolves its
crosspoints gets the file-set without new code. See
`cmd/dhs/cmd_matrix_linked.go`.

### Declared paths nest two parameters deep

The shuffler declares 6 paths with two `{…}` segments (per-channel audio
in and out, MADI in and out, per-channel delay, MAC routes); the BRIDGE
declares none. A walk expands **every** parameter, not the first — see
`expand`. Expanding one sends `{channelUuid}` to the device as a
literal and collects a 404.

## Folder layout (target shape, per root CLAUDE.md conventions)

```text
internal/ccm/
├── CLAUDE.md    ← this file
├── assets/      ← DROP ZONE: everything EVS provides goes here
│                  (OpenAPI/swagger JSON, PDFs, examples, postman
│                  collections, firmware release notes)
├── docs/        spec review, keys/endpoint catalogue, runbook.md
│                  (operate the provider), README index
├── codec/       shipped — stdlib-only UUID-keyed model (Device,
│                  Stream, Leg) + testdata/neuron-api-1.0.0.yml, the
│                  device's real OpenAPI 3.1.2
├── consumer/    shipped — package ccm: walk + export
├── provider/    shipped (PR #1065) — package ccm: replays a captured
│                  dm-tree as a CCM device. /api/v1 = the protocol,
│                  100% to spec; /x-dhs = dhs additions only. Own
│                  README.md served rendered at /x-dhs/readme
└── wireshark/   (later) dhs_ccm.lua — HTTP/JSON dissection with
                   per-endpoint Info columns (repo rule: every
                   protocol ships a dissector, no exceptions)
```

## Spec sources (fill as they arrive)

| Artifact | Where | Status |
|---|---|---|
| Live swagger from the on-site Neuron (`GET /swagger.json` or similar) | `assets/` | PENDING — day-1 capture when the device arrives (week of 2026-08-24) |
| EVS-provided docs | `assets/` | PENDING — owner storing them here |

## Review checklist (run when assets land)

1. OpenAPI version + auth model (basic? token? none?) — feeds the
   OAS tooling choice shared with dhs-srv.
2. Endpoint → verb mapping: which canonical verbs (info/walk/get/
   set/ensure/export/watch) the REST surface can back. The
   subscription question is CLOSED — REST only, `watch` polls; see
   "REST only" above.
3. Object model vs the acp2 tree: can CCM serve the SAME canonical
   tree (label paths, DM identity Model@SwRev) so DMs/manifests/packs
   stay protocol-agnostic? That is the acceptance bar.
4. Parity matrix CCM↔acp2 per object: what disappears, what's new —
   becomes the migration note for mixed fleets.
5. Rate limits / concurrency — scale targets in root CLAUDE.md apply.

## What NOT to do

- No code before the checklist verdict + owner go.
- Never delete or bypass the acp2 connector — mixed-firmware fleets
  keep both.
