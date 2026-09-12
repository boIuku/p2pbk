# p2pbk reliability review and test plan

Implementation update: the P0 regression suite and supporting fixes are described
in [TESTING.md](TESTING.md). The review below records the original baseline;
its statements about missing tests and behavior are historical.

Reviewed 2026-09-12 at commit `9adba51`. This review follows the implementation, not just the README. Findings below are code observations and their implications; the missing failure scenarios have not been reproduced unless explicitly stated. This document proposes tests and automation; it does not implement product fixes or CI.

**Assessment.** p2pbk has a working backup prototype and useful component tests. The current tests do not establish that remote backups survive realistic restarts, loss of the owner's computer, hostile peers, or extended outages. Before adding more features, establish a release contract: a reported successful backup remains recoverable after the declared failures, and every unrecoverable operation fails clearly within a bounded time.

**Verification performed.** On Windows with Go 1.21.5:

| Command | Result |
| --- | --- |
| `go test ./internal/... ./cmd/... -count=1 -timeout=3m` | Passed: 28 tested packages; `internal/version` has no tests. |
| `go test ./test/integration/e2e_test.go -count=1 -timeout=3m` | Passed. Exercises distributed blob storage with real local libp2p hosts and an in-memory locator. |
| `go test ./test/integration -count=1 -timeout=10m` | Could not run scenarios: TestMain's Docker build failed because `dockerDesktopLinuxEngine` was unavailable. This is an environment failure, not evidence of a product regression. |

Race detection, fuzzing, real NAT traversal, crash recovery, and cross-platform behavior were not validated in this review. No checked-in `.github` workflows were found.

**Main features and their actual boundaries.**

| Feature | Implementation and current boundary |
| --- | --- |
| Encrypted backup and snapshots | Kopia integration initializes/opens repositories, uploads folders, saves snapshots, and flushes writes. Kopia supplies encryption, chunking and deduplication. The integration needs tests proving these properties survive p2pbk's storage behavior. |
| Restore | Snapshot ID or `latest`, reconstructed shards, Kopia decryption, filesystem output. `latest` is selected across the repository; it is not scoped to one protected root. Restore allows overwriting existing files and symlinks. |
| Local storage | All encoded shards are stored on this machine. This is distinct from LAN peer storage and does not protect against losing this disk. |
| Distributed storage | Reed-Solomon shards go to connected peers; the live path uses signed DHT locator records and local receipts. Placement is scored round-robin. |
| Connections | Explicit bootstrap addresses, saved addresses, local mDNS plus a same-filesystem registry fallback, or remote DHT discovery. TCP and QUIC listen addresses are configured by default. |
| Background operation | Daemon serves shards, runs discovery, scheduled backups and audits. CLI and TUI expose backup, restore, config, roots, peers, status, and daemon controls. |
| Accounting and trust | SQLite stores peers, receipts and audit history. Current audits download whole shards and compare hashes. Challenge primitives also exist, but are not the main audit flow. No automatic shard repair was found. |
| Policies | Retention, maximum protected bytes, incremental toggle and cache size are exposed in config. Their intended runtime effects are not fully wired. Maintenance status is exposed, but this is not a complete tested retention/garbage-collection workflow. |

Source entry points: `internal/kopiaengine/engine.go`, `internal/node/blob_storage.go`, `internal/node/storage.go`, `cmd/p2pbk/cmd_daemon.go`, `internal/audit/audit.go`, `internal/scheduler/scheduler.go`.

**How remote peers work.**

1. The daemon creates a libp2p host. Configured public discovery peers also act as static relay candidates; NAT port mapping and, with relays configured, hole punching are enabled. It connects to public infrastructure, then configured/saved storage peers, and starts a `/p2pbk` DHT server.
2. In `remote` mode it periodically advertises and searches `p2pbk/storage/v1`. It accepts peers with relay, public IP, or DNS addresses. Private-IP-only peers are filtered out; when forced relay reachability is configured, a circuit-relay address is required. DNS is accepted without establishing that it resolves publicly. Individual connection attempts have a five-second timeout.
3. Connected peers are saved in `local.db`. Configured public discovery peer IDs are excluded from storage candidates. Other connected peers are treated as candidates without a storage-capability/capacity handshake.
4. A backup command opens a separate host. It chooses distributed storage only when there are explicit bootstrap peers or saved non-infrastructure peer IDs. It reconnects to those peers, creates a DHT client and freezes a placement list. It does not itself start the remote discovery loop or inherit the daemon's relay/hole-punch options.
5. Kopia writes encrypted blobs. p2pbk splits each blob into shards and uploads them sequentially. Each accepted shard triggers a locator read/modify/write and receipt persistence. After all shards succeed, it saves the blob's erasure manifest in local `manifests.json`.
6. Restore starts from that local manifest, looks up each shard's peer ID in the DHT, fetches shards, and validates against the manifest hashes before decoding. It currently attempts all shard indexes sequentially, even if enough valid shards could already reconstruct the blob.

