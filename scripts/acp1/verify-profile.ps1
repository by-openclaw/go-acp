# scripts/acp1/verify-profile.ps1 - acp1 `profile` integration test.
# Asserts the compliance profile reports objects walked + a classification.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs

$txt = (& $dhs consumer acp1 profile $t 2>$null) -join "`n"
Check 'reports objects walked'  ($txt -match 'objects walked\s+\d+') $txt
Check 'reports a classification' ($txt -match 'classification\s+\w+') $txt
Complete-Checks 'profile'
