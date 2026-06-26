# p2pbk

p2pbk is an experimental peer-to-peer backup system. Its core goal is to expose a distributed storage backend that can satisfy Kopia's blob storage contract while using libp2p peers for shard placement, retrieval, and accountability.

The project is intentionally split into small layers:

- `cmd/p2pbk/` contains the single user-facing product binary.
- `internal/backend/p2p/` implements Kopia-compatible distributed blob storage.
- `internal/trust/` holds identity, challenges, and reputation.
- `internal/network/` holds libp2p host, DHT, local discovery, and JSON wire protocol code.
- `internal/peer/` will hold services this node offers to other peers.
- `internal/config/` is the single source of truth for user-tunable settings.

## Current status

Stage 1 scaffold is in place:

- Go module metadata.
- Apache-2.0 license.
- Initial config schema, defaults, validation, and loader.
- Build-time version package.
- Example configuration.
- Durable identity management and local SQLite state.

## Usage model

p2pbk is distributed as one user-facing binary:

```bash
p2pbk
```

The normal first command is `init`. It creates identity and local state,
writes a JSON config file, records backup policy, and starts p2pbk in the
background unless `--start=false` is provided:

```bash
p2pbk init --dir ~/Documents --max-space 10GB --offer-space 20GB --schedule daily
```

For 10GB of protected data, the default 6+3 erasure policy stores about 15GB
across peers, plus metadata and cache overhead. Detailed settings are available
in `config.json`.

Day-to-day commands stay user-facing:

```bash
p2pbk status
p2pbk backup now
p2pbk restore latest ./restore
p2pbk peers
p2pbk audit now
```

Nodes discover peers from configured bootstrap addresses, saved peers in
`local.db`, and local LAN mDNS when enabled. Remote discovery/rendezvous can
reuse the same saved peer book later.

When `p2pbk daemon` is running, it checks configured backup roots according to
`backup.schedule` and runs due backups automatically. Set
`P2PBK_REPOSITORY_PASSWORD` in the daemon environment so scheduled backups can
open/create the Kopia repository. Kopia snapshots are logical full snapshots,
but storage is incremental/deduplicated: unchanged content is not uploaded
again.

When distributed storage is used, storage peers return shard receipts and the
owner records local audit history in `local.db`. Audits verify encrypted shard
bytes by hash; storage peers still never receive repository passwords,
plaintext data, or enough information to decrypt backups.

The background peer is an implementation detail exposed as a hidden/advanced
subcommand for containers and operators:

```bash
p2pbk --config /etc/p2pbk/config.json daemon
```

Bootstrap/discovery mode is also part of the same binary:

```bash
p2pbk --config /etc/p2pbk/config.json bootstrap
```

Docker images run the same `p2pbk` binary. Compose-based integration tests will
use that surface rather than scripts.

## Development

Build the local binary:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-release.ps1
.\dist\p2pbk.exe version
```

```powershell
go test ./...
```

Docker integration tests are driven from Go and skipped by default:

```powershell
$env:P2PBK_DOCKER_E2E="1"
go test ./test/integration -run TestDockerE2EBackupRestore -v
```

To print container logs even when the test passes:

```powershell
$env:P2PBK_DOCKER_E2E="1"
$env:P2PBK_DOCKER_E2E_LOGS="1"
go test ./test/integration -run TestDockerE2EBackupRestore -v
```
