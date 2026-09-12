# SPDX-License-Identifier: AGPL-3.0-or-later
# Experimental only: does not invoke or change the production installer.
[CmdletBinding()]
param(
    [ValidateSet('Provision', 'Validate', 'Benchmark', 'Reboot', 'XFSProvision', 'XFSValidate', 'XFSComparison', 'XFSReboot')]
    [string] $Stage = 'Validate',
    [string] $VmRoot = 'C:\ProgramData\Stackfort\Hyper-V'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$vmName = 'stackfort-storage-prototype-debian-13'
$vm = Get-VM -Name $vmName -ErrorAction Stop
$systemDisk = Get-VMHardDiskDrive -VMName $vmName | Where-Object ControllerLocation -EQ 0
if ($null -eq $systemDisk -or (Get-VMHardDiskDrive -VMName $vmName).Count -ne 2) {
    throw 'Expected only a system disk and the small NoCloud seed disk; no quota disk.'
}
if ($vm.State -eq 'Off') { Start-VM -Name $vmName }
$sshOptions = @(
    '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5', '-o', 'StrictHostKeyChecking=accept-new',
    '-o', "UserKnownHostsFile=$VmRoot\known_hosts", '-o', "HostKeyAlias=$vmName",
    '-o', 'LogLevel=ERROR', '-i', "$VmRoot\keys\stackfort-host-test-ed25519"
)
function Wait-LabSSH([string] $PreviousBootId = '') {
    $deadline = (Get-Date).AddMinutes(5)
    do {
        $ipv4 = (Get-VMNetworkAdapter -VMName $vmName).IPAddresses |
            Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } |
            Select-Object -First 1
        if ($ipv4) {
            $bootId = (& ssh.exe @sshOptions "stackfort-test@$ipv4" 'cat /proc/sys/kernel/random/boot_id' 2>$null) -join ''
            if ($LASTEXITCODE -eq 0 -and $bootId -match '^[0-9a-f-]{36}$' -and $bootId -ne $PreviousBootId) { return $ipv4 }
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw 'Dedicated storage VM SSH readiness timed out.'
}
$address = Wait-LabSSH
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$run = Join-Path $PSScriptRoot "work\storage-image-$Stage-$([DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ'))"
New-Item -ItemType Directory -Path $run | Out-Null
$binary = Join-Path $run 'storage-image.test'
$previousOS = $env:GOOS
$previousArch = $env:GOARCH
$previousCGO = $env:CGO_ENABLED
try {
    $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    Push-Location $repo
    try {
        & go test -tags=integration -c -o $binary ./tests/integration
        if ($LASTEXITCODE -ne 0) { throw 'Cross-compilation failed.' }
    } finally { Pop-Location }
} finally {
    $env:GOOS = $previousOS; $env:GOARCH = $previousArch; $env:CGO_ENABLED = $previousCGO
}
if ($Stage -eq 'Provision') {
    # Only this stage installs test dependencies, before any benchmark.
    & ssh.exe @sshOptions "stackfort-test@$address" 'sudo cloud-init status --wait --long && sudo env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 update && sudo env DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y fio quota xfsprogs podman uidmap dbus-user-session catatonit fuse-overlayfs apparmor && sudo systemctl mask --now podman.socket podman.service'
    if ($LASTEXITCODE -ne 0) { throw 'Preparing disposable test dependencies failed.' }
}
if ($Stage -in @('Reboot', 'XFSReboot')) {
    $oldBootId = (& ssh.exe @sshOptions "stackfort-test@$address" 'cat /proc/sys/kernel/random/boot_id') -join ''
    if ($LASTEXITCODE -ne 0 -or $oldBootId -notmatch '^[0-9a-f-]{36}$') { throw 'Cannot read pre-reboot boot ID.' }
    & ssh.exe @sshOptions "stackfort-test@$address" 'sudo systemctl reboot'
    if ($LASTEXITCODE -notin @(0, 255)) { throw 'Requesting guest reboot failed.' }
    Start-Sleep -Seconds 5
    $address = Wait-LabSSH -PreviousBootId $oldBootId
}
# Debian 13 may mount /tmp as tmpfs: transfer AFTER the requested reboot.
& scp.exe @sshOptions $binary "stackfort-test@${address}:/tmp/storage-image.test"
if ($LASTEXITCODE -ne 0) { throw 'Copying test binary failed.' }
$selection = @{
    Provision = '^TestDisposableStorageImage(Provision|Safety|Full)$'
    Validate = '^(TestDisposableStorageImage(IOThrottle|IOPS|ContainerQuota)|TestDisposableHost(ProjectQuotaAndAccountIsolation|OCIPrivateResources|OCIDeploymentLifecycle))$'
    Benchmark = '^TestDisposableStorageImageBenchmark$'
    Reboot = '^(TestDisposableStorageImageReboot|TestDisposableHostProjectQuotaAndAccountIsolation)$'
    XFSProvision = '^TestDisposableStorageXFS(Provision|Safety)$'
    XFSValidate = '^TestDisposableStorageXFSValidate$'
    XFSComparison = '^TestDisposableStorageXFSComparison$'
    XFSReboot = '^TestDisposableStorageXFSReboot$'
}[$Stage]
$testCommand = 'sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_STORAGE_IMAGE_PROTOTYPE=1 /tmp/storage-image.test'
$fixture = if ($Stage -in @('Validate', 'XFSValidate')) { "$testCommand -test.v -test.run='^TestDisposableStorageImageRuntimeFixture$' && " } else { '' }
# Check automatic startup before a safety test manually restarts the consumer;
# Go test declaration order must not accidentally hide failed boot dependencies.
$postRebootSafety = switch ($Stage) {
    Reboot { " && $testCommand -test.v -test.timeout=5m -test.run='^TestDisposableStorageImageSafety$'" }
    XFSReboot { " && $testCommand -test.v -test.timeout=5m -test.run='^TestDisposableStorageXFSSafety$'" }
    default { '' }
}
New-Item -ItemType File -Path (Join-Path $run 'tests.log') | Out-Null
& ssh.exe @sshOptions "stackfort-test@$address" "chmod 0755 /tmp/storage-image.test && $fixture$testCommand -test.v -test.timeout=20m -test.run='$selection'$postRebootSafety" |
    Tee-Object -FilePath (Join-Path $run 'tests.log')
$testExit = $LASTEXITCODE
# Gather only experiment evidence; never archive the 8-GiB data image or tenant data.
& ssh.exe @sshOptions "stackfort-test@$address" 'sudo tar -czf /tmp/storage-image-evidence.tar.gz --exclude="hosting.ext4*" --exclude="hosting.xfs*" --exclude="*.test" --exclude="*.bin" -C /var/lib/stackfort-storage-prototype . && sudo chmod 0644 /tmp/storage-image-evidence.tar.gz'
if ($LASTEXITCODE -ne 0) { throw 'Evidence archive creation failed.' }
& scp.exe @sshOptions "stackfort-test@${address}:/tmp/storage-image-evidence.tar.gz" (Join-Path $run 'evidence.tar.gz')
if ($LASTEXITCODE -ne 0) { throw 'Evidence archive transfer failed.' }
Get-FileHash -Algorithm SHA256 $binary, (Join-Path $run 'tests.log'), (Join-Path $run 'evidence.tar.gz')
if ($testExit -ne 0) { throw "Storage prototype $Stage failed; retained evidence: $run" }
Write-Output "Storage prototype $Stage passed; evidence: $run"
