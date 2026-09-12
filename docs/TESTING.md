# Reliability contracts

The tests are executable product requirements. A green run means the covered
scenarios passed; it does not imply that every Internet/NAT topology or physical
power-loss behavior has been tested. The original review is in
[RELIABILITY_TEST_PLAN.md](RELIABILITY_TEST_PLAN.md); this guide describes the
implemented suite.

Run the normal suite without Docker:

```sh
go test ./... -count=1 -timeout=5m
go vet ./...
go build ./cmd/...
```

Run the real CLI/container scenarios separately. Missing Docker is a failure,
never an automatic skip:

```sh
go test -tags=docker ./test/integration -run '^TestDocker' -count=1 -timeout=20m
```

On a Linux development environment with a C compiler:

```sh
go test -race ./... -count=1 -timeout=8m
go test ./internal/network/... ./internal/backend/p2p/... -shuffle=on -count=20 -timeout=15m
go test ./internal/backend/p2p/erasure -run '^$' -fuzz '^FuzzErasureRecovery$' -fuzztime=60s
```

Windows installations without a C compiler can run the Linux race suite in a
disposable container:

```sh
docker build -f deploy/docker/Dockerfile.test -t p2pbk-reliability-test .
docker run --rm p2pbk-reliability-test
```

**P0 contract map.** The comments above each test explain the invariant and why
its assertion matters. Test names are recorded in `scripts/check-test-results.py`.

| Requirement | Executable tests | What must remain true |
| --- | --- | --- |
| P0-01: stable identity | `TestDaemonPeerIdentitySurvivesRestart`, `TestDiscoveryIdentitySurvivesRestart`, `TestPeerIdentitySurvivesRestart`, `TestRestoreFromRecoveryBundle` | Restart preserves network identity, saved addresses remain meaningful, corrupt keys fail instead of being replaced, old snapshots restore after storage restarts. |
| P0-02: owner isolation | `TestShardAuthorizationAcrossOwners`, `TestUploadRejectsForgedOrMismatchedReceipt` | An authenticated second owner cannot read, overwrite or delete the first owner's shard even with its full reference. Rejections leave bytes and quota unchanged. Uploads require matching signed receipts. |
| P0-03: recover from another computer | `TestRestoreFromRecoveryBundle`, `TestMissingManifestCannotInitializeReplacementRepository` | Delete the original source/client data, restart all storage processes (losing DHT memory), import only the encrypted checkpoint, reconnect and restore two complete snapshot trees. Wrong passwords do not modify the target. Missing metadata never silently initializes a replacement repository. |
| P0-04: storage mode | `TestRepositoryStorageModeIsStable` | New peer addresses cannot turn a local repository into remote storage; an empty peer book cannot demote a remote repository. Explicit incompatible mode changes fail. |
| P0-05: bounded networking | `TestRPCCancelAfterStreamOpened`, `TestRPCRejectsOversizedMessage`, `TestRPCServerBoundsUnfinishedRequest`, `TestRemoteDiscoveryShutdownWithStalledProviderChannel` | Cancel after the peer has consumed the request; silent/partial responses must not hang. Oversized frames never reach handlers. Unfinished requests and discovery streams terminate within bounds. |
| P0-06: authenticated durable locators | `TestLocatorIsolationAcrossRepositories`, `TestLocatorRejectsWrongKeyAndReplay`, `TestLocatorReadFailureDoesNotEraseLocations`, `TestDurableLocatorSurvivesDHTLoss`, `TestSignedLocatorAcrossRealDHTNodes` | Owners sharing blob names cannot collide. Wrong keys, tampering, rollback and future-dated records are rejected. Lookup failure cannot erase accepted locations. Signed records propagate through real DHT nodes and durable metadata survives DHT loss. |
| P0-07: committed-write durability | `TestFailedOverwritePreservesCommittedBlob`, `TestCommittedBackupSurvivesCrash`, `TestConcurrentStorageWritersDoNotLoseManifests`, `TestDoNotRecreateIsAtomicAcrossWriters` | Fail each upload position or metadata commit, exit a subprocess without defers at journal/shard/commit boundaries, then reopen. Old or newly committed data stays intact; orphan generations are cleaned. Independent writers cannot drop each other's committed manifests. |
| P0-08: degraded restore | `TestRestoreFromRecoveryBundle`, `TestDefaultPolicyEveryRecoverableShardSubset`, `TestDockerRestoreAfterMaxNodeLoss`, `TestDockerRestoreFailsBeyondParity` | Every single peer outage in 2+1 restores with unchanged config and a fresh cache. All 84 six-of-nine subsets work. Beyond parity must fail specifically because shards are insufficient, before the deadline. |
| P0-09: disk budget | `TestQuotaSurvivesRestart`, `TestZeroOfferRejectsRemoteWrites`, `TestConcurrentShardQuota`, `TestFailedShardWritePreservesBytesAndQuota` | Durable bytes count after restart, zero offer disallows growth, concurrent retries count one shard once, failed writes preserve old bytes and accounting. |
| P0-10: usable remote peers | `TestDockerRemoteDiscoveryBackupRestore`, `TestRemoteBackupRequiresWritableStorageCapability`, `TestConnectionsContinuePastStaleAndMalformedPeers` | Start a client with only discovery infrastructure, discover storage nodes, back up and restore through the CLI. Infrastructure stores no shards. A merely connected non-storage peer does not satisfy write admission. Stale addresses cannot prevent connecting to useful peers. |
| Peer trust | `TestAuditFailuresPersistAndReduceTrust` | Verify healthy data first, then corrupt/remove the shard. Audit failure and reduced trust survive a database reopen. |

