# MVP implementation status

This ledger is maintained against the checkout used for each verification run.
It distinguishes implementation from verification; an unavailable platform,
tool, or elapsed-time gate remains pending or blocked.

## Evidence runs

| Run | Revision | Platform/toolchain | Result |
| --- | --- | --- | --- |
| `baseline-20260927-2028` | `main` working tree; Git VCS stamping unavailable because the checkout owner differs from the sandbox user | Windows amd64, Go 1.21.5; Python unavailable; Docker Engine unavailable | `go test -json ./... -count=1 -timeout=5m` passed; `go vet ./...` passed with a workspace `GOCACHE`; default `go build ./cmd/...` failed at VCS stamping; Docker and Python checker unverified |
| `verify-20260927-b` | working tree; `-buildvcs=false` | Windows amd64, Go 1.21.5; Python unavailable | Vet, both binary builds, both binary help/version smoke tests, and the full fast suite passed. The verifier correctly failed overall because the required Python evidence checker was unavailable. |
| `verify-final-20260927-b` | final working tree; `-buildvcs=false` | Windows amd64, Go 1.21.5; Python unavailable | Vet, both binary builds, all three binary smoke tests, and the full fast suite passed. The machine-readable report records Go 1.21.5 and exactly one contract problem: the required Python evidence checker is unavailable. |
| `verify-race-20260927` | working tree; `-buildvcs=false` | Windows amd64, Go 1.21.5, `CGO_ENABLED=0`, no `gcc`/`clang` | Vet, both builds, smoke tests, and fast suite passed; race gate was not runnable because Go race requires cgo. Overall verifier failure is therefore retained. |
| `verify-fuzz-20260927` | working tree; `-buildvcs=false` | Windows amd64, Go 1.21.5 | Vet, both builds, smoke tests, fast suite, and the configured 60-second erasure fuzz target passed. Python checker remained unavailable, so the verifier did not report an all-gates pass. |
| `release-20260927-final` | current working tree; explicit `-buildvcs=false`, `mvp-local-final` | Windows amd64; Linux amd64 cross-build evidence retained from `release-20260927` | Both current-tree `p2pbk.exe` and `p2pdiscovery.exe` built with metadata, module manifest, checksums, and archive. Direct version/help smoke tests returned 0. Native service, Docker, and 14-day gates remain unverified. |
| `direct-script-20260927` | current working tree; direct PowerShell invocation with no caller-provided `GOCACHE` | Windows amd64 | `scripts/build-release.ps1` selected its repository-local cache automatically; both binaries built and direct version/help smoke tests returned 0. The default output is executables only; archive creation is explicit via `-Archive`. |
| `self-test-failing` | verifier intentional fixture | Windows amd64, Go 1.21.5 | The verifier rejected the intentionally failing fixture and returned process exit code 1; no failure was hidden. |

Evidence is stored under `.test-results/mvp/` (with the baseline and named run
subdirectories above). The README change and this plan were pre-existing user
changes and are preserved.

## Milestone ledger