`remote` is a discovery setting, not a guarantee that a backup is remote. Default public discovery and bootstrap lists are empty. A fresh configuration with no saved storage peers uses local storage, even with `network.discovery=remote`. Conversely, `disabled` disables the discovery service, not configured/saved connections or DHT locator use. `local` means LAN discovery, not necessarily local-disk backup.

The remote design needs three separately proven properties: discovering an address, establishing a usable connection, and keeping previously stored data recoverable. A successful discovery log proves none of the latter two.

**Release blockers and the tests to add first.** P0 means required before claiming reliable remote backup. Several tests should initially fail on the current implementation; pair each with its fix.

| ID | Finding and product requirement | Required regression test and pass criteria |
| --- | --- | --- |
| P0-01 | **Network identities change.** Daemon and public discovery binary do not pass a persistent private key to the host. `identity.age` signs application records but is not wired to those host identities. Locators retain old peer IDs after a storage restart; infrastructure multiaddrs also contain IDs. | `TestDaemonPeerIdentitySurvivesRestart` and `TestDiscoveryIdentitySurvivesRestart`: restart with the same data directory; IDs remain stable. Back up, restart storage/discovery processes, restore old snapshots without rewriting addresses or locators. Test address changes separately from identity changes. |
| P0-02 | **Owner authorization is absent.** Wire handlers receive `context.Background()` without the authenticated remote identity. Blob GET/PUT/DELETE trust the caller-supplied shard ref. Owner is display name or literal `local`. A connected peer knowing a ref can request deletion/overwrite. | `TestShardAuthorizationAcrossOwners`: use two real authenticated clients. B cannot delete, replace, or access A's shards under the chosen access policy; rejected operations leave bytes/accounting unchanged. Bind ownership to a stable cryptographic principal/repository ID, support authorized recovery, and test same display names. |
| P0-03 | **Recovery metadata is not independently recoverable.** Blob listing and erasure manifests depend on local `manifests.json`; locator locations depend on the DHT. Receipts are not a restore fallback. There is no demonstrated recovery from an empty owner machine, DHT state loss or record expiry. | `TestRestoreFromRecoveryBundle`: back up, destroy the client's entire test data/cache/source tree, then recover in a fresh client using only a documented recovery bundle/password and surviving peers. Separately lose `manifests.json`, local DB, repository config and DHT records. Restore exact trees, or explicitly report which mandatory recovery material is missing. Never initialize an empty replacement repository silently. |
| P0-04 | **Storage mode can change when peer state changes.** Mode is recomputed on open and both modes use the same manifest path. Discovering a peer after a local backup can switch the backend for old blobs; losing peer records can switch a remote repository to local. | `TestRepositoryStorageModeIsStable`: local backup then discover peers; remote backup then erase only saved peer records. Existing snapshots stay readable. Persist repository storage mode and implement explicit, verified migration; remote-required backup with no peers must report unavailable protection. |
| P0-05 | **RPC cancellation does not bound stream I/O.** Client context is passed to `NewStream`, but no stream deadline/reset is tied to subsequent read/write cancellation. The nominal shard timeout therefore does not guarantee return when a peer accepts a stream and goes silent. JSON decoding also has no application payload limit. | `TestRPCCancelAfterStreamOpened`, `TestRPCRejectsOversizedMessage`: peer accepts but never replies, dribbles partial JSON, or stops reading a PUT. Calls and daemon shutdown return within a defined bound; stream handlers/goroutines are released; malformed/oversized traffic is rejected before unbounded allocation. Test both client and server. |
| P0-06 | **Locator durability/isolation is incomplete.** DHT keys contain only blob ID, not owner/repository. `recordLocation` treats every lookup error as an empty record and can replace known locations. Updates are not transactional. DHT validation accepts any nonempty value and selects by untrusted timestamp; signature verification happens later at the reader. | `TestLocatorIsolationAcrossRepositories`, `TestLocatorReadFailureDoesNotEraseLocations`, `TestLocatorRejectsWrongKeyAndReplay`: two owners use identical blob IDs; inject timeout, invalid signature, signed record under wrong key, future timestamp and concurrent updates. No cross-repository overwrite or loss of accepted locations. Add authenticated versioning/tombstones, durable local recovery data and republishing. Test real DHT nodes, not only the map fake. |
| P0-07 | **Writes are not a durable transaction.** Shards are written directly with `os.WriteFile`; a failed overwrite can damage old bytes. Blob puts can leave partial shards/locators/receipts; manifest state changes before persistence succeeds. Manifest replacement lacks explicit sync and cross-process coordination. | `TestCommittedBackupSurvivesCrash`: fault points after shard write, receipt, locator update and manifest replacement; disk-full/permission errors; kill and restart at each point. Every acknowledged backup remains readable. Failed writes preserve earlier snapshots, have recoverable pending state, and can be retried without corruption or unbounded orphan growth. |
| P0-08 | **Restore admission uses the write peer minimum.** Opening distributed storage requires `network.min_peers`, even when fewer peers contain enough shards. Default 6+3 over three round-robin peers loses three shards when one peer disappears, which is mathematically recoverable but may fail the minimum-peer check. | `TestRestoreDegradedWithoutConfigEdits`: leave config untouched and stop every allowed combination of peers, then restore in a fresh process/cache. Admission is based on available valid shards for reads. Excess loss returns a specific insufficient-shards failure, not an unrelated startup error. |
| P0-09 | **Quota forgets existing data on restart.** A new in-memory manager starts empty; zero limit means unlimited. Thus `offer_bytes=0` still allows storage if a daemon serves requests. Concurrent same-ref operations can also misaccount bytes. | `TestQuotaSurvivesRestart`, `TestZeroOfferRejectsRemoteWrites`, `TestConcurrentShardQuota`: prefill/restart/fill, grow/shrink/overwrite/delete, simultaneous requests, failed writes and physical disk exhaustion. Usage agrees with durable data; accepted writes cannot exceed the configured offer. Define zero-offer semantics explicitly. |
| P0-10 | **Remote discovery is not proven end to end.** The Docker test only waits for startup. Clients use a different host configuration from the daemon, and candidate selection has no capability check or replacement retry. | `TestRemoteDiscoveryBackupRestore`: fresh owner has only discovery infrastructure addresses, no saved/manual storage peers. Wait for actual storage readiness; create and restore a snapshot through CLI. Infrastructure receives no shards. A reachable non-storage node is excluded; a full/offline storage peer is replaced or produces an honest insufficient-capacity error. |

