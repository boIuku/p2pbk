"""Fail CI if a required reliability contract did not actually execute and pass.

Use with `go test -json`; a renamed, skipped or filtered-out contract is a failure.
Changing this inventory is a change to the product's release requirements.
"""
import argparse
import json
import sys

FAST = {
    "P0-01": ["TestDaemonPeerIdentitySurvivesRestart", "TestDiscoveryIdentitySurvivesRestart"],
    "P0-02": ["TestShardAuthorizationAcrossOwners", "TestUploadRejectsForgedOrMismatchedReceipt"],
    "P0-03": ["TestRestoreFromRecoveryBundle", "TestMissingManifestCannotInitializeReplacementRepository"],
    "P0-04": ["TestRepositoryStorageModeIsStable"],
    "P0-05": ["TestRPCCancelAfterStreamOpened", "TestRPCRejectsOversizedMessage", "TestRPCServerBoundsUnfinishedRequest"],
    "P0-06": ["TestLocatorIsolationAcrossRepositories", "TestLocatorRejectsWrongKeyAndReplay", "TestLocatorReadFailureDoesNotEraseLocations", "TestSignedLocatorAcrossRealDHTNodes", "TestDurableLocatorSurvivesDHTLoss"],
    "P0-07": ["TestCommittedBackupSurvivesCrash", "TestFailedOverwritePreservesCommittedBlob", "TestConcurrentStorageWritersDoNotLoseManifests", "TestDoNotRecreateIsAtomicAcrossWriters"],
    "P0-08": ["TestRestoreFromRecoveryBundle", "TestDefaultPolicyEveryRecoverableShardSubset"],
    "P0-09": ["TestQuotaSurvivesRestart", "TestZeroOfferRejectsRemoteWrites", "TestConcurrentShardQuota", "TestFailedShardWritePreservesBytesAndQuota"],
    "P0-10": ["TestRemoteBackupRequiresWritableStorageCapability", "TestConnectionsContinuePastStaleAndMalformedPeers", "TestRemoteDiscoveryShutdownWithStalledProviderChannel"],
    "trust": ["TestAuditFailuresPersistAndReduceTrust"],
}
DOCKER = {
    "P0-10": ["TestDockerRemoteDiscoveryBackupRestore"],
    "P0-08": ["TestDockerRestoreAfterMaxNodeLoss", "TestDockerRestoreFailsBeyondParity"],
    "backup": ["TestDockerBackupCreatesSnapshotReceiptsAndRemoteShards", "TestDockerRestoreLatest"],
}


def check(paths, required):
    passed, invalid = set(), []
    saw_package, completed_package = set(), set()
    allowed_actions = {"start", "run", "output", "pass", "fail", "skip"}
    for path in paths:
        try:
            with open(path, encoding="utf-8-sig") as stream:
                saw_lines = False
                for line_number, line in enumerate(stream, 1):
                    if not line.strip():
                        continue
                    saw_lines = True
                    try:
                        event = json.loads(line)
                    except json.JSONDecodeError as exc:
                        invalid.append(f"malformed JSON evidence {path}:{line_number}: {exc}")
                        continue
                    if not isinstance(event, dict):
                        invalid.append(f"malformed evidence {path}:{line_number}: event is not an object")
                        continue
                    action, package, test = event.get("Action"), event.get("Package", ""), event.get("Test", "")
                    if action not in allowed_actions:
                        invalid.append(f"unknown test action {path}:{line_number}: {action!r}")
                    if package and action == "start":
                        saw_package.add(package)
                    if package and action in {"pass", "fail"} and not test:
                        completed_package.add(package)
                    if action == "fail":
                        invalid.append(f"failed: {package}/{test}")
                    if action == "pass" and test:
                        passed.add(test)
                    if action == "skip" and test in required:
                        invalid.append(f"required contract skipped: {package}/{test}")
                if not saw_lines:
                    invalid.append(f"empty evidence file: {path}")
        except OSError as exc:
            invalid.append(f"cannot read evidence file {path}: {exc}")
    if not saw_package:
        invalid.append("no package start event was recorded")
    elif not completed_package:
        invalid.append("no package completion event was recorded; evidence may be truncated")
    invalid.extend(f"required contract did not pass: {name}" for name in sorted(required - passed))
    return invalid


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--suite", choices=["fast", "docker"], required=True)
    parser.add_argument("results", nargs="+")
    args = parser.parse_args()
    inventory = FAST if args.suite == "fast" else DOCKER
    names = {name for group in inventory.values() for name in group}
    problems = check(args.results, names)
    if problems:
        print("\n".join(problems), file=sys.stderr)
        sys.exit(1)
    print(f"All {len(names)} required {args.suite} contracts executed and passed.")
