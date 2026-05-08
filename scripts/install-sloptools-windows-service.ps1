# Install sloptools server as a Windows service.
#
# Requires NSSM (https://nssm.cc/) on PATH. Run from an elevated PowerShell.
#
# Usage:
#   .\scripts\install-sloptools-windows-service.ps1 \
#       [-Name sloptools] [-BinaryPath C:\path\sloptools.exe] \
#       [-ProjectDir $env:USERPROFILE] [-DataDir $env:LOCALAPPDATA\sloppy] \
#       [-Bind 127.0.0.1] [-Port 8091]
#
# Listens on loopback TCP (Unix sockets are not available on Windows).

param(
    [string]$Name = "sloptools",
    [string]$BinaryPath = "$PSScriptRoot\..\sloptools.exe",
    [string]$ProjectDir = $env:USERPROFILE,
    [string]$DataDir = "$env:LOCALAPPDATA\sloppy",
    [string]$Bind = "127.0.0.1",
    [int]$Port = 9420
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command nssm.exe -ErrorAction SilentlyContinue)) {
    throw "nssm.exe not found on PATH. Install via 'winget install NSSM.NSSM' or download from https://nssm.cc/."
}

$BinaryPath = (Resolve-Path $BinaryPath).Path
if (-not (Test-Path $BinaryPath)) {
    throw "sloptools binary not found at $BinaryPath. Build with: go build -o sloptools.exe .\cmd\sloptools"
}

New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
$logDir = "$env:LOCALAPPDATA\sloptools"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

if (Get-Service -Name $Name -ErrorAction SilentlyContinue) {
    nssm.exe stop $Name | Out-Null
    nssm.exe remove $Name confirm | Out-Null
}

nssm.exe install $Name $BinaryPath server `
    --project-dir $ProjectDir `
    --data-dir $DataDir `
    --mcp-host $Bind `
    --mcp-port $Port | Out-Null
nssm.exe set $Name Start SERVICE_AUTO_START | Out-Null
nssm.exe set $Name AppStdout "$logDir\sloptools.log" | Out-Null
nssm.exe set $Name AppStderr "$logDir\sloptools.log" | Out-Null

Start-Service $Name
Write-Host "Installed and started service '$Name' on http://${Bind}:${Port}"
