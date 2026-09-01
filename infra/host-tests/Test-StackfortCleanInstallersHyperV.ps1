# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [ValidatePattern('^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$')]
    [string] $Version = '0.0.0-dev',
    [string] $Checkpoint = 'stackfort-installer-ready-20260824',
    [string] $WafPackageDirectory,
    [string] $VinylPackageDirectory,
    [string] $DebPackagePath,
    [string] $RpmPackagePath,
    [switch] $SkipBuild
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if ([string]::IsNullOrWhiteSpace($WafPackageDirectory)) {
    $WafPackageDirectory = Join-Path $repositoryRoot 'infra\host-tests\work\waf-packages-coraza'
}
if ([string]::IsNullOrWhiteSpace($VinylPackageDirectory)) {
    $VinylPackageDirectory = Join-Path $repositoryRoot 'infra\host-tests\work\vinyl-packages'
}

$numericVersion = $Version -replace '[-+].*$', ''
$versionSuffix = $Version.Substring($numericVersion.Length)
$packageVersion = if ($versionSuffix.StartsWith('-')) {
    "$numericVersion~$($versionSuffix.Substring(1))"
} else {
    $Version
}
if ([string]::IsNullOrWhiteSpace($DebPackagePath)) {
    $DebPackagePath = Join-Path $repositoryRoot "infra\host-tests\work\core-packages\stackfort-release_$packageVersion-1_amd64.deb"
}
if ([string]::IsNullOrWhiteSpace($RpmPackagePath)) {
    $rpmVersion = $packageVersion.Replace('-', '_')
    $RpmPackagePath = Join-Path $repositoryRoot "infra\host-tests\work\core-packages\stackfort-release-$rpmVersion-1.sf1.x86_64.rpm"
}

$DebPackagePath = (Resolve-Path -LiteralPath $DebPackagePath).Path
$RpmPackagePath = (Resolve-Path -LiteralPath $RpmPackagePath).Path
$WafPackageDirectory = (Resolve-Path -LiteralPath $WafPackageDirectory).Path
$VinylPackageDirectory = (Resolve-Path -LiteralPath $VinylPackageDirectory).Path

$targets = @(
    @{ ImageId = 'debian-13'; VmName = 'stackfort-debian-13'; NativePackage = $DebPackagePath },
    @{ ImageId = 'ubuntu-26.04'; VmName = 'stackfort-ubuntu-26-04-v2'; NativePackage = $DebPackagePath },
    @{ ImageId = 'rocky-10'; VmName = 'stackfort-rocky-10'; NativePackage = $RpmPackagePath }
)
$methods = @('archive', 'bootstrap', 'native')
$results = [Collections.Generic.List[object]]::new()
$releaseBuilt = $SkipBuild.IsPresent

foreach ($target in $targets) {
    $vmName = $target.VmName
    if (-not (Get-VMCheckpoint -VMName $vmName -Name $Checkpoint -ErrorAction SilentlyContinue)) {
        throw "Clean installer checkpoint is unavailable for ${vmName}: $Checkpoint"
    }
    foreach ($method in $methods) {
        $vm = Get-VM -Name $vmName -ErrorAction Stop
        if ($vm.State -ne 'Off') {
            Stop-VM -Name $vmName -Force
        }
        Restore-VMCheckpoint -VMName $vmName -Name $Checkpoint -Confirm:$false
        try {
            $arguments = @{
                ImageId = $target.ImageId
                VmName = $vmName
                Version = $Version
                InstallMethod = $method
                WafPackageDirectory = $WafPackageDirectory
                VinylPackageDirectory = $VinylPackageDirectory
            }
            if ($releaseBuilt) {
                $arguments.SkipBuild = $true
            }
            if ($method -eq 'native') {
                $arguments.NativePackagePath = $target.NativePackage
            }
            $result = & (Join-Path $PSScriptRoot 'Test-StackfortInstallerHyperVVm.ps1') @arguments
            $results.Add($result)
            $releaseBuilt = $true
        } finally {
            $vm = Get-VM -Name $vmName -ErrorAction SilentlyContinue
            if ($vm -and $vm.State -ne 'Off') {
                Stop-VM -Name $vmName -Force
            }
        }
    }
}

$results
