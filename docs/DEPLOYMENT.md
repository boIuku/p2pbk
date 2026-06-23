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

## Native Service

Portable installs can run the same hidden daemon mode under a service manager.
Linux systemd example:

```bash
sudo install -m 0755 p2pbk /usr/local/bin/p2pbk
sudo install -m 0644 deploy/systemd/p2pbk.service /etc/systemd/system/p2pbk.service
sudo systemctl daemon-reload
sudo systemctl enable --now p2pbk.service
```
