# Operation coordination decision record

This note is the M2 design record for backup, restore, maintenance, recovery,
and future repair/garbage-collection work.

## Durable records

`local.db.operations` stores an operation ID, kind, config/data directory, root
or snapshot, state (`accepted`, `running`, `retry`, `completed`, `failed`, or
`cancelled`), attempt count, timestamps, and the last typed error. The record is
written before work is acknowledged and is updated after each commit boundary.
An unfinished record is replayable on the next daemon start; completion is
never inferred from process exit alone.

## Lock order and ownership

The repository-scoped exclusive lock is acquired in this order:

1. `operations/backup.lock` for source traversal and Kopia writer/manifest
   commits.
2. `operations/restore.lock` for destination safety and restore reads.
3. `operations/maintenance.lock` for retention/deletion and maintenance.
4. `manifests.json.lock` and shard/locator locks owned by their existing
   packages.

An operation takes only the locks it needs and never acquires an earlier lock
after a later one. Recovery export snapshots metadata under the manifest lock,
releases it before external I/O, and publishes its own atomic file. Repair and
garbage collection will use the same ordering before they are enabled.

## Crash points and restart behavior

- Before the operation record: no accepted work exists.
- After `accepted`/`running` but before a Kopia commit: the record becomes
  `retry`; old committed snapshots remain authoritative.
- After a Kopia commit: the record becomes `completed` only after the manifest
  and associated metadata are durable.
- During an external checkpoint or deletion: the record remains pending and is
  retried idempotently; a last-known-good checkpoint or retained blob is not
  removed because a process disappeared.

Lock acquisition is context-bound and uses a finite caller timeout. Cancellation
releases locks and records `cancelled` or `retry` as appropriate. A second
process cannot present the same live network identity through the same data
directory because the operation and identity locks are separate durable files.
