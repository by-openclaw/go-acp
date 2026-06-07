# scripts/acp1/verify-ensure.ps1 - repeatable acp1 `ensure` integration test.
#
# Drives the real dhs CLI against a live ACP1 device/emulator and asserts the
# idempotency contract (read -> set-if-different -> changed; run-twice = 0
# changes). Per ADR-0025 deliverable 3 + docs/protocols/verb-tests.md tier 3.
#
# Oracle: a real ACP1 device or the Synapse Simulator - NOT our own provider.
#
# Usage:
#   $env:ACP1_TEST_HOST = '10.6.239.113'
#   .\scripts\acp1\verify-ensure.ps1
# Skips (exit 0) when ACP1_TEST_HOST is unset.

$target = $env:ACP1_TEST_HOST
if (-not $target) { Write-Host 'SKIP: ACP1_TEST_HOST not set'; exit 0 }

$dhs = Join-Path $PSScriptRoot '..\..\bin\dhs.exe'
if (-not (Test-Path $dhs)) {
    Write-Error "dhs binary not found at $dhs (build: go build -o bin/dhs.exe ./cmd/dhs)"
    exit 1
}

$sel  = @('--slot', '0', '--group', 'control', '--label', 'Broadcasts')
$fail = 0
function Check($name, $cond, $detail) {
    if ($cond) { Write-Host "PASS  $name" }
    else { Write-Host "FAIL  $name`n      got: $detail"; $script:fail++ }
}

# 1. dry-run: already On -> would_change=false, no write
$r = & $dhs consumer acp1 ensure $target @sel --value On --check --json 2>$null
Check 'check On is a no-op (would_change=false)' ($r -match '"would_change":false') $r

# 2. apply On while already On -> changed=false (idempotent no-op)
$r = & $dhs consumer acp1 ensure $target @sel --value On --json 2>$null
Check 'apply On already converged (changed=false)' ($r -match '"changed":false') $r

# 3. apply Off -> changed=true
$r = & $dhs consumer acp1 ensure $target @sel --value Off --json 2>$null
Check 'apply Off changes the value (changed=true)' ($r -match '"changed":true') $r

# 4. apply Off again -> changed=false  (the idempotency proof: run-twice = 0 changes)
$r = & $dhs consumer acp1 ensure $target @sel --value Off --json 2>$null
Check 'apply Off twice is idempotent (changed=false)' ($r -match '"changed":false') $r

# 5. bad enum value -> client-side validation rejects it with exit 2 (NOT a wire
#    write, NOT exit 1). This is what Ansible's failed_when keys on.
& $dhs consumer acp1 ensure $target @sel --value Bogus 2>$null | Out-Null
Check 'bad enum value rejected with exit 2' ($LASTEXITCODE -eq 2) "exit=$LASTEXITCODE"

# 6. --check with a bad value also rejects (validation runs before the dry-run
#    report, so a dry-run cannot mask bad input).
& $dhs consumer acp1 ensure $target @sel --value Bogus --check 2>$null | Out-Null
Check 'bad value rejected even under --check (exit 2)' ($LASTEXITCODE -eq 2) "exit=$LASTEXITCODE"

# restore to On (leave the device as we found it)
& $dhs consumer acp1 ensure $target @sel --value On 2>$null | Out-Null

if ($fail -eq 0) { Write-Host "`nensure: ALL PASS"; exit 0 }
Write-Host "`nensure: $fail FAILED"; exit 1
