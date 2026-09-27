# Reliable MVP: implementation contract and agent prompts

Created: 2026-09-27. Status: implementation plan, not a claim of completed work.

## 1. Objective and scope

Deliver a dependable p2pbk MVP whose released executables, configuration, CLI,
terminal UI, daemon, peer storage, discovery infrastructure, recovery tools, and
installation workflow work together. Completing individual packages is not enough.
Every existing test must continue to pass, including the reliability contracts.
New behavior must have meaningful regression and executable integration coverage.

The product promise is scheduled, encrypted folder backup to explicitly approved
peers, recovery after losing the original client computer, and continued
recoverability after one storage machine fails. Automatic repair restores that
failure tolerance when eligible replacement capacity is available.

The first supported release profile is Windows amd64 and Linux amd64, ordinary
file/folder backups, and a small approved peer group. Preserve the existing local
storage mode and clearly distinguish its protection from remote storage. Keep the
existing CLI commands and both `p2pbk` and `p2pdiscovery` operational.

The reference remote topology has three independent storage machines and spare
capacity on a fourth machine. Default 6+3 coding can put three shards on each of
three machines: it tolerates one such machine loss, not three machine losses.
Peer IDs do not prove machine independence; support explicit failure-domain
configuration and describe the physical deployment assumption. The client and
external recovery destination must not share the failure domain being protected.

Keep public discovery/relay support for approved peers. Unrestricted storage for
strangers, economic incentives, Sybil resistance for an open marketplace, mobile
apps, a web dashboard, macOS qualification, and live database/VM-consistent
snapshots are outside this MVP. Existing features outside the release profile
must not silently regress; describe their support status accurately.

### Sources and baseline

- [Current reliability contracts](TESTING.md) are the current behavioral baseline.
- [Historical reliability review](RELIABILITY_TEST_PLAN.md) documents the original
  baseline; its old missing-feature statements are not necessarily current.
- [Codebase guide](CODEBASE_GUIDE.md) is a navigation aid; verify behavior in code.
- [Required test inventory](../scripts/check-test-results.py) currently defines
  28 distinct fast contract tests and 5 Docker contract tests. These are a subset
  of the complete suites, not permission to run only those tests.
- [CI](../.github/workflows/reliability.yml) already includes Windows/Linux,
  race, Docker, stress, and erasure fuzz jobs. Workflow presence does not prove
  that the jobs ran or that branch protection requires them.
- Existing saved September 12 test output is historical evidence only. Capture a
  fresh baseline against the actual checkout before implementation.

## 2. Reusable master prompt

Copy this prompt into an implementation task. Replace bracketed parameters. Use
`all` for the full delivery request, or a milestone ID for bounded work.

```text
Implement docs/MVP_IMPLEMENTATION_PLAN.md in this repository.

Scope: [all | M0 | M1 | M2 | M3 | M4 | M5 | M6 | M7]
Mode: [implement-and-verify | verify-only]
Evidence directory: [.test-results/mvp/<run-id>]
Supported release platforms: [Windows amd64 and Linux amd64]
Additional user constraints: [none, or explicit constraints]

Read applicable AGENTS.md instructions, this complete plan, docs/TESTING.md,
scripts/check-test-results.py, the workflow, and affected implementation code.
Inspect git state and preserve unrelated user changes. Confirm the actual
baseline rather than treating historical documentation as proof.

For scope=all, execute M0 through M7 in dependency order. Otherwise complete the
selected milestone and its necessary integration work, using verified evidence
for its prerequisites. Keep unfinished release gates explicitly unfinished.
Use the milestone-specific prompt together with this master prompt.

For mode=verify-only, inspect and execute the selected verification gates without
changing product code or tests. Write evidence and ledger/report updates only;
report required fixes instead of silently switching to implementation mode.

Own the complete behavior: backend, durable state, CLI, TUI, service lifecycle,
both binaries, config, documentation, installation, and tests. A helper function,
mock-only demonstration, successful build, or passing selected tests does not
finish a product requirement. Do not replace working architecture wholesale.

Before each behavior change, identify the failure scenario and add or strengthen
an independent regression. Keep existing tests and P0 contract names/guarantees.
Do not delete tests, add skips, weaken assertions, hide failures, narrow required
test selection, or retry until green. Test fixture changes for approved peers or
new versioned formats must retain the original success/failure scenario. If a
test oracle itself is wrong, demonstrate that independently and document the
correction; never change it merely to accept current implementation behavior.

Use typed errors, explicit contexts, bounded resources, deterministic fault
injection, secure defaults, and durable idempotent operations. Reuse Kopia,
libp2p, the current storage contracts, and shared application services. Keep
business logic consistent across CLI, TUI, scheduled jobs, and recovery tools.
Do not implement custom cryptography or a second snapshot engine.

Work in reviewable slices with focused validation, then run the full milestone
gate. Verify the actual built executables and user journeys. Keep a requirements
ledger in docs/MVP_IMPLEMENTATION_STATUS.md, with code/test/evidence references.
Report failures with their actual commands, logs, and environment. Missing Docker,
an unavailable OS, a service permission issue, or incomplete elapsed soak time is
unverified work, never a pass. Continue independent work when a gate is blocked.

Test only disposable repositories, fixtures, service accounts, and containers.
Do not delete real backups, alter the user's live backup service, expose a host
publicly, or publish a release merely to run tests. Prepare release artifacts and
report readiness; publishing or production deployment requires task authorization.

Do not claim completion until the selected milestone's acceptance tests and
cross-cutting contracts pass. For scope=all, distinguish implementation complete
from release-qualified: a real 14-day qualification cannot be simulated or
replaced by a shorter loop. Leave a precise handoff if external gates remain.
```

### Agent work cycle and evidence

For each milestone, record requirement IDs, current behavior, changed files,
regression names, commands, platform, toolchain, revision and working-tree diff,
results, and unresolved limits. Use statuses `pending`, `in progress`,
`implemented`, `verified`, and `blocked`; give a reason and next action for blocks.
M7 additionally records `release-qualified` only after every release gate passes.

Treat test-evidence logs as artifacts, not secrets or product data. Redact real
credentials and use synthetic test passwords. Store bulky logs under the ignored
`.test-results/` directory or CI artifacts; link them from the ledger. Do not mix
results from different code revisions to satisfy one release gate.

Changes to job locks, repair, recovery, and garbage collection require a short
design note identifying durable records, lock order, transaction boundaries,
crash points, versioning, cleanup rules, and restart behavior before implementation.
This is an engineering decision record, not a reason to stop for routine approval.

## 3. Contracts that apply to every milestone

