# SPDX-License-Identifier: AGPL-3.0-or-later
# Scratch files only in the already-running, fixed disposable Debian VM.
# No VM start/stop, checkpoint restore, root conversion or package installation.
[CmdletBinding()]
param()
$ErrorActionPreference='Stop'
Set-StrictMode -Version Latest
$taskVM=Get-VM -Name 'stackfort-native-quota-debian-13'
if ($taskVM.Id -ne '4361f439-15e9-4f9e-a690-9a8e44b6cbd3' -or $taskVM.State -ne 'Running') { throw 'Start the exact normally installed disposable VM explicitly' }
$taskAddress=(Get-VMNetworkAdapter -VMName $taskVM.Name).IPAddresses | Where-Object {$_ -match '^\d+(\.\d+){3}$'} | Select-Object -First 1
if (-not $taskAddress) { throw 'VM has no IPv4 address' }
$taskSSH=@('-o','BatchMode=yes','-o','ConnectTimeout=5','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile=C:\ProgramData\Stackfort\Hyper-V\known_hosts','-o','HostKeyAlias=stackfort-native-quota-debian-13','-i','C:\ProgramData\Stackfort\Hyper-V\keys\stackfort-host-test-ed25519')
$taskID=[Guid]::NewGuid().ToString('N')
$taskRun=New-Item -ItemType Directory -Path (Join-Path $PSScriptRoot "work/native-crash-replay-$taskID")
$taskBinary=Join-Path $taskRun.FullName 'native-crash-replay.test'
$taskRemoteBinary="/tmp/native-crash-replay-$taskID.test"
$taskSavedOS=$env:GOOS; $taskSavedArch=$env:GOARCH; $taskSavedCGO=$env:CGO_ENABLED
try {
    $env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
    Push-Location (Join-Path $PSScriptRoot '../..')
    try {
        & go test -tags=integration -c -o $taskBinary ./tests/integration
        if ($LASTEXITCODE -ne 0) { throw 'Compilation failed' }
    } finally { Pop-Location }
} finally { $env:GOOS=$taskSavedOS; $env:GOARCH=$taskSavedArch; $env:CGO_ENABLED=$taskSavedCGO }
& scp.exe @taskSSH $taskBinary "stackfort-test@${taskAddress}:$taskRemoteBinary"
if ($LASTEXITCODE -ne 0) { throw 'Transfer failed' }
$taskLog=Join-Path $taskRun.FullName 'tests.log'
& ssh.exe @taskSSH "stackfort-test@$taskAddress" "chmod 0755 $taskRemoteBinary && sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1 STACKFORT_NATIVE_CRASH_REPLAY=1 $taskRemoteBinary -test.v -test.timeout=30m -test.run='^(TestCrashRestoreImageGuards|TestDisposableNativeCrashReplay)`$'" | Tee-Object -FilePath $taskLog
$taskTestExit=$LASTEXITCODE
$taskMatches=@(Select-String -LiteralPath $taskLog -Pattern 'CRASH_REPLAY evidence=(/var/tmp/stackfort-crash-lab-[0-9]+)$')
if ($taskMatches.Count -eq 1) {
    $taskRemoteDirectory=$taskMatches[0].Matches[0].Groups[1].Value
    $taskArchive="/tmp/native-crash-replay-$taskID.tar.gz"
    # Keep large raw images in the guest; archive exact JSON and command logs.
    & ssh.exe @taskSSH "stackfort-test@$taskAddress" "sudo tar -czf $taskArchive --exclude='*.img' -C $taskRemoteDirectory . && sudo chmod 0644 $taskArchive"
    if ($LASTEXITCODE -ne 0) { throw 'Evidence archive failed' }
    & scp.exe @taskSSH "stackfort-test@${taskAddress}:$taskArchive" (Join-Path $taskRun.FullName 'evidence.tar.gz')
    if ($LASTEXITCODE -ne 0) { throw 'Evidence transfer failed' }
} else { throw 'Missing or ambiguous guest evidence directory; inspect the retained log' }
Get-FileHash -Algorithm SHA256 $taskBinary,$taskLog,(Join-Path $taskRun.FullName 'evidence.tar.gz')
if ($taskTestExit -ne 0 -or -not (Select-String -LiteralPath $taskLog -Quiet -Pattern 'CRASH_REPLAY PASS cases=')) { throw "Crash replay failed; evidence retained in $($taskRun.FullName)" }
Write-Output "Crash replay passed; evidence: $($taskRun.FullName). Guest images retained; VM power state unchanged."
