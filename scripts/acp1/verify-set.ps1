# scripts/acp1/verify-set.ps1 - acp1 `set` integration test.
# Round-trips a numeric object (NetwPrefix, range 0..32): set -> confirm ->
# read back -> restore to 0. Leaves the device as it was found.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$sel = @('--slot', '0', '--group', 'control', '--label', 'NetwPrefix')

$set = (& $dhs consumer acp1 set $t @sel --value 5 2>$null) -join "`n"
Check 'set confirms the new value' ($set -match 'confirmed.*\b5\b') $set

$get = (& $dhs consumer acp1 get $t @sel 2>$null) -join "`n"
Check 'get reflects the set value' ($get -match 'value\s*=\s*5\b') $get

& $dhs consumer acp1 set $t @sel --value 0 2>$null | Out-Null   # restore
$get2 = (& $dhs consumer acp1 get $t @sel 2>$null) -join "`n"
Check 'restored to 0' ($get2 -match 'value\s*=\s*0\b') $get2
Complete-Checks 'set'
