# scripts/acp1/verify-validate.ps1 - acp1 `validate` integration test (offline).
# Captures wire frames (via extract), then decodes them back through the codec
# offline (ADR-0021) and asserts a clean decode. Reached via the consumer
# dispatcher.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$dir = Join-Path $env:TEMP "acp1-validate-$PID"

& $dhs consumer acp1 extract $t --manufacturer axon --product rrs18 `
    --direction consumer --version 1601 --out $dir --slot 0 2>$null | Out-Null
$frames = Join-Path $dir 'wire.jsonl'
if (-not (Test-Path $frames)) { $frames = Join-Path $dir 'raw.acp1.jsonl' }
Check 'wire frames captured for validate' (Test-Path $frames) $frames

$out = (& $dhs consumer acp1 validate $frames 2>$null) -join "`n"
Check 'validate exits 0'        ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"
Check 'reports decoded trames'  ($out -match '\d+ trames decoded') $out

Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
Complete-Checks 'validate'
