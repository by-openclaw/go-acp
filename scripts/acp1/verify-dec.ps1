# scripts/acp1/verify-dec.ps1 - acp1 `dec` integration test (setDecValue).
# Sets NetwPrefix (byte, 0..32, step 1) to a baseline, decrements by step, and
# asserts the device-confirmed value fell by one. Restores to 0.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$sel = @('--slot', '0', '--group', 'control', '--label', 'NetwPrefix')

& $dhs consumer acp1 set $t @sel --value 6 2>$null | Out-Null
$dec = (& $dhs consumer acp1 dec $t @sel 2>$null) -join "`n"
Check 'dec exits 0'                 ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"
Check 'dec confirms value-step (5)' ($dec -match 'confirmed.*\b5\b') $dec

& $dhs consumer acp1 set $t @sel --value 0 2>$null | Out-Null   # restore
Complete-Checks 'dec'