Evidence anchors: `cmd/p2pbk/cmd_daemon.go:87`, `cmd/p2pdiscovery/main.go:69`, `internal/node/blob_storage.go:23`, `internal/node/blob_storage.go:53`, `internal/node/storage.go:114`, `internal/node/storage.go:198`, `internal/network/wire/wire.go:64`, `internal/network/wire/wire.go:82`, `internal/backend/p2p/distributed_backend.go:147`, `internal/backend/p2p/locator/dht.go:50`, `internal/backend/p2p/locator/dht.go:69`, `internal/network/dht/dht.go:51`, `internal/backend/p2p/storage.go:68`, `internal/backend/p2p/storage.go:285`, `internal/backend/p2p/storage.go:308`, `internal/peer/blobserver/blobserver.go:127`, `internal/peer/quota/quota.go:25`.

**Further tests needed for a stable product.** P1 work should follow the blockers, with the related test added before each implementation change.

| Area | Test scenarios and observable contract |
| --- | --- |
| Restore fidelity and snapshot history | Multi-root trees; repeated basenames in different directories; Unicode/long paths; empty files/directories; binary and seeded incompressible data; large files; symlinks and permissions where supported. Back up A, change/add/delete/rename files, back up B, restore both by ID into separate empty targets. Compare exact relative-path/type/size/hash manifests and supported metadata; detect extra files too. Define root selection for `latest` and overwrite behavior. |
| Deduplication and privacy | An unchanged second snapshot restores identically and avoids re-uploading old content, allowing legitimate new snapshot/index metadata. Measure backend writes rather than requiring zero writes. Search peer files for a known plaintext marker; wrong-password restore fails without producing plaintext. Treat the marker check as an integration regression, not proof of cryptographic security. |
| Erasure policy and failure domains | Exhaust all 84 six-of-nine subsets for the default policy, plus corrupt/duplicate/out-of-order/invalid indexes and insufficient shards. Test multiple shards on one peer, correlated node loss and peers sharing one host. 6+3 guarantees three lost shards, not three lost machines: three peers holding three shards each tolerate one peer loss. Preserve old manifests' coding policy when configuration changes. |
| Repair and long-term availability | Remove/corrupt a shard, audit it, introduce a new peer, reconstruct and re-place, verify receipt/locator updates, restart, restore. Then lose another original peer to prove redundancy was restored. Define repair deadlines, backoff, bandwidth budgets and degraded/unrecoverable status. Audit failure alone is not repair. |
| Receipt authenticity | Reject unsigned, forged, wrong-peer, wrong-ref and wrong-hash receipts before recording a successful placement. Current upload path normalizes missing receipt fields and does not verify signatures. Bind receipt keys to enrolled peer identity; test replay and restart. |
| Audit outcomes | Missing/corrupt shard, timeout, invalid receipt and DB write failure; health/reputation changes persist. Failed audits must not count as healthy. Test fairness if limiting each run so later receipts are eventually checked, and remove/retire receipts for intentionally deleted shards. |
| NAT and relay connectivity | Separate routed/NAT networks, direct TCP, QUIC, UDP blocked with TCP fallback, two private peers requiring relay, failed hole punch with relay fallback, relay reservation expiry/reconnect, relay outage and alternate infrastructure. Block direct paths to prove relay traffic really works. Repeat a small smoke test on controlled separate hosts before release; a shared Docker bridge is insufficient. |
| Discovery recovery | Genuine mDNS across isolated filesystem namespaces so the registry cannot mask failure; namespace isolation; infrastructure initially down then returns; changed DNS/IP/ports; stale/duplicate addresses; private/public/relay filters; shutdown during discovery. Use fake clocks/router fault injection for retry/TTL logic and real nodes for connectivity. |
| Concurrency and process lifecycle | Scheduled and manual backup overlap, two writers to one repository, backup during restore/audit, daemon start twice, stop during uploads, restart after kill. No lost manifest updates, duplicate scheduling or leaked workers. Race detection alone cannot detect cross-process lost updates. Isolate concurrent repositories/passwords; process-global password switching deserves explicit overlap tests. |
| Policy/config contracts | For every exposed field, test its effect or reject unsupported changes. Implement/test max protected size, retention, incremental toggle, cache bound and protocol ID. Current runtime references do not establish enforcement of these fields. Test retention at a fake cutoff: expired snapshots removed, retained/shared data still restorable, offline deletion retried, space eventually reclaimed. |
| Upgrade and resource limits | Golden repositories/config/DB from supported previous releases; read old snapshots and perform a new backup after upgrade. Unsupported versions fail clearly. Test interrupted migrations and preserve recovery copies. Benchmark many small files, growing blob indexes, large blobs, slow peers, memory, disk and relay bandwidth over a declared workload. |

