-- library.db, schema version 16.
-- Links between a note or sermon in Glossa and the page in Any (a connection, ADR 0014) it was sent to
-- or imported from, so sending again updates the same page and importing again updates the same note.
-- This table is per device, like folder paths: Any's page IDs belong to the Any app on this computer
-- (and its account), so a link made here is never sent to another device.
CREATE TABLE any_links (
    kind         TEXT NOT NULL CHECK (kind IN ('note', 'sermon')),
    item_id      TEXT NOT NULL,               -- the note's or document's id
    space_id     TEXT NOT NULL,               -- the Any space
    object_id    TEXT NOT NULL,               -- the page in that space
    glossa_hash  TEXT NOT NULL,               -- hash of the Glossa text at the last sync
    any_hash     TEXT NOT NULL,               -- hash of the page's text, as Any returned it, at the last sync
    any_modified TEXT NOT NULL DEFAULT '',    -- the page's last-modified time (as Any gave it) at the last sync
    synced_at    INTEGER NOT NULL,            -- Unix milliseconds
    PRIMARY KEY (kind, item_id)
);
CREATE UNIQUE INDEX any_links_object ON any_links(space_id, object_id);

PRAGMA user_version = 16;
