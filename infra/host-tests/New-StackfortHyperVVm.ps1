# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('debian-13', 'ubuntu-26.04', 'rocky-10')]
    [string] $ImageId,

    [ValidatePattern('^[a-z0-9][a-z0-9-]{2,62}$')]
    [string] $VmName,

    [string] $SwitchName = 'Default Switch',
    [string] $VmRoot = 'C:\ProgramData\Stackfort\Hyper-V',
    [UInt64] $MemoryStartupBytes = 4GB,
    [ValidateRange(2, 64)]
    [int] $ProcessorCount = 2,
    [UInt64] $SystemDiskSizeBytes = 20GB,
    [UInt64] $QuotaDiskSizeBytes = 8GB
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ([string]::IsNullOrWhiteSpace($VmName)) {
    $VmName = "stackfort-$($ImageId.Replace('.', '-'))"
}
if ($VmName -notmatch '^[a-z0-9][a-z0-9-]{2,62}$') {
    throw 'VM name must contain only lower-case ASCII letters, digits, and hyphens.'
}
if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Creating a Hyper-V VM requires an elevated PowerShell session.'
}
if (Get-VM -Name $VmName -ErrorAction SilentlyContinue) {
    throw "A VM named $VmName already exists; no existing VM will be modified."
}
if (-not (Get-VMSwitch -Name $SwitchName -ErrorAction SilentlyContinue)) {
    throw "Hyper-V switch not found: $SwitchName"
}

$qemuImg = Join-Path $env:ProgramFiles 'qemu\qemu-img.exe'
if (-not (Test-Path -LiteralPath $qemuImg -PathType Leaf)) {
    throw 'qemu-img.exe was not found under Program Files.'
}
$sshKeygen = (Get-Command ssh-keygen.exe -ErrorAction Stop).Source

$imagePath = & (Join-Path $PSScriptRoot 'prepare-image.ps1') -ImageId $ImageId
$verifiedPath = "$imagePath.verified"
if (-not (Test-Path -LiteralPath $verifiedPath -PathType Leaf)) {
    throw 'The downloaded image has no verification record.'
}
$sourceHash = ((Get-Content -LiteralPath $verifiedPath -Raw).Trim() -split '\s+')[0]
if ($sourceHash -notmatch '^[0-9a-f]{64,128}$') {
    throw 'The image verification record is malformed.'
}

$baseRoot = Join-Path $VmRoot 'base'
$vmDirectory = Join-Path (Join-Path $VmRoot 'vms') $VmName
$keyRoot = Join-Path $VmRoot 'keys'
if (Test-Path -LiteralPath $vmDirectory) {
    throw "VM directory already exists and will not be overwritten: $vmDirectory"
}
New-Item -ItemType Directory -Path $baseRoot, $keyRoot -Force | Out-Null

$baseDisk = Join-Path $baseRoot "$ImageId-$($sourceHash.Substring(0, 12)).vhdx"
if (-not (Test-Path -LiteralPath $baseDisk -PathType Leaf)) {
    $temporaryBase = Join-Path $baseRoot "$ImageId-$([Guid]::NewGuid().ToString('N')).partial.vhdx"
    & $qemuImg convert -p -f qcow2 -O vhdx -o subformat=dynamic $imagePath $temporaryBase
    if ($LASTEXITCODE -ne 0) {
        throw "qemu-img conversion failed with exit code $LASTEXITCODE."
    }
    & compact.exe /U /F /Q $temporaryBase | Out-Null
    if ($LASTEXITCODE -gt 1) {
        throw "Removing NTFS compression failed with exit code $LASTEXITCODE."
    }
    & fsutil.exe sparse setflag $temporaryBase 0 | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Removing the NTFS sparse flag failed with exit code $LASTEXITCODE."
    }
    Resize-VHD -Path $temporaryBase -SizeBytes $SystemDiskSizeBytes
    Move-Item -LiteralPath $temporaryBase -Destination $baseDisk
    (Get-Item -LiteralPath $baseDisk).IsReadOnly = $true
}

