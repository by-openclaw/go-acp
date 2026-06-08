# scripts/acp1/_lib.ps1 - shared helpers for the acp1 consumer integration
# scripts (verify-<verb>.ps1). Dot-source it:  . "$PSScriptRoot\_lib.ps1"
#
# Every script drives the real dhs CLI against a live ACP1 peer named by
# $env:ACP1_TEST_HOST - the Synapse Simulator or a real Axon device, i.e. the
# VENDOR oracle. Never point these at our own provider (ADR / oracle rule:
# a consumer is proven against ground truth, not against our producer).
# Scripts skip cleanly (exit 0) when ACP1_TEST_HOST is unset, so CI stays green
# without a device while the suite is still runnable on demand over VPN.

$script:Acp1Fail = 0

# Resolve-Acp1Target returns the test host or skips (exit 0) when unset.
function Resolve-Acp1Target {
    if (-not $env:ACP1_TEST_HOST) {
        Write-Host 'SKIP: ACP1_TEST_HOST not set'
        exit 0
    }
    return $env:ACP1_TEST_HOST
}

# Resolve-Dhs returns the path to the built dhs binary or fails (exit 1).
function Resolve-Dhs {
    $dhs = Join-Path $PSScriptRoot '..\..\bin\dhs.exe'
    if (-not (Test-Path $dhs)) {
        Write-Error "dhs binary not found at $dhs (build: go build -o bin/dhs.exe ./cmd/dhs)"
        exit 1
    }
    return $dhs
}

# Check records one assertion: PASS when $cond is true, else FAIL (+ detail).
function Check([string]$name, [bool]$cond, $detail) {
    if ($cond) {
        Write-Host "PASS  $name"
    } else {
        Write-Host "FAIL  $name`n      got: $detail"
        $script:Acp1Fail++
    }
}

# Complete-Checks prints the summary and exits 0 (all pass) or 1 (any fail).
function Complete-Checks([string]$verb) {
    if ($script:Acp1Fail -eq 0) {
        Write-Host "`n${verb}: ALL PASS"
        exit 0
    }
    Write-Host "`n${verb}: $script:Acp1Fail FAILED"
    exit 1
}
