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
$testBinary = Join-Path $workRoot 'stackfort-file-operations.test'
$agentBinary = Join-Path $workRoot 'stackfort-agent-file-operations'
if (-not $SkipBuild) {
    New-Item -ItemType Directory -Path $workRoot -Force | Out-Null
    $previousGoOS, $previousGoArch, $previousCgo = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = 'linux', 'amd64', '0'
        Push-Location $repositoryRoot
        try {
            & go.exe test -tags=integration -c -o $testBinary ./tests/integration
            if ($LASTEXITCODE -ne 0) { throw "File-operations integration build failed with exit code $LASTEXITCODE." }
            & go.exe build -o $agentBinary ./cmd/stackfort-agent
            if ($LASTEXITCODE -ne 0) { throw "File-operations agent build failed with exit code $LASTEXITCODE." }
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
foreach ($binary in @($testBinary, $agentBinary)) {
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw "Qualification binary not found: $binary" }
}

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

    $remoteTest = '/tmp/stackfort-file-operations.test'
    $remoteAgent = '/tmp/stackfort-agent-file-operations'
    & scp.exe @sshOptions $testBinary $agentBinary "${GuestUser}@${address}:/tmp/"
    if ($LASTEXITCODE -ne 0) { throw "Copying file-operations binaries failed with exit code $LASTEXITCODE." }
    $output = & ssh.exe @sshOptions "${GuestUser}@${address}" `
        "chmod 0755 '$remoteTest' '$remoteAgent' && sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_AGENT_HELPER='$remoteAgent' '$remoteTest' -test.v -test.run '^TestDisposableHostFileManagerOperations$' -test.timeout=6m" 2>&1
    $exitCode = $LASTEXITCODE
    $output | Write-Output
    if ($exitCode -ne 0) { throw "File-operations qualification failed with exit code $exitCode." }
    $text = $output -join "`n"
    if (-not $text.Contains('STACKFORT_QUALIFICATION file-manager-operations=passed')) {
        throw 'File-operations qualification marker is missing.'
    }
    [pscustomobject]@{ VMName = $VmName; ImageId = $ImageId; FileManagerOperations = 'passed' }
} finally {
    if ($startedHere -and (Get-VM -Name $VmName).State -eq 'Running') {
        Stop-VM -Name $VmName -Force | Out-Null
    }
}
