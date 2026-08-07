$ErrorActionPreference = "Stop"

$repository = "ryanmccool/static-mangal"
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repository/releases/latest" -UseBasicParsing
$tag = [string]$release.tag_name

if ($tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$' -or $release.draft -or $release.prerelease)
{
    throw "The latest GitHub release is not a stable semantic release."
}

$version = $tag.Substring(1)
$processorArchitecture = [string]$env:PROCESSOR_ARCHITECTURE
$arch = switch ($processorArchitecture.ToUpperInvariant())
{
    "AMD64" { "amd64"; break }
    "X86"   { "386"; break }
    "ARM64" { "arm64"; break }
    default { throw "Unsupported Windows architecture: $processorArchitecture" }
}

$assetName = "static-mangal_${version}_Windows_${arch}.zip"
$checksumName = "checksums.txt"
$assetUrl = "https://github.com/$repository/releases/download/$tag/$assetName"
$checksumUrl = "https://github.com/$repository/releases/download/$tag/$checksumName"
$asset = @($release.assets | Where-Object { $_.name -eq $assetName })
$checksumAsset = @($release.assets | Where-Object { $_.name -eq $checksumName })

if ($asset.Count -ne 1 -or $checksumAsset.Count -ne 1)
{
    throw "The release did not contain exactly one expected archive and checksum asset."
}
if ($asset[0].browser_download_url -ne $assetUrl -or $checksumAsset[0].browser_download_url -ne $checksumUrl)
{
    throw "The release asset URLs did not match the expected GitHub release paths."
}

$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ("static-mangal-install-" + [Guid]::NewGuid().ToString("N"))
$archivePath = Join-Path $temporaryRoot $assetName
$checksumPath = Join-Path $temporaryRoot $checksumName
$extractPath = Join-Path $temporaryRoot "extract"
$installPath = Join-Path $env:LOCALAPPDATA "static-mangal"
$executablePath = Join-Path $installPath "static-mangal.exe"

try
{
    New-Item -ItemType Directory -Path $temporaryRoot -Force | Out-Null
    Invoke-WebRequest -Uri $assetUrl -OutFile $archivePath -UseBasicParsing
    Invoke-WebRequest -Uri $checksumUrl -OutFile $checksumPath -UseBasicParsing

    $escapedAssetName = [regex]::Escape($assetName)
    $checksumLines = @(Get-Content -LiteralPath $checksumPath | Where-Object {
        $_ -match "^(?<hash>[0-9a-fA-F]{64})\s+$escapedAssetName\s*$"
    })
    if ($checksumLines.Count -ne 1)
    {
        throw "The published checksum for $assetName was not found exactly once."
    }
    $checksumMatch = [regex]::Match($checksumLines[0], "^(?<hash>[0-9a-fA-F]{64})\s+$escapedAssetName\s*$")
    $publishedHash = $checksumMatch.Groups["hash"].Value
    $actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash
    if (-not [string]::Equals($publishedHash, $actualHash, [StringComparison]::OrdinalIgnoreCase))
    {
        throw "The downloaded archive checksum did not match."
    }

    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractPath -Force
    $candidatePath = Join-Path $extractPath "static-mangal.exe"
    $candidate = Get-Item -LiteralPath $candidatePath
    if ($candidate.PSIsContainer)
    {
        throw "The archive did not contain a regular static-mangal.exe."
    }

    New-Item -ItemType Directory -Path $installPath -Force | Out-Null
    Move-Item -LiteralPath $candidatePath -Destination $executablePath -Force

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $pathEntries = @()
    if ($null -ne $userPath -and $userPath.Length -gt 0)
    {
        $pathEntries = @($userPath -split ';' | Where-Object { $_.Length -gt 0 })
    }
    if (-not ($pathEntries | Where-Object { $_.TrimEnd('\') -ieq $installPath.TrimEnd('\') }))
    {
        $pathEntries += $installPath
        [Environment]::SetEnvironmentVariable("Path", ($pathEntries -join ';'), "User")
    }
    if (-not (($env:Path -split ';') | Where-Object { $_.TrimEnd('\') -ieq $installPath.TrimEnd('\') }))
    {
        $env:Path = "$env:Path;$installPath"
    }

    Write-Host "Installed static-mangal $tag to $executablePath" -ForegroundColor Green
}
finally
{
    if (Test-Path -LiteralPath $temporaryRoot)
    {
        Remove-Item -LiteralPath $temporaryRoot -Recurse -Force
    }
}