The restore oracle compares exact relative paths, directories, file sizes and
SHA-256 hashes, including repeated basenames, Unicode names, empty files and empty
directories. A basename search or an exit code alone cannot replace that oracle.
The erasure fuzz target varies payload, policy and loss independently and bounds
input size to keep CI resource use predictable.

**Changes required to satisfy these contracts.**

- Network identities live in `network.key`. Keep this file across daemon and
  discovery restarts. Shard ownership is the authenticated network peer ID, not a
  display name. Network receipts use the storage host's signing key.
- `storage.mode` pins the repository's backend; configuration accepts
  `storage.mode=auto|local|remote`. Auto selects at first open. Configured remote
  infrastructure requests remote protection and cannot silently fall back to disk.
- `manifests.json` commits are serialized across processes. Overwrites use new
  shard generations; `.pending` records cleanup work before uploads and deletion.
  Files are written to a temporary file, synced and replaced. Abrupt-exit tests
  exercise process-crash boundaries; they do not simulate hardware write-cache loss.
- `locators.json` contains authenticated durable locations. DHT publication is
  best effort after the local record commits. The local checkpoint is the recovery
  authority; public DHT availability is not required to remember accepted shards.
- Read admission is independent of the configured write peer minimum. Write
  admission probes storage capability. Insufficient capacity produces an error;
  this is not an automatic long-term repair/rebalancing service.
- Quotas reconstruct from stored shard files. Zero means no offered capacity.
  Wire frames and request times are bounded, and cancellation resets open streams.

**Recovery workflow.** A remote backup needs an external recovery checkpoint.
Refresh it after successful backups and keep it on another device. A bundle
describes snapshots known at export time; it is not a live metadata replication
service and cannot restore data that peers no longer retain.

```sh
# P2PBK_REPOSITORY_PASSWORD must be set in the environment.
p2pbk --config /path/to/config.json recovery export /offsite/recovery.age

# On a replacement computer; the destination directory must not already exist.
p2pbk recovery import /offsite/recovery.age /new/p2pbk-data
p2pbk --config /new/p2pbk-data/config.json repo init
p2pbk --config /new/p2pbk-data/config.json restore <snapshot-id> /restore-target
```

The authenticated encrypted bundle contains identity/network keys, pinned mode,
manifests, durable locators and known peer addresses. It excludes the source tree,
shard cache and local shard store. Import resets protected roots and disables
scheduling until the replacement computer is configured. Local-only repositories
cannot use this bundle to recover lost local shards.

Existing experimental repositories without the new mode/ownership metadata are
rejected rather than silently migrated. Do not point this revision at the only
copy of a legacy repository; no automatic legacy ownership migration is included.
The public discovery image uses `/data` for its persistent identity: mount that
directory when deploying it.

**CI and AI development rules.**

`.github/workflows/reliability.yml` runs Windows/Linux contracts, Linux races and
Docker scenarios on PRs and pushes. Scheduled/manual runs add shuffled stress and
fuzzing. Jobs have timeouts, retain JSON evidence and do not retry failures to green.
The checker rejects skipped, absent or failed P0 tests so deleting a contract or
filtering it out cannot silently pass the gate. Configure the repository's branch
rules to require these jobs; merely adding a workflow does not enable branch
protection.

Capture evidence and check execution explicitly:

```sh
go test -json ./... -count=1 -timeout=5m > test-results-fast.jsonl
python scripts/check-test-results.py --suite fast test-results-fast.jsonl
```

Keep the original failure test when fixing a bug. Review changes to assertions,
fixtures, timeouts, skip logic, the execution inventory and workflow with the same
care as product code. Never satisfy a negative test with an unrelated configuration
error or timeout. Use deterministic fault seams and fresh clients for recovery;
retain real network tests to check the wiring that fakes cannot prove.

Remaining separate product work includes automatic repair/rebalancing, retention
and maintenance enforcement, full two-NAT/relay-outage topology coverage, upgrade
migration, extended workload/soak tests, and complete OS metadata fidelity. Those
are not claims made by the implemented P0 suite.

Workflow action usage follows the official
[checkout](https://github.com/actions/checkout),
[setup-go](https://github.com/actions/setup-go), and
[upload-artifact](https://github.com/actions/upload-artifact) documentation.
