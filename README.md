# p2pbk

p2pbk is an experimental peer-to-peer backup system. Its core goal is to expose a distributed storage backend that can satisfy Kopia's blob storage contract while using libp2p peers for shard placement, retrieval, and accountability.

The project is intentionally split into small layers:

- `cmd/` contains binaries: the long-running daemon, user CLI, and bootstrap node.
- `internal/backend/p2p/` will implement Kopia-compatible blob storage.
- `internal/trust/` will hold identity, receipts, challenges, and reputation.
- `internal/network/` will hold libp2p host, DHT, and wire protocol code.
- `internal/peer/` will hold services this node offers to other peers.
- `internal/config/` is the single source of truth for user-tunable settings.

## Current status

Stage 1 scaffold is in place:

- Go module metadata.
- Apache-2.0 license.
- Initial config schema, defaults, validation, and loader.
- Build-time version package.
- Example configuration.

The next stage should add durable identity management and local bbolt state.

## Development

```powershell
go test ./...
```
