# Generates the spec-clean Ember+ integration-test DM cards + manifest.
#
# Output:
#   .cache/dm/emberplus/identity-strict@1.0.0.json
#   .cache/dm/emberplus/oneToN-strict@1.0.0.json
#   .cache/dm/emberplus/oneToOne-strict@1.0.0.json
#   .cache/dm/emberplus/nToN-strict@1.0.0.json
#   .cache/dm/emberplus/dynamic-strict@1.0.0.json
#   .cache/dm/emberplus/functions-strict@1.0.0.json
#   .cache/dm/emberplus/glow-types-strict@1.0.0.json
#   .cache/manifest/emberplus-integration.json
#
# Re-run any time to regenerate. Idempotent.

$ErrorActionPreference = 'Stop'
$repoRoot = Resolve-Path "$PSScriptRoot\.."
$dmDir = Join-Path $repoRoot ".cache\dm\emberplus"
$manifestDir = Join-Path $repoRoot ".cache\manifest"
New-Item -ItemType Directory -Force -Path $dmDir | Out-Null
New-Item -ItemType Directory -Force -Path $manifestDir | Out-Null

function Save-DM($model, $swrev, $rootObj, [array]$templates = @()) {
    $obj = [ordered]@{
        model    = $model
        sw_rev   = $swrev
        protocol = "emberplus"
        root     = $rootObj
        templates = $templates
    }
    $path = Join-Path $dmDir "$model@$swrev.json"
    [System.IO.File]::WriteAllText($path, ($obj | ConvertTo-Json -Depth 80))
    Write-Host "wrote $path ($((Get-Item $path).Length) bytes)"
}

# ---------------- helpers ----------------

function StringParam($num, $ident, $path, $oid, $val, $access = "read") {
    [ordered]@{
        number = $num; identifier = $ident; path = $path; oid = $oid
        isOnline = $true; access = $access; type = "string"; value = $val
    }
}

function IntegerParam($num, $ident, $path, $oid, $val, $min, $max, $step = 1, $access = "readWrite") {
    [ordered]@{
        number = $num; identifier = $ident; path = $path; oid = $oid
        isOnline = $true; access = $access; type = "integer"
        value = $val; minimum = $min; maximum = $max; step = $step
    }
}

function NodeOf($num, $ident, $path, $oid, [array]$children, $access = "read") {
    [ordered]@{
        number = $num; identifier = $ident; path = $path; oid = $oid
        isOnline = $true; access = $access
        children = $children
    }
}

# Build a single label level: a Node with children[targets, sources] of string Parameters.
function LabelLevel($num, $ident, $path, $oid, $tgtCount, $srcCount, $prefix) {
    $tgts = @()
    for ($i = 0; $i -lt $tgtCount; $i++) {
        $tgts += StringParam $i "t-$i" "$path.targets.t-$i" "$oid.1.$i" "$prefix-T$i" "readWrite"
    }
    $srcs = @()
    for ($i = 0; $i -lt $srcCount; $i++) {
        $srcs += StringParam $i "s-$i" "$path.sources.s-$i" "$oid.2.$i" "$prefix-S$i" "readWrite"
    }
    NodeOf $num $ident $path $oid @(
        NodeOf 1 "targets" "$path.targets" "$oid.1" $tgts
        NodeOf 2 "sources" "$path.sources" "$oid.2" $srcs
    )
}

