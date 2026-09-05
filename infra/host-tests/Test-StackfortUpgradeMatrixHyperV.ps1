# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)] [string] $TargetVersion,
    [Parameter(Mandatory=$true)] [string] $TargetArchive,
    [Parameter(Mandatory=$true)] [string] $PriorArchiveDirectory,
    [Parameter(Mandatory=$true)] [string] $PriorDriverDirectory,
    [string] $Catalog = 'packaging/upgrades/supported-releases.json',
    [ValidateSet('rehearsal','release-candidate')] [string] $Kind = 'release-candidate',
    [switch] $ResetDisposableCheckpoint
)

$ErrorActionPreference='Stop'
Set-StrictMode -Version Latest
if (-not $ResetDisposableCheckpoint) { throw 'Specify -ResetDisposableCheckpoint to reset the three disposable guests for each predecessor.' }
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
Push-Location $repositoryRoot
try {
    $planOutput = & go.exe run ./cmd/stackfort-upgrade-matrix --catalog $Catalog --target $TargetVersion
    if ($LASTEXITCODE -ne 0) { throw 'Invalid upgrade support plan.' }
    $plan = ($planOutput -join "`n") | ConvertFrom-Json
    if ($plan.cells.Count -eq 0) { throw 'No supported predecessors; use the clean-installer matrix for the first release.' }
    $targetHash = (Get-FileHash -LiteralPath $TargetArchive -Algorithm SHA256).Hash.ToLowerInvariant()
    $reportPath = Join-Path $repositoryRoot "infra/host-tests/work/upgrade-matrix-$TargetVersion-$targetHash.json"
    if (Test-Path -LiteralPath $reportPath) { throw "Evidence already exists: $reportPath" }
    $results = [Collections.Generic.List[object]]::new()
    $groups = $plan.cells | Group-Object from,image
    foreach ($group in $groups) {
        $cell = $group.Group[0]
        $archive = Join-Path $PriorArchiveDirectory "stackfort-$($cell.from)-linux-amd64.tar.gz"
        if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $cell.sourceArchiveSHA256) { throw "Prior archive differs from catalog: $($cell.from)" }
        $driver = Join-Path $PriorDriverDirectory "upgrade-$($cell.from).test"
        & (Join-Path $PSScriptRoot 'Test-StackfortUpgradeMatrixHyperVVm.ps1') -ImageId $cell.image `
            -FromVersion $cell.from -ToVersion $TargetVersion -FromArchive $archive -ToArchive $TargetArchive `
            -PriorSourceDriver $driver -Kind $Kind -ResetDisposableCheckpoint
        $partPath = Join-Path $repositoryRoot "infra/host-tests/work/upgrade-$($cell.from)-to-$TargetVersion-$($cell.image)-$targetHash.json"
        $part = Get-Content -LiteralPath $partPath -Raw | ConvertFrom-Json
        foreach ($result in $part.results) { $results.Add($result) }
    }
    $report = [ordered]@{schemaVersion=1;kind=$Kind;results=@($results.ToArray())} | ConvertTo-Json -Depth 6
    [IO.File]::WriteAllText($reportPath, $report, [Text.UTF8Encoding]::new($false))
    if ($results.Count -ne $plan.cells.Count) { throw 'Generated evidence does not cover the complete plan.' }
    Write-Output "UPGRADE_MATRIX_EVIDENCE=$reportPath"
} finally { Pop-Location }
