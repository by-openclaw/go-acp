# Ember+ integration demo — PowerShell cookbook

End-to-end demo proving every spec-implemented behavior of the provider:
all four matrix types, multi-level labels, per-XPT gain, sparse dynamic
matrix, lock / unlock, salvo store/recall/list, plus a glow-types Node
with all `ParameterType` flavors and two stream Parameters for
subscribe/unsubscribe testing.

The DMs assembled here are deliberately compact (4×4 for fixed
matrices, 10 sparse signals in a 1000×1000 dynamic matrix) so a full
walk completes in well under a second. All seeds live under
`.cache/dm/emberplus/` and are regenerated from
`scripts/emberplus/gen-emberplus-demo-dms.ps1`.

## 1. Generate seeds + start the producer

```powershell
# (Re)generate all DMs + manifest. Idempotent.
powershell -ExecutionPolicy Bypass -File .\scripts\emberplus\gen-emberplus-demo-dms.ps1

# (Optional) End-to-end verifier: labels + per-tgt/src/XPT params + crosspoints
# powershell -ExecutionPolicy Bypass -File .\scripts\emberplus\verify-emberplus-integration.ps1

# Build the binary
go build -o bin/dhs.exe ./cmd/dhs

# Serve via the manifest
.\bin\dhs.exe producer emberplus serve `
    --manifest .cache\manifest\emberplus-integration.json `
    --port 9100 `
    --log-level debug
```

Producer listens on `[::]:9100` and exposes the tree:

```
router (synthetic root, oid "1")
├── identity        (1.0)  — product / company / version
├── oneToN          (1.1)  — 4×4, 2 label levels, target 2 pre-locked
├── oneToOne        (1.2)  — 4×4, 2 label levels
├── nToN            (1.3)  — 4×4, 2 label levels, per-XPT gain
├── dynamic         (1.4)  — 1000×1000, 10 sparse signals at 0/50/137/200/350/401/500/750/888/999
├── functions       (1.5)  — setLock, listLocks, storeSalvo, recallSalvo, listSalvos
└── types           (1.6)  — one Parameter per ParameterType + 2 stream Parameters
```

Synthetic root identifier is `dhs-emberplus-integration` (the manifest's
`device.name`). All paths below begin with it; OIDs are absolute.

## 2. Walk + watch from the dhs consumer

```powershell
# Full walk + write canonical DM to .audit\demo\
.\bin\dhs.exe consumer emberplus walk 127.0.0.1 --port 9100 --capture .audit\demo

# Watch every parameter change + matrix connection (no filter)
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100

# Watch only the two stream Parameters (vu_left, vu_right under types/)
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.types.vu_left,dhs-emberplus-integration.types.vu_right `
    --streams-only

# Watch only the locked-target tally on the oneToN matrix
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToN.matrix
```

## 3. Matrix Connect / Disconnect / Absolute

`dhs consumer emberplus set --path <matrix path>` is for Parameters, not
Connections — for crosspoint changes use EmberPlusView's right-click
"Apply"  / Cerebrum's drag-and-drop / VSM router operations. Wire shape
(spec p.86 Command + p.89 Connection):

