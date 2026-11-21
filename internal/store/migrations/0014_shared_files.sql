-- library.db, schema version 14.
-- Syncing books and modules (ADR 0013, T20b).
--
-- `files` and `library_folders` hold paths on one device, so they are no longer synced (see
-- store/remote.go). What travels instead is `shared_files`: the identity of a book file or a Bible
-- module (its hash, size, name and licence), never a path. The bytes travel as encrypted blobs.

CREATE TABLE shared_files (
    id           TEXT PRIMARY KEY,              -- derived from the book and hash, or the module, so two devices make the same row
    kind         TEXT NOT NULL CHECK (kind IN ('book', 'module')),
    item_id      TEXT REFERENCES items(id),     -- books: the library item this file belongs to
    module_id    TEXT,                          -- modules: the ID in modules/<id>.db
    name         TEXT NOT NULL,                 -- file name shown and used when it is copied to another device
    format       TEXT NOT NULL,                 -- epub, pdf, ... or module
    size_bytes   INTEGER NOT NULL,
    sha256       TEXT NOT NULL,                 -- of the plain file; names the blob
    licence_json TEXT,                          -- the licence kept with the file (modules; books use the licences table)
    deleted_at   INTEGER
);
CREATE INDEX shared_files_item ON shared_files(item_id);
CREATE INDEX shared_files_sha ON shared_files(sha256);

-- This device's own record of what it has put in or taken from the folder. Never logged or synced.
CREATE TABLE sync_files (
    file_id       TEXT PRIMARY KEY,             -- shared_files.id
    sha256        TEXT NOT NULL,                -- what this device holds
    size_bytes    INTEGER NOT NULL,
    mtime         INTEGER NOT NULL,             -- of the local file, so an unchanged module is not hashed again
    origin        TEXT NOT NULL CHECK (origin IN ('local', 'synced')),
    local_file_id TEXT                          -- books: this device's files row
);

PRAGMA user_version = 14;
