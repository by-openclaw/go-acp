# scripts/acp1/verify-extract.ps1 - acp1 `extract` integration test.
# Captures a per-product DM triple (meta + wire + tree) from the device and
# asserts all three artifacts are produced.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$dir = Join-Path $env:TEMP "acp1-extract-$PID"

& $dhs consumer acp1 extract $t --manufacturer axon --product rrs18 `
    --direction consumer --version 1601 --out $dir --slot 0 2>$null | Out-Null
Check 'extract exits 0' ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"
Check 'meta.json produced' (Test-Path (Join-Path $dir 'meta.json')) $dir
Check 'tree.json produced' (Test-Path (Join-Path $dir 'tree.json')) $dir
$frames = (Test-Path (Join-Path $dir 'wire.jsonl')) -or (Test-Path (Join-Path $dir 'raw.acp1.jsonl'))
Check 'wire frames captured' $frames $dir

Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
Complete-Checks 'extract'
