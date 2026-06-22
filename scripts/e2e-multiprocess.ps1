param(
    [string]$Root = "",
    [switch]$KeepWorkdir
)

$ErrorActionPreference = "Stop"

function RepoRoot {
    return (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}

function Write-NodeConfig {
    param(
        [string]$Path,
        [string]$DataDir,
        [string]$DisplayName,
        [string[]]$BootstrapPeers,
        [int]$MinPeers
    )

    $peerLines = if ($BootstrapPeers.Count -eq 0) {
        "  bootstrap_peers: []"
    } else {
        "  bootstrap_peers:`n" + (($BootstrapPeers | ForEach-Object { "    - $_" }) -join "`n")
    }

    $content = @"
node:
  data_dir: $($DataDir -replace '\\','/')
  identity: identity.age
  display_name: "$DisplayName"

storage:
  store_dir: store
  cache_dir: cache
  cache_size_bytes: 1073741824
  offer_bytes: 10737418240

network:
  listen_addrs:
    - /ip4/127.0.0.1/tcp/0
$peerLines
  protocol_id: /p2pbk/1.0.0
  min_peers: $MinPeers

backup:
  data_shards: 2
  parity_shards: 1
  roots: []

trust:
  challenge_interval: 1h
  reputation_window: 720h
  challenge_max_length: 65536
  identity_passphrase: ""
"@

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    Set-Content -LiteralPath $Path -Value $content -Encoding UTF8
}

function Wait-ForPeerAddress {
    param(
        [string]$LogPath,
        [int]$TimeoutSeconds = 20
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if (Test-Path -LiteralPath $LogPath) {
            $text = Get-Content -LiteralPath $LogPath -Raw -ErrorAction SilentlyContinue
            if ($text -match "listening:\s+(.+/p2p/[A-Za-z0-9]+)") {
                return $Matches[1]
            }
        }
        Start-Sleep -Milliseconds 250
    }

    if (Test-Path -LiteralPath $LogPath) {
        Write-Host "---- $LogPath ----"
        Get-Content -LiteralPath $LogPath
    }
    throw "timed out waiting for peer address in $LogPath"
}

function Stop-StartedProcess {
    param([System.Diagnostics.Process]$Process)

    if ($null -eq $Process -or $Process.HasExited) {
        return
    }

    try {
        $Process.Kill()
        $Process.WaitForExit(5000) | Out-Null
    } catch {
        Write-Warning "failed to stop process $($Process.Id): $_"
    }
}

$repo = RepoRoot
if ($Root -eq "") {
    $Root = Join-Path ([System.IO.Path]::GetTempPath()) ("p2pbk-e2e-" + [Guid]::NewGuid().ToString("N"))
}
$Root = [System.IO.Path]::GetFullPath($Root)

$bin = Join-Path $Root "bin"
$nodes = Join-Path $Root "nodes"
$logs = Join-Path $Root "logs"
$fixture = Join-Path $Root "fixture"
$restore = Join-Path $Root "restore"

New-Item -ItemType Directory -Force -Path $bin, $nodes, $logs, $fixture, $restore | Out-Null

$p2pbkExe = Join-Path $bin "p2pbk.exe"
$backupdExe = Join-Path $bin "backupd.exe"
$bootstrapExe = Join-Path $bin "bootstrap.exe"

Write-Host "workspace: $repo"
Write-Host "workdir:   $Root"
Write-Host "building binaries..."
Push-Location $repo
try {
    go build -o $p2pbkExe ./cmd/p2pbk
    go build -o $backupdExe ./cmd/backupd
    go build -o $bootstrapExe ./cmd/bootstrap
} finally {
    Pop-Location
}

New-Item -ItemType Directory -Force -Path (Join-Path $fixture "nested") | Out-Null
Set-Content -LiteralPath (Join-Path $fixture "nested\file.txt") -Value "hello from multi-process p2pbk" -Encoding UTF8
Set-Content -LiteralPath (Join-Path $fixture "root.txt") -Value "root file" -Encoding UTF8

$processes = @()
try {
    $peerAddrs = @()
    for ($i = 1; $i -le 3; $i++) {
        $nodeDir = Join-Path $nodes "storage-$i"
        $cfg = Join-Path $nodeDir "config.yaml"
        Write-NodeConfig -Path $cfg -DataDir $nodeDir -DisplayName "storage-$i" -BootstrapPeers @() -MinPeers 0

        $log = Join-Path $logs "storage-$i.log"
        Write-Host "starting storage node $i..."
        $proc = Start-Process -FilePath $backupdExe -ArgumentList @($cfg) -RedirectStandardOutput $log -RedirectStandardError "$log.err" -PassThru -WindowStyle Hidden
        $processes += $proc
        $peerAddrs += Wait-ForPeerAddress -LogPath $log
    }

    $clientDir = Join-Path $nodes "client"
    $clientCfg = Join-Path $clientDir "config.yaml"
    Write-Host "initializing client identity..."
    $env:P2PBK_IDENTITY_PASSPHRASE = "identity-pass"
    & $p2pbkExe init --data-dir $clientDir --config $clientCfg --passphrase "identity-pass" | Write-Host
    if ($LASTEXITCODE -ne 0) { throw "p2pbk init failed" }

    Write-NodeConfig -Path $clientCfg -DataDir $clientDir -DisplayName "client" -BootstrapPeers $peerAddrs -MinPeers 3

    $env:P2PBK_REPOSITORY_PASSWORD = "repo-pass"
    Write-Host "initializing Kopia repository over p2pbk storage..."
    & $p2pbkExe --config $clientCfg repo init | Write-Host
    if ($LASTEXITCODE -ne 0) { throw "p2pbk repo init failed" }

    Write-Host "running backup..."
    & $p2pbkExe --config $clientCfg backup $fixture "snapshot-1" | Write-Host
    if ($LASTEXITCODE -ne 0) { throw "p2pbk backup failed" }

    Write-Host "listing snapshots..."
    & $p2pbkExe --config $clientCfg snapshot list | Write-Host
    if ($LASTEXITCODE -ne 0) { throw "p2pbk snapshot list failed" }

    Write-Host "running restore..."
    & $p2pbkExe --config $clientCfg restore latest $restore | Write-Host
    if ($LASTEXITCODE -ne 0) { throw "p2pbk restore failed" }

    $expected = Get-Content -LiteralPath (Join-Path $fixture "nested\file.txt") -Raw
    $restoredFile = Get-ChildItem -LiteralPath $restore -Recurse -File -Filter "file.txt" | Select-Object -First 1
    if ($null -eq $restoredFile) {
        throw "restored file.txt not found under $restore"
    }
    $actual = Get-Content -LiteralPath $restoredFile.FullName -Raw
    if ($actual -ne $expected) {
        throw "restored file content mismatch"
    }

    Write-Host "SUCCESS: multi-process p2pbk backup/restore passed"
    Write-Host "restored file: $($restoredFile.FullName)"
} finally {
    foreach ($proc in $processes) {
        Stop-StartedProcess -Process $proc
    }

    Remove-Item Env:\P2PBK_REPOSITORY_PASSWORD -ErrorAction SilentlyContinue
    Remove-Item Env:\P2PBK_IDENTITY_PASSPHRASE -ErrorAction SilentlyContinue

    if (-not $KeepWorkdir) {
        Remove-Item -LiteralPath $Root -Recurse -Force -ErrorAction SilentlyContinue
    } else {
        Write-Host "kept workdir: $Root"
    }
}
