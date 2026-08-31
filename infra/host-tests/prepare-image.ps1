# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('debian-13', 'ubuntu-26.04', 'rocky-10')]
    [string] $ImageId,

    [string] $CacheRoot = (Join-Path $PSScriptRoot 'cache')
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$catalogPath = Join-Path $PSScriptRoot 'images.json'
$catalog = Get-Content -LiteralPath $catalogPath -Raw | ConvertFrom-Json
$image = $catalog.images | Where-Object id -EQ $ImageId | Select-Object -First 1
if ($null -eq $image) {
    throw "Unsupported image ID: $ImageId"
}

$imageLocation = if ($null -ne $image.PSObject.Properties['windowsMirror']) {
    $image.windowsMirror
} else {
    $image.image
}
$imageUri = [Uri] $imageLocation
$checksumUri = [Uri] $image.checksums
$allowedImageHosts = @{
    'debian-13'   = 'cloud.debian.org'
    'ubuntu-26.04' = 'cloud-images.ubuntu.com'
    'rocky-10'    = 'mirror.hs-esslingen.de'
}
$allowedChecksumHosts = @{
    'debian-13'   = 'cloud.debian.org'
    'ubuntu-26.04' = 'cloud-images.ubuntu.com'
    'rocky-10'    = 'dl.rockylinux.org'
}
if ($imageUri.Scheme -ne 'https' -or $checksumUri.Scheme -ne 'https' -or
    $imageUri.Host -ne $allowedImageHosts[$ImageId] -or
    $checksumUri.Host -ne $allowedChecksumHosts[$ImageId]) {
    throw "Image catalog entry for $ImageId does not use its allowlisted HTTPS image and checksum hosts."
}

$imageName = [IO.Path]::GetFileName($imageUri.AbsolutePath)
$imagePath = Join-Path $CacheRoot $imageName
$partialPath = "$imagePath.partial"
$checksumPath = "$imagePath.checksums"
New-Item -ItemType Directory -Path $CacheRoot -Force | Out-Null

if (-not (Test-Path -LiteralPath $imagePath -PathType Leaf)) {
    & curl.exe --fail --location --proto '=https' --tlsv1.2 --retry 3 `
        --continue-at - --output $partialPath $imageUri.AbsoluteUri
    if ($LASTEXITCODE -ne 0) {
        throw "Image download failed with exit code $LASTEXITCODE."
    }
}

& curl.exe --fail --location --proto '=https' --tlsv1.2 --retry 3 `
    --output $checksumPath $checksumUri.AbsoluteUri
if ($LASTEXITCODE -ne 0) {
    throw "Checksum download failed with exit code $LASTEXITCODE."
}

$candidatePath = if (Test-Path -LiteralPath $imagePath -PathType Leaf) { $imagePath } else { $partialPath }
$checksumText = Get-Content -LiteralPath $checksumPath -Raw
$escapedName = [Regex]::Escape($imageName)
switch ($image.checksumAlgorithm) {
    'sha256' {
        $match = [Regex]::Match($checksumText, "(?im)^([0-9a-f]{64})\s+\*?$escapedName\s*$")
        $hashAlgorithm = 'SHA256'
    }
    'sha512' {
        $match = [Regex]::Match($checksumText, "(?im)^([0-9a-f]{128})\s+\*?$escapedName\s*$")
        $hashAlgorithm = 'SHA512'
    }
    'sha256-rocky' {
        $match = [Regex]::Match($checksumText, "(?im)^SHA256 \($escapedName\) = ([0-9a-f]{64})\s*$")
        $hashAlgorithm = 'SHA256'
    }
    default {
        throw "Unsupported checksum algorithm: $($image.checksumAlgorithm)"
    }
}
if (-not $match.Success) {
    throw "No vendor checksum was found for $imageName."
}

$expected = $match.Groups[1].Value.ToLowerInvariant()
$actual = (Get-FileHash -LiteralPath $candidatePath -Algorithm $hashAlgorithm).Hash.ToLowerInvariant()
if ($actual -ne $expected) {
    throw "Checksum mismatch for $imageName."
}

if ($candidatePath -eq $partialPath) {
    Move-Item -LiteralPath $partialPath -Destination $imagePath
}
[IO.File]::WriteAllText("$imagePath.verified", "$actual  $imageName`n", [Text.UTF8Encoding]::new($false))
Write-Output $imagePath
