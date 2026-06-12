# scripts/acp1/verify-inc.ps1 - acp1 `inc` integration test (setIncValue).
# Sets NetwPrefix (byte, 0..32, step 1) to a baseline, increments by step, and
# asserts the device-confirmed value rose by one. Restores to 0.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$sel = @('--slot', '0', '--group', 'control', '--label', 'NetwPrefix')

& $dhs consumer acp1 set $t @sel --value 5 2>$null | Out-Null
$inc = (& $dhs consumer acp1 inc $t @sel 2>$null) -join "`n"
Check 'inc exits 0'                 ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"
Check 'inc confirms value+step (6)' ($inc -match 'confirmed.*\b6\b') $inc

& $dhs consumer acp1 set $t @sel --value 0 2>$null | Out-Null   # restore
Complete-Checks 'inc'
