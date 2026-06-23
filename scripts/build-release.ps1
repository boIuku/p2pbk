param(
    [string]$Version = "dev",
    [string]$Commit = "",
    [string]$Date = "",
    [string]$OutputDir = "dist"
)

$ErrorActionPreference = "Stop"

if ($Commit -eq "") {
    try {
        $Commit = (git rev-parse --short HEAD 2>$null).Trim()
    } catch {
        $Commit = "unknown"
    }
    if ($Commit -eq "") {
        $Commit = "unknown"
    }
}

if ($Date -eq "") {
    $Date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
}

$repo = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$out = Join-Path $repo $OutputDir
New-Item -ItemType Directory -Force -Path $out | Out-Null

$goos = (& go env GOOS).Trim()
$exe = if ($goos -eq "windows") { ".exe" } else { "" }
$target = Join-Path $out ("p2pbk" + $exe)
$ldflags = "-X p2pbk/internal/version.Version=$Version -X p2pbk/internal/version.Commit=$Commit -X p2pbk/internal/version.Date=$Date"

Push-Location $repo
try {
    go build -ldflags $ldflags -o $target ./cmd/p2pbk
} finally {
    Pop-Location
}

Write-Host "built $target"
