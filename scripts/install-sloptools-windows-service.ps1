# Install or uninstall sloptools server as a Windows service via NSSM.
#
# Self-elevates to administrator. Listens on loopback TCP (no Unix sockets
# on Windows). Prompts for a service account so the daemon has access to
# user-scoped paths; cancel the prompt for LocalSystem.
#
# Usage:
#   .\scripts\install-sloptools-windows-service.ps1
#       [-Name sloptools] [-BinaryPath C:\path\sloptools.exe]
#       [-ProjectDir $env:USERPROFILE] [-DataDir $env:LOCALAPPDATA\sloppy]
#       [-Bind 127.0.0.1] [-Port 9420] [-Credential <PSCredential>]
#   .\scripts\install-sloptools-windows-service.ps1 -Uninstall [-Name sloptools]

[CmdletBinding()]
param(
    [switch]$Uninstall,
    [string]$Name = "sloptools",
    [string]$BinaryPath = (Join-Path $PSScriptRoot "..\sloptools.exe"),
    [string]$ProjectDir = $env:USERPROFILE,
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "sloppy"),
    [string]$Bind = "127.0.0.1",
    [int]$Port = 9420,
    [System.Management.Automation.PSCredential]$Credential
)

$ErrorActionPreference = "Stop"

function Test-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    return [Security.Principal.WindowsPrincipal]::new($id).IsInRole(
        [Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Invoke-SelfElevation {
    $argList = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $PSCommandPath)
    if ($Uninstall)  { $argList += '-Uninstall' }
    if ($Name)       { $argList += @('-Name',       $Name) }
    if ($BinaryPath) { $argList += @('-BinaryPath', $BinaryPath) }
    if ($ProjectDir) { $argList += @('-ProjectDir', $ProjectDir) }
    if ($DataDir)    { $argList += @('-DataDir',    $DataDir) }
    if ($Bind)       { $argList += @('-Bind',       $Bind) }
    if ($Port)       { $argList += @('-Port',       $Port) }
    $proc = Start-Process -FilePath 'powershell.exe' -ArgumentList $argList -Verb RunAs -Wait -PassThru
    exit $proc.ExitCode
}

if (-not (Test-Admin)) { Invoke-SelfElevation }

if (-not (Get-Command nssm.exe -ErrorAction SilentlyContinue)) {
    throw "nssm.exe not found on PATH. Install via 'winget install NSSM.NSSM' or download from https://nssm.cc/."
}

if ($Uninstall) {
    if (Get-Service -Name $Name -ErrorAction SilentlyContinue) {
        & nssm.exe stop   $Name | Out-Null
        & nssm.exe remove $Name confirm | Out-Null
        Write-Host "Removed service '$Name'"
    } else {
        Write-Host "Service '$Name' is not installed"
    }
    exit 0
}

if (-not (Test-Path -LiteralPath $BinaryPath)) {
    throw "sloptools binary not found at '$BinaryPath'. Build with: go build -o sloptools.exe .\cmd\sloptools"
}
$BinaryPath = (Resolve-Path -LiteralPath $BinaryPath).Path

New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
$logDir = Join-Path $env:LOCALAPPDATA "sloptools"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

if (-not $Credential) {
    Write-Host "Service account: cancel the prompt to run as LocalSystem (user-scoped state will be unavailable)."
    $Credential = Get-Credential -Message "Service account for '$Name' (cancel for LocalSystem)" -ErrorAction SilentlyContinue
}

if (Get-Service -Name $Name -ErrorAction SilentlyContinue) {
    & nssm.exe stop   $Name | Out-Null
    & nssm.exe remove $Name confirm | Out-Null
}

$appParameters = 'server --project-dir "{0}" --data-dir "{1}" --mcp-host "{2}" --mcp-port {3}' `
    -f $ProjectDir, $DataDir, $Bind, $Port

& nssm.exe install $Name $BinaryPath | Out-Null
& nssm.exe set     $Name AppParameters $appParameters | Out-Null
& nssm.exe set     $Name Start SERVICE_AUTO_START | Out-Null
& nssm.exe set     $Name AppStdout (Join-Path $logDir "sloptools.log") | Out-Null
& nssm.exe set     $Name AppStderr (Join-Path $logDir "sloptools.log") | Out-Null

if ($Credential) {
    $user = $Credential.UserName
    $pass = $Credential.GetNetworkCredential().Password
    & nssm.exe set $Name ObjectName $user $pass | Out-Null
    Write-Host "Service '$Name' will run as $user"
} else {
    Write-Host "Service '$Name' will run as LocalSystem"
}

Start-Service $Name
Write-Host "Started service '$Name' on http://${Bind}:${Port}"
