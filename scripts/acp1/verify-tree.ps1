# scripts/acp1/verify-tree.ps1 - acp1 `tree` integration test.
# Asserts the ASCII tree render shows branches + the control group.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs

$txt = (& $dhs consumer acp1 tree $t --slot 0 2>$null) -join "`n"
Check 'renders tree branches'  ($txt -match '\+--') $txt
Check 'includes control group' ($txt -match '\+--\s*control') $txt
Complete-Checks 'tree'
