# SPDX-License-Identifier: AGPL-3.0-or-later
# Controlled lab-only switch from an independent rescue OS to the restored disk.
[CmdletBinding()]
param([Parameter(Mandatory)][string]$LabManifest)
$ErrorActionPreference='Stop'
Set-StrictMode -Version Latest
$taskResolved=(Resolve-Path -LiteralPath $LabManifest).Path
$taskWork=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot 'work'))+[IO.Path]::DirectorySeparatorChar
if (-not $taskResolved.StartsWith($taskWork,[StringComparison]::OrdinalIgnoreCase) -or [IO.Path]::GetFileName($taskResolved) -ne 'lab.json') { throw 'Manifest must be a retained lab artifact' }
$taskLab=Get-Content -Raw -LiteralPath $taskResolved | ConvertFrom-Json
if ([IO.Path]::GetDirectoryName($taskResolved) -ne $taskLab.Root -or $taskLab.OriginalVM -ne '4361f439-15e9-4f9e-a690-9a8e44b6cbd3' -or $taskLab.Snapshot -ne '9e62e0ac-89ad-4851-8ae2-e6b936e40eae') { throw 'Wrong lab provenance' }
$taskOriginal=Get-VM -Id $taskLab.OriginalVM
$taskVM=Get-VM -Id $taskLab.RescueVM
if ($taskOriginal.State -ne 'Off' -or $taskVM.State -ne 'Off' -or $taskVM.Name -ne 'stackfort-native-restore-rescue') { throw 'Original and exact rescue VM must be off' }
$taskPlan=Get-Content -Raw -LiteralPath (Join-Path $taskLab.Root 'native-boot-plan.json') | ConvertFrom-Json
if ($taskPlan.rawSHA256 -ne 'eb2eea722d0dfd8926e0e017e6eb8ee9c47d350ce61ac544f64220df69b6e32e' -or $taskPlan.rescueDMI -ne $taskLab.RescueDMI -or $taskPlan.backupSHA256 -ne $taskLab.BackupSHA256 -or @($taskPlan.files.PSObject.Properties).Count -ne 8) { throw 'Missing reviewed offline validation' }
if ((Get-FileHash -LiteralPath $taskLab.Backup).Hash.ToLowerInvariant() -ne $taskLab.BackupSHA256) { throw 'External backup changed' }
$taskTarget=Get-VHD -Path $taskLab.Target
if ($taskTarget.Size -ne 50GB -or $taskTarget.DiskIdentifier.ToLowerInvariant() -ne $taskLab.TargetDiskIdentifier) { throw 'Replacement identity changed' }
$taskDrives=@(Get-VMHardDiskDrive -VM $taskVM)
$taskRootDrive=@($taskDrives | Where-Object ControllerLocation -eq 0)
$taskSeedDrive=@($taskDrives | Where-Object ControllerLocation -eq 1)
$taskExtra=@($taskDrives | Where-Object ControllerLocation -eq 2)
if ($taskDrives.Count -ne 3 -or $taskRootDrive.Count -ne 1 -or $taskSeedDrive.Count -ne 1 -or $taskExtra.Count -ne 1 -or $taskRootDrive[0].Path -ne $taskLab.RescueSystem -or $taskExtra[0].Path -ne $taskLab.Target) { throw 'Unexpected rescue attachment graph' }
$taskSeed=Join-Path $taskLab.Root 'restored-cidata.vhdx'
if (-not (Test-Path -LiteralPath $taskSeed -PathType Leaf)) { throw 'Missing separately exported original cloud-init seed' }
$taskTransition=Join-Path $taskLab.Root 'boot-transition.json'
if (Test-Path -LiteralPath $taskTransition) { throw 'Boot transition already attempted; inspect, do not replay' }
$taskBefore=@{VM=$taskVM.Id;Drives=@($taskDrives | Select-Object ControllerNumber,ControllerLocation,Path);Firmware=(Get-VMFirmware -VM $taskVM | Select-Object EnableSecureBoot,SecureBootTemplate);MAC=(Get-VMNetworkAdapter -VM $taskVM).MacAddress;SeedSHA256=(Get-FileHash -LiteralPath $taskSeed).Hash.ToLowerInvariant()}
[IO.File]::WriteAllText($taskTransition,($taskBefore | ConvertTo-Json -Depth 5),[Text.UTF8Encoding]::new($false))
Remove-VMHardDiskDrive -VMHardDiskDrive $taskExtra[0]
Set-VMHardDiskDrive -VMHardDiskDrive $taskRootDrive[0] -Path $taskLab.Target
Set-VMHardDiskDrive -VMHardDiskDrive $taskSeedDrive[0] -Path $taskSeed
$taskMAC=(Get-VMNetworkAdapter -VM $taskOriginal).MacAddress
Set-VMNetworkAdapter -VMName $taskVM.Name -StaticMacAddress $taskMAC
$taskBoot=Get-VMHardDiskDrive -VM $taskVM | Where-Object ControllerLocation -eq 0
Set-VMFirmware -VM $taskVM -EnableSecureBoot On -SecureBootTemplate MicrosoftUEFICertificateAuthority -FirstBootDevice $taskBoot
Set-VM -VM $taskVM -Notes 'Disposable pre-install restore clone. Duplicate OS/network identity: never run alongside stackfort-native-quota-debian-13. Rescue and original disks preserved offline.'
if (@(Get-VMHardDiskDrive -VM $taskVM).Count -ne 2) { throw 'Unexpected final attachment graph' }
Start-VM -VM $taskVM
Write-Output 'Restored disk started. Rescue system and authoritative backup are detached; original remains off.'
