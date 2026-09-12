# SPDX-License-Identifier: AGPL-3.0-or-later
# Fixed disposable VM only; checkpoint restoration is always a separate action.
[CmdletBinding()]
param([ValidateSet('Prepare','Arm','Validate','NormalBoot','Rejected','RejectedCurrent','ValidateCurrent')][string] $Stage='Validate', [switch] $AcceptDisposableReinstallationRisk)
$ErrorActionPreference='Stop'
Set-StrictMode -Version Latest
if ($Stage -eq 'Prepare' -and -not $AcceptDisposableReinstallationRisk) { throw 'Prepare requires -AcceptDisposableReinstallationRisk for this fresh disposable lab VM; this does not authorize restore or provider reinstallation' }
if ($Stage -ne 'Prepare' -and $AcceptDisposableReinstallationRisk) { throw 'Recovery consent cannot be added to an existing operation' }
$taskVMName='stackfort-native-quota-debian-13'
$taskVM=Get-VM -Name $taskVMName
if ($taskVM.Id -ne '4361f439-15e9-4f9e-a690-9a8e44b6cbd3') { throw 'Unexpected VM identity' }
if ($taskVM.State -ne 'Running') { throw 'Start the exact disposable VM explicitly before qualification' }
if ((Get-VM -Name 'stackfort-native-restore-rescue').State -ne 'Off') { throw 'The duplicate-identity restored clone must remain off' }
$taskSSH=@('-o','BatchMode=yes','-o','ConnectTimeout=5','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile=C:\ProgramData\Stackfort\Hyper-V\known_hosts','-o',"HostKeyAlias=$taskVMName",'-i','C:\ProgramData\Stackfort\Hyper-V\keys\stackfort-host-test-ed25519')
function Wait-BootSSH([string]$Previous='') {
    $deadline=(Get-Date).AddMinutes(3)
    do {
        $address=(Get-VMNetworkAdapter -VMName $taskVMName).IPAddresses | Where-Object {$_ -match '^\d+(\.\d+){3}$'} | Select-Object -First 1
        if ($address) {
            $boot=(& ssh.exe @taskSSH "stackfort-test@$address" 'cat /proc/sys/kernel/random/boot_id' 2>$null) -join ''
            if ($LASTEXITCODE -eq 0 -and $boot -match '^[0-9a-f-]{36}$' -and $boot -ne $Previous) { return $address }
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw 'SSH unavailable: preserve state, do not re-arm'
}
$address=Wait-BootSSH
$taskRun=New-Item -ItemType Directory -Path (Join-Path $PSScriptRoot ('work/native-boot-'+$Stage+'-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')))
$taskBinary=Join-Path $taskRun.FullName 'native-boot.test'
$taskInstaller=Join-Path $taskRun.FullName 'native-installer'
$savedOS=$env:GOOS; $savedArch=$env:GOARCH; $savedCGO=$env:CGO_ENABLED; $savedCache=$env:GOCACHE
try {
    $env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'; $env:GOCACHE=Join-Path $PSScriptRoot 'work/go-build'
    Push-Location (Join-Path $PSScriptRoot '../..')
    try {
        & go test -tags=integration -c -o $taskBinary ./tests/integration
        if ($LASTEXITCODE -ne 0) { throw 'Test compilation failed' }
        & go build -o $taskInstaller ./cmd/stackfort-installer
        if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed' }
    } finally { Pop-Location }
} finally { $env:GOOS=$savedOS; $env:GOARCH=$savedArch; $env:CGO_ENABLED=$savedCGO; $env:GOCACHE=$savedCache }
if ($Stage -eq 'Prepare') {
    $candidate=Join-Path $PSScriptRoot 'work/candidates/34182191221'
    & scp.exe @taskSSH (Join-Path $candidate 'stackfort-0.1.0-beta.3-linux-amd64.tar.gz') (Join-Path $candidate 'build-attestation.jsonl') $taskInstaller "stackfort-test@${address}:/tmp/"
    if ($LASTEXITCODE -ne 0) { throw 'Fixture transfer failed' }
    & ssh.exe @taskSSH "stackfort-test@$address" 'sudo test ! -e /var/tmp/stackfort-origin-lab && sudo test ! -e /var/tmp/stackfort-origin-retired && sudo test ! -e /var/lib/stackfort-boot-qualification && sudo install -d -m 0700 /var/tmp/stackfort-origin-lab /var/lib/stackfort-boot-qualification && sudo install -m 0600 /tmp/stackfort-0.1.0-beta.3-linux-amd64.tar.gz /var/tmp/stackfort-origin-lab/archive.tar.gz && sudo install -m 0600 /tmp/build-attestation.jsonl /var/tmp/stackfort-origin-lab/attestations.jsonl && sudo install -m 0500 /tmp/native-installer /var/lib/stackfort-boot-qualification/native-installer && sudo tar --no-same-owner -xzf /var/tmp/stackfort-origin-lab/archive.tar.gz -C /var/tmp/stackfort-origin-lab'
    if ($LASTEXITCODE -ne 0) { throw 'Fresh fixture setup failed' }
} else {
    $remoteHash=(& ssh.exe @taskSSH "stackfort-test@$address" 'sudo sha256sum /var/lib/stackfort-installer/native-runtime-installer') -join ''
    if ($LASTEXITCODE -ne 0 -or $remoteHash.Split(' ')[0] -ne (Get-FileHash $taskInstaller -Algorithm SHA256).Hash.ToLowerInvariant()) { throw 'Sealed installer differs from current build; restart qualification from clean checkpoint' }
}
if ($Stage -in @('Validate','NormalBoot','Rejected')) {
    $oldBoot=(& ssh.exe @taskSSH "stackfort-test@$address" 'cat /proc/sys/kernel/random/boot_id') -join ''
    if ($LASTEXITCODE -ne 0 -or $oldBoot -notmatch '^[0-9a-f-]{36}$') { throw 'Unknown boot identity' }
    & ssh.exe @taskSSH "stackfort-test@$address" 'sudo systemctl reboot'
    if ($LASTEXITCODE -notin @(0,255)) { throw 'Reboot request failed' }
    $address=Wait-BootSSH $oldBoot
}
& scp.exe @taskSSH $taskBinary "stackfort-test@${address}:/tmp/native-boot.test"
if ($LASTEXITCODE -ne 0) { throw 'Test transfer failed' }
$selection=@{Prepare='Prepare';Arm='Arm';Validate='Validate';NormalBoot='Validate';ValidateCurrent='Validate';Rejected='Rejected';RejectedCurrent='Rejected'}[$Stage]
$normal=if ($Stage -eq 'NormalBoot') {'1'} else {'0'}
$recovery=if ($Stage -eq 'Prepare' -and $AcceptDisposableReinstallationRisk) {'1'} else {'0'}
& ssh.exe @taskSSH "stackfort-test@$address" "chmod 0755 /tmp/native-boot.test && sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1 STACKFORT_NATIVE_DISPOSABLE_RECOVERY_ACCEPTED=$recovery STACKFORT_NATIVE_JOURNAL_NORMAL_BOOT=$normal /tmp/native-boot.test -test.v -test.timeout=10m -test.run='^TestDisposableNativeBoot$selection`$'" | Tee-Object -FilePath (Join-Path $taskRun.FullName 'tests.log')
$testExit=$LASTEXITCODE
& ssh.exe @taskSSH "stackfort-test@$address" 'sudo journalctl -b -u stackfort-native-admission-gate.service -u stackfort-native-storage-finalize.service -u stackfort-native-storage-verify.service -u stackfort-native-install.service --no-pager -n 220; sudo cat /var/lib/stackfort-installer/storage-state.json; sudo cat /var/lib/stackfort-installer/installation-admission.json 2>/dev/null; sudo grub-editenv /boot/grub/grubenv list; sudo cat /run/initramfs/stackfort-native-quota.log 2>/dev/null; cat /proc/cmdline' | Tee-Object -FilePath (Join-Path $taskRun.FullName 'boot.log')
& ssh.exe @taskSSH "stackfort-test@$address" 'sudo tar --ignore-failed-read -czf /tmp/native-boot-evidence.tar.gz --exclude=native-runtime-installer --exclude=retired-one-shot.img --exclude=resume-source --exclude=resume-origin -C / var/lib/stackfort-installer run/initramfs && sudo chmod 0644 /tmp/native-boot-evidence.tar.gz'
if ($LASTEXITCODE -ne 0) { throw 'Evidence archive failed' }
& scp.exe @taskSSH "stackfort-test@${address}:/tmp/native-boot-evidence.tar.gz" (Join-Path $taskRun.FullName 'evidence.tar.gz')
if ($LASTEXITCODE -ne 0) { throw 'Evidence transfer failed' }
Get-FileHash -Algorithm SHA256 $taskBinary,$taskInstaller,(Join-Path $taskRun.FullName 'tests.log'),(Join-Path $taskRun.FullName 'boot.log'),(Join-Path $taskRun.FullName 'evidence.tar.gz')
if ($testExit -ne 0) { throw "Native boot $Stage failed; evidence preserved in $($taskRun.FullName)" }
Write-Output "Native boot $Stage passed; evidence: $($taskRun.FullName)"
