# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidatePattern('^stackfort-[a-z0-9][a-z0-9-]{2,53}$')]
    [string] $VmName,

    [string] $VmRoot = 'C:\ProgramData\Stackfort\Hyper-V',
    [switch] $Force
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Removing a Hyper-V VM requires an elevated PowerShell session.'
}

$vm = Get-VM -Name $VmName -ErrorAction Stop
$vmsRoot = [IO.Path]::GetFullPath((Join-Path $VmRoot 'vms'))
$vmDirectory = [IO.Path]::GetFullPath((Join-Path $vmsRoot $VmName))
if (-not $vmDirectory.StartsWith($vmsRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Resolved VM directory is outside the Stackfort VM root.'
}
$configurationPath = [IO.Path]::GetFullPath($vm.Path)
if (-not $configurationPath.StartsWith($vmDirectory + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Hyper-V configuration path is outside the expected Stackfort VM directory.'
}
if ($Force) {
    $ConfirmPreference = 'None'
}
if (-not $PSCmdlet.ShouldProcess($VmName, "Remove VM registration and $vmDirectory")) {
    return
}

if ($vm.State -ne 'Off') {
    Stop-VM -Name $VmName
    $deadline = (Get-Date).AddMinutes(1)
    do {
        $state = (Get-VM -Name $VmName).State
        if ($state -eq 'Off') { break }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    if ($state -ne 'Off') {
        throw 'VM did not shut down cleanly; no registration or files were removed.'
    }
}
Remove-VM -Name $VmName -Force
if (Test-Path -LiteralPath $vmDirectory) {
    Remove-Item -LiteralPath $vmDirectory -Recurse -Force
}
Write-Output "Removed disposable VM $VmName. Shared verified images, VHDX bases, and SSH keys were retained."
