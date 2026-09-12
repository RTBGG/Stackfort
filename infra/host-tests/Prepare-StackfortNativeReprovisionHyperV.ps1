# SPDX-License-Identifier: AGPL-3.0-or-later
# TEST ONLY. Prepare independent vendor-OS disks; NEVER attach, stop, start,
# restore, remove a VM/disk, or update known_hosts. ReadPublicReport is read-only.
# The Debian image was obtained over official HTTPS and checked with SHA512SUMS;
# this script does not claim detached-signature verification.
# KVP layout/locking: Linux v6.12 tools/hv/hv_kvp_daemon.c and
# include/uapi/linux/hyperv.h (512-byte key + 2048-byte value, POSIX record locks).
# Host API: https://learn.microsoft.com/windows/win32/hyperv_v2/msvm-kvpexchangecomponent
#requires -Version 7.2
[CmdletBinding()]
param(
    [ValidateSet('Prepare', 'ReadPublicReport')][string] $Stage = 'Prepare',
    [string] $PreparationDirectory,
    [ValidateRange(1, 600)][int] $ReportTimeoutSeconds = 120
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$reproVMID = '4361f439-15e9-4f9e-a690-9a8e44b6cbd3'
$reproVMName = 'stackfort-native-quota-debian-13'
$reproImageSHA = '85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3'
$reproImageSHA512 = '8ea9faae810043a0b35b0149f05014f26705c2339ffb11ead308f33e844a87cc3ef46ec81d5262b38817b6a88af404874d48a5857ebe072ef6a31dfb6e371f50'
$reproKVPKey = 'stackfort.native.reprovision.public.v1'
$reproWork = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot 'work'))