# Per-signal control params: a Node tree where each target / source has
# its own Node containing gain + mode + mute Parameters. Sits as a
# sibling of the label levels — labels stay simple string Parameters
# (Lawo basePath/<num> convention) while ParamsBank carries the
# controllable per-signal state. Used by every matrix demo so live
# clients can SetValue against gain / mode / mute per signal.
function SignalParams($num, $ident, $path, $oid, $signalCount) {
    $kids = @()
    for ($i = 0; $i -lt $signalCount; $i++) {
        $sigPath = "$path.$i"
        $sigOid  = "$oid.$i"
        $kids += NodeOf $i "$i" $sigPath $sigOid @(
            (IntegerParam 1 "gain" "$sigPath.gain" "$sigOid.1" 0 -1000 100 1 "readWrite")
            ([ordered]@{
                number = 2; identifier = "mode"; path = "$sigPath.mode"; oid = "$sigOid.2"
                isOnline = $true; access = "readWrite"; type = "enum"; value = 0
                enumMap = @(
                    [ordered]@{ key = "auto";    value = 0 }
                    [ordered]@{ key = "manual";  value = 1 }
                    [ordered]@{ key = "bypass";  value = 2 }
                )
            })
            ([ordered]@{
                number = 3; identifier = "mute"; path = "$sigPath.mute"; oid = "$sigOid.3"
                isOnline = $true; access = "readWrite"; type = "boolean"; value = $false
            })
        )
    }
    NodeOf $num $ident $path $oid $kids
}

# ---------------- 1. identity-strict ----------------
$identityRoot = NodeOf 0 "identity" "identity" "1.0" @(
    StringParam 1 "product"  "identity.product"  "1.0.1" "dhs-emberplus-integration"
    StringParam 2 "company"  "identity.company"  "1.0.2" "BY-Systems"
    StringParam 3 "version"  "identity.version"  "1.0.3" "1.0.0"
)
Save-DM "identity-strict" "1.0.0" $identityRoot

# ---------------- 2. oneToN-strict (4x4, 2 label levels, target 2 pre-locked) ----------------
function BuildMatrixSubtree($rootIdent, $rootOid, $matrixType, $tgtCount, $srcCount, $lockedTargets, $extraOpts) {
    $base = $rootOid
    $children = @(
        (LabelLevel 1 "labelsPrimary"   "$rootIdent.labelsPrimary"   "$base.1" $tgtCount $srcCount "Pri")
        (LabelLevel 2 "labelsSecondary" "$rootIdent.labelsSecondary" "$base.2" $tgtCount $srcCount "Sec")
        (SignalParams 4 "targetParams"  "$rootIdent.targetParams"    "$base.4" $tgtCount)
        (SignalParams 5 "sourceParams"  "$rootIdent.sourceParams"    "$base.5" $srcCount)
    )
    $conns = @()
    for ($i = 0; $i -lt [Math]::Min($tgtCount, $srcCount); $i++) {
        $disp = if ($lockedTargets -contains $i) { "locked" } else { "tally" }
        $lk   = if ($lockedTargets -contains $i) { $true } else { $false }
        $conns += [ordered]@{
            target = $i; sources = @($i); operation = "absolute"
            disposition = $disp; locked = $lk
        }
    }
    $tgtsArr = @(); for ($i=0; $i -lt $tgtCount; $i++) { $tgtsArr += [ordered]@{ number = $i } }
    $srcsArr = @(); for ($i=0; $i -lt $srcCount; $i++) { $srcsArr += [ordered]@{ number = $i } }
    # oneToN / oneToOne: cardinality invariant means maxConnectsPerTarget=1.
    # Declared explicitly so consumers see the wire field per spec p.88 [7].
    $maxPerTgt = $null
    if ($matrixType -eq "oneToN" -or $matrixType -eq "oneToOne") {
        $maxPerTgt = 1
    }
    $matrix = [ordered]@{
        number = 3; identifier = "matrix"; path = "$rootIdent.matrix"; oid = "$base.3"
        isOnline = $true; access = "readWrite"
        type = $matrixType; mode = "linear"
        targetCount = $tgtCount; sourceCount = $srcCount
        maximumTotalConnects = $null
        maximumConnectsPerTarget = $maxPerTgt
        parametersLocation = $null
        gainParameterNumber = $null
        labels = @(
            [ordered]@{ basePath = "$base.1"; description = "Primary" }
            [ordered]@{ basePath = "$base.2"; description = "Secondary" }
        )
        targets = $tgtsArr
        sources = $srcsArr
        connections = $conns
        targetLabels = $null; sourceLabels = $null
        targetParams = $null; sourceParams = $null; connectionParams = $null
    }
    foreach ($k in $extraOpts.Keys) { $matrix[$k] = $extraOpts[$k] }
    $children += $matrix
    NodeOf 1 $rootIdent $rootIdent $rootOid $children
}

