# verify-emberplus-integration.ps1
#
# End-to-end verification of labels (Primary + Secondary), per-target /
# per-source signal params, and per-crosspoint params on an Ember+
# provider, driven by bin\dhs.exe consumer. No GUI required.
#
# Usage (matches the integration demo defaults):
#   .\scripts\verify-emberplus-integration.ps1
#
# With custom target:
#   .\scripts\verify-emberplus-integration.ps1 -Target 10.6.239.113 -Port 9100

param(
    [string]$Target    = "127.0.0.1",
    [int]   $Port      = 9100,
    [string]$Dm        = "dhs-emberplus-integration@1.0.0",
    [string]$RootIdent = "dhs-emberplus-integration"
)

$ErrorActionPreference = "Continue"
$dhs = Join-Path $PSScriptRoot "..\bin\dhs.exe"
if (-not (Test-Path $dhs)) {
    Write-Error "dhs.exe not found at $dhs"
    exit 2
}

$pass = 0
$fail = 0
$failed = New-Object System.Collections.ArrayList

# get-value: read a Parameter, return its string value (or $null on miss).
# set-value: write a value (best-effort, no return).
# Both are inline lambdas held in variables so the script stays one
# flat block — no nested-function gymnastics that trip PS 5.1.
$getVal = {
    param($p)
    # No --dm here: each call walks the wire so the read sees the
    # provider's live state (not the on-disk DM snapshot, which is a
    # frozen structure cache, not a live-value cache). Slower per call
    # (~2-3 s walk) but the only way to verify round-trip semantics
    # without a single-session integration runner.
    $out = & $dhs consumer emberplus get $Target --port $Port --path $p 2>$null
    foreach ($line in $out) {
        if ($line -match '^value\s*=\s*(.*)$') {
            $v = $Matches[1].Trim()
            if ($v -match '^"(.*)"$') { $v = $Matches[1] }
            return $v
        }
    }
    return $null
}
$setVal = {
    param($p, $v)
    & $dhs consumer emberplus set $Target --port $Port --path $p --value $v 2>$null | Out-Null
}

# step <name> <bool>: print PASS / FAIL line and tally.
function step([string]$name, [bool]$ok) {
    if ($ok) { Write-Host "  PASS  $name"; $script:pass++ }
    else     { Write-Host "  FAIL  $name" -ForegroundColor Red; $script:fail++; [void]$script:failed.Add($name) }
}

# roundtrip <label> <path> <marker>: read original, write marker, read
# back, restore. PASS when readback == marker.
function roundtrip([string]$label, [string]$path, [string]$marker) {
    $orig = & $getVal $path
    if ($null -eq $orig) { step "$label : roundtrip $path" $false; return }
    if ($orig -eq $marker) { $marker = "${marker}-alt" }
    & $setVal $path $marker
    $after = & $getVal $path
    & $setVal $path $orig
    step "$label : roundtrip $path" ($after -eq $marker)
}

# independence <label> <writePath> <readPath> <marker>: write to A,
# verify B unchanged.
function independence([string]$label, [string]$writePath, [string]$readPath, [string]$marker) {
    $origA = & $getVal $writePath
    $origB = & $getVal $readPath
    if ($null -eq $origA -or $null -eq $origB) { step "$label : independence" $false; return }
    if ($origA -eq $marker) { $marker = "${marker}-alt" }
    & $setVal $writePath $marker
    $bStill = & $getVal $readPath
    & $setVal $writePath $origA
    step "$label : write $writePath leaves $readPath unchanged" ($bStill -eq $origB)
}

# suite <matrix> <hasXPT> [<xptT> <xptS>]: run the standard tests
# against one matrix root.
function suite([string]$matrix, [bool]$hasXPT, [int]$xptT = 0, [int]$xptS = 0) {
    $root = "$RootIdent.$matrix"
    Write-Host ""
    Write-Host "==== $root ===="

    roundtrip "labels.Primary  T0"  "$root.labelsPrimary.targets.t-0"   "VERIFY-PRI-T0"
    roundtrip "labels.Secondary T0" "$root.labelsSecondary.targets.t-0" "VERIFY-SEC-T0"
    independence "labels" "$root.labelsSecondary.targets.t-0" "$root.labelsPrimary.targets.t-0" "ONLY-SEC"

    roundtrip "targetParams T0 gain" "$root.targetParams.0.gain" "-250"
    roundtrip "targetParams T0 mute" "$root.targetParams.0.mute" "true"
    roundtrip "sourceParams S0 gain" "$root.sourceParams.0.gain" "-100"
    roundtrip "sourceParams S0 mute" "$root.sourceParams.0.mute" "true"

    if ($hasXPT) {
        roundtrip "params XPT($xptT,$xptS) gain" "$root.params.$xptT.$xptS.gain" "-300"
    }
}

Write-Host ""
Write-Host "verify-emberplus-integration  target=$Target port=$Port dm=$Dm"

suite "oneToN"   $false
suite "oneToOne" $false
suite "nToN"     $true  0 0

Write-Host ""
Write-Host ("== Summary: PASS={0}  FAIL={1} ==" -f $pass, $fail)
if ($fail -gt 0) {
    Write-Host "Failed:"
    foreach ($n in $failed) { Write-Host "  - $n" }
    exit 1
}
exit 0
