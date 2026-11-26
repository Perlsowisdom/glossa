-- library.db, schema version 19.
-- Links between a note or sermon in Glossa and the Markdown file in an Obsidian vault it was exported to or
-- imported from (a connection, ADR 0014), so exporting again updates the same file and importing again updates
-- the same note, and so Glossa can tell which side changed since the last time they matched.
--
-- This table is per device, like any_links and folder paths: the path is relative to the vault folder chosen on
-- this computer, and another computer may have a different vault (or none), so a link made here is never sent
-- to another device. The vault folder itself is the setting "connect.obsidian.vault", also per device.
CREATE TABLE obsidian_links (
    kind        TEXT NOT NULL CHECK (kind IN ('note', 'sermon')),
    item_id     TEXT NOT NULL,                -- the note's or document's id
    path        TEXT NOT NULL,                -- the file, relative to the vault, with forward slashes
    glossa_hash TEXT NOT NULL DEFAULT '',     -- hash of the Glossa side (title, text, passages, tags) at the last sync; '' = unknown
    file_hash   TEXT NOT NULL DEFAULT '',     -- hash of the file's content, read the same way, at the last sync; '' = unknown
    synced_at   INTEGER NOT NULL,             -- Unix milliseconds
    PRIMARY KEY (kind, item_id)
);
CREATE UNIQUE INDEX obsidian_links_path ON obsidian_links(path);

PRAGMA user_version = 19;