Save-DM "oneToN-strict"  "1.0.0" (BuildMatrixSubtree "oneToN"  "1.1" "oneToN"  4 4 @(2) @{})
Save-DM "oneToOne-strict" "1.0.0" (BuildMatrixSubtree "oneToOne" "1.2" "oneToOne" 4 4 @()  @{})

# ---------------- 4. nToN-strict (4x4, 2 label levels, per-XPT gain via parametersLocation) ----------------
# parametersLocation = "1.3.3" -> a Node tree with per-target/per-source/gain Parameter.
$nTNRoot = "nToN"
$nTNOid = "1.3"
$paramsBase = "$nTNOid.3"
$tgtCount = 4; $srcCount = 4

$paramTargets = @()
for ($t = 0; $t -lt $tgtCount; $t++) {
    $srcsForTarget = @()
    for ($s = 0; $s -lt $srcCount; $s++) {
        $gainParam = IntegerParam 1 "gain" "$nTNRoot.params.$t.$s.gain" "$paramsBase.$t.$s.1" 0 -1000 100 1 "readWrite"
        $srcsForTarget += NodeOf $s "$s" "$nTNRoot.params.$t.$s" "$paramsBase.$t.$s" @($gainParam)
    }
    $paramTargets += NodeOf $t "$t" "$nTNRoot.params.$t" "$paramsBase.$t" $srcsForTarget
}
$paramsNode = NodeOf 3 "params" "$nTNRoot.params" $paramsBase $paramTargets

$nTNChildren = @(
    (LabelLevel 1 "labelsPrimary"   "$nTNRoot.labelsPrimary"   "$nTNOid.1" $tgtCount $srcCount "Pri")
    (LabelLevel 2 "labelsSecondary" "$nTNRoot.labelsSecondary" "$nTNOid.2" $tgtCount $srcCount "Sec")
    $paramsNode
    (SignalParams 5 "targetParams" "$nTNRoot.targetParams" "$nTNOid.5" $tgtCount)
    (SignalParams 6 "sourceParams" "$nTNRoot.sourceParams" "$nTNOid.6" $srcCount)
)

$nTNConns = @(
    [ordered]@{ target = 0; sources = @(0,1); operation = "absolute"; disposition = "tally"; locked = $false }
    [ordered]@{ target = 1; sources = @(1,2); operation = "absolute"; disposition = "tally"; locked = $false }
    [ordered]@{ target = 2; sources = @(2);   operation = "absolute"; disposition = "tally"; locked = $false }
    [ordered]@{ target = 3; sources = @();    operation = "absolute"; disposition = "tally"; locked = $false }
)
$tgtsArr = @(); for ($i=0; $i -lt $tgtCount; $i++) { $tgtsArr += [ordered]@{ number = $i } }
$srcsArr = @(); for ($i=0; $i -lt $srcCount; $i++) { $srcsArr += [ordered]@{ number = $i } }
$nTNMatrix = [ordered]@{
    number = 4; identifier = "matrix"; path = "$nTNRoot.matrix"; oid = "$nTNOid.4"
    isOnline = $true; access = "readWrite"
    type = "nToN"; mode = "linear"
    targetCount = $tgtCount; sourceCount = $srcCount
    maximumTotalConnects = 16
    maximumConnectsPerTarget = 4
    parametersLocation = $paramsBase
    gainParameterNumber = 1
    labels = @(
        [ordered]@{ basePath = "$nTNOid.1"; description = "Primary" }
        [ordered]@{ basePath = "$nTNOid.2"; description = "Secondary" }
    )
    targets = $tgtsArr
    sources = $srcsArr
    connections = $nTNConns
    targetLabels = $null; sourceLabels = $null
    targetParams = $null; sourceParams = $null; connectionParams = $null
}
$nTNChildren += $nTNMatrix
Save-DM "nToN-strict" "1.0.0" (NodeOf 3 $nTNRoot $nTNRoot $nTNOid $nTNChildren)

