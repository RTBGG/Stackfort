# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('debian-13', 'ubuntu-26.04', 'rocky-10')]
    [string] $ImageId,
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?$')]
    [string] $FromVersion,
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?$')]
    [string] $ToVersion,
    [Parameter(Mandatory = $true)] [string] $FromArchive,
    [Parameter(Mandatory = $true)] [string] $ToArchive,
    [Parameter(Mandatory = $true)] [string] $PriorSourceDriver,
    [ValidateSet('rehearsal', 'release-candidate')] [string] $Kind = 'rehearsal',
    [string] $Checkpoint = 'stackfort-installer-ready-20260824',
    [switch] $ResetDisposableCheckpoint
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $ResetDisposableCheckpoint) { throw 'Specify -ResetDisposableCheckpoint to restore the named disposable host checkpoint.' }
$vmNames = @{'debian-13'='stackfort-debian-13'; 'ubuntu-26.04'='stackfort-ubuntu-26-04-v2'; 'rocky-10'='stackfort-rocky-10'}
$vmName = $vmNames[$ImageId]
$vmRoot = 'C:\ProgramData\Stackfort\Hyper-V'
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$workRoot = Join-Path $repositoryRoot 'infra\host-tests\work'
$fromPath = (Resolve-Path -LiteralPath $FromArchive).Path
$toPath = (Resolve-Path -LiteralPath $ToArchive).Path
$driverPath = (Resolve-Path -LiteralPath $PriorSourceDriver).Path
$fromHash = (Get-FileHash -LiteralPath $fromPath -Algorithm SHA256).Hash.ToLowerInvariant()
$toHash = (Get-FileHash -LiteralPath $toPath -Algorithm SHA256).Hash.ToLowerInvariant()
$driverHash = (Get-FileHash -LiteralPath $driverPath -Algorithm SHA256).Hash.ToLowerInvariant()
$reportPath = Join-Path $workRoot "upgrade-$FromVersion-to-$ToVersion-$ImageId-$toHash.json"
if (Test-Path -LiteralPath $reportPath) { throw "Evidence already exists; preserve or move it before rerunning: $reportPath" }
$snapshot = Get-VMSnapshot -VMName $vmName -Name $Checkpoint -ErrorAction Stop
if (@($snapshot).Count -ne 1) { throw 'Expected one exact disposable checkpoint.' }
if ((Get-VM -Name $vmName).State -ne 'Off') { throw 'Disposable VM must be Off before checkpoint restoration.' }
Restore-VMSnapshot -VMSnapshot $snapshot -Confirm:$false
Start-VM -Name $vmName | Out-Null
$remoteRoot = $null
$address = $null
$sshOptions = @('-o','BatchMode=yes','-o','ConnectTimeout=5','-o','StrictHostKeyChecking=accept-new',
    '-o',"UserKnownHostsFile=$vmRoot\known_hosts",'-o',"HostKeyAlias=$vmName",'-o','LogLevel=ERROR',
    '-i',"$vmRoot\keys\stackfort-host-test-ed25519")
