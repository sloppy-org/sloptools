# Register sloptools server as a per-user Scheduled Task that runs at logon.
#
# No admin required, no third-party tools. Runs in the current user's
# session so user state (%USERPROFILE%, Bitwarden) is available.
# Auto-restarts on failure.
#
# Usage:
#   .\scripts\install-sloptools-windows-service.ps1
#       [-Name sloptools] [-BinaryPath C:\path\sloptools.exe]
#       [-ProjectDir $env:USERPROFILE] [-DataDir $env:LOCALAPPDATA\sloppy]
#       [-Bind 127.0.0.1] [-Port 9420]
#   .\scripts\install-sloptools-windows-service.ps1 -Uninstall [-Name sloptools]

[CmdletBinding()]
param(
    [switch]$Uninstall,
    [string]$Name = "sloptools",
    [string]$BinaryPath = (Join-Path $PSScriptRoot "..\sloptools.exe"),
    [string]$ProjectDir = $env:USERPROFILE,
    [string]$DataDir = (Join-Path $env:LOCALAPPDATA "sloppy"),
    [string]$Bind = "127.0.0.1",
    [int]$Port = 9420
)

$ErrorActionPreference = "Stop"
$taskName = "sloptools-$Name"
$userId   = "$env:USERDOMAIN\$env:USERNAME"

if ($Uninstall) {
    if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
        Stop-ScheduledTask       -TaskName $taskName -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
        Write-Host "Removed task '$taskName'"
    } else {
        Write-Host "Task '$taskName' is not registered"
    }
    exit 0
}

if (-not (Test-Path -LiteralPath $BinaryPath)) {
    throw "sloptools binary not found at '$BinaryPath'. Build with: go build -o sloptools.exe .\cmd\sloptools"
}
$BinaryPath = (Resolve-Path -LiteralPath $BinaryPath).Path

New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

$arguments = 'server --project-dir "{0}" --data-dir "{1}" --mcp-host "{2}" --mcp-port {3}' `
    -f $ProjectDir, $DataDir, $Bind, $Port

$action = New-ScheduledTaskAction `
    -Execute          $BinaryPath `
    -Argument         $arguments `
    -WorkingDirectory (Split-Path -Parent $BinaryPath)

$trigger = New-ScheduledTaskTrigger -AtLogOn -User $userId

$principal = New-ScheduledTaskPrincipal `
    -UserId    $userId `
    -LogonType Interactive `
    -RunLevel  Limited

$settings = New-ScheduledTaskSettingsSet `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -StartWhenAvailable `
    -RestartCount 999 `
    -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit ([TimeSpan]::Zero)

if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
    Stop-ScheduledTask       -TaskName $taskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
}

Register-ScheduledTask `
    -TaskName  $taskName `
    -Action    $action `
    -Trigger   $trigger `
    -Principal $principal `
    -Settings  $settings | Out-Null

Start-ScheduledTask -TaskName $taskName
Write-Host "Registered '$taskName' on http://${Bind}:${Port} (auto-starts at logon)"