| ID | Required behavior |
| --- | --- |
| C01 | A failed/cancelled new operation cannot invalidate previously committed snapshots. Never silently initialize a replacement repository when metadata is missing. |
| C02 | Reads use the shards actually required for reconstruction, independently of write admission or configured minimum write peers. |
| C03 | Stable network identity, authenticated ownership, signed matching receipts, hash verification, and authenticated locator records survive all changes. |
| C04 | Every remote operation, lock wait, retry loop, discovery attempt, and shutdown has cancellation and a documented time/resource bound. |
| C05 | Durable jobs can be replayed without double accounting, lost manifests, premature deletion, or reporting incomplete work as complete. |
| C06 | CLI, TUI, daemon, and scripts use the same validated application behavior and protection-state calculation. |
| C07 | All exposed configuration fields have documented runtime effects or explicit unsupported-value errors. Invalid settings cannot silently become defaults. |
| C08 | Both binaries build and execute on supported systems. Fresh-install help/version work without config, credentials, or network. |
| C09 | No secret appears in normal config/status output, logs, errors, process command lines, diagnostic bundles, or test evidence. Preserve explicit environment-password compatibility at the boundary. |
| C10 | Existing reliability tests and complete ordinary suites remain mandatory. A test skip, absence, timeout, wrong failure reason, or missing environment is not proof. |
| C11 | Source roots, local peer storage, caches, and external recovery data have distinct lifecycles. Reset/uninstall never implicitly deletes original source files or silently abandons stored peer data. Preserve existing reset refusal tests. |
| C12 | Restore correctness means exact paths/types/bytes and supported metadata, checked by an independently produced manifest. Empty directories, repeated basenames, Unicode, and multiple snapshots are included. |
| C13 | Unknown, stale, offline, degraded, saved, and protected are distinct states. UI refresh or missing evidence cannot manufacture success. |

### Shared protection model

Store and expose separate facts rather than a single optimistic boolean:

- **Snapshot:** absent, running, committed, failed, or cancelled; include incomplete
  source-read errors and identify the affected root and snapshot.
- **Redundancy:** unknown, healthy, degraded-but-recoverable, or unrecoverable;
  derive it per live blob from distinct valid shard indexes and placement domains.
- **Verification:** never checked, fresh, or stale; include the timestamp and
  scope. Historical reputation is not current reachability or an end-to-end restore.
- **Recovery checkpoint:** absent, publishing, current, stale, or failed, with
  the metadata generation and covered snapshot IDs.
- **Scheduling:** active, overdue, paused, or unavailable, with next due time and
  a reason. Manual and scheduled successes update the same per-root history.

For the remote MVP, label a snapshot `protected` only if its committed generation
has policy-compliant placement, sufficiently fresh verification evidence, and a
durable external checkpoint covering the required metadata. Document that this
is evidence as of a timestamp, not a guarantee that a peer cannot fail afterward.
If export fails after snapshot commit, preserve the snapshot, report `saved;
recovery checkpoint pending`, and retry. Never pretend the upload failed to exist.

For D data and P parity shards, after removing any one configured failure domain
there must still be at least D distinct valid indexes for every live blob.
Counting peers, connections, total receipts, or an average across blobs cannot
establish this condition. Local-only repositories must never receive the remote
protection label.

## 4. Milestone order and integration responsibilities

M0 establishes evidence. M1 establishes safe admission and supported dependencies.
M2 provides operation coordination. M3 and M4 build repair/recovery on that
foundation. M5 must account for both before enabling deletion. M6 completes the
user experience and health model. M7 qualifies the complete release.

The UI is not postponed wholesale until M6: wire each new behavior into CLI/TUI
when it is implemented, and fix the event-routing defects in M2 before relying on
the TUI to manage long operations. Land foundational schemas for later milestones
only when the current behavior needs them; avoid speculative frameworks.

## M0. Establish a fresh baseline and whole-product inventory

**Entry:** the current checkout. **Exit:** reproducible baseline evidence and a
requirements ledger; no existing failure is hidden.

Implementation requirements:

- M0.1 Inventory both command trees, hidden commands, CLI flags, all config fields,
  TUI actions, environment variables, on-disk formats, Docker images, and scripts.
  Use the surface matrix in section 5; mark each surface covered or still missing.
- M0.2 Run the existing ordinary, contract-checker, vet/build, race, Docker, and
  bounded stress/fuzz gates where supported. Record historical versus fresh runs.
  Investigate baseline failures rather than declaring them unrelated and ignoring
  them. Preserve their regression cases throughout the work.
- M0.3 Capture sanitized golden fixtures for the current supported experimental
  repository/config/DB/recovery formats before changing them. Explicitly document
  unsupported older formats already rejected by the current code.
- M0.4 Add a portable verification entry point for final implementation work that
  propagates every subprocess failure and writes machine-readable evidence. Keep
  the existing commands and checker independently runnable.
- M0.5 Create the requirements ledger. Treat milestone test names below as proposed
  new tests; they do not exist merely because this plan names them.

Acceptance: both executables' baseline command inventories are captured, all
existing test results are accounted for, and an intentionally failing fixture
causes the verification entry point to fail. No baseline gate can be marked green
from an old JSON file.

```text
Execute M0 under the master prompt. Inventory the full product, run and preserve
fresh baseline evidence, and identify existing failures with reproduction steps.
Create the requirements ledger and sanitized compatibility fixtures. Make the
verification harness fail closed. Do not start a broad product rewrite or claim
the MVP is reliable from a green baseline alone.
```

## M1. Supported dependencies, approved peers, and bounded resource use

**Depends on:** M0. **Primary areas:** `go.mod`, workflows, Dockerfiles,
`internal/network`, `internal/peer`, `internal/trust`, `internal/secrets`, config.

Implementation requirements:

- M1.1 Move build/CI/container toolchains to a currently supported, pinned Go
  release and upgrade compatible dependencies in reviewable groups. Use official
  release/advisory sources and `govulncheck`; record applicability and call paths.
  Do not merely edit version strings or suppress findings. Re-run recovery and
  wire interoperability checks after dependency changes.
- M1.2 Persist explicit owner/storage-peer approval keyed by authenticated peer
  identity. Discovery finds candidates; it never grants storage permission.
  Unknown owners cannot write shards. Distinguish discovery/relay infrastructure
  from storage nodes. Add approve/list/revoke actions through shared app services
  and expose them in CLI/TUI. Define revocation and access to already stored data
  so removal cannot accidentally strand an honest owner's backups.
- M1.3 Enforce total and per-owner storage budgets across concurrent requests and
  restart. Bound per-peer/global active requests, decoded message memory, queues,
  disk reservations, and bandwidth/request rates. Reserve capacity before large
  allocations where possible. Preserve readable committed shards when capacity
  is reduced. Use finite configurable defaults validated under the M7 workload.
- M1.4 Validate untrusted paths, shard references, frame lengths, bundle sizes,
  indexes, and protocol fields, including Windows reserved/path forms and symlink
  escape cases. Rejections must not mutate bytes, quotas, or ownership.
