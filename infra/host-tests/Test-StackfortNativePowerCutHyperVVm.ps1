# SPDX-License-Identifier: AGPL-3.0-or-later
# Explicit destructive qualification of one disposable VM only. No checkpoint
# restoration, filesystem repair, forced conversion or test code in initrd.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Cut','Recovery')][string] $Mode,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')][string] $OperationID,
    [ValidateSet('recovery-latch-flushed','precheck-start','quota-start','quota-flushed','postcheck-start','postcheck-flushed')][string] $Event='quota-start'
)
$ErrorActionPreference='Stop'
Set-StrictMode -Version Latest
$taskName='stackfort-native-quota-debian-13'
$taskVM=Get-VM -Name $taskName
if ($taskVM.Id -ne '4361f439-15e9-4f9e-a690-9a8e44b6cbd3' -or $taskVM.State -ne 'Off') {throw 'Requires exact disposable VM, currently off'}
$taskPort=Get-VMComPort -VMName $taskName -Number 1
if ($taskPort.Path -ne '\\.\pipe\stackfort-native-powercut') {throw 'Configure the dedicated serial pipe before this test'}
$taskRun=New-Item -ItemType Directory -Path (Join-Path $PSScriptRoot ('work/native-powercut-'+$Mode+'-'+$Event+'-'+[DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')))
$taskLog=Join-Path $taskRun.FullName 'serial.log'
$taskWriter=[IO.StreamWriter]::new($taskLog,$false,[Text.UTF8Encoding]::new($false))
$taskWriter.AutoFlush=$true
$taskPipe=[IO.Pipes.NamedPipeClientStream]::new('.','stackfort-native-powercut',[IO.Pipes.PipeDirection]::InOut,[IO.Pipes.PipeOptions]::Asynchronous)
$taskText=[Text.StringBuilder]::new()
$taskCut=$false
$taskVerified=$false
$taskDiagnosticsSent=$false
try {
    Start-VM -Name $taskName
    $taskPipe.Connect(15000)
    $taskBuffer=New-Object byte[] 4096
    $taskRead=$taskPipe.ReadAsync($taskBuffer,0,$taskBuffer.Length)
    $taskDeadline=(Get-Date).AddMinutes(4)
    $taskTarget="NATIVE_BOOT_EVENT operation=$OperationID event=$Event"
    if ($Mode -eq 'Recovery') {$taskTarget="NATIVE_BOOT_EVENT operation=$OperationID event=recovery-only-root-unmounted"}
    do {
        if (-not $taskRead.Wait(100)) {continue}
        $taskCount=$taskRead.Result
        if ($taskCount -eq 0) {break}
        $taskChunk=[Text.Encoding]::UTF8.GetString($taskBuffer,0,$taskCount)
        $taskWriter.Write($taskChunk)
        [void]$taskText.Append($taskChunk)
        if ($taskText.Length -gt 2097152) {throw 'Serial output exceeded bound'}
        $taskAll=$taskText.ToString()
        if ($Mode -eq 'Cut' -and $taskAll.Contains($taskTarget)) {
            $taskObserved=[DateTime]::UtcNow
            Stop-VM -Name $taskName -TurnOff -Force -Confirm:$false
            $taskCut=$true
            $taskWriter.WriteLine("HOST_POWER_OFF requested-after=$Event observedUTC=$($taskObserved.ToString('o')) completedUTC=$([DateTime]::UtcNow.ToString('o'))")
            break
        }
        if ($Mode -eq 'Recovery' -and $taskAll.Contains($taskTarget) -and $taskAll.Contains('(initramfs)') -and -not $taskDiagnosticsSent) {
            # Read-only commands at a verified initramfs prompt. Never mount,
            # fsck, tune a filesystem, exit the recovery shell or delete state.
            $taskCommand="echo STACKFORT_DIAG_BEGIN; cat /proc/mounts; cat /proc/cmdline; cat /proc/1/comm; ps; /usr/sbin/tune2fs -l /dev/disk/by-partuuid/54964bdf-2add-41b7-b41e-6d483962d021; /usr/sbin/debugfs -D -R 'cat /boot/grub/grubenv' /dev/disk/by-partuuid/54964bdf-2add-41b7-b41e-6d483962d021; echo STACKFORT_DIAG_END"
            $taskBytes=[Text.Encoding]::ASCII.GetBytes($taskCommand+[char]13)
            $taskPipe.Write($taskBytes,0,$taskBytes.Length);$taskPipe.Flush()
            $taskDiagnosticsSent=$true
        }
        if ($Mode -eq 'Recovery' -and $taskDiagnosticsSent -and $taskAll -match '(?s)\r?\nSTACKFORT_DIAG_BEGIN\r?\n(.*?)STACKFORT_DIAG_END\r?\n') {
            $taskDiag=$Matches[1]
            if ($taskDiag -match '(?m)^/dev/\S+\s+\S+\s+ext4\s' -or $taskAll -match "NATIVE_BOOT_EVENT operation=$OperationID event=(precheck|quota|postcheck)-start") {throw 'Recovery mounted root or repeated conversion'}
            if (-not $taskDiag.Contains("stackfort.native-recovery=$OperationID") -or -not $taskDiag.Contains("stackfort_native_consumed=$OperationID")) {throw 'Recovery identity/consumed latch missing'}
            $taskVerified=$true
            Stop-VM -Name $taskName -TurnOff -Force -Confirm:$false
            break
        }
        $taskRead=$taskPipe.ReadAsync($taskBuffer,0,$taskBuffer.Length)
    } while ((Get-Date) -lt $taskDeadline)
} finally {
    $taskPipe.Dispose()
    $taskWriter.Dispose()
}
if (($Mode -eq 'Cut' -and -not $taskCut) -or ($Mode -eq 'Recovery' -and -not $taskVerified)) {throw "Expected boundary not verified; preserve VM state and $taskLog"}
if ((Get-VM -Name $taskName).State -ne 'Off') {throw 'Expected powered-off fixture'}
Get-FileHash -Algorithm SHA256 $taskLog
Write-Output "Native power-cut $Mode passed; event=$Event; evidence=$taskLog"
