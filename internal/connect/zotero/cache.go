package zotero

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// The kept copies live in library.db's cite_items table (schema 15), one row per item, so a document
// that cites a Zotero item still shows its citation when Zotero is off or not reachable.

type cachedEntry struct {
	Entry
	Version  int
	CachedAt int64
}

func rowID(key string) string { return IDPrefix + key }

// saveCache keeps a copy of an item and reports whether the kept copy changed.
func saveCache(ctx context.Context, env *command.Env, e Entry, version int) (bool, error) {
	if env == nil || env.Store == nil {
		return false, command.Errorf("unavailable", "keeping a copy needs an open library")
	}
	raw, err := json.Marshal(e.Item)
	if err != nil {
		return false, err
	}
	changed := false
	err = env.Store.Write(ctx, "zotero.cite", func(tx *store.Tx) error {
		k := store.Row{"id": rowID(e.Key)}
		row, err := tx.Get("cite_items", k)
		if errors.Is(err, store.ErrNotFound) {
			changed = true
			return tx.Create("cite_items", store.Row{"id": rowID(e.Key), "connection": "zotero", "item_key": e.Key,
				"csl_json": string(raw), "title": e.Item.Title, "library_version": version, "cached_at": time.Now().UnixMilli()})
		} else if err != nil {
			return err
		}
		changed = row["csl_json"] != string(raw)
		fields := store.Row{"library_version": version}
		if changed {
			fields["csl_json"], fields["title"], fields["cached_at"] = string(raw), e.Item.Title, time.Now().UnixMilli()
		}
		return tx.Update("cite_items", k, fields)
	})
	return changed, err
}

func scan(rows interface{ Scan(...any) error }) (cachedEntry, error) {
	var c cachedEntry
	var raw string
	var ver sql.NullInt64
	if err := rows.Scan(&c.Key, &raw, &ver, &c.CachedAt); err != nil {
		return c, err
	}
	c.Version = int(ver.Int64)
	if err := json.Unmarshal([]byte(raw), &c.Item); err != nil {
		return c, err
	}
	return c, nil
}

func loadCache(ctx context.Context, env *command.Env, key string) (cachedEntry, bool) {
	if env == nil || env.Store == nil {
		return cachedEntry{}, false
	}
	c, err := scan(env.Store.QueryRow(ctx, `SELECT item_key, csl_json, library_version, cached_at FROM cite_items WHERE id = ?`, rowID(key)))
	return c, err == nil
}

func listCache(ctx context.Context, env *command.Env) ([]cachedEntry, error) {
	if env == nil || env.Store == nil {
		return nil, nil
	}
	rows, err := env.Store.Query(ctx, `SELECT item_key, csl_json, library_version, cached_at FROM cite_items WHERE connection = 'zotero' ORDER BY title COLLATE NOCASE, item_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []cachedEntry
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
