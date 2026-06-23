# Deployment Strategy

p2pbk is one product and one user-facing executable:

```bash
p2pbk
```

Normal users start with `init`; they do not need to know about the background
process:

```bash
p2pbk init --dir ~/Documents --max-space 10GB --offer-space 20GB --schedule daily
```

`init` creates identity and local state, writes `config.json`, records backup
policy, explains erasure-code storage overhead, and starts p2pbk in the
background unless `--start=false` is provided.

## Commands

User-facing commands:

```bash
p2pbk init
p2pbk status
p2pbk backup now
p2pbk restore latest ./restore
p2pbk config
```

Advanced/operator commands are hidden from normal help but available for
containers and service managers:

```bash
p2pbk --config /etc/p2pbk/config.json daemon
p2pbk --config /etc/p2pbk/config.json bootstrap
```

## Configuration

Detailed settings live in JSON:

```text
config.json
```

See `examples/config.json` for the full shape.

The important user-level fields are:

```json
{
  "backup": {
    "roots": ["/home/alice/Documents"],
    "max_bytes": 10000000000,
    "schedule": "daily",
    "retention": "30d",
    "initial_full": true,
    "incremental": true
  },
  "storage": {
    "offer_bytes": 20000000000
  }
}
```

`backup.max_bytes` is the amount of source data the user wants to protect.
`storage.offer_bytes` is the local disk space this node contributes to peers.

With the default 6 data shards and 3 parity shards, 10GB of protected source
data requires about 15GB across peers before metadata and cache overhead.

## Peer Discovery

p2pbk now has a local-first discovery path:

```text
configured bootstrap peers
saved peers from localdb
LAN peers discovered through mDNS
```

When local discovery is enabled, a daemon advertises and browses the configured
mDNS service:

```json
{
  "network": {
    "enable_local_discovery": true,
    "local_discovery_service": "_p2pbk._udp"
  }
}
```

Discovered and successfully connected peers are persisted in the local peer
book (`local.db`, `peers` bucket). On the next startup, p2pbk tries configured
peers and saved peers before checking whether enough peers are connected for
storage placement.

Remote discovery can build on the same peer book later by adding signed
rendezvous/DHT records as another peer source.

## Scheduled Backups

The daemon runs due backups for configured roots:

```json
{
  "backup": {
    "roots": ["/home/alice/Documents"],
    "schedule": "daily",
    "initial_full": true,
    "incremental": true
  }
}
```

Supported schedule values include `hourly`, `daily`, `weekly`, or a Go duration
such as `6h`. Use `manual` or `off` to disable daemon scheduling.

The daemon must have the repository password in its environment:

```bash
P2PBK_REPOSITORY_PASSWORD=... p2pbk --config /etc/p2pbk/config.json daemon
```

For distributed storage, it also needs access to the node identity passphrase:

```bash
P2PBK_IDENTITY_PASSPHRASE=...
```

Kopia handles incremental behavior. Each scheduled backup creates a restorable
snapshot of the full source tree, while Kopia uploads only new/changed content
thanks to content-addressing and deduplication.

## Docker

Docker uses the same binary and command surface:

```bash
docker run ghcr.io/OWNER/p2pbk --config /etc/p2pbk/config.json daemon
```

The integration setup uses a Go test to generate and drive Docker Compose with
several `p2pbk daemon` containers and one client/assertion container. Scripts
are not the integration path.

```powershell
$env:P2PBK_DOCKER_E2E="1"
go test ./test/integration -run TestDockerE2EBackupRestore -v
```

To print all container logs even when the test passes:

```powershell
$env:P2PBK_DOCKER_E2E="1"
$env:P2PBK_DOCKER_E2E_LOGS="1"
go test ./test/integration -run TestDockerE2EBackupRestore -v
```

## Native Service

Portable installs can run the same hidden daemon mode under a service manager.
Linux systemd example:

```bash
sudo install -m 0755 p2pbk /usr/local/bin/p2pbk
sudo install -m 0644 deploy/systemd/p2pbk.service /etc/systemd/system/p2pbk.service
sudo systemctl daemon-reload
sudo systemctl enable --now p2pbk.service
```
