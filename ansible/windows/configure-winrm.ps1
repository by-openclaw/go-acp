<#
.SYNOPSIS
  Configure WinRM for Ansible on a Windows host (Windows 11 or Windows Server).
  Idempotent - safe to re-run. Run ONCE per host from an ELEVATED PowerShell.

.DESCRIPTION
  Enables the standard NTLM/Negotiate-over-HTTP (port 5985) path that Ansible's
  `winrm` connection uses (matches inventory/group_vars/windows.yml:
  ansible_connection=winrm, transport=ntlm, port 5985).

  The non-obvious requirement for a LOCAL account (workgroup / non-domain) is
  LocalAccountTokenFilterPolicy=1. Without it, Windows UAC strips a local
  admin's token on *network* logon, and WinRM rejects the authentication
  (HTTP 401 / SSPI 0x8009030d) even though the password is correct and
  interactive login works. This was the exact blocker on dhs-win11.

  NTLM over HTTP is message-encrypted by pywinrm on the wire. For a hardened
  setup prefer HTTPS (5986) + a real certificate; pass -AllowUnencrypted only
  if you knowingly need plaintext (not recommended).

.EXAMPLE
  # On the Windows target, elevated:
  powershell -ExecutionPolicy Bypass -File .\configure-winrm.ps1

  # Then from the Ansible control node:
  ansible <host> -i inventory/hosts.ini -m ansible.windows.win_ping
#>
[CmdletBinding()]
param(
    [switch]$AllowUnencrypted
)
$ErrorActionPreference = 'Stop'

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)
if (-not $isAdmin) { throw 'Run this from an elevated (Administrator) PowerShell.' }

Write-Host '== Enable PS remoting: WinRM service + HTTP listener + firewall =='
Enable-PSRemoting -Force -SkipNetworkProfileCheck | Out-Null

Write-Host '== Allow Negotiate (NTLM) auth on the WinRM service =='
Set-Item -Path WSMan:\localhost\Service\Auth\Negotiate -Value $true

Write-Host '== LocalAccountTokenFilterPolicy=1 (local-admin network logon) =='
$sys = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System'
New-ItemProperty -Path $sys -Name LocalAccountTokenFilterPolicy -Value 1 -PropertyType DWord -Force | Out-Null

if ($AllowUnencrypted) {
    Write-Host '== AllowUnencrypted=true (NOT recommended; NTLM already encrypts payload) =='
    Set-Item -Path WSMan:\localhost\Service\AllowUnencrypted -Value $true
}

Write-Host '== Headroom for Ansible modules (MaxMemoryPerShellMB) =='
Set-Item -Path WSMan:\localhost\Shell\MaxMemoryPerShellMB -Value 1024

Restart-Service WinRM

Write-Host ''
Write-Host 'WinRM is configured for Ansible (NTLM/Negotiate over HTTP 5985).'
Write-Host 'Verify from the control node:'
Write-Host '  ansible <host> -i inventory/hosts.ini -m ansible.windows.win_ping'
