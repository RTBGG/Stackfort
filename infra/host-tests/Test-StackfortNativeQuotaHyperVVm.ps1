# SPDX-License-Identifier: AGPL-3.0-or-later
# Lab-only native root conversion. Never targets the customer or existing VMs.
[CmdletBinding()]
param(
    [ValidateSet('Prepare', 'ArmReject', 'CheckReject', 'Arm', 'Validate', 'Reboot', 'ValidateCurrent', 'Safety', 'BenchmarkBefore', 'BenchmarkAfter')]
    [string] $Stage = 'Validate'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$vmName = 'stackfort-native-quota-debian-13'
$vm = Get-VM -Name $vmName
if ((Get-VMHardDiskDrive -VMName $vmName).Count -ne 2) { throw 'Expected system plus NoCloud seed only.' }
if (-not (Get-VMSnapshot -VMName $vmName -Name 'native-quota-before-conversion' -ErrorAction SilentlyContinue)) {
    throw 'An offline recovery checkpoint is required before the native quota experiment.'
}
if ($vm.State -eq 'Off') { Start-VM -Name $vmName }
$sshOptions = @('-o','BatchMode=yes','-o','ConnectTimeout=5','-o','StrictHostKeyChecking=accept-new',
    '-o','UserKnownHostsFile=C:\ProgramData\Stackfort\Hyper-V\known_hosts',
    '-o',"HostKeyAlias=$vmName",'-o','LogLevel=ERROR',
    '-i','C:\ProgramData\Stackfort\Hyper-V\keys\stackfort-host-test-ed25519')
function Wait-NativeSSH([string] $PreviousBoot = '') {
    $deadline = (Get-Date).AddMinutes(5)
    do {
        $ip = (Get-VMNetworkAdapter -VMName $vmName).IPAddresses |
            Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } |
            Select-Object -First 1
        if ($ip) {
            $boot = (& ssh.exe @sshOptions "stackfort-test@$ip" 'cat /proc/sys/kernel/random/boot_id' 2>$null) -join ''
            if ($LASTEXITCODE -eq 0 -and $boot -match '^[0-9a-f-]{36}$' -and $boot -ne $PreviousBoot) { return $ip }
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw 'Native quota lab SSH not ready; retained VM/checkpoint for diagnosis.'
}
$address = Wait-NativeSSH
$run = New-Item -ItemType Directory -Path (Join-Path $PSScriptRoot ('work/native-quota-' + $Stage + '-' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')))
$binary = Join-Path $run.FullName 'native-quota.test'
$savedOS = $env:GOOS; $savedArch = $env:GOARCH; $savedCGO = $env:CGO_ENABLED
try {
    $env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
    Push-Location (Join-Path $PSScriptRoot '../..')
    try { & go test -tags=integration -c -o $binary ./tests/integration; if ($LASTEXITCODE -ne 0) { throw 'Compile failed.' } }
    finally { Pop-Location }
} finally { $env:GOOS=$savedOS; $env:GOARCH=$savedArch; $env:CGO_ENABLED=$savedCGO }
if ($Stage -in @('CheckReject', 'Validate', 'Reboot')) {
    $oldBoot = (& ssh.exe @sshOptions "stackfort-test@$address" 'cat /proc/sys/kernel/random/boot_id') -join ''
    if ($LASTEXITCODE -ne 0 -or $oldBoot -notmatch '^[0-9a-f-]{36}$') { throw 'Cannot identify pre-reboot boot.' }
    & ssh.exe @sshOptions "stackfort-test@$address" 'sudo systemctl reboot'
    if ($LASTEXITCODE -notin @(0,255)) { throw 'Reboot request failed.' }
    Start-Sleep -Seconds 5
    $address = Wait-NativeSSH -PreviousBoot $oldBoot
}
& scp.exe @sshOptions $binary "stackfort-test@${address}:/tmp/native-quota.test"
if ($LASTEXITCODE -ne 0) { throw 'Test transfer failed.' }
$selection = @{
    Prepare='^(TestNativeQuotaPolicy|TestDisposableNativeQuotaPrepare)$'
    ArmReject='^TestDisposableNativeQuotaArm$'
    CheckReject='^TestDisposableNativeQuotaRejected$'
    Arm='^TestDisposableNativeQuotaArm$'
    Validate='^TestDisposableNativeQuotaValidate$'
    Reboot='^TestDisposableNativeQuotaValidate$'
    ValidateCurrent='^(TestNativeQuotaPolicy|TestDisposableNativeQuotaValidate)$'
    Safety='^TestDisposableNativeQuotaResumeSafety$'
    BenchmarkBefore='^TestDisposableNativeQuotaBenchmark$'
    BenchmarkAfter='^TestDisposableNativeQuotaBenchmark$'
}[$Stage]
$reject = if ($Stage -eq 'ArmReject') { 'STACKFORT_NATIVE_QUOTA_REJECT=1 ' } else { '' }
if ($Stage -eq 'BenchmarkBefore') { $reject = 'STACKFORT_NATIVE_QUOTA_BENCHMARK=before ' }
if ($Stage -eq 'BenchmarkAfter') { $reject = 'STACKFORT_NATIVE_QUOTA_BENCHMARK=after ' }
if ($Stage -eq 'Validate') { $reject = 'STACKFORT_NATIVE_QUOTA_EXPECT_BOOT=converted ' }
if ($Stage -eq 'Reboot') { $reject = 'STACKFORT_NATIVE_QUOTA_EXPECT_BOOT=already-ready ' }
& ssh.exe @sshOptions "stackfort-test@$address" "chmod 0755 /tmp/native-quota.test && sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1 $reject/tmp/native-quota.test -test.v -test.timeout=10m -test.run='$selection'" |
    Tee-Object -FilePath (Join-Path $run.FullName 'tests.log')
$testExit = $LASTEXITCODE
& ssh.exe @sshOptions "stackfort-test@$address" 'sudo journalctl -b -u stackfort-native-quota-resume.service -u stackfort-native-quota-consumer.service --no-pager; sudo cat /run/initramfs/stackfort-native-quota.log 2>/dev/null; sudo cat /run/initramfs/stackfort-native-quota.json 2>/dev/null' |
    Tee-Object -FilePath (Join-Path $run.FullName 'boot.log')
& ssh.exe @sshOptions "stackfort-test@$address" 'sudo tar -czf /tmp/native-quota-evidence.tar.gz --exclude=probe.test --exclude=initrd.before -C /var/lib/stackfort-native-quota-prototype . && sudo chmod 0644 /tmp/native-quota-evidence.tar.gz'
if ($LASTEXITCODE -ne 0) { throw 'Evidence archive failed.' }
& scp.exe @sshOptions "stackfort-test@${address}:/tmp/native-quota-evidence.tar.gz" (Join-Path $run.FullName 'evidence.tar.gz')
if ($LASTEXITCODE -ne 0) { throw 'Evidence transfer failed.' }
Get-FileHash -Algorithm SHA256 $binary,(Join-Path $run.FullName 'tests.log'),(Join-Path $run.FullName 'boot.log'),(Join-Path $run.FullName 'evidence.tar.gz')
if ($testExit -ne 0) { throw "Native quota $Stage failed; evidence: $($run.FullName)" }
Write-Output "Native quota $Stage passed; evidence: $($run.FullName)"
