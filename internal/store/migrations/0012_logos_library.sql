-- library.db, schema version 12.
-- The list of books in the user's Logos library, read by the Logos importer (internal/logosimport)
-- from the library catalogue. Metadata only: title, author, type, resource ID and the rights
-- fields. A refresh updates these rows; nothing here is a book's content.

CREATE TABLE logos_books (
    resource_id   TEXT PRIMARY KEY,             -- the book's ID in Logos
    title         TEXT NOT NULL,
    abbreviation  TEXT NOT NULL DEFAULT '',
    authors       TEXT NOT NULL DEFAULT '',     -- authors, separated by "; "
    type          TEXT NOT NULL DEFAULT '',     -- Logos's resource type, for example text.monograph.bible
    published     TEXT NOT NULL DEFAULT '',     -- as the catalogue writes it
    edition       TEXT NOT NULL DEFAULT '',
    languages     TEXT NOT NULL DEFAULT '',     -- language codes, separated by commas
    rights        TEXT NOT NULL DEFAULT '',     -- the catalogue's copyright field, word for word
    public_domain INTEGER NOT NULL DEFAULT 0,   -- 1 when the rights or the date clearly say so
    pd_basis      TEXT NOT NULL DEFAULT ''      -- rights or date: what the public-domain call rests on
);

CREATE TABLE logos_library_state (
    id            TEXT PRIMARY KEY,             -- always 'library'
    refreshed_at  INTEGER NOT NULL,             -- Unix time in milliseconds of the last refresh
    source        TEXT NOT NULL DEFAULT '',     -- the Logos folder it was read from
    books         INTEGER NOT NULL DEFAULT 0
);

PRAGMA user_version = 12;
