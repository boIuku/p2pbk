param(
    [string]$Binary = (Join-Path $PSScriptRoot '../p2pbk.exe')
)

$ErrorActionPreference = 'Stop'
$binaryPath = (Resolve-Path $Binary).Path
$demo = Join-Path ([System.IO.Path]::GetTempPath()) ('p2pbk-demo-' + [guid]::NewGuid().ToString('N'))
$source = Join-Path $demo 'source'
$node = Join-Path $demo 'node'
$config = Join-Path $node 'config.json'
$restored = Join-Path $demo 'restored'
$oldPassword = $env:P2PBK_REPOSITORY_PASSWORD

function Invoke-P2PBK {
    & $binaryPath @args
    if ($LASTEXITCODE -ne 0) { throw "p2pbk exited with code $LASTEXITCODE" }
}

try {
    New-Item -ItemType Directory -Path $source | Out-Null
    [System.IO.File]::WriteAllText((Join-Path $source 'hello.txt'), "Hello from p2pbk`n")
    $env:P2PBK_REPOSITORY_PASSWORD = 'demo-only-password'

    Write-Host '1. Initialize a local demo node'
    Invoke-P2PBK --config $config init --data-dir $node --dir $source --start=false
    # Pin local mode before opening the repository; no peers or daemon are needed.
    $settings = Get-Content -Raw $config | ConvertFrom-Json
    $settings.storage.mode = 'local'
    [System.IO.File]::WriteAllText($config, ($settings | ConvertTo-Json -Depth 20), [System.Text.UTF8Encoding]::new($false))

    Write-Host "`n2. Back up the sample file"
    Invoke-P2PBK --config $config backup now
    Write-Host "`n3. Restore it into a separate directory"
    Invoke-P2PBK --config $config restore latest $restored
    $original = [System.IO.File]::ReadAllBytes((Join-Path $source 'hello.txt'))
    $copy = [System.IO.File]::ReadAllBytes((Join-Path $restored 'hello.txt'))
    if ([Convert]::ToBase64String($original) -ne [Convert]::ToBase64String($copy)) {
        throw 'Restored file does not match the original'
    }
    Write-Host "Verified: restored hello.txt matches the original."
    Write-Host "Demo files: $demo"
} finally {
    $env:P2PBK_REPOSITORY_PASSWORD = $oldPassword
}
