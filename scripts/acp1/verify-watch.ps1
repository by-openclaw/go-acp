# scripts/acp1/verify-watch.ps1 - acp1 `watch` integration test.
#
# watch renders live UDP broadcast announces (MTID=0). Those are link-local
# broadcasts that do NOT route across subnets, so a REMOTE DMZ emulator may
# deliver none to this host. We therefore assert only that watch starts and
# subscribes; receiving a live announce is reported but treated as SKIP (not
# FAIL) when no announce arrives, since absence is a network-topology fact, not
# a consumer bug. Run on the same L2 as the device to exercise the live path.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs
$sel = @('--slot', '0', '--group', 'control', '--label', 'NetwPrefix')

$job = Start-Job -ScriptBlock {
    & $using:dhs consumer acp1 watch $using:t --slot 0 2>$null
}
Start-Sleep -Seconds 1
# Provoke a value-change announce by toggling a numeric object.
& $dhs consumer acp1 set $t @sel --value 7 2>$null | Out-Null
Start-Sleep -Seconds 2
Stop-Job $job
$txt = (Receive-Job $job) -join "`n"
Remove-Job $job -Force
& $dhs consumer acp1 set $t @sel --value 0 2>$null | Out-Null   # restore

Check 'watch starts and subscribes' ($txt -match 'watching') $txt
if ($txt -match 'NetwPrefix|s0\.control|live') {
    Check 'received a live announce' $true $txt
} else {
    Write-Host 'SKIP  no announce received (UDP broadcast not routed from remote emulator) - run on the same L2 to exercise'
}
Complete-Checks 'watch'