- M1.5 Protect ownership keys with native Windows ACLs and Unix permissions.
  Provide service-compatible OS-protected repository credentials; avoid plaintext
  password persistence. Retain supported environment input without changing the
  process-global password during concurrent repository operations. Test distinct
  repositories/passwords in one process. Missing credentials produce a visible
  locked state, never a generated replacement identity.
- M1.6 Document the threat model and supported peer group. Add automated secret
  redaction checks and a vulnerability-reporting policy. Rate limiting is not a
  claim of resistance to an unrestricted hostile Internet.

Acceptance tests to add and require:

| Proposed test | Observable result |
| --- | --- |
| `TestUnapprovedPeerCannotStoreShard` | Real authenticated but unapproved peer is rejected before storage/accounting mutation. |
| `TestApprovalAndRevocationSurviveRestart` | Persisted admission rules and defined existing-data access work after restart. |
| `TestPerOwnerQuotaAndAdmissionBounds` | Concurrent owners cannot exceed either budget or starve a healthy request indefinitely. |
| `TestHostileInputLeavesStorageUnchanged` | Malformed/oversized/path-escaping requests fail for the intended validation reason. |
| `TestConcurrentRepositoryCredentialsAreIsolated` | Concurrent operations cannot consume another repository's password. |
| `TestSecretsAbsentFromUserVisibleOutput` | Config/status/log/error and service installation outputs contain no fixture secret. |

Keep P0-01/02/05/09 and all discovery contracts. Update integration enrollment
fixtures explicitly; never disable approval globally to restore green tests.
Exercise the real abuse/resource limits under load, including a healthy client
sharing the node with a noisy one. Resolve applicable vulnerability findings or
record a verified non-applicability/mitigation; unreviewed findings block release.

```text
Implement M1 under the master prompt. Establish approved-peer admission, bounded
resource use, credential isolation, and a supported dependency baseline without
weakening existing ownership/receipt/identity contracts. Integrate peer management
and actionable errors into both interfaces. Prove rejection behavior using real
peers and measured resource bounds, then run the full baseline gates.
```

## M2. Unattended service, operation coordination, and resilient uploads

**Depends on:** M1. **Primary areas:** `internal/app`, `internal/scheduler`,
`internal/backend/p2p`, `internal/network`, `internal/tui`, `cmd/p2pbk`, deployment.

Implementation requirements:

- M2.1 Provide service install/start/stop/status/uninstall for Windows service
  management and Linux systemd. Document the service account, data-directory
  permissions, credential access, restart policy, and startup dependencies.
  Installation is idempotent; uninstall leaves backups and keys intact.
- M2.2 One coordinated execution path handles manual, TUI, and scheduled jobs.
  Prevent duplicate daemon instances and overlapping writers across processes.
  Persist operation IDs, affected repository/root, state, attempts, and errors.
  Define lock ordering for backup, repair, maintenance, recovery, and shutdown.
  Avoid competing live hosts presenting one ownership identity without a tested
  connection strategy. Keep read-only inspection responsive.
- M2.3 On reboot/crash, safely resume or retry interrupted work. Retry transient
  peer/network failures with bounded exponential backoff and jitter; distinguish
  permanent authentication/configuration errors. Refresh addresses and try another
  eligible storage peer when one is unavailable/full while preserving placement
  constraints. Retries must not duplicate committed snapshots or quota charges.
- M2.4 Preserve atomic writes, journals, locks, pinned mode, and old manifests.
  Persist retry/cleanup intent before acknowledging acceptance. Handle partial
  success across multiple roots independently and report all failed roots.
- M2.5 Define catch-up after missed schedules, sleep/wake, and clock changes.
  Successful manual and scheduled jobs update the same durable history. A failed
  source read or uncommitted snapshot is never recorded as successful protection.
- M2.6 Propagate command/service contexts. Cancel bounded work and release locks;
  shut down cleanly or leave replayable jobs on forced termination. Dashboard
  closure must have explicit foreground/background semantics.
- M2.7 Fix TUI global event routing before adding more jobs: process status,
  operation, tick, cancellation, and completion events while any form/picker is
  open. Track operation IDs and stale results. Cancelling a password prompt clears
  its pending action. A later unlock must not execute an abandoned restore.
- M2.8 Validate source/state/store/cache/recovery path overlap, including symlink
  aliases. Reject unsafe layouts or explicitly exclude product-owned directories
  from a containing source root with a visible explanation. Backing up a home
  directory must not recursively ingest the repository, shard cache, or newly
  generated recovery bundles. Preserve unrelated source files and verify this
  behavior with a real backup/restore fixture.

Acceptance tests to add and require:

| Proposed test | Observable result |
| --- | --- |
| `TestServiceLifecycleAfterRestart` | Platform integration: one service restarts with the same identity and resumes scheduled work. |
| `TestManualAndScheduledBackupDoNotOverlap` | Multiple real processes cannot race repository writes or silently lose jobs. |
| `TestInterruptedUploadRetriesIdempotently` | Disconnect/kill at upload/commit boundaries; prior snapshots survive and retry does not double-account. |
| `TestUploadReplacesUnavailableOrFullPeer` | Alternate approved capacity is used without violating shard placement. |
| `TestMultipleRootFailuresAreReported` | A failed root cannot hide another result or produce an all-success exit/status. |
| `TestBackupDoesNotRecursivelyIncludeProductState` | A containing source root cannot ingest generated state/store/cache/recovery data or grow recursively across backups. |
| `TestTUIProcessesBackgroundEventsWhileEditing` | Form/picker activity does not stop progress consumption, completion, or refresh scheduling. |
| `TestTUICancelledUnlockClearsPendingAction` | Cancel restore unlock, later enter a password: no abandoned restore starts. |

Use actual subprocess crash tests, service-account tests, and Docker network faults
as well as deterministic unit tests. A fake service-manager call is insufficient
proof of boot/login behavior. Keep every existing committed-write contract.

```text
Implement M2 under the master prompt. Make all user entry points share durable,
cancelable operation coordination. Integrate native service lifecycle, automatic
reconnection/retry, safe multi-root reporting, and the TUI event-routing fixes.
Exercise reboot/crash/disconnect and competing-process scenarios. Preserve old
snapshots and prove that repeated execution is safe, not merely that retries occur.
```

## M3. Automatic redundancy repair and failure-aware placement

**Depends on:** M2. **Primary areas:** `internal/audit`, placement, dispatcher,
storage/locator metadata, local DB, scheduler, shared health/status services.

Implementation requirements:

- M3.1 Maintain a durable inventory of live blobs, coding policy, generations,
  distinct shard indexes, locations, receipts, and verification times. Use each
  stored blob's policy after config changes. Do not infer health from peer count.