function Assert-ReprovisionPath([string] $Path) {
    $resolved = [IO.Path]::GetFullPath($Path)
    $cursor = $resolved
    while ($cursor) {
        if (Test-Path -LiteralPath $cursor) {
            if (((Get-Item -Force -LiteralPath $cursor).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Reprovision paths must not traverse reparse points.' }
        }
        $cursor = [IO.Path]::GetDirectoryName($cursor)
    }
    return $resolved
}
function Convert-ReprovisionPublicKey([string] $Text) {
    $parts = $Text.Trim() -split '\s+'
    if ($Text.Length -gt 512 -or $parts.Count -lt 2 -or $parts.Count -gt 3 -or $parts[0] -cne 'ssh-ed25519' -or $parts[1] -cnotmatch '^[A-Za-z0-9+/]{68}$') { throw 'Expected one bounded Ed25519 public key.' }
    $blob = [Convert]::FromBase64String($parts[1])
    $prefix = [byte[]] @(0, 0, 0, 11, 115, 115, 104, 45, 101, 100, 50, 53, 53, 49, 57, 0, 0, 0, 32)
    if ($blob.Length -ne 51 -or [Convert]::ToBase64String($blob) -cne $parts[1]) { throw 'Noncanonical Ed25519 public key.' }
    for ($index = 0; $index -lt $prefix.Length; $index++) { if ($blob[$index] -ne $prefix[$index]) { throw 'Invalid SSH Ed25519 public key wire format.' } }
    return "ssh-ed25519 $($parts[1])"
}
function Get-ReprovisionVMIdentity {
    $vm = Get-VM -Id $reproVMID
    if ($vm.Name -cne $reproVMName -or $vm.Generation -ne 2) { throw 'Expected exact disposable generation-2 VM.' }
    $system = Get-CimInstance -Namespace root/virtualization/v2 -ClassName Msvm_ComputerSystem -Filter "Name='$reproVMID'"
    $settings = @(Get-CimAssociatedInstance -InputObject $system -ResultClassName Msvm_VirtualSystemSettingData |
        Where-Object VirtualSystemType -eq 'Microsoft:Hyper-V:System:Realized')
    if ($settings.Count -ne 1) { throw 'Could not uniquely identify the active VM firmware identity.' }
    $dmi = ([Guid] $settings[0].BIOSGUID).ToString('D')
    return [pscustomobject]@{ VM = $vm; System = $system; DMI = $dmi }
}
function Read-ReprovisionKVP([string[]] $Items, $Manifest) {
    if ($Items.Count -gt 128) { throw 'KVP inventory exceeds its bound.' }
    $reports = @()
    foreach ($item in $Items) {
        if ($item.Length -gt 8192) { throw 'KVP XML exceeds its bound.' }
        $settings = [Xml.XmlReaderSettings]::new()
        $settings.DtdProcessing = [Xml.DtdProcessing]::Prohibit
        $settings.XmlResolver = $null
        $settings.MaxCharactersInDocument = 8192
        $reader = [Xml.XmlReader]::Create([IO.StringReader]::new($item), $settings)
        try { $document = [Xml.XmlDocument]::new(); $document.XmlResolver = $null; $document.Load($reader) } finally { $reader.Dispose() }
        $name = $document.SelectNodes('/INSTANCE/PROPERTY[@NAME="Name"]/VALUE')
        if ($name.Count -ne 1) { throw 'Malformed KVP name property.' }
        if ($name[0].InnerText -cne $reproKVPKey) { continue }
        $data = $document.SelectNodes('/INSTANCE/PROPERTY[@NAME="Data"]/VALUE')
        $source = $document.SelectNodes('/INSTANCE/PROPERTY[@NAME="Source"]/VALUE')
        if ($data.Count -ne 1 -or $source.Count -ne 1 -or $source[0].InnerText -cne '1' -or $data[0].InnerText.Length -gt 2047) { throw 'Malformed guest public KVP report.' }
        $report = $data[0].InnerText | ConvertFrom-Json
        if ($report.schemaVersion -ne 1 -or $report.kind -cne 'fresh-debian-public-host-key' -or
            $report.instanceId -cne $Manifest.InstanceID -or $report.nonce -cne $Manifest.Nonce -or
            $report.machineId -cne $Manifest.DMI -or $report.imageSHA256 -cne $reproImageSHA -or
            $report.bootId -cnotmatch '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') { throw 'KVP report does not match the fresh instance/nonce/vendor image/VM identity.' }
        $canonicalKey = Convert-ReprovisionPublicKey $report.hostKey
        if ($canonicalKey -cne $report.hostKey) { throw 'Guest public host key must not contain a comment.' }
        $reports += $report
    }
    if ($reports.Count -gt 1) { throw 'Duplicate fresh public KVP reports.' }
    if ($reports.Count -eq 1) { return $reports[0] }
    return $null
}

$reproIdentity = Get-ReprovisionVMIdentity
if ($Stage -eq 'ReadPublicReport') {
    if ([string]::IsNullOrEmpty($PreparationDirectory)) { throw 'ReadPublicReport requires the exact retained preparation directory.' }
    $reproRoot = Assert-ReprovisionPath $PreparationDirectory
    if (-not $reproRoot.StartsWith($reproWork + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Preparation must be under the retained host-test work directory.' }
    $manifestPath = Join-Path $reproRoot 'reprovision.json'
    $manifestFile = Get-Item -LiteralPath $manifestPath
    if ($manifestFile.Length -gt 16384 -or $manifestFile.PSIsContainer) { throw 'Invalid reprovision manifest.' }
    $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
    if ($manifest.SchemaVersion -ne 1 -or $manifest.Stage -cne 'prepared-not-attached' -or $manifest.Root -cne $reproRoot -or
        $manifest.VMID -cne $reproVMID -or $manifest.DMI -cne $reproIdentity.DMI -or $manifest.SourceSHA256 -cne $reproImageSHA -or
        $manifest.InstanceID -cnotmatch '^native-reprovision-[0-9a-f]{32}$' -or $manifest.Nonce -cnotmatch '^[0-9a-f]{64}$' -or
        $manifest.SystemDisk -cne (Join-Path $reproRoot 'system.vhdx') -or $manifest.SeedDisk -cne (Join-Path $reproRoot 'cidata.vhdx')) { throw 'Preparation manifest is not bound to this independent vendor OS and VM.' }
    if ($reproIdentity.VM.State -ne 'Running') { throw 'The exact VM must be running to report its new public host key.' }
    $drives = @(Get-VMHardDiskDrive -VM $reproIdentity.VM)
    $rootDrive = @($drives | Where-Object { $_.ControllerType -eq 'SCSI' -and $_.ControllerNumber -eq 0 -and $_.ControllerLocation -eq 0 })
    $seedDrive = @($drives | Where-Object { $_.ControllerType -eq 'SCSI' -and $_.ControllerNumber -eq 0 -and $_.ControllerLocation -eq 1 })
    if ($drives.Count -ne 2 -or $rootDrive.Count -ne 1 -or $seedDrive.Count -ne 1 -or $rootDrive[0].Path -cne $manifest.SystemDisk -or $seedDrive[0].Path -cne $manifest.SeedDisk) { throw 'Fresh independent system/seed disks are not the exact active attachment graph.' }
    foreach ($diskInfo in @(@{ Path = $manifest.SystemDisk; ID = $manifest.SystemDiskID; Size = 50GB }, @{ Path = $manifest.SeedDisk; ID = $manifest.SeedDiskID; Size = 64MB })) {
        [void] (Assert-ReprovisionPath $diskInfo.Path)
        $disk = Get-VHD -Path $diskInfo.Path
        if ($disk.VhdType -ne 'Dynamic' -or $disk.ParentPath -or $disk.Size -ne $diskInfo.Size -or ([Guid] $disk.DiskIdentifier).ToString('D') -cne $diskInfo.ID) { throw 'Active disk identity, size or independence differs from preparation.' }
    }
    $deadline = [DateTime]::UtcNow.AddSeconds($ReportTimeoutSeconds)
    do {
        $components = @(Get-CimAssociatedInstance -InputObject $reproIdentity.System -ResultClassName Msvm_KvpExchangeComponent)
        if ($components.Count -ne 1) { throw 'Could not uniquely locate the exact VM KVP component.' }
        $report = Read-ReprovisionKVP -Items @($components[0].GuestExchangeItems | Where-Object { $null -ne $_ }) -Manifest $manifest
        if ($null -ne $report) {
            $blob = [Convert]::FromBase64String(($report.hostKey -split ' ')[1])
            return [pscustomobject]@{ Stage = 'public-report-read-only'; VMID = $reproVMID; DMI = $manifest.DMI; InstanceID = $manifest.InstanceID;
                BootID = $report.bootId; HostPublicKey = $report.hostKey; Fingerprint = 'SHA256:' + [Convert]::ToBase64String([Security.Cryptography.SHA256]::HashData($blob)).TrimEnd('=');
                ProposedKnownHostsLine = "$reproVMName $($report.hostKey)"; KnownHostsModified = $false; PrivateKeyRead = $false }
        }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'No public report for this exact fresh instance arrived within the bounded wait. Do not accept an unauthenticated SSH host key.'
}

if (-not [string]::IsNullOrEmpty($PreparationDirectory)) { throw 'Prepare creates an exclusive fresh directory itself; it never reuses caller-selected output paths.' }
if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Preparing fresh VHDX files requires an elevated host shell.' }
$reproImage = Assert-ReprovisionPath (Join-Path $reproWork 'native-reprovision-media-20260912/debian-13-genericcloud-amd64.qcow2')
$reproImageFile = Get-Item -LiteralPath $reproImage
if ($reproImageFile.PSIsContainer -or $reproImageFile.Length -ne 339214336 -or
    (Get-FileHash -LiteralPath $reproImage -Algorithm SHA256).Hash.ToLowerInvariant() -cne $reproImageSHA -or
    (Get-FileHash -LiteralPath $reproImage -Algorithm SHA512).Hash.ToLowerInvariant() -cne $reproImageSHA512) { throw 'The independent Debian vendor image differs from its verified SHA256/SHA512 pins.' }
$reproPublicKeyFile = Assert-ReprovisionPath 'C:\ProgramData\Stackfort\Hyper-V\keys\stackfort-host-test-ed25519.pub'
if ((Get-Item -LiteralPath $reproPublicKeyFile).Length -gt 512) { throw 'Authorized public key exceeds its bound.' }
$reproAuthorizedKey = Convert-ReprovisionPublicKey (Get-Content -LiteralPath $reproPublicKeyFile -Raw)
$reproRunID = [Guid]::NewGuid().ToString('N')
$reproInstanceID = "native-reprovision-$reproRunID"
$reproNonce = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).ToLowerInvariant()
$reproRoot = Assert-ReprovisionPath (Join-Path $reproWork $reproInstanceID)
New-Item -ItemType Directory -Path $reproRoot | Out-Null
$reproACL = [Security.AccessControl.DirectorySecurity]::new()
$reproACL.SetAccessRuleProtection($true, $false)
foreach ($principal in @('S-1-5-18', 'S-1-5-32-544')) {
    $reproACL.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new([Security.Principal.SecurityIdentifier]::new($principal), 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow'))
}
Set-Acl -LiteralPath $reproRoot -AclObject $reproACL
$reproSystem = Join-Path $reproRoot 'system.vhdx'
$reproSeed = Join-Path $reproRoot 'cidata.vhdx'

# Capture only bounded diagnostics from the fixed conversion utilities. A failed
# preparation is retained for inspection, never deleted or attached automatically.
function Invoke-ReprovisionTool([string] $Executable, [string[]] $Arguments, [int] $Seconds = 900) {
    $info = [Diagnostics.ProcessStartInfo]::new($Executable)
    $info.UseShellExecute = $false; $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true; $info.RedirectStandardError = $true
    foreach ($argument in $Arguments) { $info.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::new(); $process.StartInfo = $info; $started = $false
    try {
        if (-not $process.Start()) { throw 'Could not start the fixed conversion utility.' }
        $started = $true
        $outBuffer = [char[]]::new(4096); $errBuffer = [char[]]::new(4096)
        $output = [Text.StringBuilder]::new(); $errorOutput = [Text.StringBuilder]::new()
        $stdout = $process.StandardOutput.ReadAsync($outBuffer, 0, $outBuffer.Length)
        $stderr = $process.StandardError.ReadAsync($errBuffer, 0, $errBuffer.Length)
        $deadline = [DateTime]::UtcNow.AddSeconds($Seconds)
        while (-not $process.HasExited -or $null -ne $stdout -or $null -ne $stderr) {
            if ([DateTime]::UtcNow -gt $deadline) { throw 'Fixed conversion utility timed out.' }
            if ($null -ne $stdout -and $stdout.IsCompleted) {
                $count = $stdout.GetAwaiter().GetResult()
                if ($output.Length + $count -gt 65536) { throw 'Conversion stdout exceeds its bound.' }
                [void] $output.Append($outBuffer, 0, $count)
                $stdout = if ($count -eq 0) { $null } else { $process.StandardOutput.ReadAsync($outBuffer, 0, $outBuffer.Length) }
            }
            if ($null -ne $stderr -and $stderr.IsCompleted) {
                $count = $stderr.GetAwaiter().GetResult()
                if ($errorOutput.Length + $count -gt 65536) { throw 'Conversion stderr exceeds its bound.' }
                [void] $errorOutput.Append($errBuffer, 0, $count)
                $stderr = if ($count -eq 0) { $null } else { $process.StandardError.ReadAsync($errBuffer, 0, $errBuffer.Length) }
            }
            [Threading.Thread]::Sleep(10)
        }
        if ($process.ExitCode -ne 0) { throw 'Fixed conversion utility failed; prepared files remain unattached.' }
        return $output.ToString()
    } finally {
        if ($started) { try { if (-not $process.HasExited) { $process.Kill($true); [void] $process.WaitForExit(3000) } } catch { } }
        $process.Dispose()
    }
}
$reproQemu = Assert-ReprovisionPath 'C:\Program Files\qemu\qemu-img.exe'
Write-Host 'Preparing a standalone vendor Debian VHDX and fresh public-key-only seed; the VM remains unchanged.'
[void] (Invoke-ReprovisionTool $reproQemu @('convert', '-f', 'qcow2', '-O', 'vhdx', '-o', 'subformat=dynamic', $reproImage, $reproSystem))
[void] (Invoke-ReprovisionTool 'C:\Windows\System32\compact.exe' @('/U', '/F', '/Q', $reproSystem) 30)
[void] (Invoke-ReprovisionTool 'C:\Windows\System32\fsutil.exe' @('sparse', 'setflag', $reproSystem, '0') 30)
Resize-VHD -Path $reproSystem -SizeBytes 50GB
$reproSystemInfo = Get-VHD -Path $reproSystem
if ($reproSystemInfo.VhdType -ne 'Dynamic' -or $reproSystemInfo.ParentPath -or $reproSystemInfo.Size -ne 50GB -or $reproSystemInfo.Attached) { throw 'Converted OS disk is not an independent, unattached 50-GiB dynamic VHDX.' }
New-VHD -Path $reproSeed -Dynamic -SizeBytes 64MB -BlockSizeBytes 1MB | Out-Null

# Fresh cloud-init uses Python already supplied by the vendor image/cloud-init,
# not an added package. Only the named .pub file can be opened; no secret input.
$reproGuestTemplate = @'
#!/usr/bin/python3
import base64, fcntl, json, os, stat, time, uuid

INSTANCE = "@INSTANCE@"
NONCE = "@NONCE@"
MACHINE = "@MACHINE@"
IMAGE = "@IMAGE@"
KEY = b"stackfort.native.reprovision.public.v1"
POOL = "/var/lib/hyperv/.kvp_pool_1"
RECORD = 512 + 2048
MAXIMUM = 128 * RECORD

def read_bounded(path, maximum):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise RuntimeError("Unsafe public identity input")
        value = os.read(fd, maximum + 1)
        if len(value) > maximum:
            raise RuntimeError("Public identity input exceeds bound")
        return value.decode("ascii").strip()
    finally:
        os.close(fd)

def main():
    if os.geteuid() != 0:
        raise RuntimeError("Public KVP writer requires root")
    if read_bounded("/var/lib/cloud/data/instance-id", 128) != INSTANCE:
        raise RuntimeError("Unexpected cloud instance")
    machine = read_bounded("/sys/class/dmi/id/product_uuid", 64).lower()
    boot = read_bounded("/proc/sys/kernel/random/boot_id", 64)
    if machine != MACHINE or str(uuid.UUID(boot)) != boot:
        raise RuntimeError("Unexpected VM or boot identity")
    parts = read_bounded("/etc/ssh/ssh_host_ed25519_key.pub", 512).split()
    if len(parts) not in (2, 3) or parts[0] != "ssh-ed25519":
        raise RuntimeError("Missing canonical public host key")
    raw = base64.b64decode(parts[1], validate=True)
    prefix = b"\x00\x00\x00\x0bssh-ed25519\x00\x00\x00\x20"
    if len(raw) != 51 or not raw.startswith(prefix) or base64.b64encode(raw).decode("ascii") != parts[1]:
        raise RuntimeError("Invalid public host key wire format")
    payload = json.dumps(dict(schemaVersion=1, kind="fresh-debian-public-host-key", instanceId=INSTANCE,
        nonce=NONCE, machineId=machine, bootId=boot, imageSHA256=IMAGE,
        hostKey=" ".join(parts[:2])), separators=(",", ":"), sort_keys=True).encode("ascii")
    if len(payload) >= 2048:
        raise RuntimeError("Public report exceeds KVP bound")
    encoded = KEY.ljust(512, b"\0") + payload.ljust(2048, b"\0")
    deadline = time.monotonic() + 120
    while True:
        try:
            directory = os.stat("/var/lib/hyperv", follow_symlinks=False)
            if not stat.S_ISDIR(directory.st_mode) or directory.st_uid != 0 or directory.st_mode & 0o022:
                raise RuntimeError("Unsafe KVP directory")
            fd = os.open(POOL, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
            break
        except FileNotFoundError:
            if time.monotonic() >= deadline:
                raise RuntimeError("KVP daemon pool did not become available")
            time.sleep(1)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or info.st_mode & 0o022:
            raise RuntimeError("Unsafe KVP pool")
        # POSIX lockf, NOT flock: this matches hv_kvp_daemon's F_SETLKW lock.
        deadline = time.monotonic() + 5
        while True:
            try:
                fcntl.lockf(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() >= deadline:
                    raise RuntimeError("KVP pool lock timed out")
                time.sleep(0.1)
        existing = os.pread(fd, MAXIMUM + 1, 0)
        if len(existing) > MAXIMUM - RECORD or len(existing) % RECORD:
            raise RuntimeError("KVP inventory exceeds bound or is incomplete")
        matching = [existing[offset:offset + RECORD] for offset in range(0, len(existing), RECORD)
            if existing[offset:offset + 512].split(b"\0", 1)[0] == KEY]
        if matching:
            if matching != [encoded]:
                raise RuntimeError("Conflicting existing public KVP report")
            return
        # Append under the daemon's lock; never truncate or replace unrelated records.
        offset = len(existing)
        while offset - len(existing) < len(encoded):
            count = os.pwrite(fd, encoded[offset - len(existing):], offset)
            if count <= 0:
                raise RuntimeError("Public KVP report write failed")
            offset += count
        os.fsync(fd)
    finally:
        os.close(fd)

if __name__ == "__main__":
    main()
'@
$reproGuest = $reproGuestTemplate.Replace('@INSTANCE@', $reproInstanceID).Replace('@NONCE@', $reproNonce).Replace('@MACHINE@', $reproIdentity.DMI).Replace('@IMAGE@', $reproImageSHA)
$reproGuestIndented = ($reproGuest -replace "`r", '' -split "`n" | ForEach-Object { '      ' + $_ }) -join "`n"
$reproUserData = @"
#cloud-config
preserve_hostname: false
hostname: $reproVMName
manage_etc_hosts: true
ssh_pwauth: false
disable_root: true
ssh_deletekeys: true
ssh_genkeytypes: [ed25519]
ssh_quiet_keygen: true
ssh_publish_hostkeys:
  enabled: false
users:
  - name: stackfort-test
    gecos: Disposable vendor-OS host test
    groups: [sudo]
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    lock_passwd: true
    ssh_authorized_keys:
      - $reproAuthorizedKey
package_update: true
package_upgrade: false
packages: [sudo, hyperv-daemons, curl, ca-certificates]
write_files:
  - path: /usr/local/sbin/native-reprovision-public-report.py
    owner: root:root
    permissions: '0750'
    content: |
$reproGuestIndented
runcmd:
  - [/usr/bin/python3, /usr/local/sbin/native-reprovision-public-report.py]
final_message: Fresh Debian vendor OS initialization finished.
"@
$reproUserData = ($reproUserData -replace "`r", '') + "`n"
$reproMetaData = "instance-id: $reproInstanceID`nlocal-hostname: $reproVMName`n"
$reproMounted = $false
try {
    $mounted = Mount-VHD -Path $reproSeed -PassThru
    $reproMounted = $true
    if ([IO.Path]::GetFullPath($mounted.Path) -cne $reproSeed) { throw 'Mounted seed path differs from the exclusively created seed.' }
    $disk = Get-Disk -Number $mounted.DiskNumber
    if ($disk.IsBoot -or $disk.IsSystem -or $disk.Size -ne 64MB -or $disk.PartitionStyle -ne 'RAW') { throw 'Refusing to initialize any disk other than the fresh raw 64-MiB seed.' }
    $partition = Initialize-Disk -Number $disk.Number -PartitionStyle MBR -PassThru | New-Partition -UseMaximumSize -AssignDriveLetter
    $volume = $partition | Format-Volume -FileSystem FAT -NewFileSystemLabel cidata -Confirm:$false
    $seedRoot = "$($volume.DriveLetter):\"
    foreach ($entry in @(@{ Name = 'user-data'; Value = $reproUserData }, @{ Name = 'meta-data'; Value = $reproMetaData })) {
        $path = Join-Path $seedRoot $entry.Name
        $stream = [IO.File]::Open($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        try { $bytes = [Text.UTF8Encoding]::new($false).GetBytes($entry.Value); $stream.Write($bytes); $stream.Flush($true) } finally { $stream.Dispose() }
    }
} finally { if ($reproMounted) { Dismount-VHD -Path $reproSeed } }
$reproSeedInfo = Get-VHD -Path $reproSeed
if ($reproSeedInfo.ParentPath -or $reproSeedInfo.Attached -or $reproSeedInfo.Size -ne 64MB) { throw 'Seed did not return to an independent unattached state.' }
$reproManifest = [ordered]@{
    SchemaVersion = 1; Stage = 'prepared-not-attached'; CreatedAt = [DateTimeOffset]::UtcNow.ToString('o'); Root = $reproRoot
    VMID = $reproVMID; VMName = $reproVMName; DMI = $reproIdentity.DMI; InstanceID = $reproInstanceID; Nonce = $reproNonce
    SourceImage = $reproImage; SourceSHA256 = $reproImageSHA; SourceSHA512 = $reproImageSHA512
    SourceURL = 'https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2'
    SourceAuthentication = 'official HTTPS plus published SHA512SUMS; no detached signature verified'
    SystemDisk = $reproSystem; SystemDiskID = ([Guid] $reproSystemInfo.DiskIdentifier).ToString('D'); SystemDiskSize = 50GB
    SystemDiskInitialSHA256 = (Get-FileHash -LiteralPath $reproSystem -Algorithm SHA256).Hash.ToLowerInvariant()
    SeedDisk = $reproSeed; SeedDiskID = ([Guid] $reproSeedInfo.DiskIdentifier).ToString('D'); SeedDiskSize = 64MB
    SeedSHA256 = (Get-FileHash -LiteralPath $reproSeed -Algorithm SHA256).Hash.ToLowerInvariant()
    UserDataSHA256 = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($reproUserData))).ToLowerInvariant()
    KVPKey = $reproKVPKey; Packages = @('sudo', 'hyperv-daemons', 'curl', 'ca-certificates')
    CopiedInstalledDisk = $false; CopiedOldSeed = $false; PrivateKeyRead = $false; VMModified = $false; KnownHostsModified = $false
}
$manifestStream = [IO.File]::Open((Join-Path $reproRoot 'reprovision.json'), [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
try { $bytes = [Text.UTF8Encoding]::new($false).GetBytes(($reproManifest | ConvertTo-Json -Depth 5) + "`n"); $manifestStream.Write($bytes); $manifestStream.Flush($true) } finally { $manifestStream.Dispose() }
Write-Output ([pscustomobject] $reproManifest)
