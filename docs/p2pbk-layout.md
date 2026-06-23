# p2pbk Project Layout

Directory tree only. Each line describes what lives at that path.

```text
p2pbk/
├── go.mod
├── go.sum
├── README.md
├── LICENSE
│
├── cmd/
│   └── p2pbk/                 # one user-facing product binary
│       ├── main.go
│       ├── root.go
│       ├── cmd_init.go         # onboarding, JSON config, background startup
│       ├── cmd_backup.go       # on-demand backup commands
│       ├── cmd_restore.go
│       ├── cmd_status.go
│       ├── cmd_config.go
│       ├── cmd_daemon.go       # hidden background service mode
│       └── cmd_bootstrap.go    # hidden bootstrap/discovery mode
│
├── internal/
│   ├── backend/p2p/            # Kopia-compatible distributed blob storage
│   ├── config/                 # JSON/YAML/env config loading and validation
│   ├── kopiaengine/            # Kopia repository operations
│   ├── network/                # libp2p host, DHT, and wire protocol
│   ├── node/                   # distributed storage assembly
│   ├── peer/                   # services this node offers to other peers
│   ├── trust/                  # identity, receipts, challenges, reputation
│   └── version/                # build-time version metadata
│
├── api/
│   └── wire.proto
│
├── deploy/
│   ├── docker/
│   │   ├── Dockerfile          # builds the single p2pbk image
│   │   └── compose.yaml        # local Docker network scaffold
│   └── systemd/
│       └── p2pbk.service       # runs p2pbk daemon mode
│
├── examples/
│   ├── config.json             # detailed config example
│   ├── config.yaml             # legacy YAML example still accepted by loader
│   └── bootstrap-nodes.txt
│
├── scripts/
│   ├── build-release.sh        # builds the p2pbk binary
│   ├── gen-proto.sh
│   └── install-systemd.sh
│
├── test/
│   └── integration/            # current Go integration tests
│
└── docs/
    ├── DEPLOYMENT.md
    ├── IMPLEMENTATION-SUMMARY.md
    └── p2pbk-layout.md
```

`p2pbk` is the only product executable. The daemon and bootstrap roles remain
available for Docker and operators, but they are subcommands of the same binary.
