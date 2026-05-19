No# p2pbk — project layout

Directory tree only. No code. Each line describes what lives at that path.

```
p2pbk/
├── go.mod                              # module + pinned deps (kopia, libp2p, reedsolomon, bbolt, cobra, viper, age)
├── go.sum
├── README.md                           # what this is, how it maps to the architecture
├── LICENSE                             # Apache-2.0 (compatible with kopia upstream)
│
├── cmd/                                # binaries — each is a small main package
│   ├── backupd/                        # long-running daemon: watches dirs, serves peers, runs challenges
│   │   └── main.go
│   ├── p2pbk/                          # user CLI: init / add / status / backup / restore / config
│   │   ├── main.go
│   │   ├── cmd_init.go
│   │   ├── cmd_add.go
│   │   ├── cmd_status.go
│   │   ├── cmd_backup.go
│   │   ├── cmd_restore.go
│   │   └── cmd_config.go
│   └── bootstrap/                      # tiny binary for peer discovery, stores no user data
│       └── main.go
│
├── internal/                           # private to this module (not importable elsewhere)
│   │
│   ├── backend/
│   │   └── p2p/                        # THE keystone — implements kopia's blob.Storage
│   │       ├── storage.go              # PutBlob / GetBlob / ListBlobs / DeleteBlob / GetMetadata / ...
│   │       ├── options.go              # Options struct: shards, geo, cache size, min peers
│   │       ├── storage_test.go         # contract tests against kopia's reference suite
│   │       │
│   │       ├── erasure/                # Reed-Solomon split/reconstruct (klauspost/reedsolomon)
│   │       │   ├── codec.go
│   │       │   └── codec_test.go
│   │       ├── placement/              # which peers receive shards (geo, score, ratio)
│   │       │   ├── strategy.go
│   │       │   └── strategy_test.go
│   │       ├── locator/                # DHT mapping: blob_id -> [peer_ids], signed records
│   │       │   ├── locator.go
│   │       │   └── record.go
│   │       ├── dispatcher/             # parallel shard upload/download across peers
│   │       │   ├── dispatcher.go
│   │       │   └── result.go
│   │       ├── cache/                  # local LRU for hot blobs (index/snapshot reads)
│   │       │   └── lru.go
│   │       └── manifest/               # snapshot/index blobs: replicate instead of erasure code
│   │           └── manifest.go
│   │
│   ├── trust/                          # crypto + accountability primitives
│   │   ├── identity/                   # Ed25519 master key, libp2p peer ID derivation, age-encrypted at rest
│   │   │   └── identity.go
│   │   ├── receipts/                   # signed "I am storing your blob" attestations
│   │   │   └── receipts.go
│   │   ├── challenges/                 # proof-of-storage: random byte-range audits
│   │   │   ├── challenges.go
│   │   │   └── issuer.go
│   │   └── reputation/                 # rolling 30-day score per peer; feeds placement
│   │       └── reputation.go
│   │
│   ├── network/                        # libp2p plumbing
│   │   ├── host/                       # host setup (transports, NAT, mTLS, QUIC)
│   │   │   └── host.go
│   │   ├── dht/                        # Kademlia wrapper (peer discovery + locator backing)
│   │   │   └── dht.go
│   │   └── wire/                       # protobuf RPCs on a /p2pbk/1.0.0 protocol id
│   │       ├── wire.go                 # protocol IDs, method names
│   │       ├── handlers.go             # stream handlers for inbound RPCs
│   │       └── pb/                     # generated protobuf code (regenerate via scripts/)
│   │           └── messages.pb.go
│   │
│   ├── peer/                           # services this node OFFERS to other peers
│   │   ├── blobserver/                 # accept and serve foreign shards on disk
│   │   │   ├── blobserver.go
│   │   │   └── layout.go               # on-disk path scheme: $STORE/<owner>/<blob>/<idx>.shard
│   │   ├── responder/                  # answer challenges with merkle/hash proofs
│   │   │   └── responder.go
│   │   ├── quota/                      # enforce promised storage offer
│   │   │   └── quota.go
│   │   ├── selfheal/                   # fsnotify watch on store dir; re-fetch on tamper/delete
│   │   │   └── selfheal.go
│   │   └── localdb/                    # bbolt: peers, receipts, owned blobs, challenge history
│   │       ├── localdb.go
│   │       └── buckets.go              # bucket names + schema constants
│   │
│   ├── config/                         # YAML schema + viper loader, hot reload
│   │   ├── config.go
│   │   ├── defaults.go
│   │   └── validate.go
│   │
│   └── version/                        # build-time version info (-ldflags)
│       └── version.go
│
├── api/
│   └── wire.proto                      # protobuf source for internal/network/wire/pb
│
├── scripts/
│   ├── gen-proto.sh                    # invokes buf/protoc to regenerate pb/
│   ├── build-release.sh                # cross-compile binaries with version stamping
│   └── install-systemd.sh              # install backupd as a system service
│
├── deploy/
│   ├── systemd/
│   │   └── backupd.service             # systemd unit template
│   └── docker/
│       ├── Dockerfile                  # builds backupd + bootstrap images
│       └── compose.yaml                # local 4-node test network
│
├── examples/
│   ├── config.yaml                     # fully commented example config
│   └── bootstrap-nodes.txt             # default bootstrap multiaddrs
│
├── test/
│   ├── integration/                    # spins up N daemons in-process, runs end-to-end backup/restore
│   │   └── e2e_test.go
│   └── fixtures/                       # canned test data
│
└── docs/
    ├── ARCHITECTURE.md                 # the five-layer diagram, written out
    ├── ROADMAP.md                      # phased build order, see below
    ├── THREAT-MODEL.md                 # what we defend against, what we don't
    ├── PROTOCOL.md                     # wire protocol spec (versioned)
    └── OPERATIONS.md                   # running a bootstrap node, monitoring, backups of the bootstrap
```

## What lives where, in one sentence

- **`cmd/`** — three binaries. `backupd` runs forever. `p2pbk` is what the user types. `bootstrap` is the only thing that's not pure P2P.
- **`internal/backend/p2p/`** — the entire reason this project exists. Implements kopia's `blob.Storage`. Everything else exists to make this work.
- **`internal/trust/`** — the four primitives that turn a network of strangers into a backup network: identity, receipts, challenges, reputation.
- **`internal/network/`** — libp2p. Mostly composition of existing pieces.
- **`internal/peer/`** — what the node does FOR the network (vs. what it does WITH the network, which is the backend).
- **`internal/config/`** — single source of truth for user-tunable knobs.
- **`api/wire.proto`** — the only file describing inter-peer messages; everything in `wire/pb/` is generated from it.
- **`test/integration/`** — the only place where you find out if the whole thing actually works.

## Build order (referenced from `docs/ROADMAP.md`)

1. `trust/identity` → `config` → `peer/localdb`. Foundational; everything else depends on these.
2. `backend/p2p/erasure` standalone. Easy win, fully testable in isolation.
3. `network/host` + `network/dht` + `network/wire`. Get two daemons talking.
4. `peer/blobserver` + `dispatcher`. Single shard from one peer to another, end-to-end.
5. `backend/p2p/storage` with a fake dispatcher. Kopia round-trip on one box.
6. Real dispatcher + `placement` + `locator`. First multi-peer backup.
7. `trust/receipts` + `challenges` + `reputation`. Make it trustworthy.
8. `peer/quota` + `selfheal`. Make it polite.
9. `manifest` replicator. Make it durable for the small critical blobs.
10. `cmd/p2pbk` polish + `examples/config.yaml`. Make it usable.
```
