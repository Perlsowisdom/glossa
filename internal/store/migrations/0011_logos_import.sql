-- library.db, schema version 11.
-- The log of what the Logos importer (internal/logosimport) has brought in, so running it again
-- adds only what is new or changed, and a whole import can be undone.

CREATE TABLE logos_import_runs (
    id           TEXT PRIMARY KEY,
    started_at   INTEGER NOT NULL,
    source       TEXT,                          -- where the Logos data was read from
    counts_json  TEXT NOT NULL,                 -- what the run created, updated and left out
    undone_at    INTEGER
);

CREATE TABLE logos_import_log (
    kind         TEXT NOT NULL,                 -- note, highlight, clipping, passage_list, word_list, prayer_list, reading_plan, folder
    logos_key    TEXT NOT NULL,                 -- the thing's ID in Logos, with its kind
    changed      TEXT NOT NULL,                 -- when it last changed in Logos, as Logos wrote it
    object_type  TEXT NOT NULL,                 -- the Glossa table it became: notes, highlights, lists, reading_plans, folders
    object_id    TEXT NOT NULL,
    run_id       TEXT NOT NULL REFERENCES logos_import_runs(id),
    detail_json  TEXT,                          -- what Glossa has no column for, such as a highlight's original style name
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (kind, logos_key)
);
CREATE INDEX logos_import_log_run ON logos_import_log(run_id);

PRAGMA user_version = 11;
