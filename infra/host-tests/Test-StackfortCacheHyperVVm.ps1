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
$testBinary = Join-Path $workRoot 'stackfort-cache.test'
if (-not $SkipBuild) {
    New-Item -ItemType Directory -Path $workRoot -Force | Out-Null
    $previousGoOS, $previousGoArch, $previousCgo = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = 'linux', 'amd64', '0'
        Push-Location $repositoryRoot
        try {
            & go.exe test -tags=integration -c -o $testBinary ./tests/integration
            if ($LASTEXITCODE -ne 0) { throw "Cache integration build failed with exit code $LASTEXITCODE." }
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
        if (-not $address) {
            $mac = $adapter.MacAddress -replace '(.{2})(?!$)', '$1-'
            $interfaceAlias = "vEthernet ($($adapter.SwitchName))"
            $address = Get-NetNeighbor -InterfaceAlias $interfaceAlias -AddressFamily IPv4 -ErrorAction SilentlyContinue |
                Where-Object { $_.LinkLayerAddress -eq $mac -and $_.State -ne 'Unreachable' } |
                Select-Object -ExpandProperty IPAddress -First 1
        }
        if ($address) {
            & ssh.exe @sshOptions "${GuestUser}@${address}" 'true' 2>$null
            $sshReady = $LASTEXITCODE -eq 0
        }
        if (-not $sshReady) { Start-Sleep -Seconds 2 }
    } while (-not $sshReady -and (Get-Date) -lt $deadline)
    if (-not $sshReady) { throw "SSH did not become ready for $VmName." }

    $packageCheck = if ($ImageId -eq 'rocky-10') {
        'sudo rpm -q stackfort-waf vinyl-cache >/dev/null && sudo rpm -V stackfort-waf vinyl-cache && test "$(getenforce)" = Enforcing && sudo semodule -lfull | grep -q stackfort_nginx_panel'
    } else {
        'sudo dpkg-query -W stackfort-waf vinyl-cache >/dev/null && sudo dpkg --verify stackfort-waf vinyl-cache && grep -q "^Y" /sys/module/apparmor/parameters/enabled'
    }
    & ssh.exe @sshOptions "${GuestUser}@${address}" $packageCheck
    if ($LASTEXITCODE -ne 0) { throw "Installed WAF/Vinyl security prerequisite check failed for $VmName." }

    $remoteBinary = '/tmp/stackfort-cache.test'
    & scp.exe @sshOptions $testBinary "${GuestUser}@${address}:${remoteBinary}"
    if ($LASTEXITCODE -ne 0) { throw "Copying the cache test failed with exit code $LASTEXITCODE." }
    $output = & ssh.exe @sshOptions "${GuestUser}@${address}" `
        "chmod 0755 '$remoteBinary' && sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 '$remoteBinary' -test.v -test.run '^TestDisposableHostVinylCacheSafetyWAFAndPerformance$' -test.timeout=8m" 2>&1
    $exitCode = $LASTEXITCODE
    $output | Write-Output
    if ($exitCode -ne 0) { throw "Cache qualification failed with exit code $exitCode." }
    $text = $output -join "`n"
    foreach ($marker in @(
        'STACKFORT_QUALIFICATION vinyl-runtime-sandbox-and-loopback=passed',
        'STACKFORT_QUALIFICATION cache-personalization-isolation=passed',
        'STACKFORT_QUALIFICATION cache-waf-order-and-exceptions=passed',
        'STACKFORT_QUALIFICATION cache-scoped-purge-and-metrics=passed',
        '"name":"cache-direct-waf-off"',
        '"name":"cache-nginx-fastcgi-waf-off"',
        '"name":"cache-vinyl-waf-off"',
        '"name":"cache-direct-waf-detection"',
        '"name":"cache-nginx-fastcgi-waf-detection"',
        '"name":"cache-vinyl-waf-detection"',
        '"name":"cache-direct-waf-blocking"',
        '"name":"cache-nginx-fastcgi-waf-blocking"',
        '"name":"cache-vinyl-waf-blocking"',
        '"name":"php-cache-comparison"',
        '"name":"php-cache-waf-detection-comparison"',
        '"name":"php-cache-waf-blocking-comparison"',
        '"wafMode":"off"',
        '"wafMode":"detection_only"',
        '"wafMode":"blocking_pl1"'
    )) {
        if (-not $text.Contains($marker)) { throw "Cache qualification marker is missing: $marker" }
    }
    [pscustomobject]@{
        VMName = $VmName
        ImageId = $ImageId
        VinylRuntime = 'passed'
        PersonalizationIsolation = 'passed'
        WAFOrderingAndExceptions = 'passed'
        ScopedPurgeAndMetrics = 'passed'
        PerformanceComparison = 'published'
    }
} finally {
    if ($startedHere -and (Get-VM -Name $VmName).State -eq 'Running') {
        Stop-VM -Name $VmName -Force | Out-Null
    }
}