**Existing coverage worth keeping, and where it is misleading.**

- `internal/backend/p2p/erasure/codec_test.go` already tests round trips, empty data, missing/corrupt shards and policy mismatch. Extend these tests instead of duplicating them.
- Storage tests cover CRUD, ranges, metadata, sequential `DoNotRecreate`, manifest persistence and one missing shard. Distributed tests use real local hosts but substitute the locator; they do not prove live DHT persistence or recovery.
- Kopia's local integration test backs up and restores one file. It finds the output by basename, so it does not establish exact directory layout, multiple snapshots, deduplication, cold-cache restore or metadata preservation.
- `TestDockerRestoreAfterMaxNodeLoss` stops one node, then rewrites bootstrap peers **and min_peers**. Keep a test for deliberate reconfiguration, but add the unchanged-config outage case as the release gate.
- `TestDockerRestoreFailsBeyondParity` accepts any command error. Assert an explicit insufficient-shards outcome and confirm the process completed before timeout; a connection/configuration failure must not satisfy the assertion.
- `TestDockerRemoteDiscoveryModeStarts` only starts nodes and checks logs. It does not assert discovered storage peers, transfer a blob, or restore a snapshot.
- Scheduler tests cover interval/due calculation, not execution, failure history, overlap, cancellation or restart. Quota tests cover only an in-memory reserve/release cycle. Receipt signature unit tests do not prove the upload path verifies receipts.

**Automation to build.** Start by making the suites independently runnable. Today `test/integration/docker_harness_test.go:27` unconditionally builds Docker in TestMain, even when selecting only an in-process test. `-short` and `-run` do not avoid that setup. Move Docker tests/harness to their own package or apply a `docker` build tag to every Docker-only file. Keep deterministic component/in-process integration tests in the ordinary suite. If a required Docker job lacks Docker, fail that job; do not silently skip and report a successful gate.

