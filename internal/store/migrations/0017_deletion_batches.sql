-- library.db, schema version 17.
-- Deletion batches (ADR 0015). When deleting a folder or a collection also deletes what is inside it, every row
-- that goes gets the same batch id, so restoring brings back exactly that batch and not something that was
-- deleted on its own at about the same time. Rows deleted before this version have no batch (NULL); restoring
-- those falls back to comparing deleted_at, as before.
-- The column is an ordinary synced field: the change that sets it travels like any other change.
ALTER TABLE folders     ADD COLUMN deletion_batch TEXT;
ALTER TABLE notes       ADD COLUMN deletion_batch TEXT;
ALTER TABLE documents   ADD COLUMN deletion_batch TEXT;
ALTER TABLE collections ADD COLUMN deletion_batch TEXT;

PRAGMA user_version = 17;