# ---------------- 5. dynamic-strict (1000x1000 declared, 10 sparse signals) ----------------
$dynRoot = "dynamic"
$dynOid = "1.4"
$dynSparseTgts = @(0, 50, 137, 200, 350, 401, 500, 750, 888, 999)
$dynSparseSrcs = @(0, 50, 137, 200, 350, 401, 500, 750, 888, 999)
$dynTgtCount = 1000; $dynSrcCount = 1000

# Sparse label levels — Node trees containing only the declared signals
function SparseLabelLevel($num, $ident, $path, $oid, $tgtNums, $srcNums, $prefix) {
    $tgts = @()
    foreach ($n in $tgtNums) {
        $tgts += StringParam $n "t-$n" "$path.targets.t-$n" "$oid.1.$n" "$prefix-T$n" "readWrite"
    }
    $srcs = @()
    foreach ($n in $srcNums) {
        $srcs += StringParam $n "s-$n" "$path.sources.s-$n" "$oid.2.$n" "$prefix-S$n" "readWrite"
    }
    NodeOf $num $ident $path $oid @(
        NodeOf 1 "targets" "$path.targets" "$oid.1" $tgts
        NodeOf 2 "sources" "$path.sources" "$oid.2" $srcs
    )
}

# Sparse per-signal control params: same shape as SignalParams but keyed
# by the explicit signal numbers — only the declared ten signals carry
# state, matching the sparse-matrix declaration.
function SparseSignalParams($num, $ident, $path, $oid, $signalNums) {
    $kids = @()
    foreach ($n in $signalNums) {
        $sigPath = "$path.$n"
        $sigOid  = "$oid.$n"
        $kids += NodeOf $n "$n" $sigPath $sigOid @(
            (IntegerParam 1 "gain" "$sigPath.gain" "$sigOid.1" 0 -1000 100 1 "readWrite")
            ([ordered]@{
                number = 2; identifier = "mode"; path = "$sigPath.mode"; oid = "$sigOid.2"
                isOnline = $true; access = "readWrite"; type = "enum"; value = 0
                enumMap = @(
                    [ordered]@{ key = "auto";   value = 0 }
                    [ordered]@{ key = "manual"; value = 1 }
                    [ordered]@{ key = "bypass"; value = 2 }
                )
            })
            ([ordered]@{
                number = 3; identifier = "mute"; path = "$sigPath.mute"; oid = "$sigOid.3"
                isOnline = $true; access = "readWrite"; type = "boolean"; value = $false
            })
        )
    }
    NodeOf $num $ident $path $oid $kids
}

$dynChildren = @(
    (SparseLabelLevel 1 "labelsPrimary"   "$dynRoot.labelsPrimary"   "$dynOid.1" $dynSparseTgts $dynSparseSrcs "Pri")
    (SparseLabelLevel 2 "labelsSecondary" "$dynRoot.labelsSecondary" "$dynOid.2" $dynSparseTgts $dynSparseSrcs "Sec")
    (SparseSignalParams 4 "targetParams" "$dynRoot.targetParams" "$dynOid.4" $dynSparseTgts)
    (SparseSignalParams 5 "sourceParams" "$dynRoot.sourceParams" "$dynOid.5" $dynSparseSrcs)
)

