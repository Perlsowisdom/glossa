-- library.db, schema version 13.
-- Folder sync (ADR 0013). These tables are this device's own bookkeeping: they are never logged and
-- never synced.

CREATE TABLE sync_state (
    key    TEXT PRIMARY KEY,                    -- folder, exported_hlc, applied:<device>, last_sync ...
    value  TEXT NOT NULL
);

-- A text document (note, sermon) edited on two devices: the older edit lost, and is kept here and in
-- the change history so it can be restored.
CREATE TABLE sync_conflicts (
    id               TEXT PRIMARY KEY,          -- loser change ID + ':' + winner change ID, so re-applying is harmless
    object_type      TEXT NOT NULL,
    object_id        TEXT NOT NULL,
    field            TEXT NOT NULL,
    loser_change_id  TEXT NOT NULL,
    loser_device     TEXT NOT NULL,
    loser_value      TEXT,                      -- the text that was replaced
    winner_change_id TEXT NOT NULL,
    note             TEXT NOT NULL,             -- "Changed on <device> too"
    created_at       INTEGER NOT NULL,
    dismissed_at     INTEGER
);
CREATE INDEX sync_conflicts_object ON sync_conflicts(object_type, object_id);

PRAGMA user_version = 13;
