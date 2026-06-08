# scripts/acp1/verify-export.ps1 - acp1 `export` integration test.
# Dumps slot 0 to JSON and asserts it parses + carries device/protocol + slots.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$out = Join-Path $env:TEMP "acp1-export-$PID.json"

& $dhs consumer acp1 export $t --slot 0 --out $out 2>$null | Out-Null
Check 'export file created' (Test-Path $out) $out

$ok = $false; $hasProto = $false; $hasSlots = $false; $j = $null
if (Test-Path $out) {
    try {
        $j = Get-Content $out -Raw | ConvertFrom-Json
        $ok = $true
        $hasProto = ($j.device.protocol -eq 'acp1')
        $hasSlots = (@($j.slots).Count -ge 1)
    } catch {}
}
Check 'export is valid JSON'           $ok 'ConvertFrom-Json failed'
Check 'export device.protocol is acp1' $hasProto "protocol=$($j.device.protocol)"
Check 'export has at least one slot'   $hasSlots "slots=$(@($j.slots).Count)"

Remove-Item $out -ErrorAction SilentlyContinue
Complete-Checks 'export'
