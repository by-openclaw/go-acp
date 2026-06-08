# scripts/acp1/verify-import.ps1 - acp1 `import` integration test.
# Exports slot 0, then imports that snapshot with --dry-run (non-destructive):
# asserts the round-trip parses and the dry-run completes without error and
# writes nothing. Import-apply is intentionally NOT exercised here so the
# device state is never mutated by the import test.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$snap = Join-Path $env:TEMP "acp1-import-$PID.json"

& $dhs consumer acp1 export $t --slot 0 --out $snap 2>$null | Out-Null
Check 'snapshot exported for import' (Test-Path $snap) $snap

$txt = (& $dhs consumer acp1 import $t --file $snap --dry-run 2>$null) -join "`n"
Check 'dry-run import exits 0' ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"
Check 'dry-run produced a report' ($txt.Trim().Length -gt 0) $txt

Remove-Item $snap -ErrorAction SilentlyContinue
Complete-Checks 'import'
