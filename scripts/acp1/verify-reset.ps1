# scripts/acp1/verify-reset.ps1 - acp1 `reset` integration test (setDefValue).
# Sets NetwPrefix (byte, default 0) off its default, resets, and asserts the
# device-confirmed value is the declared default.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$sel = @('--slot', '0', '--group', 'control', '--label', 'NetwPrefix')

& $dhs consumer acp1 set $t @sel --value 7 2>$null | Out-Null
$rst = (& $dhs consumer acp1 reset $t @sel 2>$null) -join "`n"
Check 'reset exits 0'              ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"
Check 'reset confirms default (0)' ($rst -match 'confirmed.*\b0\b') $rst

& $dhs consumer acp1 set $t @sel --value 0 2>$null | Out-Null   # restore
Complete-Checks 'reset'