- M3.2 Audit live shards fairly with a persistent cursor or equivalent bounded
  schedule. Classify unavailable versus hash-corrupt shards, use retry/backoff for
  transient failures, and queue persistent repair jobs. Retire obsolete receipts
  after committed moves/deletions so they cannot pollute health or starve audits.
- M3.3 Reconstruct only from authenticated/hash-verified shards. Upload replacement
  shards, validate receipts, commit new metadata durably, and only then retire old
  locations. Serialize conflicting repair/backup/delete operations per generation.
  Preserve old readable locations if any replacement/metadata step fails.
- M3.4 Enforce the one-failure-domain tolerance rule from section 3. Respect
  approval, free capacity, and failure domains when selecting a replacement.
  Add enough topology validation to avoid falsely calling three IDs on one host
  independent storage. A full node or missing spare yields actionable degraded
  status, not a fabricated repair success.
- M3.5 Persist repair progress/retries and resume after restart. Bound memory,
  temporary disk, connections, and repair bandwidth; prioritize restore traffic.
  Expose repair status and an explicit request-to-recheck through CLI/TUI.
- M3.6 Produce metadata-change events consumed by M4. Until current external
  recovery metadata is confirmed, report checkpoint pending. Do not destructively
  retire locations still required by a supported recovery checkpoint.

Acceptance tests to add and require:

| Proposed test | Observable result |
| --- | --- |
| `TestRepairRestoresFailureTolerance` | Lose A, repair onto D, then lose B; clear client caches and restore exact bytes. |
| `TestRepairSurvivesCrashAtCommitBoundaries` | Crash before upload, after receipt, and around metadata commit; reopen and converge safely. |
| `TestRepairWithoutCapacityRemainsDegraded` | No candidate capacity causes persistent visible pending work; adding capacity resumes it. |
| `TestPlacementHonorsFailureDomains` | Default 6+3 survives any one declared domain loss; invalid placements cannot be marked healthy. |
| `TestAuditsCoverAllLiveShardsAndRetireOldReceipts` | Time progression reaches every live shard; deleted/moved generations do not recur as false failures. |
| `TestRepairRejectsCorruptOrInsufficientSources` | Too few valid indexes yields an explicit unrecoverable result, never fabricated bytes. |

Keep all 84 default six-of-nine reconstruction cases and existing degraded-restore
tests. Add the full A-to-D-to-B sequence to real CLI/container coverage in M7.
Qualify detection/repair timing against the configured audit interval and measured
transfer time; do not invent a universal deadline independent of workload/network.

```text
Implement M3 under the master prompt. Turn audits into durable repair work and
compute redundancy from live shards and failure domains. Preserve a usable
generation across every crash point, expose pending/degraded states, and prepare
checkpoint coordination. The central proof is losing A, repairing to D, then
losing B and restoring from a fresh client. Do not count a queued repair as healed.
```

## M4. Automatic external recovery checkpoints and safe, exact restores

**Depends on:** M2 and M3's metadata contract. **Primary areas:**
`internal/recovery`, `internal/kopiaengine`, app/CLI/TUI restore and setup flows.

Implementation requirements:

- M4.1 Support a configured external directory as the first checkpoint destination
  (a separately attached device or an OS-mounted remote filesystem). Do not build
  cloud-provider integrations for this MVP. Explain that a second directory on
  the source disk does not survive losing that disk. An unavailable mount must
  not silently become a local fallback directory.
- M4.2 Snapshot a consistent metadata generation under appropriate locks, then
  release locks before slow external I/O. Encrypt/authenticate the bundle, write
  durably, validate it, and atomically publish it. Trigger after committed backups
  and locator/repair/retention changes; persist and coalesce retry jobs. Record
  covered snapshot IDs, version, generation, export time, and validation result.
- M4.3 A successful remote backup distinguishes committed data from a confirmed
  recovery checkpoint. Trigger publication immediately after commit. If the
  destination is unavailable, keep the last valid checkpoint, retry with backoff,
  and display the covered-versus-uncovered generations. Never overwrite the only
  good checkpoint with a partial or corrupt file.
- M4.4 Keep at least two valid generations when two exist. Define checkpoint
  retention and its shard-reference pins with M5. Advertise exactly which older
  checkpoints remain supported; an arbitrary expired copy cannot be promised to
  restore snapshots later intentionally removed by retention. Include required
  identity, locator, mode, config, and peer information, without source/cache data.
- M4.5 Add a recovery verification operation that authenticates/parses the bundle
  and reports its scope without mutating a live repository. Distinguish bundle
  validation from end-to-end data verification. Restore/import must work on a
  clean install without the original config, source directory, DB, or caches.
- M4.6 Import into a new directory only, with restrictive permissions and safe
  staging. Wrong password, truncated bundle, tampering, unsupported version, or
  missing required metadata leaves the destination unchanged and cleans staging.
  Preserve ownership keys and pinned mode; disable source scheduling until the
  replacement client is configured. No silent legacy migration or identity reset.
- M4.7 Default restores to a new/empty destination. An existing conflicting path
  requires explicit overwrite selection in CLI/TUI, with source snapshot, full
  source root, destination, and scope visible before execution. Prevent path
  traversal and symlink escape. Cancellation preserves existing destination data
  and reports any newly restored subset rather than claiming an atomic tree restore.
- M4.8 Use structured repository/source/root identity for snapshot selection.
  Never match by substring or basename. Define `latest` per root and require
  disambiguation for multiple roots. Show source machine/root and timestamp so a
  user can distinguish identical folder names. Keep manual snapshot-ID restore.
- M4.9 Define file fidelity: exact regular-file bytes, paths, empty files/directories,
  timestamps and supported platform permissions/symlinks. Explicitly report
  unsupported ACLs, special files, and unreadable/changing sources; do not quietly
  label incomplete backups as fully protected. Do not claim application-consistent
  database backup without filesystem/application snapshot support.

Acceptance tests to add and require:

| Proposed test | Observable result |
| --- | --- |
| `TestAutomaticCheckpointTracksCommittedGenerations` | Backup and repair commits publish a valid checkpoint with the required metadata. |
| `TestCheckpointFailurePreservesLastGoodGeneration` | Failed write, missing mount, and interrupted publication retain the previous valid bundle. |
| `TestRecoveryImportFailureLeavesTargetUnchanged` | Bad password, tampering, truncation, and unsupported format do not modify the target. |
| `TestFreshClientRestoresAfterRepairAndRestart` | Destroy disposable original client/source/cache, restart all peers, import only current external material, and restore old/new snapshots exactly. |
| `TestRestoreRefusesOverwriteByDefault` | Real CLI and app/TUI paths refuse conflicting destinations; explicit overwrite follows the documented contract. |
| `TestSnapshotSelectionUsesExactSourceIdentity` | Repeated basenames across roots/machines cannot select another source's snapshot. |
| `TestRestoreFidelityAndInterruptedDestination` | Exact path/type/hash and supported metadata checks include Unicode, empty directories, symlinks and interrupted restore. |

