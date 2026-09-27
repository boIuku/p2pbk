param(
    [string]$Version = "dev",
    [string]$Commit = "",
    [string]$Date = "",
    [string]$OutputDir = "dist",
    [string]$TargetOS = "",
    [string]$TargetArch = "",
    [switch]$Archive
)

$ErrorActionPreference = "Stop"

$repo = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if ($TargetOS -eq "") { $TargetOS = (& go env GOOS).Trim() }
if ($TargetArch -eq "") { $TargetArch = (& go env GOARCH).Trim() }
if ($Commit -eq "") {
    $Commit = (& git -c "safe.directory=$repo" rev-parse --short HEAD 2>$null).Trim()
    if ($LASTEXITCODE -ne 0 -or $Commit -eq "") { $Commit = "unknown" }
}
if ($Date -eq "") { $Date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ") }

$out = Join-Path $repo $OutputDir
New-Item -ItemType Directory -Force -Path $out | Out-Null
$binDir = Join-Path $out "bin"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$previousGOCACHE = $env:GOCACHE
if ([string]::IsNullOrWhiteSpace($env:GOCACHE)) {
    $env:GOCACHE = Join-Path $repo ".test-results\build\go-cache"
    New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null
}
$exeSuffix = if ($TargetOS -eq "windows") { ".exe" } else { "" }
$ldflags = "-X p2pbk/internal/version.Version=$Version -X p2pbk/internal/version.Commit=$Commit -X p2pbk/internal/version.Date=$Date"

$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$env:GOOS = $TargetOS
$env:GOARCH = $TargetArch
try {
    $artifacts = @(
        @{ Name = "p2pbk$exeSuffix"; Package = "./cmd/p2pbk" },
        @{ Name = "p2pdiscovery$exeSuffix"; Package = "./cmd/p2pdiscovery" }
    )
    foreach ($artifact in $artifacts) {
        $target = Join-Path $binDir $artifact.Name
        & go build -buildvcs=false -trimpath -ldflags $ldflags -o $target $artifact.Package
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $($artifact.Package) with exit code $LASTEXITCODE" }
        if (-not (Test-Path -LiteralPath $target)) { throw "expected build artifact is missing: $target" }
    }

    $moduleManifest = Join-Path $out "modules.json"
    & go list -m -json all > $moduleManifest
    if ($LASTEXITCODE -ne 0) { throw "go list -m -json all failed with exit code $LASTEXITCODE" }
    if (-not (Test-Path -LiteralPath $moduleManifest)) { throw "module manifest is missing" }

    $binaryMetadata = Join-Path $out "binary-metadata.txt"
    Remove-Item -LiteralPath $binaryMetadata -Force -ErrorAction SilentlyContinue
    foreach ($artifact in $artifacts) {
        Add-Content -LiteralPath $binaryMetadata -Value "[$($artifact.Name)]"
        & go version -m (Join-Path $binDir $artifact.Name) >> $binaryMetadata
        if ($LASTEXITCODE -ne 0) { throw "go version -m failed for $($artifact.Name) with exit code $LASTEXITCODE" }
    }

    $checksums = Join-Path $out "sha256sums.txt"
    Get-ChildItem -LiteralPath $binDir -File | ForEach-Object {
        $hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  bin/$($_.Name)"
    } | Set-Content -LiteralPath $checksums -Encoding ascii
    if (-not (Test-Path -LiteralPath $checksums)) { throw "checksum manifest is missing" }

    if ($Archive) {
        $archive = Join-Path $out "p2pbk-$Version-$TargetOS-$TargetArch.zip"
        if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
        Compress-Archive -Path (Join-Path $out "bin"), $moduleManifest, $binaryMetadata, $checksums -DestinationPath $archive
        if (-not (Test-Path -LiteralPath $archive)) { throw "release archive is missing" }
        Write-Host "built $archive"
    }
    Write-Host "built $binDir\p2pbk$exeSuffix"
    Write-Host "built $binDir\p2pdiscovery$exeSuffix"
} finally {
    if ($null -eq $previousGOOS) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $previousGOOS }
    if ($null -eq $previousGOARCH) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $previousGOARCH }
    if ($null -eq $previousGOCACHE) { Remove-Item Env:GOCACHE -ErrorAction SilentlyContinue } else { $env:GOCACHE = $previousGOCACHE }
}