| Wire op | oneToN / oneToOne | nToN / dynamic |
|---|---|---|
| Connect    | Coerced → Absolute (replace single src) | Union (add src to target's set) |
| Disconnect | Subtract — empty after is valid (target unrouted) | Subtract — same |
| Absolute   | Replace target's sources verbatim | Replace target's sources verbatim |

Per spec p.89, the locked target rejects every Connect/Disconnect/
Absolute and echoes back `disposition: locked` with current sources.
Target 2 on the oneToN matrix is **pre-locked from the seed** — try
routing to it and watch the rejection.

## 4. Function invocation — setLock / salvos

```powershell
# Lock target 3 on the oneToN matrix (matrixPath accepts OID or dotted path).
# Returns previous lock state (false = was unlocked before).
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.functions.setLock" `
    --args "1.1.3,3,true"

# List currently locked targets on the oneToN matrix.
# Result is a flat sequence of locked target numbers.
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.functions.listLocks" `
    --args "1.1.3"

# Unlock target 3 again (returns true = was locked before).
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.functions.setLock" `
    --args "1.1.3,3,false"

# Snapshot the oneToN matrix's current routing into salvo ID 5.
# 3rd arg is a CSV of target numbers to include — empty = all targets.
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.functions.storeSalvo" `
    --args "1.1.3,5,"

# Or snapshot only targets 0, 2, 5 from the oneToN matrix.
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.functions.storeSalvo" `
    --args "1.1.3,5,0,2,5"

# Recall salvo 5 — restores the snapshotted routing. Returns rows restored.
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.functions.recallSalvo" `
    --args "1.1.3,5"

# List salvo IDs that have a snapshot for the oneToN matrix.
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.functions.listSalvos" `
    --args "1.1.3"
```

The same five functions work against any matrix in the demo — replace
`1.1.3` with `1.2.3` (oneToOne), `1.3.4` (nToN), or `1.4.3` (dynamic).

## 5. Parameter SetValue — typed values, gain, streams

```powershell
# Set an integer Parameter (one of every type lives under types/).
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.types.vInteger" --value 99

# Real, string, boolean, enum, octets — same verb, different value.
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.types.vReal" --value -12.5

.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.types.vEnum" --value 3

# Cell gain for nToN connection target=0 source=0 (per-XPT gain under
# parametersLocation per spec p.88).
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path "dhs-emberplus-integration.nToN.params.0.0.gain" --value -200
```

## 6. tshark verification

```powershell
# Capture every Ember+ frame to a pcap while you fire test commands.
& "C:\Program Files\Wireshark\tshark.exe" -i Loopback -f "tcp port 9100" `
    -w .audit\demo\wire.pcapng

# Decode all Glow frames after capture:
& "C:\Program Files\Wireshark\tshark.exe" -r .audit\demo\wire.pcapng `
    -Y "tcp.len > 20" -V | Select-String "Glow|Matrix|Connection|Lock"
```

S101 app-bytes on every frame should read `01 02 3c 02`
(DTD = Glow 2.60). Every matrix Connect should round-trip as
`QualifiedMatrix [APP 17]` → CTX[5] Connection list.

## 7. Live verify checklist

Spec-strict behaviors to confirm:

| Path | Test | Spec |
|---|---|---|
| `oneToN.matrix` (1.1.3) | Click empty cell → Connect coerced to Absolute → single src replace | p.88 |
| `oneToN.matrix` target 2 | Try to reroute → echoed `disposition: locked` + unchanged sources | p.89 |
| `oneToN.matrix` target 0 | Click lit cell → Disconnect → target.sources=[] (unrouted) | p.89 |
| `oneToOne.matrix` (1.2.3) | Route src 0 onto a target that doesn't hold it → previous target loses src (bijection) | p.88 oneToOne |
| `nToN.matrix` (1.3.4) | Click empty cell → Connect → sources UNION (multi-src per target) | p.88 nToN |
| `nToN.params.X.Y.gain` | SetValue on cell gain Parameter — integer in [-1000, 100] | p.85 ParameterAccess |
| `dynamic.matrix` (1.4.3) | Walk → only the 10 sparse signals appear despite targetCount=1000 | p.88 nonLinear |
| `types.vu_left` (1.6.10) | Subscribe → receive periodic stream updates | p.86 Subscribe |
| `types.vu_left` | Unsubscribe → updates stop | p.86 Unsubscribe |
| `functions.setLock` (1.5.1) | Invoke with `(matrixPath, target, true)` → returns previous state | p.91 Invocation |
| `functions.storeSalvo` (1.5.3) | Invoke → snapshot connections; recall later via 1.5.4 | p.91 |

Every test above is reproducible against the running producer with no
external tool beyond `dhs.exe` + PowerShell. EmberPlusView / Cerebrum /
VSM are validation peers — same wire, no special setup.
