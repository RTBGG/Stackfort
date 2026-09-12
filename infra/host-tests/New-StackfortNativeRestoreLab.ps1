# SPDX-License-Identifier: AGPL-3.0-or-later
# Dedicated whole-disk rescue lab. Never modifies/restores an existing VM.
[CmdletBinding()]
param()
$ErrorActionPreference='Stop'
Set-StrictMode -Version Latest
$taskOriginal=Get-VM -Name 'stackfort-native-quota-debian-13'
if ($taskOriginal.Id -ne '4361f439-15e9-4f9e-a690-9a8e44b6cbd3' -or $taskOriginal.State -ne 'Off') { throw 'Expected original disposable VM off' }
$taskName='stackfort-native-restore-rescue'
if (Get-VM -Name $taskName -ErrorAction SilentlyContinue) { throw 'Rescue VM exists; refusing adoption' }
$taskRoot=Join-Path $PSScriptRoot ('work/native-disk-restore-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $taskRoot | Out-Null
$taskSnapshot=Get-VMSnapshot -VM $taskOriginal | Where-Object Id -eq '9e62e0ac-89ad-4851-8ae2-e6b936e40eae'
if (-not $taskSnapshot -or $taskSnapshot.Name -ne 'native-quota-before-conversion') { throw 'Wrong pre-conversion checkpoint' }
$taskSource=@(Get-VMHardDiskDrive -VMSnapshot $taskSnapshot | Where-Object ControllerLocation -eq 0)
if ($taskSource.Count -ne 1) { throw 'Ambiguous source disk' }
$taskSourcePath=$taskSource[0].Path
$taskSourceHash=(Get-FileHash -LiteralPath $taskSourcePath).Hash.ToLowerInvariant()
$taskBackup=Join-Path $taskRoot 'pre-conversion-backup.vhdx'
Convert-VHD -Path $taskSourcePath -DestinationPath $taskBackup -VHDType Dynamic
$taskBackupInfo=Get-VHD -Path $taskBackup
if ($taskBackupInfo.Size -ne 50GB -or $taskBackupInfo.ParentPath) { throw 'Backup is not a standalone 50 GiB disk' }
if ((Get-FileHash -LiteralPath $taskSourcePath).Hash.ToLowerInvariant() -ne $taskSourceHash) { throw 'Source changed during export' }
(Get-Item -LiteralPath $taskBackup).IsReadOnly=$true
$taskBackupHash=(Get-FileHash -LiteralPath $taskBackup).Hash.ToLowerInvariant()

# Independently cached Debian image from earlier host qualification. Its root
# identity is checked in the guest before accepting the restore operation.
$taskBase='C:\ProgramData\Stackfort\Hyper-V\base\debian-13-0ce1f1d67573.vhdx'
if ((Get-FileHash -LiteralPath $taskBase).Hash -ne '7B44FCAB32C87643542D600BA04837DCACFA554414D9BC0983CBB776DC396711') { throw 'Rescue base changed' }
$taskSystem=Join-Path $taskRoot 'rescue-system.vhdx'
$taskSeed=Join-Path $taskRoot 'rescue-cidata.vhdx'
$taskTarget=Join-Path $taskRoot 'replacement-system.vhdx'
New-VHD -Path $taskSystem -ParentPath $taskBase -Differencing | Out-Null
New-VHD -Path $taskTarget -Dynamic -SizeBytes 50GB -LogicalSectorSizeBytes 512 -PhysicalSectorSizeBytes 4096 -BlockSizeBytes 1MB | Out-Null
New-VHD -Path $taskSeed -Dynamic -SizeBytes 64MB -BlockSizeBytes 1MB | Out-Null
$taskPublicKey=(Get-Content -Raw -LiteralPath 'C:\ProgramData\Stackfort\Hyper-V\keys\stackfort-host-test-ed25519.pub').Trim()
if ($taskPublicKey -notmatch '^ssh-ed25519 [A-Za-z0-9+/=]+(?: .*)?$') { throw 'Invalid public key' }
$taskUserData=@"
#cloud-config
hostname: $taskName
manage_etc_hosts: true
ssh_pwauth: false
disable_root: true
users:
  - default
  - name: stackfort-test
    groups: [sudo]
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    lock_passwd: true
    ssh_authorized_keys:
      - $taskPublicKey
package_update: true
packages: [sudo, hyperv-daemons, qemu-utils]
runcmd:
  - [systemctl, mask, --now, udisks2.service, autofs.service]
  - [touch, /var/lib/stackfort-rescue-ready]
"@
$taskMeta="instance-id: $taskName-$([Guid]::NewGuid().ToString('N'))`nlocal-hostname: $taskName`n"
$taskSeedMounted=$false
try {
    $taskDisk=Mount-VHD -Path $taskSeed -Passthru
    $taskSeedMounted=$true
    if ((Get-VHD -Path $taskSeed).DiskNumber -ne $taskDisk.Number -or (Get-Disk -Number $taskDisk.Number).Size -ne 64MB) { throw 'Seed disk identity differs' }
    $taskPartition=Initialize-Disk -Number $taskDisk.Number -PartitionStyle MBR -PassThru | New-Partition -UseMaximumSize -AssignDriveLetter
    $taskVolume=$taskPartition | Format-Volume -FileSystem FAT -NewFileSystemLabel cidata -Confirm:$false
    $taskSeedRoot="$($taskVolume.DriveLetter):\"
    [IO.File]::WriteAllText((Join-Path $taskSeedRoot 'user-data'),$taskUserData,[Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $taskSeedRoot 'meta-data'),$taskMeta,[Text.UTF8Encoding]::new($false))
} finally { if($taskSeedMounted){Dismount-VHD -Path $taskSeed} }
New-VM -Name $taskName -Generation 2 -Path $taskRoot -VHDPath $taskSystem -SwitchName 'Default Switch' -MemoryStartupBytes 4GB | Out-Null
$taskRescue=Get-VM -Name $taskName
Set-VMMemory -VM $taskRescue -DynamicMemoryEnabled $false
Set-VMProcessor -VM $taskRescue -Count 2
Set-VM -VM $taskRescue -AutomaticCheckpointsEnabled $false -AutomaticStartAction Nothing -AutomaticStopAction ShutDown
Add-VMHardDiskDrive -VM $taskRescue -ControllerType SCSI -ControllerNumber 0 -ControllerLocation 1 -Path $taskSeed
$taskBoot=Get-VMHardDiskDrive -VM $taskRescue | Where-Object ControllerLocation -eq 0
Set-VMFirmware -VM $taskRescue -EnableSecureBoot On -SecureBootTemplate MicrosoftUEFICertificateAuthority -FirstBootDevice $taskBoot
$taskBIOS=Get-CimInstance -Namespace root/virtualization/v2 -ClassName Msvm_VirtualSystemSettingData | Where-Object {$_.VirtualSystemIdentifier -eq $taskRescue.Id -and $_.VirtualSystemType -eq 'Microsoft:Hyper-V:System:Realized'}
$taskManifest=[ordered]@{SchemaVersion=1;Root=$taskRoot;OriginalVM=$taskOriginal.Id.ToString();Snapshot=$taskSnapshot.Id.ToString();Source=$taskSourcePath;SourceSHA256=$taskSourceHash;Backup=$taskBackup;BackupSHA256=$taskBackupHash;Bytes=50GB;RescueVM=$taskRescue.Id.ToString();RescueDMI=$taskBIOS.BIOSGUID.ToLowerInvariant();RescueSystem=$taskSystem;Target=$taskTarget;TargetDiskIdentifier=(Get-VHD -Path $taskTarget).DiskIdentifier.ToLowerInvariant()}
[IO.File]::WriteAllText((Join-Path $taskRoot 'lab.json'),($taskManifest | ConvertTo-Json -Depth 4),[Text.UTF8Encoding]::new($false))
Start-VM -VM $taskRescue
$taskManifest | ConvertTo-Json -Depth 4
