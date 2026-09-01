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

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$workRoot = Join-Path $repositoryRoot 'infra\host-tests\work'
$testBinary = Join-Path $workRoot 'stackfort-oci-deployment.test'
if (-not $SkipBuild) {
    New-Item -ItemType Directory -Path $workRoot -Force | Out-Null
    $previousGoOS, $previousGoArch, $previousCgo = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = 'linux', 'amd64', '0'
        Push-Location $repositoryRoot
        try {
            & go.exe test -tags=integration -c -o $testBinary ./tests/integration
            if ($LASTEXITCODE -ne 0) { throw "OCI deployment integration build failed with exit code $LASTEXITCODE." }
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
    $sshReady = $false
    do {
        $adapter = Get-VMNetworkAdapter -VMName $VmName
        $address = $adapter.IPAddresses |
            Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } |
            Select-Object -First 1
        if ($address) {
            & ssh.exe @sshOptions "${GuestUser}@${address}" 'true' 2>$null
            $sshReady = $LASTEXITCODE -eq 0
        }
        if (-not $sshReady) { Start-Sleep -Seconds 2 }
    } while (-not $sshReady -and (Get-Date) -lt $deadline)
    if (-not $sshReady) { throw "SSH did not become ready for $VmName." }

    $packageCommand = switch ($ImageId) {
        'debian-13' { 'sudo env DEBIAN_FRONTEND=noninteractive apt-get update -qq && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq podman uidmap slirp4netns fuse-overlayfs passt' }
        'ubuntu-26.04' { 'sudo env DEBIAN_FRONTEND=noninteractive apt-get update -qq && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq podman uidmap slirp4netns fuse-overlayfs passt' }
        'rocky-10' { 'sudo dnf install -y -q podman shadow-utils-subid slirp4netns fuse-overlayfs passt' }
    }
    & ssh.exe @sshOptions "${GuestUser}@${address}" $packageCommand
    if ($LASTEXITCODE -ne 0) { throw "Preparing the rootless OCI runtime failed with exit code $LASTEXITCODE." }

    $remoteBinary = '/tmp/stackfort-oci-deployment.test'
    & scp.exe @sshOptions $testBinary "${GuestUser}@${address}:${remoteBinary}"
    if ($LASTEXITCODE -ne 0) { throw "Copying the OCI deployment test failed with exit code $LASTEXITCODE." }
    $output = & ssh.exe @sshOptions "${GuestUser}@${address}" `
        "chmod 0755 '$remoteBinary' && sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 '$remoteBinary' -test.v -test.run '^TestDisposableHostOCIDeploymentLifecycle$' -test.timeout=12m" 2>&1
    $exitCode = $LASTEXITCODE
    $output | Write-Output
    if ($exitCode -ne 0) { throw "OCI deployment qualification failed with exit code $exitCode." }
    $text = $output -join "`n"
    if (-not $text.Contains('STACKFORT_QUALIFICATION oci-deployment-lifecycle=passed')) {
        throw 'OCI deployment qualification marker is missing.'
    }
    [pscustomobject]@{ VMName = $VmName; ImageId = $ImageId; OCIDeployment = 'passed' }
} finally {
    if ($startedHere -and (Get-VM -Name $VmName).State -eq 'Running') {
        Stop-VM -Name $VmName -Force | Out-Null
    }
}