| Trigger | Required automation | Acceptance |
| --- | --- | --- |
| Every PR | Windows and Linux unit/component tests, in-process integration, formatting on changed Go files, `go vet`, build both binaries. Use a pinned, verified toolchain compatible with the dependency set. | Fast reproducible check; nonzero on any failure. Capture structured test output. |
| Every PR | Linux race run and Docker backup/restore contract suite: explicit peers, actual remote discovery, storage restart, unchanged-config degraded restore, cross-owner rejection and stalled RPC. Introduce the new contracts with their fixes. | All required scenarios actually execute. Fixed job timeouts; no automatic retry-to-green. |
| Nightly | Seeded failure combinations, repeated shuffled concurrency/discovery tests, short fuzz runs for codec/manifest/wire/path/locator parsing, network partitions and repair cycles. | Preserve seed, minimized input, logs and scenario state for reproduction. No unexplained flakes. |
| Before release | Recovery from fresh owner, full storage/infrastructure restart, previous-release repository compatibility, controlled NAT/relay smoke, longer disk/connection churn and workload soak. | Exact restored trees and recovery guarantees hold after the declared failures. |

Commands available now:

```powershell
go test ./internal/... ./cmd/... -count=1 -timeout=3m
go test ./test/integration/e2e_test.go -count=1 -timeout=3m
# Requires Docker Desktop Linux engine or a Linux Docker host:
go test ./test/integration -count=1 -timeout=15m
```

Proposed CI commands **after splitting/tagging the Docker harness**:

```sh
go test ./... -count=1 -timeout=5m
go vet ./...
go build ./cmd/...
go test -race ./internal/... ./cmd/... -count=1 -timeout=10m
go test -tags=docker ./test/integration -run '^TestDocker' -count=1 -timeout=20m
go test ./internal/network/... ./internal/backend/p2p/... -shuffle=on -count=20 -timeout=15m
```

Set time budgets from measured CI runtimes. Fuzz commands should name the actual targets once implemented, with bounded input sizes and a bounded fuzz duration; no fuzz targets currently exist. Add dependency vulnerability scanning and reviewed dependency-update PRs, with the same compatibility/recovery gates applied to upgrades.

**Make tests dependable enough for AI changes.**

1. Use narrow injectable interfaces for shard transport, locator, clock, backup runner and durable metadata operations. Model specific errors and crash points; retain real libp2p/Kopia tests to catch wiring mistakes the fakes cannot catch.
2. Use readiness probes that perform the required operation, not just log matches or fixed sleeps. Bound every wait and subprocess, including image builds and cleanup. When a negative test expects failure, distinguish the expected error from deadline expiry.
3. Give each test independent directories, ports, peer identities and network namespaces. Real mDNS/NAT tests must not share a registry or filesystem that supplies a hidden shortcut. Keep public Internet dependencies out of ordinary PR checks.
4. Build Docker once per job, isolate each Compose project, and always collect failure logs with a fresh cleanup context before teardown. Save configs without passwords, peer IDs/addresses, placement counts, receipt hashes, exit codes and failing seed. The current harness prints test commands containing its fixed test password; future real credentials must be redacted. Remove job-created images/resources deliberately.
5. Compare independently generated source/restore manifests. Remove source data and clear only disposable client caches for recovery tests, using a fresh client process. Do not allow a warm cache or local shard copy to satisfy a remote durability test.
6. Put requirements into tests, not implementation details: exact restore, owner isolation, bounded termination, committed durability, idempotent retry, recoverable degradation. Do not test only that a mock was called or that a success line appeared.
7. For each bug, add a regression that fails before the fix and passes afterward. Challenge important tests with deliberate mutations: ignore a hash check, remove authorization, drop a locator update or skip persistence. The relevant test must fail for the intended reason.
8. Require human review of changed assertions, golden fixtures, skips, timeouts and CI gates. AI changes must not turn an error into an accepted result or weaken a recovery check merely to make the suite green. Coverage reports help find untested branches; a percentage alone is not a durability guarantee.

**Recommended implementation order.** First split the harness and install the basic CI gate. Then fix and lock down stable identities/owner authorization, explicit storage mode and bounded RPCs. Next implement durable recovery metadata and transactional/crash-safe writes, then unchanged-config degraded restore and persistent quotas. Complete real discovery/relay tests and receipt validation, followed by repair, retention, upgrades and soak tests. Keep each change small enough that its regression and product behavior can be reviewed together.

The release criterion is practical: from a fresh client, restore the declared snapshot exactly after the supported peer losses and restarts, using only documented recovery material, without manual peer/config repair. When that guarantee cannot be met, the product must state which protection is missing and avoid reporting the backup as safely protected.
