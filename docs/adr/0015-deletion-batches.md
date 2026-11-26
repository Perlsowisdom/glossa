# 0015: Deletion batches

Status: **Proposed** (quality pass 5, audit item 24). Builds on 0004 (change history), 0010 (deleting for
good) and 0013 (folder sync). Schema migration 0017.

## Context

Deleting a folder also deletes the notes and sermons in it and its subfolders; deleting a collection also
deletes the collections inside it. The bin lists the folder or collection alone, and restoring it must bring
back what went with it and nothing else.

Until now "went with it" was worked out from times: a row counted as deleted with its folder if its
`deleted_at` was no earlier than the folder's. That has two weaknesses. Two things deleted on one device in the
same millisecond could not be told apart (fixed in pass 4 by stamping every soft delete strictly later than the
one before it). And after sync it is guesswork: a device that merges a delete takes the time from the change,
not from the first device, so a note deleted on its own on another device, a moment after the folder, reads as
"deleted with" it and comes back by surprise.

## Decision

**A cascading delete stamps one deletion batch id on everything it deletes.**

- `folders`, `notes`, `documents` and `collections` get a nullable `deletion_batch` text column (migration
  0017). `folder.delete` and `collection.delete` make one new id (`store.NewID`) and set it on the folder or
  collection and on every row that goes with it, in the same transaction, before the row is deleted.
- Deleting one thing on its own sets no batch. Lists and reading plans cascade nothing and need no column.
- `bin.list` counts what went with a folder or collection by batch, `bin.restore` brings back the rows in the
  batch, and `bin.purge` treats the rows in the batch as the ones that go (anything filed in the folder that is
  not in the batch stays, at the top, as before). Restoring a row clears its batch.
- **Old rows keep working.** A folder or collection with no batch (deleted before 0017, or by a device that is
  not updated yet) is restored by the old time rule: rows whose `deleted_at` is no earlier than its own.

**Sync.** `deletion_batch` is an ordinary column, so setting it is an ordinary logged field change
(`update`, field `deletion_batch`) that travels in the device's change file like any other, and no sync code
changes. It is not per-device (it is not in the `perDeviceTable` list in `internal/store/remote.go`): the batch
means the same thing on every device. The batch is set before the delete in the same transaction, so a device
never sees a deleted folder without its batch except from an older device, which gets the time rule. 

## Consequences

- Restore is exact on every device, whatever order changes arrive in.
- Each cascading delete writes one extra change per row (the batch), so the history is slightly longer; the
  purge in 0010 erases the values as for any other field.
- A restore through `history.restore` of an old state sets the column back to what it then was, as for any
  field.
- Nothing is backfilled: old deletions stay on the time rule for good, and that is fine, because they are in
  the bin only until it is emptied.