try {
    $deadline = (Get-Date).AddMinutes(6)
    do {
        $address = (Get-VMNetworkAdapter -VMName $vmName).IPAddresses |
            Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } | Select-Object -First 1
        if ($address) {
            & ssh.exe @sshOptions "stackfort-test@$address" 'true' 2>$null
            if ($LASTEXITCODE -eq 0) { break }
        }
        $address = $null
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    if (-not $address) { throw 'Disposable guest SSH did not become ready.' }
    $actualImage = (& ssh.exe @sshOptions "stackfort-test@$address" '. /etc/os-release; printf "%s-%s" "$ID" "$VERSION_ID"') -join ''
    if ($LASTEXITCODE -ne 0 -or ($actualImage -ne $ImageId -and -not $actualImage.StartsWith("$ImageId."))) {
        throw "Guest image mismatch: $actualImage; expected $ImageId"
    }
    $remoteRoot = (& ssh.exe @sshOptions "stackfort-test@$address" 'sudo mktemp -d /var/tmp/stackfort-upgrade.XXXXXXXX') -join ''
    if ($LASTEXITCODE -ne 0 -or $remoteRoot -notmatch '^/var/tmp/stackfort-upgrade\.[a-zA-Z0-9]{8}$') { throw 'Could not create private guest workspace.' }
    foreach ($item in @(@{Path=$fromPath;Name='from.tar.gz';Hash=$fromHash}, @{Path=$toPath;Name='to.tar.gz';Hash=$toHash}, @{Path=$driverPath;Name='driver.test';Hash=$driverHash})) {
        $upload = "/var/tmp/stackfort-upload-$($item.Hash)"
        & scp.exe @sshOptions $item.Path "stackfort-test@${address}:$upload"
        if ($LASTEXITCODE -ne 0) { throw 'Artifact transfer failed.' }
        & ssh.exe @sshOptions "stackfort-test@$address" "set -eu; printf '$($item.Hash)  $upload\n' | sha256sum --check --strict -; sudo install -m 0700 '$upload' '$remoteRoot/$($item.Name)'; rm -- '$upload'"
        if ($LASTEXITCODE -ne 0) { throw 'Artifact transfer digest or private installation failed.' }
    }
    $command = "sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_UPGRADE_KIND='$Kind' STACKFORT_UPGRADE_ROOT='$remoteRoot' STACKFORT_UPGRADE_FROM='$FromVersion' STACKFORT_UPGRADE_TO='$ToVersion' STACKFORT_UPGRADE_FROM_SHA256='$fromHash' STACKFORT_UPGRADE_TO_SHA256='$toHash' '$remoteRoot/driver.test' -test.v -test.run '^TestDisposableHostUpgradeMatrix$' -test.timeout=35m"
    $output = & ssh.exe @sshOptions "stackfort-test@$address" $command 2>&1
    $exitCode = $LASTEXITCODE
    $output | Tee-Object -FilePath (Join-Path $workRoot "upgrade-$ImageId-$toHash.log") | Write-Output
    if ($exitCode -ne 0) { throw "Upgrade matrix guest qualification failed: $exitCode" }
    $receipts = @($output | Where-Object { "$_".StartsWith('STACKFORT_UPGRADE_RECEIPT ') })
    if ($receipts.Count -ne 1) { throw 'Exactly one completed qualification receipt is required.' }
    $receipt = ("$($receipts[0])".Substring('STACKFORT_UPGRADE_RECEIPT '.Length)) | ConvertFrom-Json
    if ($receipt.from -ne $FromVersion -or $receipt.to -ne $ToVersion -or $receipt.sourceArchiveSHA256 -ne $fromHash -or $receipt.targetArchiveSHA256 -ne $toHash) { throw 'Guest receipt does not match the requested artifacts.' }
    if ((($receipt.scenarios | Sort-Object) -join ',') -ne 'health-rollback,interrupted-recovery,success') { throw 'Incomplete guest scenarios.' }
    $results = @($receipt.scenarios | ForEach-Object {
        [ordered]@{from=$FromVersion;to=$ToVersion;image=$ImageId;architecture='amd64';scenario=$_;sourceArchiveSHA256=$fromHash;targetArchiveSHA256=$toHash;status='passed'}
    })
    $report = [ordered]@{schemaVersion=1;kind=$Kind;results=$results} | ConvertTo-Json -Depth 5
    [IO.File]::WriteAllText($reportPath, $report, [Text.UTF8Encoding]::new($false))
    Write-Output "UPGRADE_EVIDENCE=$reportPath"
} finally {
    # These dedicated guests remain available for diagnostics; their named
    # checkpoint supplies the next clean run. Never remove a user VM or VHD.
    if ((Get-VM -Name $vmName).State -eq 'Running') { Stop-VM -Name $vmName -Force | Out-Null }
}
