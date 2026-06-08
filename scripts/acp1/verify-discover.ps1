# scripts/acp1/verify-discover.ps1 - acp1 `discover` integration test.
# Active+passive subnet scan; asserts it finds the test host.
. "$PSScriptRoot\_lib.ps1"
$t = Resolve-Acp1Target
$dhs = Resolve-Dhs

$out = (& $dhs consumer acp1 discover --duration 4s 2>$null) -join "`n"
Check 'discover exits 0'              ($LASTEXITCODE -eq 0) "exit=$LASTEXITCODE"
Check 'reports a device count'        ($out -match '\d+ device') $out
Check 'discovers the test host'       ($out -match [regex]::Escape($t)) $out
Complete-Checks 'discover'
