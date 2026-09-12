# SPDX-License-Identifier: AGPL-3.0-or-later
# Dedicated Debian lab only. Never restores checkpoints or publishes artifacts.
[CmdletBinding()]
param(
    [ValidateSet('Prepare','Arm','Validate','NormalBoot','Recovery','RecoveryBoot','BlockedInstallBoot','InterruptInstall','InspectAdmission','RecoverInstallation','WebGateProbe','ValidateCurrent','RuntimeInterrupt')]
    [string] $Stage = 'Validate',
    [ValidateSet('convert','reject','lost-proof')]
    [string] $Mode = 'convert',
    [switch] $WithRelease,
    [switch] $WithInstallation,
    [switch] $WithRuntime,
    [ValidateSet('','pause-services-once')]
    [string] $InstallationFault = '',
    [ValidatePattern('^([0-9a-f]{64})?$')]
    [string] $AdmissionStateSHA256 = '',
    [ValidatePattern('^([0-9a-f]{64})?$')]
    [string] $AdmissionPackageSHA256 = ''
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($WithInstallation -and -not $WithRelease) { throw 'Installation qualification requires -WithRelease.' }
if ($WithRuntime -and (-not $WithInstallation -or $InstallationFault)) { throw 'Runtime qualification requires installation without test-only in-process faults.' }
if ($InstallationFault -and ($Stage -ne 'Prepare' -or -not $WithInstallation -or -not $WithRelease)) { throw 'Fault mode must be sealed by Prepare with release and installation.' }
if ($Stage -eq 'RecoverInstallation') {
    if ($AdmissionStateSHA256.Length -ne 64 -or $AdmissionPackageSHA256.Length -ne 64) { throw 'Recovery requires both explicitly reviewed SHA256 snapshots.' }
} elseif ($AdmissionStateSHA256 -or $AdmissionPackageSHA256) { throw 'Review digests are only valid for RecoverInstallation.' }
$vmName = 'stackfort-native-quota-debian-13'
$vm = Get-VM -Name $vmName
if ($vm.Id -ne '4361f439-15e9-4f9e-a690-9a8e44b6cbd3') { throw 'Unexpected VM identity.' }
if ((Get-VMHardDiskDrive -VMName $vmName).Count -ne 2) { throw 'Expected system disk and NoCloud seed only.' }
if (-not (Get-VMSnapshot -VMName $vmName -Name 'native-quota-before-conversion' -ErrorAction SilentlyContinue)) { throw 'Offline pre-conversion checkpoint required.' }
if ($vm.State -eq 'Off') { Start-VM -Name $vmName }
$sshOptions = @('-o','BatchMode=yes','-o','ConnectTimeout=5','-o','StrictHostKeyChecking=yes',
    '-o','UserKnownHostsFile=C:\ProgramData\Stackfort\Hyper-V\known_hosts',
    '-o',"HostKeyAlias=$vmName",'-o','LogLevel=ERROR',
    '-i','C:\ProgramData\Stackfort\Hyper-V\keys\stackfort-host-test-ed25519')
function Wait-JournalSSH([string] $PreviousBoot = '') {
    $deadline = (Get-Date).AddMinutes(3)
    do {
        $ip = (Get-VMNetworkAdapter -VMName $vmName).IPAddresses |
            Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } | Select-Object -First 1
        if ($ip) {
            $boot = (& ssh.exe @sshOptions "stackfort-test@$ip" 'cat /proc/sys/kernel/random/boot_id' 2>$null) -join ''
            if ($LASTEXITCODE -eq 0 -and $boot -match '^[0-9a-f-]{36}$' -and $boot -ne $PreviousBoot) { return $ip }
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw 'Boot did not return SSH; keep checkpoint and inspect console, do not re-arm.'
}
$address = Wait-JournalSSH
$run = New-Item -ItemType Directory -Path (Join-Path $PSScriptRoot ('work/native-journal-' + $Stage + '-' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')))
$binary = Join-Path $run.FullName 'native-journal.test'
$operator = Join-Path $run.FullName 'native-operator'
$savedOS=$env:GOOS; $savedArch=$env:GOARCH; $savedCGO=$env:CGO_ENABLED; $savedCache=$env:GOCACHE
try {
    $env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
    $env:GOCACHE=Join-Path $PSScriptRoot 'work/go-build'
    Push-Location (Join-Path $PSScriptRoot '../..')
    try {
        & go test -tags=integration -c -o $binary ./tests/integration
        if ($LASTEXITCODE -ne 0) { throw 'Compilation failed.' }
        & go build -o $operator ./cmd/stackfort-installer
        if ($LASTEXITCODE -ne 0) { throw 'Operator CLI compilation failed.' }
    }
    finally { Pop-Location }
} finally { $env:GOOS=$savedOS; $env:GOARCH=$savedArch; $env:CGO_ENABLED=$savedCGO; $env:GOCACHE=$savedCache }
if ($Stage -in @('Validate','NormalBoot','Recovery','RecoveryBoot','BlockedInstallBoot','InterruptInstall')) {
    $oldBoot=(& ssh.exe @sshOptions "stackfort-test@$address" 'cat /proc/sys/kernel/random/boot_id') -join ''
    if ($LASTEXITCODE -ne 0 -or $oldBoot -notmatch '^[0-9a-f-]{36}$') { throw 'Cannot identify boot.' }
    & ssh.exe @sshOptions "stackfort-test@$address" 'sudo systemctl reboot'
    if ($LASTEXITCODE -notin @(0,255)) { throw 'Reboot request failed.' }
    Start-Sleep -Seconds 3
    $address=Wait-JournalSSH -PreviousBoot $oldBoot
}
& scp.exe @sshOptions $binary "stackfort-test@${address}:/tmp/native-journal.test"
if ($LASTEXITCODE -ne 0) { throw 'Transfer failed.' }
if ($Stage -eq 'Prepare' -and $WithInstallation) {
    & scp.exe @sshOptions $operator "stackfort-test@${address}:/tmp/native-operator"
    if ($LASTEXITCODE -ne 0) { throw 'Operator transfer failed.' }
}
$selection=@{
    Prepare='^TestDisposableNativeJournalPrepare$'; Arm='^TestDisposableNativeJournalArm$'
    Validate='^TestDisposableNativeJournalValidate$'; NormalBoot='^TestDisposableNativeJournalValidate$'
    Recovery='^TestDisposableNativeJournalRecovery$'; RecoveryBoot='^TestDisposableNativeJournalRecovery$'
    BlockedInstallBoot='^TestDisposableNativeInstallBlockedBoot$'
    InterruptInstall='^TestDisposableNativeAdmissionInterrupt$'
    InspectAdmission='^TestDisposableNativeAdmissionInspect$'
    RecoverInstallation='^TestDisposableNativeAdmissionRecover$'
    WebGateProbe='^TestDisposableNativeAdmissionWebProbe$'
    ValidateCurrent='^TestDisposableNativeJournalValidate$'
    RuntimeInterrupt='^TestDisposableNativeRuntimeInterrupt$'
}[$Stage]
$normalBoot=if ($Stage -in @('NormalBoot','RecoveryBoot')) { '1' } else { '0' }
$withReleaseValue=if ($WithRelease) { '1' } else { '0' }
$withInstallValue=if ($WithInstallation) { '1' } else { '0' }
$withRuntimeValue=if ($WithRuntime) { '1' } else { '0' }
& ssh.exe @sshOptions "stackfort-test@$address" "chmod 0755 /tmp/native-journal.test && sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1 STACKFORT_NATIVE_JOURNAL_RUNTIME=$withRuntimeValue STACKFORT_NATIVE_JOURNAL_RELEASE=$withReleaseValue STACKFORT_NATIVE_JOURNAL_INSTALL=$withInstallValue STACKFORT_NATIVE_JOURNAL_MODE=$Mode STACKFORT_NATIVE_JOURNAL_NORMAL_BOOT=$normalBoot STACKFORT_NATIVE_INSTALL_FAULT=$InstallationFault STACKFORT_ADMISSION_STATE_SHA256=$AdmissionStateSHA256 STACKFORT_ADMISSION_PACKAGE_SHA256=$AdmissionPackageSHA256 /tmp/native-journal.test -test.v -test.timeout=10m -test.run='$selection'" |
    Tee-Object -FilePath (Join-Path $run.FullName 'tests.log')
$testExit=$LASTEXITCODE
& ssh.exe @sshOptions "stackfort-test@$address" 'sudo journalctl -b -u stackfort-native-admission-gate.service -u stackfort-native-quota-resume.service -u stackfort-native-quota-consumer.service -u stackfort-native-install.service --no-pager -n 220; sudo cat /var/lib/stackfort-installer/storage-state.json; sudo cat /var/lib/stackfort-installer/installation-admission.json 2>/dev/null; sudo grub-editenv /boot/grub/grubenv list; sudo cat /run/initramfs/stackfort-native-quota.log 2>/dev/null; cat /proc/cmdline' |
    Tee-Object -FilePath (Join-Path $run.FullName 'boot.log')
& ssh.exe @sshOptions "stackfort-test@$address" 'sudo tar --ignore-failed-read -czf /tmp/native-journal-evidence.tar.gz --exclude=probe.test --exclude=operator --exclude=initrd.before --exclude=retired-one-shot.img -C / var/lib/stackfort-native-quota-prototype var/lib/stackfort-installer && sudo chmod 0644 /tmp/native-journal-evidence.tar.gz'
if ($LASTEXITCODE -ne 0) { throw 'Evidence archive failed.' }
& scp.exe @sshOptions "stackfort-test@${address}:/tmp/native-journal-evidence.tar.gz" (Join-Path $run.FullName 'evidence.tar.gz')
if ($LASTEXITCODE -ne 0) { throw 'Evidence transfer failed.' }
Get-FileHash -Algorithm SHA256 $binary,$operator,(Join-Path $run.FullName 'tests.log'),(Join-Path $run.FullName 'boot.log'),(Join-Path $run.FullName 'evidence.tar.gz')
if ($testExit -ne 0) { throw "Journal boot $Stage failed; evidence retained in $($run.FullName)" }
Write-Output "Journal boot $Stage passed; evidence: $($run.FullName)"
