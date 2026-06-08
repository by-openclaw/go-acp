# scripts/acp1/verify-walk.ps1 - acp1 `walk` integration test.
# Asserts the slot enumeration: groups + a known object + an object count.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs

$txt = (& $dhs consumer acp1 walk $t --slot 0 2>$null) -join "`n"
Check 'shows control group'    ($txt -match '\[control\]') $txt
Check 'shows identity group'   ($txt -match '\[identity\]') $txt
Check 'lists Broadcasts object' ($txt -match 'Broadcasts') $txt
Check 'reports an object count' ($txt -match '\d+ objects') $txt
Complete-Checks 'walk'
