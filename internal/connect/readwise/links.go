package readwise

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// The link tables (migration 0018) and the last-import setting.

const syncSetting = "connect.readwise.sync" // per device: the "connect." prefix is never synced (store/remote.go)

// States of a source's link.
const (
	stateLinked  = "linked"
	stateKept    = "kept"    // the person chose to keep their own text
	stateRemoved = "removed" // the person deleted the note
)

type source struct {
	ID, NoteID, ItemID, State, BodyHash string
	Meta                                Meta
}

func scanSource(r interface{ Scan(...any) error }) (source, error) {
	var s source
	var item sql.NullString
	var meta string
	if err := r.Scan(&s.ID, &s.NoteID, &item, &s.State, &s.BodyHash, &meta); err != nil {
		return s, err
	}
	s.ItemID = item.String
	if err := json.Unmarshal([]byte(meta), &s.Meta); err != nil {
		return s, err
	}
	return s, nil
}

const sourceCols = `source_id, note_id, item_id, state, body_hash, meta_json`

func loadSource(ctx context.Context, env *command.Env, id string) (*source, error) {
	s, err := scanSource(env.Store.QueryRow(ctx, `SELECT `+sourceCols+` FROM readwise_sources WHERE source_id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

func loadSources(ctx context.Context, env *command.Env) ([]source, error) {
	rows, err := env.Store.Query(ctx, `SELECT `+sourceCols+` FROM readwise_sources ORDER BY source_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []source
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func loadHighlights(ctx context.Context, env *command.Env, sid string) (map[string]HL, error) {
	rows, err := env.Store.Query(ctx, `SELECT highlight_id, block, loc, at, updated_at FROM readwise_highlights WHERE source_id = ?`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]HL{}
	for rows.Next() {
		var h HL
		if err := rows.Scan(&h.ID, &h.Block, &h.Loc, &h.At, &h.UpdatedAt); err != nil {
			return nil, err
		}
		out[h.ID] = h
	}
	return out, rows.Err()
}

func list(m map[string]HL) []HL {
	out := make([]HL, 0, len(m))
	for _, h := range m {
		out = append(out, h)
	}
	return out
}

// noteState is what is in the person's note now.
type noteState struct {
	Exists      bool
	Title, Body string
	Tags        []string
}

func readNote(ctx context.Context, env *command.Env, id string) (noteState, error) {
	var title sql.NullString
	var body string
	var deleted sql.NullInt64
	err := env.Store.QueryRow(ctx, `SELECT title, body_md, deleted_at FROM notes WHERE id = ?`, id).Scan(&title, &body, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return noteState{}, nil
	}
	if err != nil {
		return noteState{}, err
	}
	if deleted.Valid {
		return noteState{}, nil
	}
	n := noteState{Exists: true, Title: title.String, Body: body}
	rows, err := env.Store.Query(ctx, `SELECT tag FROM note_tags WHERE note_id = ? ORDER BY tag`, id)
	if err != nil {
		return n, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return n, err
		}
		n.Tags = append(n.Tags, t)
	}
	return n, rows.Err()
}

// delta is what one source's import changes in the link tables.
type delta struct {
	Put    []HL
	Remove []string
	Source *source // written when not nil
}

func (d delta) empty() bool { return len(d.Put) == 0 && len(d.Remove) == 0 && d.Source == nil }

func apply(ctx context.Context, env *command.Env, sid string, d delta) error {
	if d.empty() {
		return nil
	}
	return env.Store.Write(ctx, "readwise.import", func(tx *store.Tx) error {
		for _, h := range d.Put {
			k := store.Row{"highlight_id": h.ID}
			fields := store.Row{"source_id": sid, "block": h.Block, "loc": h.Loc, "at": h.At, "updated_at": h.UpdatedAt}
			if _, err := tx.Get("readwise_highlights", k); errors.Is(err, store.ErrNotFound) {
				fields["highlight_id"] = h.ID
				err = tx.Create("readwise_highlights", fields)
				if err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if err := tx.Update("readwise_highlights", k, fields); err != nil {
				return err
			}
		}
		for _, id := range d.Remove {
			if err := tx.Delete("readwise_highlights", store.Row{"highlight_id": id}); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		if s := d.Source; s != nil {
			meta, _ := json.Marshal(s.Meta)
			var item any
			if s.ItemID != "" {
				item = s.ItemID
			}
			k := store.Row{"source_id": s.ID}
			fields := store.Row{"note_id": s.NoteID, "item_id": item, "state": s.State, "body_hash": s.BodyHash,
				"meta_json": string(meta), "synced_at": time.Now().UnixMilli()}
			if _, err := tx.Get("readwise_sources", k); errors.Is(err, store.ErrNotFound) {
				fields["source_id"] = s.ID
				return tx.Create("readwise_sources", fields)
			} else if err != nil {
				return err
			}
			return tx.Update("readwise_sources", k, fields)
		}
		return nil
	})
}

// LastImport is what the last successful import left behind, per device.
type LastImport struct {
	At           string `json:"at" desc:"When the last import started (UTC); the next one asks only for what changed after it"`
	Sources      int    `json:"sources"`
	Highlights   int    `json:"highlights"`
	NotesNew     int    `json:"notes_new"`
	NotesUpdated int    `json:"notes_updated"`
}

func loadLast(ctx context.Context, env *command.Env) (*LastImport, error) {
	var raw string
	err := env.Store.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, syncSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l LastImport
	if json.Unmarshal([]byte(raw), &l) != nil || l.At == "" {
		return nil, nil
	}
	return &l, nil
}

func saveLast(ctx context.Context, env *command.Env, l LastImport) error {
	raw, _ := json.Marshal(l)
	return env.Store.Write(ctx, "readwise.import", func(tx *store.Tx) error {
		k := store.Row{"key": syncSetting}
		if _, err := tx.Get("settings", k); errors.Is(err, store.ErrNotFound) {
			return tx.Create("settings", store.Row{"key": syncSetting, "value_json": string(raw)})
		} else if err != nil {
			return err
		}
		return tx.Update("settings", k, store.Row{"value_json": string(raw)})
	})
}