Strengthen existing simple Docker restore assertions with the independent full
tree oracle without removing their current checks. Keep
`TestRestoreFromRecoveryBundle` and missing-manifest/wrong-password contracts.

```text
Implement M4 under the master prompt. Make external recovery material a versioned,
automatically maintained part of remote protection. Coordinate its lifetime with
repair and forthcoming garbage collection. Deliver clean-install import and safe
restore through both interfaces, with exact source identity and explicit overwrite
behavior. Prove restoration after removal of the original disposable client and
all its caches; distinguish bundle validation from actual data recoverability.
```

## M5. Retention, quotas, maintenance, and safe space reclamation

**Depends on:** M3 and M4. **Primary areas:** Kopia policies/maintenance, storage
journals, local DB, config, scheduler, status and config interfaces.

Implementation requirements:

- M5.1 Wire retention to supported Kopia policy/maintenance primitives. Define
  per-root retention and keep at least the most recent successful snapshot per
  root by default. A failed backup or unreadable source must not cause the last
  good snapshot to expire. Never implement blob deletion by snapshot filename or
  age alone; retained snapshots share deduplicated data.
- M5.2 Define and enforce `backup.max_bytes` as the total logical regular-file
  bytes in the configured protected roots for a backup set, counting overlapping
  roots once. Distinguish this from historical repository usage and offered peer
  storage. Preflight where possible and enforce during traversal so source growth
  cannot bypass admission. Reject unsupported limit values explicitly.
- M5.3 Enforce actual disk budgets, including bounded staging/cache overhead,
  global/per-owner offered storage, and cache eviction. Preserve old snapshots on
  ENOSPC, permission failure, or interrupted replacement. Report required headroom
  for repair and maintenance instead of deleting retained data to make space.
- M5.4 Implement a durable maintenance/deletion queue with idempotent retries for
  offline peers. Remove a receipt/location only at the correct commit boundary;
  account once across crashes and retries. Cancellation leaves replayable work.
- M5.5 Coordinate garbage collection with snapshots, readers, repair generations,
  and supported recovery checkpoints. Publish a verified successor checkpoint
  before retiring an older recovery generation. Keep referenced blobs/locations
  pinned while that generation is supported. If external publication is failing,
  defer unsafe collection and report retained space rather than invalidating the
  last known-good recovery path. Bound retained checkpoint history with a
  documented policy; never promise indefinite recovery from expired bundles.
- M5.6 Audit every config field, including retention, incremental/initial-full,
  cache size, protocol ID, challenge interval/size, and reputation window. Test
  its actual effect, implement it, or return an explicit unsupported-value error.
  Preserve supported defaults and compatibility contracts. Path/mode changes that
  require migration must not be accepted as ordinary live edits.
- M5.7 Persist config atomically after full validation. Define which settings apply
  live versus after service restart, and make CLI/TUI show that distinction. A
  failed update preserves the old working configuration.

Acceptance tests to add and require:

| Proposed test | Observable result |
| --- | --- |
| `TestRetentionPreservesSharedDataAndLastGoodSnapshot` | Expire eligible snapshots at a controlled clock; retained/last-good trees still restore. |
| `TestProtectedByteLimitEnforcedDuringBackup` | Oversized/growing inputs fail admission honestly without damaging earlier backups. |
| `TestCacheAndStagingRespectDiskBudgets` | Measured cache/staging consumption stays within documented bounds under load. |
| `TestDeletionQueueSurvivesOfflinePeersAndRestart` | Offline deletion is pending, then finishes once after peer return/restart. |
| `TestGCDoesNotInvalidateSupportedRecoveryCheckpoint` | Repeated repair/retention/crash/export failures cannot invalidate a checkpoint still advertised as supported. |
| `TestFailedConfigUpdatePreservesWorkingConfiguration` | Invalid or interrupted config edits leave prior configuration usable. |
| `TestEveryExposedSettingHasAnEnforcedContract` | A table-driven inventory exercises every setting's effect or explicit rejection, rather than checking serialization only. |

Use short fault-injection tests on every change and a real filling-filesystem
scenario during release qualification. Keep all existing quota/crash contracts.

```text
Implement M5 under the master prompt. Finish retention and all exposed capacity
policies using Kopia's supported lifecycle. Design deletion around durable
references from retained snapshots, readers, repair, and supported checkpoints.
Demonstrate reclaimed space and continued exact restoration after offline peers,
disk-full failures, and interrupted maintenance. Do not get a smaller disk usage
number by discarding recoverability or silently ignoring configuration.
```

## M6. Trustworthy status and a usable complete TUI/CLI

**Depends on:** shared status/jobs from M2-M5. **Primary areas:** `internal/app`,
`internal/tui`, `cmd/p2pbk`, logging, service notifications, command tests.

Implementation requirements:

- M6.1 Use the shared protection model from section 3 for all surfaces. Show last
  successful backup, next scheduled run, covered snapshot, last verification,
  policy tolerance, pending repairs, checkpoint freshness, and actionable failure
  reasons. Separate connected/approved/reputation/verified peer counts.
- M6.2 Support human-readable and stable machine-readable status with meaningful
  exit behavior. A health check must fail on the declared unhealthy conditions;
  ordinary status display should distinguish command failure from a successfully
  reported degraded state. Document exit codes and stderr/stdout contracts.
- M6.3 Keep critical failures visible until acknowledged/resolved. Store bounded
  operation history, rotate logs, and expose a sanitized diagnostic view. Add
  persistent warning events and at least one tested unattended notification path
  per supported service environment. Deduplicate repeated alerts and emit recovery
  events; never require users to watch the dashboard to learn backups stopped.
- M6.4 Make initialization a guided basic flow: protected folders, schedule,
  storage mode, approved peers, password/credential handling, external recovery
  destination, and service startup. Show a concrete readiness summary and first
  backup result. Put protocol/shard/network tuning in advanced settings. Explain
  local-only protection. Failed setup is safely retryable with explicit partial
  state; it cannot replace an existing identity.
- M6.5 Finish every UX contract in the table below, including fixes already
  implemented in M2/M4. Preserve normal editing conventions and context-sensitive
  help, with no hidden destructive keyboard shortcut.
- M6.6 Keep configuration, password, service, and error behavior identical across
  CLI and TUI. Share operations, not copied business logic. Represent missing,
  loading, empty, stale, error, and unsupported states distinctly.

### Required TUI regressions and interaction contracts

The observations below came from source review; reproduce them against the
implementation under test. They are behavioral requirements, not cosmetic mocks.