$dynTgtsArr = @(); foreach ($n in $dynSparseTgts) { $dynTgtsArr += [ordered]@{ number = $n } }
$dynSrcsArr = @(); foreach ($n in $dynSparseSrcs) { $dynSrcsArr += [ordered]@{ number = $n } }
# Seed a few connections among the sparse signals
$dynConns = @(
    [ordered]@{ target = 0;   sources = @(0);          operation = "absolute"; disposition = "tally"; locked = $false }
    [ordered]@{ target = 137; sources = @(401, 750);   operation = "absolute"; disposition = "tally"; locked = $false }
    [ordered]@{ target = 888; sources = @(999);        operation = "absolute"; disposition = "tally"; locked = $true  }
)
$dynMatrix = [ordered]@{
    number = 3; identifier = "matrix"; path = "$dynRoot.matrix"; oid = "$dynOid.3"
    isOnline = $true; access = "readWrite"
    type = "nToN"; mode = "nonLinear"
    targetCount = $dynTgtCount; sourceCount = $dynSrcCount
    maximumTotalConnects = 50
    maximumConnectsPerTarget = 4
    parametersLocation = $null
    gainParameterNumber = $null
    labels = @(
        [ordered]@{ basePath = "$dynOid.1"; description = "Primary" }
        [ordered]@{ basePath = "$dynOid.2"; description = "Secondary" }
    )
    targets = $dynTgtsArr
    sources = $dynSrcsArr
    connections = $dynConns
    targetLabels = $null; sourceLabels = $null
    targetParams = $null; sourceParams = $null; connectionParams = $null
}
$dynChildren += $dynMatrix
Save-DM "dynamic-strict" "1.0.0" (NodeOf 4 $dynRoot $dynRoot $dynOid $dynChildren)

# ---------------- 6. functions-strict (5 control fns) ----------------
function FunctionOf($num, $ident, $path, $oid, $fnArgs, $fnResult) {
    [ordered]@{
        number = $num; identifier = $ident; path = $path; oid = $oid
        isOnline = $true; access = "read"
        arguments = $fnArgs; result = $fnResult
    }
}
function Arg($name, $type) { [ordered]@{ name = $name; type = $type } }

$fnRoot = "functions"
$fnOid  = "1.5"
$argsSetLock     = ,(Arg "matrixPath" "string") + ,(Arg "target" "integer") + ,(Arg "locked" "boolean")
$argsListLocks   = ,(Arg "matrixPath" "string")
$argsStoreSalvo  = ,(Arg "matrixPath" "string") + ,(Arg "salvoID" "integer") + ,(Arg "targets" "string")
$argsRecallSalvo = ,(Arg "matrixPath" "string") + ,(Arg "salvoID" "integer")
$argsListSalvos  = ,(Arg "matrixPath" "string")
$retOk           = ,(Arg "ok" "boolean")
$retCount        = ,(Arg "count" "integer")

$fnChildren = @(
    (FunctionOf 1 "setLock"     "$fnRoot.setLock"     "$fnOid.1" $argsSetLock     $retOk)
    (FunctionOf 2 "listLocks"   "$fnRoot.listLocks"   "$fnOid.2" $argsListLocks   $retCount)
    (FunctionOf 3 "storeSalvo"  "$fnRoot.storeSalvo"  "$fnOid.3" $argsStoreSalvo  $retOk)
    (FunctionOf 4 "recallSalvo" "$fnRoot.recallSalvo" "$fnOid.4" $argsRecallSalvo $retCount)
    (FunctionOf 5 "listSalvos"  "$fnRoot.listSalvos"  "$fnOid.5" $argsListSalvos  $retCount)
)
Save-DM "functions-strict" "1.0.0" (NodeOf 5 $fnRoot $fnRoot $fnOid $fnChildren)

