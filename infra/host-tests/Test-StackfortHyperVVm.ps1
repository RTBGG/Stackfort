# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('debian-13', 'ubuntu-26.04', 'rocky-10')]
    [string] $ImageId,

    [ValidatePattern('^[a-z0-9][a-z0-9-]{2,62}$')]
    [string] $VmName,

    [string] $VmRoot = 'C:\ProgramData\Stackfort\Hyper-V',
    [TimeSpan] $StartupTimeout = [TimeSpan]::FromMinutes(5)
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ([string]::IsNullOrWhiteSpace($VmName)) {
    $VmName = "stackfort-$($ImageId.Replace('.', '-'))"
}
if ($VmName -notmatch '^[a-z0-9][a-z0-9-]{2,62}$') {
    throw 'VM name must contain only lower-case ASCII letters, digits, and hyphens.'
}
$vm = Get-VM -Name $VmName -ErrorAction Stop
if ($vm.State -ne 'Running') {
    Start-VM -Name $VmName
}

$keyPath = Join-Path (Join-Path $VmRoot 'keys') 'stackfort-host-test-ed25519'
if (-not (Test-Path -LiteralPath $keyPath -PathType Leaf)) {
    throw "Stackfort host-test SSH key not found: $keyPath"
}
$knownHosts = Join-Path $VmRoot 'known_hosts'
$sshOptions = @(
    '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5',
    '-o', 'StrictHostKeyChecking=accept-new', '-o', "UserKnownHostsFile=$knownHosts",
    '-o', "HostKeyAlias=$VmName", '-o', 'LogLevel=ERROR', '-i', $keyPath
)
$guestUser = 'stackfort-test'

$deadline = (Get-Date).Add($StartupTimeout)
$address = $null
do {
    $adapter = Get-VMNetworkAdapter -VMName $VmName
    $address = $adapter.IPAddresses |
        Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } |
        Select-Object -First 1
    if (-not $address) {
        $mac = $adapter.MacAddress -replace '(.{2})(?!$)', '$1-'
        $interfaceAlias = "vEthernet ($($adapter.SwitchName))"
        $address = Get-NetNeighbor -InterfaceAlias $interfaceAlias -AddressFamily IPv4 -ErrorAction SilentlyContinue |
            Where-Object { $_.LinkLayerAddress -eq $mac -and $_.State -ne 'Unreachable' } |
            Select-Object -ExpandProperty IPAddress -First 1
    }
    if (-not $address) {
        Start-Sleep -Seconds 2
    }
} while (-not $address -and (Get-Date) -lt $deadline)
if (-not $address) {
    throw "No IPv4 address was found for $VmName before the startup timeout."
}

$sshReady = $false
do {
    & ssh.exe @sshOptions "$guestUser@$address" 'true' 2>$null
    $sshReady = $LASTEXITCODE -eq 0
    if (-not $sshReady) {
        Start-Sleep -Seconds 2
    }
} while (-not $sshReady -and (Get-Date) -lt $deadline)
if (-not $sshReady) {
    throw "SSH did not become ready for $VmName before the startup timeout."
}

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$workRoot = Join-Path $PSScriptRoot 'work'
$testBinary = Join-Path $workRoot "stackfort-host-integration-$VmName.test"
$installerBinary = Join-Path $workRoot "stackfort-installer-$VmName"
New-Item -ItemType Directory -Path $workRoot -Force | Out-Null

$previousGoOS = $env:GOOS
$previousGoArch = $env:GOARCH
$previousCGoEnabled = $env:CGO_ENABLED
try {
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    Push-Location $repositoryRoot
    try {
        & go test -tags=integration -c -o $testBinary .\tests\integration
        if ($LASTEXITCODE -ne 0) {
            throw "Linux integration-test build failed with exit code $LASTEXITCODE."
        }
        & go build -trimpath -o $installerBinary .\cmd\stackfort-installer
        if ($LASTEXITCODE -ne 0) {
            throw "Linux installer build failed with exit code $LASTEXITCODE."
        }
    } finally {
        Pop-Location
    }
} finally {
    if ($null -eq $previousGoOS) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $previousGoOS }
    if ($null -eq $previousGoArch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $previousGoArch }
    if ($null -eq $previousCGoEnabled) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $previousCGoEnabled }
}

$capabilityScript = Join-Path $repositoryRoot 'scripts\host-capabilities.sh'
$nginxPreparationScript = Join-Path $repositoryRoot 'scripts\prepare-host-test-nginx.sh'
& scp.exe @sshOptions $capabilityScript "${guestUser}@${address}:/tmp/host-capabilities.sh"
if ($LASTEXITCODE -ne 0) {
    throw "Copying the host capability script failed with exit code $LASTEXITCODE."
}
& scp.exe @sshOptions $nginxPreparationScript "${guestUser}@${address}:/tmp/prepare-host-test-nginx.sh"
if ($LASTEXITCODE -ne 0) {
    throw "Copying the NGINX host-test preparation script failed with exit code $LASTEXITCODE."
}
& scp.exe @sshOptions $testBinary "${guestUser}@${address}:/tmp/stackfort-host-integration.test"
if ($LASTEXITCODE -ne 0) {
    throw "Copying the host integration test failed with exit code $LASTEXITCODE."
}
& scp.exe @sshOptions $installerBinary "${guestUser}@${address}:/tmp/stackfort-installer"
if ($LASTEXITCODE -ne 0) {
    throw "Copying the installer preflight failed with exit code $LASTEXITCODE."
}

$expected = @{
    'debian-13' = @('debian', '13')
    'ubuntu-26.04' = @('ubuntu', '26.04')
    'rocky-10' = @('rocky', '10')
}[$ImageId]
$remoteCommand = "sudo cloud-init status --wait --long && " +
    "sudo env STACKFORT_EXPECTED_OS_ID=$($expected[0]) STACKFORT_EXPECTED_VERSION_PREFIX=$($expected[1]) " +
    "STACKFORT_QUOTA_PATH=/srv/stackfort-quota bash /tmp/host-capabilities.sh && " +
    "chmod 0755 /tmp/stackfort-installer && sudo /tmp/stackfort-installer preflight --format=json && " +
    "sudo sh /tmp/prepare-host-test-nginx.sh && " +
    "chmod 0755 /tmp/stackfort-host-integration.test && " +
    "sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 /tmp/stackfort-host-integration.test -test.v"
& ssh.exe @sshOptions "$guestUser@$address" $remoteCommand
if ($LASTEXITCODE -ne 0) {
    throw "Disposable host validation failed with exit code $LASTEXITCODE."
}

[pscustomobject]@{
    VMName = $VmName
    ImageId = $ImageId
    IPv4 = $address
    CapabilityGate = 'passed'
    InstallerPreflight = 'passed'
    QuotaAndIsolation = 'passed'
    ResourceControl = 'passed'
    NGINXBaseline = 'passed'
    NGINXRenderer = 'passed'
    NGINXTransactionalActivation = 'passed'
    StaticDomainLifecycle = 'passed'
    CanonicalAndRedirectRouting = 'passed'
    ACMEHTTP01Routing = 'passed'
    TLSCertificateLifecycle = 'passed'
    PrivateAcmeAgentRPCLifecycle = 'passed'
}
