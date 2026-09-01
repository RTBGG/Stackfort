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
$testBinary = Join-Path $workRoot 'stackfort-oci-exit.test'
if (-not $SkipBuild) {
    New-Item -ItemType Directory -Path $workRoot -Force | Out-Null
    $previousGoOS, $previousGoArch, $previousCgo = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = 'linux', 'amd64', '0'
        Push-Location $repositoryRoot
        try {
            & go.exe test -tags=integration -c -o $testBinary ./tests/integration
            if ($LASTEXITCODE -ne 0) { throw "OCI exit-matrix build failed with exit code $LASTEXITCODE." }
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

    function Wait-StackfortGuest([string] $PreviousBootId = '') {
        $deadline = (Get-Date).Add($StartupTimeout)
        do {
            $adapter = Get-VMNetworkAdapter -VMName $VmName
            $candidate = $adapter.IPAddresses |
                Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } |
                Select-Object -First 1
            if ($candidate) {
                $bootId = (& ssh.exe @sshOptions "${GuestUser}@${candidate}" 'cat /proc/sys/kernel/random/boot_id' 2>$null) -join ''
                if ($LASTEXITCODE -eq 0 -and $bootId -and ($PreviousBootId -eq '' -or $bootId -ne $PreviousBootId)) {
                    return @{ Address = $candidate; BootId = $bootId.Trim() }
                }
            }
            Start-Sleep -Seconds 2
        } while ((Get-Date) -lt $deadline)
        throw "SSH did not become ready with the expected boot identity for $VmName."
    }

    function Invoke-StackfortGuestTest([string] $Pattern, [string[]] $Markers) {
        $output = & ssh.exe @sshOptions "${GuestUser}@$address" `
            "sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 '$remoteBinary' -test.v -test.run '$Pattern' -test.timeout=18m" 2>&1
        $exitCode = $LASTEXITCODE
        $output | Write-Output
        if ($exitCode -ne 0) { throw "OCI exit-matrix guest test failed with exit code $exitCode." }
        $text = $output -join "`n"
        foreach ($marker in $Markers) {
            if (-not $text.Contains($marker)) { throw "OCI exit-matrix marker is missing: $marker" }
        }
    }

    $guest = Wait-StackfortGuest
    $address = $guest.Address
    $packageCommand = switch ($ImageId) {
        'debian-13' { 'sudo env DEBIAN_FRONTEND=noninteractive apt-get update -qq && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq podman uidmap slirp4netns fuse-overlayfs passt && sudo systemctl mask --now podman.socket podman.service' }
        'ubuntu-26.04' { 'sudo env DEBIAN_FRONTEND=noninteractive apt-get update -qq && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq podman uidmap slirp4netns fuse-overlayfs passt && sudo systemctl mask --now podman.socket podman.service' }
        'rocky-10' { 'sudo dnf install -y -q podman shadow-utils-subid slirp4netns fuse-overlayfs passt && sudo systemctl mask --now podman.socket podman.service' }
    }
    & ssh.exe @sshOptions "${GuestUser}@${address}" $packageCommand
    if ($LASTEXITCODE -ne 0) { throw "Preparing the rootless OCI runtime failed with exit code $LASTEXITCODE." }

    $remoteBinary = '/var/tmp/stackfort-oci-exit.test'
    & scp.exe @sshOptions $testBinary "${GuestUser}@${address}:${remoteBinary}"
    if ($LASTEXITCODE -ne 0) { throw "Copying the OCI exit-matrix test failed with exit code $LASTEXITCODE." }
    & ssh.exe @sshOptions "${GuestUser}@${address}" "chmod 0755 '$remoteBinary'"
    if ($LASTEXITCODE -ne 0) { throw 'Making the OCI exit-matrix test executable failed.' }

    Invoke-StackfortGuestTest '^TestDisposableHostOCIRebootCleanup$' @('STACKFORT_QUALIFICATION oci-reboot-cleanup=')
    Invoke-StackfortGuestTest '^TestDisposableHost(ProjectQuotaAndAccountIsolation|OCIPrivateResources|OCIDeploymentLifecycle|OCIMaliciousPolicyCorpus)$' @(
		'STACKFORT_QUALIFICATION filesystem-isolation-and-quota=passed',
		'STACKFORT_QUALIFICATION oci-private-resources=passed',
		'STACKFORT_QUALIFICATION oci-deployment-lifecycle=passed',
		'STACKFORT_QUALIFICATION oci-malicious-policy-corpus=passed'
	)
    Invoke-StackfortGuestTest '^TestDisposableHostOCIRebootPrepare$' @('STACKFORT_QUALIFICATION oci-reboot-prepare=passed')

    $bootBefore = $guest.BootId
    & ssh.exe @sshOptions "${GuestUser}@${address}" 'sudo systemctl reboot' 2>$null
    $guest = Wait-StackfortGuest -PreviousBootId $bootBefore
    $address = $guest.Address
    Invoke-StackfortGuestTest '^TestDisposableHostOCIRebootVerify$' @('STACKFORT_QUALIFICATION oci-reboot-recovery=passed')

    [pscustomobject]@{
        VMName = $VmName; ImageId = $ImageId; AggregateBoundary = 'passed'
        Exhaustion = 'passed'; Isolation = 'passed'; RebootRecovery = 'passed'
    }
} finally {
    if ($startedHere -and (Get-VM -Name $VmName).State -eq 'Running') {
        Stop-VM -Name $VmName -Force | Out-Null
    }
}
