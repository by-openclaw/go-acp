# Builds bin/dhs.exe for Windows (amd64) with version metadata, then
# stages an empty portable layout (logs/ + captures/) next to the
# binary so a fresh deploy on a Cerebrum host has nothing left to
# create on first run.
#
# Usage:
#   pwsh ./scripts/build-windows.ps1
#   pwsh ./scripts/build-windows.ps1 -Output C:\dhs       # custom dest dir
#   pwsh ./scripts/build-windows.ps1 -Zip                  # also produce dhs-windows-amd64.zip
#
# Run from the repo root.

param(
    [string]$Output = "bin",
    [switch]$Zip
)

$ErrorActionPreference = "Stop"

# Resolve repo root (this script lives in <repo>/scripts/).
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $RepoRoot

# Version metadata: prefer the latest annotated tag, fall back to commit hash.
$gitTag    = (& git describe --tags --abbrev=0 2>$null)
if (-not $gitTag) { $gitTag = "v0.0.0-dev" }
$gitCommit = (& git rev-parse --short HEAD)
$buildDate = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssK")

$ldflags = "-X main.version=$gitTag -X main.commit=$gitCommit -X main.date=$buildDate -X main.gitTag=$gitTag"

# Ensure output dir exists.
$null = New-Item -ItemType Directory -Force -Path $Output
$exePath = Join-Path $Output "dhs.exe"

Write-Host "building dhs.exe ..."
Write-Host "  output:    $exePath"
Write-Host "  version:   $gitTag"
Write-Host "  commit:    $gitCommit"
Write-Host "  date:      $buildDate"

$env:GOOS    = "windows"
$env:GOARCH  = "amd64"
$env:CGO_ENABLED = "0"

& go build -trimpath -ldflags $ldflags -o $exePath ./cmd/dhs
if ($LASTEXITCODE -ne 0) {
    Write-Error "go build failed (exit $LASTEXITCODE)"
    exit 1
}

# Stage the portable layout next to the binary so a fresh deploy has
# nothing left to create at runtime: logs/ + captures/.
$null = New-Item -ItemType Directory -Force -Path (Join-Path $Output "logs")
$null = New-Item -ItemType Directory -Force -Path (Join-Path $Output "captures")
$null = New-Item -ItemType Directory -Force -Path (Join-Path $Output "captures\xml")
$null = New-Item -ItemType Directory -Force -Path (Join-Path $Output "captures\pcap")

Write-Host ""
Write-Host "binary: $exePath"
Write-Host "portable layout staged: logs/ captures/{xml,pcap}/"

if ($Zip) {
    $zipPath = Join-Path $RepoRoot "dhs-windows-amd64.zip"
    if (Test-Path $zipPath) { Remove-Item $zipPath -Force }
    Compress-Archive -Path (Join-Path $Output "*") -DestinationPath $zipPath -Force
    Write-Host "zip:    $zipPath"
}

Write-Host ""
Write-Host "Deploy on a Cerebrum host:"
Write-Host "  1. Copy the contents of '$Output\' to e.g. C:\dhs\"
Write-Host "  2. Set credentials (PowerShell):"
Write-Host "       `$env:DHS_CEREBRUM_USER = 'admin'"
Write-Host "       `$env:DHS_CEREBRUM_PASS = 's3cr3t'"
Write-Host "  3. Connect and listen:"
Write-Host "       C:\dhs\dhs.exe consumer cerebrum-nb listen 127.0.0.1"