$keyPath = Join-Path $keyRoot 'stackfort-host-test-ed25519'
if (-not (Test-Path -LiteralPath $keyPath -PathType Leaf)) {
    $arguments = "-q -t ed25519 -N `"`" -C stackfort-host-test -f `"$keyPath`""
    $process = Start-Process -FilePath $sshKeygen -ArgumentList $arguments `
        -NoNewWindow -Wait -PassThru
    if ($process.ExitCode -ne 0) {
        throw "ssh-keygen failed with exit code $($process.ExitCode)."
    }
}
$publicKeyParts = (Get-Content -LiteralPath "$keyPath.pub" -Raw).Trim() -split '\s+'
if ($publicKeyParts.Count -lt 2) {
    throw 'Generated SSH public key is malformed.'
}
$publicKey = "$($publicKeyParts[0]) $($publicKeyParts[1])"

$systemDisk = Join-Path $vmDirectory 'system.vhdx'
$seedDisk = Join-Path $vmDirectory 'cidata.vhdx'
$quotaDisk = Join-Path $vmDirectory 'quota.vhdx'
New-Item -ItemType Directory -Path $vmDirectory | Out-Null
New-VHD -Path $systemDisk -ParentPath $baseDisk -Differencing | Out-Null
New-VHD -Path $quotaDisk -Dynamic -SizeBytes $QuotaDiskSizeBytes -BlockSizeBytes 1MB | Out-Null
New-VHD -Path $seedDisk -Dynamic -SizeBytes 64MB -BlockSizeBytes 1MB | Out-Null

$linuxGroup = if ($ImageId -eq 'rocky-10') { 'wheel' } else { 'sudo' }
$filesystem = if ($ImageId -eq 'rocky-10') { 'xfs' } else { 'ext4' }
$formatCommand = if ($filesystem -eq 'xfs') {
    'mkfs.xfs -f "$device"'
} else {
    'mkfs.ext4 -F -O quota,project "$device"'
}
$packages = if ($ImageId -eq 'rocky-10') {
    "  - sudo`n  - hyperv-daemons`n  - quota`n  - xfsprogs`n  - git`n  - curl`n  - ca-certificates`n  - gcc`n  - make"
} elseif ($ImageId -eq 'ubuntu-26.04') {
    "  - sudo`n  - linux-cloud-tools-virtual`n  - quota`n  - xfsprogs`n  - git`n  - curl`n  - ca-certificates`n  - rsync`n  - build-essential"
} else {
    "  - sudo`n  - hyperv-daemons`n  - quota`n  - xfsprogs`n  - git`n  - curl`n  - ca-certificates`n  - rsync`n  - build-essential"
}
$instanceId = "$VmName-$([Guid]::NewGuid().ToString('N'))"
$userData = @"
#cloud-config
preserve_hostname: false
hostname: $VmName
manage_etc_hosts: true
ssh_pwauth: false
disable_root: true
users:
  - default
  - name: stackfort-test
    gecos: Stackfort host test
    groups: [$linuxGroup]
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    lock_passwd: true
    ssh_authorized_keys:
      - $publicKey
package_update: true
packages:
$packages
write_files:
  - path: /usr/local/sbin/stackfort-prepare-quota.sh
    owner: root:root
    permissions: '0755'
    content: |
      #!/usr/bin/env bash
      set -euo pipefail
      device=''
      for attempt in {1..30}; do
        while read -r candidate size type; do
          if [[ "`$type" == disk && "`$size" -eq $QuotaDiskSizeBytes && "`$(lsblk -nro TYPE "`$candidate" | wc -l)" -eq 1 ]]; then
            device="`$candidate"
            break 2
          fi
        done < <(lsblk -bndo PATH,SIZE,TYPE)
        udevadm settle || true
        sleep 1
      done
      if [[ -z "`$device" ]]; then
        echo 'Dedicated quota disk was not found.' >&2
        exit 1
      fi
      $formatCommand
      uuid="`$(blkid -s UUID -o value "`$device")"
      install -d -m 0755 /srv/hosting /srv/stackfort-quota
      printf 'UUID=%s /srv/hosting $filesystem defaults,prjquota 0 2\n' "`$uuid" >> /etc/fstab
      mount /srv/hosting
      printf '/srv/hosting /srv/stackfort-quota none bind 0 0\n' >> /etc/fstab
      mount /srv/stackfort-quota
      findmnt --target /srv/hosting
      touch /var/lib/stackfort-host-ready
runcmd:
  - [/usr/local/sbin/stackfort-prepare-quota.sh]
final_message: Stackfort Hyper-V test node is ready.
"@
$metaData = "instance-id: $instanceId`nlocal-hostname: $VmName`n"

$mountedSeed = $false
try {
    $disk = Mount-VHD -Path $seedDisk -Passthru
    $mountedSeed = $true
    $partition = Initialize-Disk -Number $disk.Number -PartitionStyle MBR -PassThru |
        New-Partition -UseMaximumSize -AssignDriveLetter
    $volume = $partition | Format-Volume -FileSystem FAT -NewFileSystemLabel cidata -Confirm:$false
    $seedRoot = "$($volume.DriveLetter):\"
    [IO.File]::WriteAllText((Join-Path $seedRoot 'user-data'), $userData, [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $seedRoot 'meta-data'), $metaData, [Text.UTF8Encoding]::new($false))
} finally {
    if ($mountedSeed) {
        Dismount-VHD -Path $seedDisk
    }
}

New-VM -Name $VmName -Generation 2 -Path $vmDirectory -VHDPath $systemDisk `
    -SwitchName $SwitchName -MemoryStartupBytes $MemoryStartupBytes | Out-Null
Set-VMMemory -VMName $VmName -DynamicMemoryEnabled $false
Set-VMProcessor -VMName $VmName -Count $ProcessorCount
Set-VM -Name $VmName -AutomaticCheckpointsEnabled $false -AutomaticStartAction Nothing `
    -AutomaticStopAction ShutDown
Add-VMHardDiskDrive -VMName $VmName -ControllerType SCSI -ControllerNumber 0 `
    -ControllerLocation 1 -Path $seedDisk | Out-Null
Add-VMHardDiskDrive -VMName $VmName -ControllerType SCSI -ControllerNumber 0 `
    -ControllerLocation 2 -Path $quotaDisk | Out-Null
$bootDisk = Get-VMHardDiskDrive -VMName $VmName | Where-Object ControllerLocation -EQ 0
Set-VMFirmware -VMName $VmName -EnableSecureBoot On `
    -SecureBootTemplate MicrosoftUEFICertificateAuthority -FirstBootDevice $bootDisk
Start-VM -Name $VmName

[pscustomobject]@{
    VMName = $VmName
    State = (Get-VM -Name $VmName).State
    SSHUser = 'stackfort'
    SSHPrivateKey = $keyPath
    QuotaFilesystem = $filesystem
    VMPath = $vmDirectory
}