| ID / proposed test | Required behavior and proof |
| --- | --- |
| UX01 `TestTUIRefreshPreservesSnapshotsAndSelection` | Fast status refresh retains the last complete snapshot set, selected stable IDs, and snapshot-load errors until a valid replacement arrives. Test several timer cycles and out-of-order results. |
| UX02 `TestTUIProcessesBackgroundEventsWhileEditing` | Forms/pickers do not swallow operation completion, status, spinner/tick, or notification messages. Opening/closing a modal cannot strand progress or stop future refreshes. Owned by M2. |
| UX03 `TestTUICancelledUnlockClearsPendingAction` | Escape clears the cancelled action; later unlock cannot launch it. Validate credentials before announcing unlock success. Owned by M2. |
| UX04 `TestTUIFormValidationPreservesInput` | Invalid submit keeps values and focus, shows a field-specific error, and does not mutate config. Submission has explicit save/cancel behavior. |
| UX05 `TestTUITextEditingAndPickerFocus` | Real text input supports cursor movement, deletion and Unicode/grapheme-safe editing. While typing, `h/j/k/l/y`, spaces, arrows and backspace edit text; table commands apply only with table focus. Typed/pasted paths are actually used instead of an unrelated highlighted row. |
| UX06 `TestTUIRestoreReviewAndOverwriteChoice` | Review shows exact source, snapshot time/ID, target and overwrite policy; default refusal matches CLI. Cancellation leaves no pending destructive action. Owned jointly with M4. |
| UX07 `TestSnapshotSelectionUsesExactSourceIdentity` | Same basename on different paths/machines stays separate in lists, counts, health, and restore selection. Owned jointly with M4. |
| UX08 `TestTUIProtectionUsesSnapshotEvidence` | Global receipt count, historical peer reputation, or six healthy peers cannot mark an unrelated snapshot protected. A valid 6+3 layout across three domains is evaluated by shards, not six-peer admission. |
| UX09 `TestTUIQuitAndEscapeOperationSemantics` | Escape navigates back/cancels the current form; it does not unexpectedly quit from home. Quitting the dashboard preserves service-owned jobs. Foreground cancellation/detachment is explicit and tested. |
| UX10 `TestTUILayoutFitsSupportedTerminals` | All screens/forms/help/errors fit 80x24 and 120x40 terminal cells; a 60x20 fallback is navigable or clearly requests resizing. Long/Unicode paths, no-color output, focus, scrolling and resize are tested. |
| UX11 `TestTUICriticalErrorsPersistUntilResolved` | Critical errors do not vanish after a short toast timeout. The user can inspect full details, retry or acknowledge; health still reflects an unresolved issue. |
| UX12 `TestTUIFirstRunAndServiceReadiness` | A fresh user can initialize, approve peers, choose recovery storage, unlock, start service, run backup and inspect its actual protection result without editing raw config. |

Use deterministic Bubble Tea model/update tests for state transitions and terminal
cell-aware render checks for layout. Add actual terminal/PTY smoke coverage and
inspect representative rendered screens on supported terminals. Screenshot/golden
updates alone do not prove behavior. Do not fix a production bug by updating the
expected screenshot to hide it.

Acceptance also requires bounded-cost status refresh: do not recursively scan all
protected data or read unbounded logs on every UI tick. Use cached measurements,
bounded asynchronous refresh, cancellation and request-generation IDs. Show the
age of cached data. A stalled peer must not freeze typing or directory navigation.

```text
Implement M6 under the master prompt. Make the complete CLI/TUI reflect verified
application state and help a user complete setup, backup, repair inspection and
safe recovery. Satisfy UX01-UX12 with behavioral tests, including all modal/event,
selection, editing, resize, cancellation and stale-status cases. Integrate durable
alerts and diagnostics. Inspect real rendered screens, but prioritize truthful
state and safe interactions over decorative redesign. Run all product gates.
```

## M7. Whole-binary qualification, upgrade safety, and release artifacts

**Depends on:** M1-M6 verified. **Primary areas:** both commands, installers,
scripts, workflows, integration harness, compatibility fixtures, documentation.

Implementation requirements:

- M7.1 Run every surface in section 5 against built release binaries, not only
  imported Go functions. Test fresh home directories, explicit config, malformed
  config, missing secrets, Unicode/spaced paths, no interactive terminal, and
  real service accounts. Compare CLI/TUI/daemon outcomes for the same operation.
- M7.2 Version persistent formats and wire compatibility. Upgrade the sanitized
  M0 baseline fixtures, then restore old snapshots and create/restore new ones.
  Test interrupted migration. Back up metadata before migration; fail safely on
  newer/unsupported formats. Define mixed-version peer support and clear errors
  for incompatible versions. Never advise downgrading into an unreadable format.
- M7.3 Verify real separated-network behavior: two private peers requiring relay,
  blocked UDP with supported fallback, stale/changed addresses, discovery startup
  outage/recovery, relay loss/reconnect, and alternate infrastructure. Block direct
  connectivity in relay tests and prove actual shard transfer through the relay.
  A shared Docker bridge/startup log is not a substitute. Include a controlled
  separate-host smoke test and keep public Internet dependencies out of PR tests.
- M7.4 Build both binaries with version/commit/build metadata. Make release scripts
  check native process exit codes explicitly (including PowerShell), fail if an
  artifact is missing, and produce checksums and dependency/SBOM information.
  Smoke-test the produced archives/images from a clean install. Align toolchains
  across local scripts, CI and all Dockerfiles. Pin and verify CI dependencies.
- M7.5 Complete the license text, contribution/security guides, supported-platform
  and limits documentation, setup/service/restore/recovery/upgrade instructions,
  command examples, and release notes. Examples must be executed by tests or a
  documented release smoke run. Never publish real keys or credentials.
- M7.6 Enforce CI jobs and required-test inventories as release gates. Check actual
  branch-rule settings when access allows; if repository permissions prevent it,
  provide the exact required checks and mark that external release gate pending.
- M7.7 Execute the release scenario and real-duration qualification below.
  Generate a report tied to exact artifacts and source revision. If code changes
  during qualification, document affected evidence and rerun affected gates;
  final artifacts must have complete consistent evidence.

### Required destructive-fault scenario (disposable fixtures only)

1. Start three enrolled storage nodes A/B/C, spare D, discovery/relay infrastructure,
   a source client, and an independent recovery destination. Use default 6+3 in
   at least one real network/CLI scenario, in addition to existing 2+1 tests.
2. Back up two roots with repeated basenames, empty files/directories, Unicode,
   binary data, and platform-supported metadata. Modify/add/delete files and
   create another snapshot. Independently record exact expected trees for each.
3. Interrupt an upload and restart the service. Verify previous snapshots and
   eventual retry, correct quota, and truthful operation status.
