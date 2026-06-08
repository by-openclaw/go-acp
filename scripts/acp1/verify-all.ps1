# scripts/acp1/verify-all.ps1 - run the full acp1 consumer integration matrix.
#
# Runs every verify-<verb>.ps1 in this directory against the live ACP1 peer named
# by $env:ACP1_TEST_HOST (the Synapse Simulator or a real Axon device - the
# vendor oracle, NEVER our own provider). Each script is isolated in its own
# PowerShell process so one script's exit doesn't abort the run. Skips cleanly
# (exit 0) when ACP1_TEST_HOST is unset.
#
# Usage:
#   $env:ACP1_TEST_HOST = '10.6.239.113'
#   .\scripts\acp1\verify-all.ps1
if (-not $env:ACP1_TEST_HOST) {
    Write-Host 'SKIP: ACP1_TEST_HOST not set (point it at the Synapse emulator / device host)'
    exit 0
}

$scripts = Get-ChildItem -Path $PSScriptRoot -Filter 'verify-*.ps1' |
    Where-Object { $_.Name -ne 'verify-all.ps1' } |
    Sort-Object Name
$fail = 0
foreach ($s in $scripts) {
    Write-Host "`n=== $($s.Name) ==="
    & powershell -NoProfile -ExecutionPolicy Bypass -File $s.FullName
    if ($LASTEXITCODE -ne 0) { $fail++ }
}

Write-Host "`n========================================"
if ($fail -eq 0) {
    Write-Host 'acp1 integration: ALL SCRIPTS PASS'
    exit 0
}
Write-Host "acp1 integration: $fail SCRIPT(S) FAILED"
exit 1
