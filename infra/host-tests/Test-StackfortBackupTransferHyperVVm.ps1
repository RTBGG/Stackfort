# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('debian-13', 'ubuntu-26.04', 'rocky-10')]
    [string] $ImageId,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[a-z0-9][a-z0-9-]{2,62}$')]
    [string] $VmName,

    [string] $VmRoot = 'C:\ProgramData\Stackfort\Hyper-V',
    [string] $GuestUser = 'stackfort-test',
    [TimeSpan] $StartupTimeout = [TimeSpan]::FromMinutes(5),
    [switch] $SkipBuild
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$arguments = @{
    ImageId = $ImageId
    VmName = $VmName
    VmRoot = $VmRoot
    GuestUser = $GuestUser
    StartupTimeout = $StartupTimeout
    SkipBuild = $SkipBuild
}
$output = & (Join-Path $PSScriptRoot 'Test-StackfortLocalBackupsHyperVVm.ps1') @arguments
$output | Write-Output
$text = $output | Out-String
if (-not $text.Contains('STACKFORT_QUALIFICATION backup-transfer-retention=passed')) {
    throw 'K-007 backup transfer/retention qualification marker is missing.'
}
