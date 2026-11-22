-- Sources the person cited from a connection (Zotero, ...), kept as CSL-JSON so a document still
-- renders its citations when the connection is off or offline. Synced like any other user data.
CREATE TABLE cite_items (
    id               TEXT PRIMARY KEY,            -- "<connection>:<item key>", for example zotero:ABCD2345
    connection       TEXT NOT NULL,               -- zotero, ...
    item_key         TEXT NOT NULL,               -- the item's key in that connection
    csl_json         TEXT NOT NULL,               -- one CSL-JSON item (internal/connect/csljson)
    title            TEXT NOT NULL DEFAULT '',    -- for lists and search
    library_version  INTEGER,                     -- the connection's library version when fetched, to ask "changed since?"
    cached_at        INTEGER NOT NULL             -- Unix milliseconds
);
CREATE INDEX cite_items_connection ON cite_items(connection);

PRAGMA user_version = 15;
