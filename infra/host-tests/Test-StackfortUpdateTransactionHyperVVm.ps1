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
    [TimeSpan] $StartupTimeout = [TimeSpan]::FromMinutes(6),
    [switch] $SkipBuild
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$workRoot = Join-Path $repositoryRoot 'infra\host-tests\work'
$testBinary = Join-Path $workRoot 'stackfort-update-transaction.test'
if (-not $SkipBuild) {
    New-Item -ItemType Directory -Path $workRoot -Force | Out-Null
    $previousGoOS, $previousGoArch, $previousCgo = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = 'linux', 'amd64', '0'
        Push-Location $repositoryRoot
        try {
            & go.exe test -tags=integration -c -o $testBinary ./internal/updateapply
            if ($LASTEXITCODE -ne 0) { throw "Update transaction build failed with exit code $LASTEXITCODE." }
        } finally { Pop-Location }
    } finally {
        foreach ($item in @(
            @{ Name = 'GOOS'; Value = $previousGoOS },
            @{ Name = 'GOARCH'; Value = $previousGoArch },
            @{ Name = 'CGO_ENABLED'; Value = $previousCgo }
        )) {
            if ($null -eq $item.Value) { Remove-Item "Env:$($item.Name)" -ErrorAction SilentlyContinue }
            else { Set-Item "Env:$($item.Name)" $item.Value }
        }
    }
}
if (-not (Test-Path -LiteralPath $testBinary -PathType Leaf)) { throw "Test binary not found: $testBinary" }

$vm = Get-VM -Name $VmName -ErrorAction Stop
$startedHere = $vm.State -ne 'Running'
if ($startedHere) { Start-VM -Name $VmName | Out-Null }
try {
    $keyPath = Join-Path (Join-Path $VmRoot 'keys') 'stackfort-host-test-ed25519'
    if (-not (Test-Path -LiteralPath $keyPath -PathType Leaf)) { throw "SSH key not found: $keyPath" }
    $knownHosts = Join-Path $VmRoot 'known_hosts'
    $sshOptions = @(
        '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5',
        '-o', 'StrictHostKeyChecking=accept-new', '-o', "UserKnownHostsFile=$knownHosts",
        '-o', "HostKeyAlias=$VmName", '-o', 'LogLevel=ERROR', '-i', $keyPath
    )

    $deadline = (Get-Date).Add($StartupTimeout)
    $address = $null
    do {
        $address = (Get-VMNetworkAdapter -VMName $VmName).IPAddresses |
            Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } |
            Select-Object -First 1
        if ($address) {
            & ssh.exe @sshOptions "${GuestUser}@${address}" 'true' 2>$null
            if ($LASTEXITCODE -eq 0) { break }
        }
        $address = $null
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    if (-not $address) { throw "SSH did not become ready for $VmName." }

    $remoteBinary = '/var/tmp/stackfort-update-transaction.test'
    & scp.exe @sshOptions $testBinary "${GuestUser}@${address}:${remoteBinary}"
    if ($LASTEXITCODE -ne 0) { throw "Copying the update transaction test failed with exit code $LASTEXITCODE." }
    & ssh.exe @sshOptions "${GuestUser}@${address}" "chmod 0755 '$remoteBinary'"
    if ($LASTEXITCODE -ne 0) { throw 'Making the update transaction test executable failed.' }

    $output = & ssh.exe @sshOptions "${GuestUser}@${address}" `
        "sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 '$remoteBinary' -test.v -test.run '^(TestDisposableHostStagedUpdateTransaction|TestStagerRequiresCompleteImmutableDigestedAttestedRelease)$' -test.timeout=5m" 2>&1
    $exitCode = $LASTEXITCODE
    $output | Write-Output
    if ($exitCode -ne 0) { throw "Update transaction guest test failed with exit code $exitCode." }
    if (-not (($output -join "`n").Contains('STACKFORT_QUALIFICATION staged-update-transaction=passed'))) {
        throw 'Update transaction qualification marker is missing.'
    }

    [pscustomobject]@{
        VMName = $VmName; ImageId = $ImageId; Transaction = 'passed'
        Migration = 'passed'; HealthRollback = 'passed'; InterruptionRecovery = 'passed'
    }
} finally {
    if ($startedHere -and (Get-VM -Name $VmName).State -eq 'Running') {
        Stop-VM -Name $VmName -Force | Out-Null
    }
}