4. Permanently remove A. Wait for verified repair to D and a new external recovery
   checkpoint. Then remove B. Prove that the remaining placement is recoverable.
5. Destroy the original disposable source/client state and all client caches.
   Restart the remaining peers/infrastructure so in-memory DHT state is gone.
6. On a clean client, use only password and the current external checkpoint to
   restore every snapshot still promised by retention. Compare full tree manifests.
7. Exercise retention while a peer/checkpoint destination is offline. Restore all
   retained data, bring the peer/destination back, and verify safe eventual cleanup.
8. Exceed the supported loss threshold. Restore must fail specifically because
   valid shards are insufficient, within its bound, with no success label or
   fabricated output. An unrelated config error or timeout cannot satisfy this.

### Workload and elapsed-time qualification

The initial qualification profile is at least 10 GiB of mixed compressible and
seeded incompressible data, 100,000 regular files, two roots, and one file larger
than 1 GiB. Include many empty/small files and deep/Unicode paths. Configure the
protected-byte limit above the fixture size for successful runs; test admission
limits separately. Provide enough storage for coding overhead, history and repair.

Run a 14-day unattended trial on the supported deployment profile. Capture host
hardware, OS, network constraints and daily measurements of memory, disk/staging,
goroutines/handles, connections, retries, repair delay, backup duration and restore
throughput. Set finite resource budgets before the run, based on the M1/M5
configuration and a measured baseline; do not invent favorable thresholds after
observing a failure. Each supported OS must participate as client and storage peer.

Exercise scheduled changes, at least one service/host restart, forced process
termination, peer/network outages, full capacity, repair, recovery export failure,
retention and fresh-client restore drills. Expected injected failures are allowed
only if they are correctly reported and recovered according to the contract.
There must be no unexplained corruption, lost committed snapshot, stuck operation,
resource growth beyond budget, or false protected/success result.

Clock-controlled unit tests may accelerate retention/scheduling logic. They cannot
replace the elapsed-time trial. If no approved environment can run it, finish the
harness and remaining work, then report `implemented; release qualification
pending` with exact instructions. Do not schedule background infrastructure or
claim the 14 days elapsed merely because an agent turn is ending.

```text
Implement and execute M7 under the master prompt. Qualify the entire shipped
product, including real binaries, native services, compatibility, NAT/relay,
resource budgets, installers, and documentation. Run the A-to-D-to-B fresh-client
recovery scenario and the complete test matrix. Produce artifacts with provenance
and a release report. Treat unavailable environments and unfinished real-duration
qualification as pending; never substitute optimistic prose or old logs for proof.
Do not publish or deploy production artifacts without task authorization.
```

## 5. Entire product surface: required final audit

Run this audit after each relevant milestone and completely during M7. Inventory
new commands/flags as they are added; this table is not permission to ignore a
surface missing from the current list.

| Surface | Required final behavior | Primary milestones |
| --- | --- | --- |
| Root command, help, completion if present, version | Work on fresh/broken-config installs without network; correct args, exit codes and build metadata. | M0, M6, M7 |
| `init` and TUI first run | Validate before mutation; retry partial setup safely; consistent defaults; explicit mode/peer/recovery/service readiness. | M1, M2, M4, M6 |
| `config` and TUI settings | Same schema/validation; atomic save; secret redaction; unknown keys rejected; restart/migration implications visible. Audit every field. | M1, M5, M6 |
| `add`, TUI add/remove roots | Exact normalized identity; spaces/Unicode/overlap/missing paths; removal stops future protection without deleting source or silently deleting history. | M2, M4, M6 |
| `backup now` and explicit source backup | Shared coordinator, independent per-root results, bounded retries, source-read errors, exact commit/protection distinction. | M2, M3, M4 |
| `snapshot list/show` | Accurate source/root association, stable IDs, explicit empty/locked/error states, defined `latest`, consistent CLI/TUI selection. | M4, M6 |
| `restore` and TUI restore | Exact root/snapshot, safe destination/overwrite, authentication before writes, cancellation reporting, permissions fidelity. | M4, M6 |
| `repo init/connect/status/maintenance` | No accidental reinitialization, stable mode/identity, clear missing metadata, actual maintenance integration versus status-only reporting. | M2, M4, M5 |
| `recovery export/import` plus verify | Consistent authenticated bundles; independent destination; safe import; supported generations; freshness/errors visible. | M4, M5, M6 |
| `status` / long / machine-readable health | Root/snapshot-specific evidence, cached-data age, bounded collection cost, meaningful health-check exit behavior. | M3, M4, M6 |
| `peers` plus approve/revoke | Separate discovery, connection, admission and reputation; durable rules, capabilities, quotas and data-ownership implications. | M1, M3, M6 |
| `audit now` and scheduled audits/repair | Fair coverage of live shards; no obsolete receipt failures; correct corruption/unavailability outcomes and repair state. | M3, M5, M6 |
| TUI forms/picker/navigation/progress | UX01-UX12; actions stay responsive and safe across modal states, resize, cancellation, async errors and refresh. | M2, M4, M6 |
| `daemon` plus native service operations | Stable identity, one instance, appropriate account/credentials, boot/restart, job persistence, safe stop/uninstall. | M1, M2, M7 |
| `bootstrap` and `p2pdiscovery` | Persistent mounted identity, clear advertised/listen addresses, graceful signals, bounded relay/discovery, no accidental storage role. | M1, M2, M7 |
| `reset` and TUI reset | Existing refusal/explicit-confirmation tests remain; enumerate affected paths, stop conflicting jobs, protect originals, disclose owned/hosted backup consequences. Never recommend deleting sources as a troubleshooting shortcut. | M2, M4, M6, M7 |
| Local backend | Keep CRUD/restore/durability/quota contracts; accurately show local-only protection; no automatic promotion/demotion. | All |
| Docker images/Compose/sample configs | Complete runnable examples, persistent identities/data volumes, readiness checks, no real secrets; discovery and storage roles correct. | M1, M2, M7 |
| Build/release/install scripts | Both binaries, failures propagated, verified metadata/checksums, clean install/upgrade/uninstall, explicit platform limits. | M0, M7 |

The config-field inventory must include all `node`, `storage`, `network`, `backup`
and `trust` fields plus new service/admission/repair/recovery/notification fields.
For each, record default, units, valid range, precedence (flags/environment/file),
runtime consumer, live/restart behavior, migration implications, CLI/TUI exposure,
and the test proving its effect. Do not count a config round trip as enforcement.

## 6. Test and verification gates

### Existing commands: mandatory baseline, not invented capabilities

The following commands exist now. Run from the repository root. Keep structured
output in a new run directory. A verification script must preserve the command's
exit status as well as run the checker; the checker alone cannot prove a process
finished successfully. Use the installed Python interpreter (`python3` or
`python`) and record it. Missing tooling is a prerequisite failure.

