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
    for path in paths:
        with open(path, encoding="utf-8-sig") as stream:
            for line in stream:
                if not line.strip():
                    continue
                event = json.loads(line)
                action, test = event.get("Action"), event.get("Test", "")
                if action == "fail":
                    invalid.append(f"failed: {event.get('Package')}/{test}")
                if action == "pass" and test:
                    passed.add(test)
                if action == "skip" and test in required:
                    invalid.append(f"required contract skipped: {test}")
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