# ---------------- 7. glow-types-strict (all ParameterType + 2 streams) ----------------
$glowRoot = "types"
$glowOid  = "1.6"
$glowChildren = @(
    # One of each ParameterType (spec p.85 ParameterType enum)
    IntegerParam 1 "vInteger" "$glowRoot.vInteger" "$glowOid.1" 42      -100 100
    [ordered]@{ number=2; identifier="vReal";    path="$glowRoot.vReal";    oid="$glowOid.2"; isOnline=$true; access="readWrite"; type="real";    value=3.14;   minimum=-1000.0; maximum=1000.0 }
    [ordered]@{ number=3; identifier="vString";  path="$glowRoot.vString";  oid="$glowOid.3"; isOnline=$true; access="readWrite"; type="string";  value="hello" }
    [ordered]@{ number=4; identifier="vBoolean"; path="$glowRoot.vBoolean"; oid="$glowOid.4"; isOnline=$true; access="readWrite"; type="boolean"; value=$true }
    [ordered]@{ number=5; identifier="vTrigger"; path="$glowRoot.vTrigger"; oid="$glowOid.5"; isOnline=$true; access="write";     type="trigger"; value=$null }
    [ordered]@{ number=6; identifier="vEnum";    path="$glowRoot.vEnum";    oid="$glowOid.6"; isOnline=$true; access="readWrite"; type="enum";    value=1;
                enumMap=@(
                    [ordered]@{ key="Off";    value=0 }
                    [ordered]@{ key="Low";    value=1 }
                    [ordered]@{ key="Medium"; value=2 }
                    [ordered]@{ key="High";   value=3 }
                ) }
    [ordered]@{ number=7; identifier="vOctets";  path="$glowRoot.vOctets";  oid="$glowOid.7"; isOnline=$true; access="readWrite"; type="octets";  value="aGVsbG8=" }
    # Two stream Parameters with streamIdentifier (spec p.85 [14] streamIdentifier).
    # Subscribe(30) on these enables the streamer.
    [ordered]@{ number=10; identifier="vu_left";  path="$glowRoot.vu_left";  oid="$glowOid.10"; isOnline=$true; access="read"; type="real"; value=-60.0;
                minimum=-128.0; maximum=15.0; format="%.2f °dB"; streamIdentifier=1001 }
    [ordered]@{ number=11; identifier="vu_right"; path="$glowRoot.vu_right"; oid="$glowOid.11"; isOnline=$true; access="read"; type="real"; value=-60.0;
                minimum=-128.0; maximum=15.0; format="%.2f °dB"; streamIdentifier=1002 }
)
Save-DM "glow-types-strict" "1.0.0" (NodeOf 6 $glowRoot $glowRoot $glowOid $glowChildren)

# ---------------- 8. manifest assembling all DMs ----------------
$manifest = [ordered]@{
    device = [ordered]@{
        name     = "dhs-emberplus-integration"
        protocol = "emberplus"
        endpoints = @(
            [ordered]@{ ip = "0.0.0.0"; port = 9100; transport = "tcp" }
        )
    }
    frames = @(
        [ordered]@{
            name  = "main"
            slots = @(
                [ordered]@{ addr = [ordered]@{ oid = "1.0" }; dm = "identity-strict@1.0.0" }
                [ordered]@{ addr = [ordered]@{ oid = "1.1" }; dm = "oneToN-strict@1.0.0" }
                [ordered]@{ addr = [ordered]@{ oid = "1.2" }; dm = "oneToOne-strict@1.0.0" }
                [ordered]@{ addr = [ordered]@{ oid = "1.3" }; dm = "nToN-strict@1.0.0" }
                [ordered]@{ addr = [ordered]@{ oid = "1.4" }; dm = "dynamic-strict@1.0.0" }
                [ordered]@{ addr = [ordered]@{ oid = "1.5" }; dm = "functions-strict@1.0.0" }
                [ordered]@{ addr = [ordered]@{ oid = "1.6" }; dm = "glow-types-strict@1.0.0" }
            )
        }
    )
}
$manifestPath = Join-Path $manifestDir "emberplus-integration.json"
[System.IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json -Depth 30))
Write-Host "wrote $manifestPath ($((Get-Item $manifestPath).Length) bytes)"

Write-Host ""
Write-Host "DONE. Start producer with:"
Write-Host "  .\bin\dhs.exe producer emberplus serve --manifest .cache\manifest\emberplus-integration.json --port 9100 --log-level debug"
