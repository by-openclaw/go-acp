# scripts/acp1/verify-convert.ps1 - acp1 `convert` integration test (offline).
# Exports the device to JSON, then converts json -> yaml and asserts the output.
# `convert` is offline but reached via `dhs consumer acp1 convert`; this also
# guards the regression where it rejected the dispatcher-injected --protocol.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$json = Join-Path $env:TEMP "acp1-conv-$PID.json"
$yaml = Join-Path $env:TEMP "acp1-conv-$PID.yaml"

& $dhs consumer acp1 export $t --slot 0 --out $json 2>$null | Out-Null
Check 'export produced source json' (Test-Path $json) $json

& $dhs consumer acp1 convert --in $json --out $yaml 2>$null | Out-Null
Check 'convert exits 0 (accepts injected --protocol)' ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"
Check 'yaml output created'          (Test-Path $yaml) $yaml
Check 'yaml is non-empty'            ((Test-Path $yaml) -and (Get-Item $yaml).Length -gt 0) $yaml

Remove-Item $json, $yaml -ErrorAction SilentlyContinue
Complete-Checks 'convert'
