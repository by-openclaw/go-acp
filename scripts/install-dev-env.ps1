# install-dev-env.ps1 - one-shot dev environment installer for acp
#
# Installs Go, Node.js, make, golangci-lint, GitHub CLI, and VS Code
# extensions needed to develop acp on Windows 11.
#
# Usage (from any PowerShell, normal or admin):
#   cd C:\Users\BY-SYSTEMSSRLBoujraf\Downloads\acp
#   .\scripts\install-dev-env.ps1
#
# If not elevated, it self-elevates via UAC.
# Idempotent - re-running skips already-installed tools.

$ErrorActionPreference = 'Continue'

# -------------------------------------------------------------------- Self-elevate

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "Not running as Administrator - relaunching elevated..." -ForegroundColor Yellow
    $scriptPath = $MyInvocation.MyCommand.Path
    $workDir    = Split-Path -Parent (Split-Path -Parent $scriptPath)
    Start-Process powershell -Verb RunAs -ArgumentList @(
        "-NoExit",
        "-ExecutionPolicy","Bypass",
        "-File",$scriptPath
    )
    exit 0
}

Write-Host ""
Write-Host "======================================================" -ForegroundColor Cyan
Write-Host "  acp dev environment installer" -ForegroundColor Cyan
Write-Host "======================================================" -ForegroundColor Cyan

# -------------------------------------------------------------------- Helper

function Test-Cmd($name) {
    return [bool](Get-Command $name -ErrorAction SilentlyContinue)
}

function Install-Tool($wingetId, $probeCmd, $displayName) {
    Write-Host ""
    Write-Host ">>> $displayName" -ForegroundColor Cyan
    if (Test-Cmd $probeCmd) {
        Write-Host "    SKIP  $probeCmd already on PATH" -ForegroundColor DarkGray
        return
    }
    Write-Host "    installing $wingetId ..." -ForegroundColor White
    $proc = Start-Process -FilePath "winget" -ArgumentList @(
        "install","--id",$wingetId,"-e",
        "--accept-source-agreements","--accept-package-agreements",
        "--silent"
    ) -Wait -PassThru -NoNewWindow
    if ($proc.ExitCode -eq 0) {
        Write-Host "    OK    $wingetId installed" -ForegroundColor Green
    } else {
        Write-Host "    WARN  winget exit code $($proc.ExitCode) for $wingetId" -ForegroundColor Yellow
    }
}

# -------------------------------------------------------------------- Sanity

Write-Host ""
Write-Host ">>> Checking prerequisites" -ForegroundColor Cyan

if (-not (Test-Cmd winget)) {
    Write-Host "    FAIL  winget not found" -ForegroundColor Red
    Write-Host "          Install 'App Installer' from the Microsoft Store and re-run."
    Read-Host "Press Enter to exit"
    exit 1
}
Write-Host "    OK    winget present" -ForegroundColor Green

# -------------------------------------------------------------------- Install

Install-Tool "Git.Git"                       "git"           "Git"
Install-Tool "GoLang.Go"                     "go"            "Go toolchain"
Install-Tool "OpenJS.NodeJS.LTS"             "node"          "Node.js LTS"
Install-Tool "GnuWin32.Make"                 "make"          "GNU Make"
Install-Tool "GitHub.cli"                    "gh"            "GitHub CLI"
Install-Tool "golangci-lint.golangci-lint"   "golangci-lint" "golangci-lint"

# -------------------------------------------------------------------- PATH refresh

Write-Host ""
Write-Host ">>> Refreshing PATH for this session" -ForegroundColor Cyan
$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
$userPath    = [Environment]::GetEnvironmentVariable('Path', 'User')
$env:Path    = "$machinePath;$userPath"
# Add user's go/bin for go-installed tools
$goBin = Join-Path $env:USERPROFILE "go\bin"
if ($env:Path -notlike "*$goBin*") {
    $env:Path = "$env:Path;$goBin"
}
Write-Host "    OK    PATH refreshed" -ForegroundColor Green

# -------------------------------------------------------------------- Go tools

Write-Host ""
Write-Host ">>> Installing Go-managed tools (goimports, delve)" -ForegroundColor Cyan
if (Test-Cmd go) {
    & go install golang.org/x/tools/cmd/goimports@latest 2>&1 | Out-Host
    & go install github.com/go-delve/delve/cmd/dlv@latest 2>&1 | Out-Host
    Write-Host "    OK    goimports + dlv installed into $goBin" -ForegroundColor Green
} else {
    Write-Host "    WARN  go not on PATH yet - open a NEW PowerShell after this script finishes, then run:" -ForegroundColor Yellow
    Write-Host "            go install golang.org/x/tools/cmd/goimports@latest"
    Write-Host "            go install github.com/go-delve/delve/cmd/dlv@latest"
}

# -------------------------------------------------------------------- VS Code extensions

Write-Host ""
Write-Host ">>> Installing VS Code extensions" -ForegroundColor Cyan
if (Test-Cmd code) {
    $extensions = @(
        "ms-vscode-remote.remote-containers",
        "golang.go",
        "dbaeumer.vscode-eslint",
        "esbenp.prettier-vscode",
        "ms-azuretools.vscode-docker",
        "eamodio.gitlens"
    )
    foreach ($ext in $extensions) {
        & code --install-extension $ext --force 2>&1 | Out-Null
        Write-Host "    OK    $ext" -ForegroundColor Green
    }
} else {
    Write-Host "    SKIP  VS Code 'code' command not on PATH" -ForegroundColor DarkGray
}

# -------------------------------------------------------------------- Verify

Write-Host ""
Write-Host ">>> Verifying installed versions" -ForegroundColor Cyan

$tools = @(
    @{ Name = "go";            Args = "version" },
    @{ Name = "node";          Args = "--version" },
    @{ Name = "npm";           Args = "--version" },
    @{ Name = "make";          Args = "--version" },
    @{ Name = "git";           Args = "--version" },
    @{ Name = "gh";            Args = "--version" },
    @{ Name = "golangci-lint"; Args = "--version" },
    @{ Name = "docker";        Args = "--version" },
    @{ Name = "code";          Args = "--version" }
)

foreach ($t in $tools) {
    $name = $t.Name
    if (Test-Cmd $name) {
        try {
            $ver = (& $name $t.Args 2>&1 | Select-Object -First 1)
            Write-Host ("    OK    {0,-15} {1}" -f $name, $ver) -ForegroundColor Green
        } catch {
            Write-Host "    WARN  $name installed but version check failed" -ForegroundColor Yellow
        }
    } else {
        Write-Host ("    MISS  {0,-15} not on PATH yet (open a new shell)" -f $name) -ForegroundColor Yellow
    }
}

# -------------------------------------------------------------------- Next steps

Write-Host ""
Write-Host "======================================================" -ForegroundColor Cyan
Write-Host "  Done" -ForegroundColor Cyan
Write-Host "======================================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  1. Close THIS PowerShell window."
Write-Host "  2. Open a NEW PowerShell so PATH fully refreshes."
Write-Host "  3. cd C:\Users\BY-SYSTEMSSRLBoujraf\Downloads\acp"
Write-Host "  4. Pick your workflow:"
Write-Host ""
Write-Host "     Devcontainer:   code ."
Write-Host "                     then click 'Reopen in Container' in the toast"
Write-Host ""
Write-Host "     Native:         go mod tidy"
Write-Host "                     make build"
Write-Host "                     make test"
Write-Host ""
Write-Host "Full instructions: runbook.md" -ForegroundColor Cyan
Write-Host ""
Read-Host "Press Enter to close this window"
