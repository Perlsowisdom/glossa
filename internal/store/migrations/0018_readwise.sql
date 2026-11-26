-- library.db, schema version 18.
-- Readwise import (a connection, ADR 0014). Each Readwise source (a book, article...) becomes one note; these
-- tables remember which, so importing again updates that note instead of making another.
--
-- Both tables sync between devices (they are not per device): the notes they point to sync, so a second
-- device that imports finds the links and updates the same notes instead of making copies. Only the time of
-- the last import is per device (the setting "connect.readwise.sync", covered by the "connect." prefix).

CREATE TABLE readwise_sources (
    source_id  TEXT PRIMARY KEY,                -- Readwise's user_book_id
    note_id    TEXT NOT NULL,                   -- the note made from it
    item_id    TEXT,                            -- the library book it matched, or NULL
    state      TEXT NOT NULL DEFAULT 'linked' CHECK (state IN ('linked', 'kept', 'removed')),
                                                -- kept: the person chose to keep their own edited text;
                                                -- removed: the person deleted the note, so it is not made again
    body_hash  TEXT NOT NULL,                   -- hash of the note's title and text as Glossa last wrote them
    meta_json  TEXT NOT NULL,                   -- title, author, links and tags, to rebuild the note without the network
    synced_at  INTEGER NOT NULL                 -- Unix milliseconds
);
CREATE INDEX readwise_sources_note ON readwise_sources(note_id);

-- One row per live highlight. block is the highlight as Markdown, kept so the note can be rebuilt when
-- only some highlights changed (an incremental import sends only those).
CREATE TABLE readwise_highlights (
    highlight_id TEXT PRIMARY KEY,              -- Readwise's highlight id
    source_id    TEXT NOT NULL,
    block        TEXT NOT NULL,
    loc          INTEGER NOT NULL DEFAULT 0,    -- its place in the source, for ordering (0 when it has none)
    at           TEXT NOT NULL DEFAULT '',      -- when it was highlighted, for ordering
    updated_at   TEXT NOT NULL DEFAULT ''       -- Readwise's updated_at, as sent
);
CREATE INDEX readwise_highlights_source ON readwise_highlights(source_id);

PRAGMA user_version = 18;
