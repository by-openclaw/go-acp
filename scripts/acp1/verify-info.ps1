# scripts/acp1/verify-info.ps1 - acp1 `info` integration test.
# Asserts the device-info reply: protocol, slot count, per-slot status.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs

$txt = (& $dhs consumer acp1 info $t 2>$null) -join "`n"
Check 'reports protocol acp1' ($txt -match 'protocol\s+acp1') $txt
Check 'reports a slot count'  ($txt -match 'slots\s+\d+') $txt
Check 'lists per-slot status' ($txt -match 'per-slot status') $txt
Check 'slot 0 is present'     ($txt -match 'slot\s+0\s+status=present') $txt
Complete-Checks 'info'