| Milestone / requirements | Status | Current behavior and implementation record | Regression / verification evidence | Next action or limit |
| --- | --- | --- | --- | --- |
| M0.1 surface inventory | implemented | Command trees, config fields, environment variables, persistence files, Docker files, scripts, both binaries, and TUI entry points are inventoried. | Repository inspection plus release manifest and binary metadata | Keep the inventory synchronized with future surface changes. |
| M0.2 fresh baseline | verified (partial) | Fresh ordinary suite and vet passed; corrected verifier, fuzz, packaging, and cross-build evidence are recorded. Race, Docker, Python checker, stress, and elapsed qualification remain separate gates. | Baseline and `verify-*` evidence directories listed above | Re-run unavailable gates when the environment provides their prerequisites. |
| M0.3 compatibility fixtures | implemented (verification partial) | Sanitized config, storage mode, manifest/locator, and recovery-bundle fixtures are checked into `testdata/mvp/golden/`; no credentials or source data are included. | Fixture review; fast suite passed | Add fixture assertions to any future format migration. |
| M0.4 fail-closed verification entry point | implemented (verification partial) | `scripts/verify_mvp.go` runs independent gates, preserves subprocess exit codes, writes machine-readable evidence, checks malformed/empty/truncated/unknown evidence, and rejects an intentional failing fixture. | Corrected run, race/fuzz runs, and `self-test-failing` evidence | Python checker must be installed for the verifier to report an overall pass. |
| M1 approved peers / resource bounds / input validation / secrets | implemented (verification partial) | Persisted peer approval/revocation gates wire storage; per-owner and total quotas, bounded frames/paths, strict config keys, redacted persistence, and protected credential paths are implemented. | `TestUnapprovedPeerCannotStoreShard`, `TestApprovalAndRevocationSurviveRestart`, `TestManagerEnforcesPerOwnerLimit`; full suite passed | Complete abuse-load and concurrent-credential qualification. |
| M2 service / coordinator / retry / TUI routing / safe source layout | implemented (verification partial) | Durable operation records and lock files coordinate backup/restore/maintenance; backup retries/alternate placement, source overlap exclusion, native Windows/Linux service definitions, and global TUI event routing are implemented. | `TestTUIProcessesBackgroundEventsWhileEditing`, `TestTUICancelledUnlockClearsPendingAction`; full suite passed | Exercise native service lifecycle and fault-injected retry/restart gates on supported hosts. |
| M3 repair / failure domains / audit inventory | partial | Durable repair jobs with bounded retry, fair audit cursor/classification, and failure-domain-aware placement are implemented. Repair execution/reconstruction and crash-boundary qualification are not complete. | `TestPlacementHonorsFailureDomains`, `TestPlacementRejectsInsufficientFailureDomains`; full suite passed | Implement repair workers and commit-boundary recovery tests. |
| M4 automatic checkpoint / exact safe restore | implemented (verification partial) | Committed backups and maintenance publish verified encrypted generations when an external path is configured; generations are pinned/pruned, import verifies before mutation, overwrite is explicit, and source selection is exact. | `TestRestoreRefusesOverwriteByDefault`, recovery tests, full suite passed | Run fresh-client restore and checkpoint fault-injection qualification. |
| M5 retention / maintenance / deletion queue / config atomicity | partial | Retention policy application, maintenance safety checks, protected/cache budgets, checkpoint pins, multi-root error aggregation, and atomic config persistence are implemented. Durable deletion work and complete setting-effect qualification remain. | `TestProtectedByteLimitEnforcedDuringBackup`, `TestCacheAndStagingRespectDiskBudgets`; full suite passed | Add offline-peer deletion queue and retention/GC fault-injection tests. |
| M6 truthful status / CLI / TUI / alerts | partial | CLI/TUI expose operation state, exact source identity, snapshot/receipt evidence, and checkpoint state without fabricating protected status; full UX/PTY and persistent-alert contracts remain. | TUI background-event/cancel tests and full suite passed | Complete behavioral TUI and persistent critical-alert coverage. |
| M7 binary qualification / upgrade / networking / packaging / 14-day trial | implemented (verification partial) | Both binaries build with metadata/checksums; optional archive packaging is available via `-Archive`. Windows smoke tests and Linux amd64 cross-build passed. Native service runtime, Docker, race, Python checker, stress, and the actual 14-day trial are not verified. | `release-20260927`, `verify-race-20260927`, `verify-fuzz-20260927`, `direct-exe-20260927` | Obtain missing prerequisites and run the supported-platform and elapsed qualification gates. |

## Required contract inventory

The proposed test names in the implementation plan are requirements, not proof
that tests exist. Missing entries stay marked pending until a package-qualified
test executes and its evidence is linked here.

| Requirement group | Proposed contracts | Current state |
| --- | --- | --- |
| M1 admission and secrets | `p2pbk/internal/peer/blobserver.TestUnapprovedPeerCannotStoreShard`, `p2pbk/internal/peer/localdb.TestApprovalAndRevocationSurviveRestart`, `p2pbk/internal/peer/quota.TestManagerEnforcesPerOwnerLimit`, plus hostile-input/concurrent-credential/output-redaction contracts | Three named contracts executed; remaining proposed contracts pending. |
| M2 coordination and UX | `p2pbk/internal/tui.TestTUIProcessesBackgroundEventsWhileEditing`, `p2pbk/internal/tui.TestTUICancelledUnlockClearsPendingAction`, plus service/retry/overlap/multi-root contracts | Two named contracts executed; service lifecycle and fault-injection contracts pending. |
| M3 repair | `p2pbk/internal/backend/p2p/placement.TestPlacementHonorsFailureDomains`, `p2pbk/internal/backend/p2p/placement.TestPlacementRejectsInsufficientFailureDomains`, plus durable repair queue implementation | Placement contracts executed; repair execution, crash, and full audit contracts pending. |
| M4 recovery and restore | `p2pbk/internal/kopiaengine.TestRestoreRefusesOverwriteByDefault` and existing recovery tests | Safe-overwrite contract executed; automatic checkpoint fault-injection, fresh-client, and fidelity contracts pending. |
| M5 lifecycle | `p2pbk/internal/app.TestProtectedByteLimitEnforcedDuringBackup`, `p2pbk/internal/app.TestCacheAndStagingRespectDiskBudgets` | Budget contracts executed; deletion queue, GC, retention, and all-setting contracts pending. |
| M6 UX | Background-event and cancel contracts are recorded under M2; the remaining TUI/PTY/alert contracts are not yet present. | Partial; remaining UX contracts pending. |

## Whole-product limits

- Linux amd64 cross-compilation and Windows packaging smoke tests passed, but no
  claim is made for Linux runtime behavior, Docker, native service accounts,
  relay/NAT, govulncheck, stress, or the 14-day qualification from this run.
- Existing reliability tests remain mandatory. No test was skipped or weakened
  to obtain the fresh ordinary-suite result.
- Release qualification can only be marked `release-qualified` after every
  supported-platform gate and the actual elapsed trial pass against the final
  artifacts.