Bash example for Linux (or configured Windows CI Bash for non-race commands):

```bash
set -euo pipefail
mkdir -p .test-results/mvp/current
go version
go vet ./...
go build ./cmd/...
go test -json ./... -count=1 -timeout=5m \
  > .test-results/mvp/current/fast.jsonl
python3 scripts/check-test-results.py --suite fast \
  .test-results/mvp/current/fast.jsonl

# Linux runner with a C compiler; all packages, including in-process integration.
go test -race -json ./... -count=1 -timeout=8m \
  > .test-results/mvp/current/race.jsonl
python3 scripts/check-test-results.py --suite fast \
  .test-results/mvp/current/race.jsonl

docker info
go test -tags=docker -json ./test/integration -run '^TestDocker' \
  -count=1 -timeout=20m > .test-results/mvp/current/docker.jsonl
python3 scripts/check-test-results.py --suite docker \
  .test-results/mvp/current/docker.jsonl

go test -json ./internal/network/... ./internal/backend/p2p/... \
  -shuffle=on -count=20 -timeout=15m \
  > .test-results/mvp/current/stress.jsonl
go test ./internal/backend/p2p/erasure -run '^$' \
  -fuzz '^FuzzErasureRecovery$' -fuzztime=60s -timeout=2m
```

Use unique directories in real execution; `current` is an example placeholder.
The final verification entry point must run independent suites even when one
fails and then exit nonzero with an aggregate result, so evidence is complete.
On PowerShell, check `$LASTEXITCODE` immediately after every native command;
`$ErrorActionPreference = 'Stop'` alone is insufficient. Write valid UTF-8 JSON
evidence. Do not allow `tee`, a checker, or a later successful command to mask a
failed test/build. Run new OS service/PTY suites separately on their required OS.

### Required additions to the verification system

- Maintain all existing P0 inventory entries. Add a separate MVP contract
  inventory mapping each new requirement to package-qualified test names and
  required platforms/suites. Proposed test names in this plan become runnable
  contracts when implemented; until then they are recorded as missing, not passed.
- The existing checker currently supports only `fast` and `docker`. Extend its
  interface or add a companion checker deliberately; do not document a new
  `--suite mvp` command as working before implementing and testing it.
- Detect failed packages/tests, missing expected tests, required skips, malformed
  or truncated evidence, and absent package completion. Never accept the same
  test name from an unrelated package as satisfying a requirement. Exercise the
  checker with deliberately failing, missing, skipped and truncated fixtures.
- Keep platform-specific tests in explicit platform inventories. A Windows service
  test cannot be satisfied by a Linux mock, and unsupported OS exclusions cannot
  hide an unexecuted supported-platform requirement.
- Add bounded fuzz targets for wire/reference parsing, locators, recovery import,
  and config/path boundaries where malformed input threatens security/durability.
  Preserve failing corpora; ensure tests cannot escape disposable directories.
- Add `govulncheck` with a pinned compatible tool version and official advisory
  review. Distinguish imported vulnerable modules from reachable vulnerable paths;
  record reviewed mitigation/non-applicability, never blanket suppressions.
- Enforce formatting, vet, module consistency, both binary builds, normal tests,
  platform integration, races, and Docker gates on relevant changes. Run bounded
  stress/fuzz regularly and full qualification before release. Set job timeouts
  from measurements and record justified changes; never extend a timeout merely
  to make a deadlock appear successful.

### Matrix and completion rule

| Gate | Required evidence |
| --- | --- |
| Ordinary suite | All packages on Windows and Linux; fresh execution (`-count=1`), P0 and MVP inventories complete. |
| Build/static | Formatting, vet, module verification, build both commands, actual help/version and negative CLI smoke runs. |
| Race/concurrency | Linux full `-race` suite plus competing-process contracts; no race reports or hidden failures. |
| Docker/CLI | All existing Docker tests plus new approved-peer/repair/recovery/retention/failure scenarios. |
| TUI | Deterministic state tests, render bounds, and real terminal interaction checks on supported environments. |
| Service | Native Windows/Linux install, credential access, start/stop, restart, duplicate-instance prevention and uninstall preservation. |
| Networking | Real direct/relay/fallback/outage paths and at least one separate-host smoke run. |
| Security | Applicable vulnerability findings resolved/mitigated with evidence; admission, quota, untrusted-input, redaction and credential isolation tests. |
| Upgrade/recovery | Golden baseline migration, old/new snapshot restores, interrupted migration, clean-client external recovery. |
| Stress/fuzz | Shuffled repeated runs, bounded parser/codec fuzzing, failure seeds/corpora preserved. |
| Qualification | 14 actual days, declared workload/budgets, fault schedule, restore oracles and resource evidence tied to final artifacts. |

Unit tests, contract tests and integration tests must assert product outcomes,
not implementation call counts alone. Use fake clocks for time policy, explicit
fault seams for durable failures, readiness predicates rather than blind sleeps,
and real network/process tests for wiring. Negative tests must assert the intended
typed failure and absence of forbidden side effects. Preserve the first failure
log when investigating; a rerun passing does not explain or close a flaky test.

## 7. Final deliverables and release decision

- [ ] M0-M7 ledger entries link implemented requirements to code, tests and evidence.
- [ ] Every existing test and required new contract passes on its supported matrix.
- [ ] Every surface in section 5 has actual binary/user-journey coverage.
- [ ] The full fresh-client recovery-after-repair scenario passes exactly.
- [ ] Defaults, supported limits, failure assumptions, formats and upgrade policy
  are explicit and agree with the shipped behavior.
- [ ] Both binaries and deployment artifacts have version/provenance/checksums;
  clean installation, service operation and upgrade have been exercised.
- [ ] Security/dependency findings and data-loss defects have no unresolved
  release-blocking issues; known remaining limits are documented accurately.
- [ ] The elapsed-time qualification is complete and the report references the
  actual release candidates. No missing environment or incomplete run is green.
- [ ] README, complete license, security/contribution guides, troubleshooting,
  service operation, recovery and tested release notes are ready.

Final implementation report format:

```text
Result: [implemented with pending qualification | release-qualified | incomplete]
Source revision and working-tree changes:
Artifacts and checksums:
Requirements completed and evidence links:
Existing reliability contracts: [results by platform/suite]
New MVP/whole-binary/UX contracts: [results by platform/suite]
Crash, recovery, repair, retention and upgrade evidence:
Resource/network/14-day qualification evidence:
Known limits, failed or unavailable gates, and exact remaining actions:
```

The objective is not a particular test count or coverage percentage. Delivery
requires the complete executable product to preserve committed data, recover
under the stated failures, resume routine operation automatically, and tell the
user accurately when those conditions are not met.
