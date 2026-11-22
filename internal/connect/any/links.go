package anyapp

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// A link ties one note or sermon to one page in Any. It remembers what both sides looked like at the
// last sync, so Glossa can tell which side changed since. Links live in library.db's any_links table,
// which is per device (see store/remote.go): the page IDs belong to the Any app on this computer.

const (
	kindNote   = "note"
	kindSermon = "sermon"

	spaceSetting = "connect.any.space" // the default space; per device like every connect.* setting
)

type link struct {
	Kind, ItemID, SpaceID, ObjectID  string
	GlossaHash, AnyHash, AnyModified string
	SyncedAt                         int64
}

func hashOf(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// glossaHash is what Glossa's side looked like: title, text and passages.
func glossaHash(it item) string {
	return hashOf(strings.TrimSpace(it.Title), strings.TrimSpace(it.Body), it.Passages)
}

// anyHash is what the page looked like, as Any returned it.
func anyHash(p Page) string {
	return hashOf(strings.TrimSpace(p.Name), strings.TrimSpace(p.Markdown))
}

const linkCols = `kind, item_id, space_id, object_id, glossa_hash, any_hash, any_modified, synced_at`

func scanLink(r interface{ Scan(...any) error }) (link, error) {
	var l link
	err := r.Scan(&l.Kind, &l.ItemID, &l.SpaceID, &l.ObjectID, &l.GlossaHash, &l.AnyHash, &l.AnyModified, &l.SyncedAt)
	return l, err
}

func linkOfItem(ctx context.Context, env *command.Env, kind, id string) (*link, error) {
	l, err := scanLink(env.Store.QueryRow(ctx, `SELECT `+linkCols+` FROM any_links WHERE kind = ? AND item_id = ?`, kind, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &l, err
}

func linkOfPage(ctx context.Context, env *command.Env, space, object string) (*link, error) {
	l, err := scanLink(env.Store.QueryRow(ctx, `SELECT `+linkCols+` FROM any_links WHERE space_id = ? AND object_id = ?`, space, object))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &l, err
}

// linksInSpace maps page ID to link, for marking pages already linked in a picker.
func linksInSpace(ctx context.Context, env *command.Env, space string) (map[string]link, error) {
	rows, err := env.Store.Query(ctx, `SELECT `+linkCols+` FROM any_links WHERE space_id = ?`, space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]link{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out[l.ObjectID] = l
	}
	return out, rows.Err()
}

func saveLink(ctx context.Context, env *command.Env, l link) error {
	l.SyncedAt = time.Now().UnixMilli()
	return env.Store.Write(ctx, "any.link", func(tx *store.Tx) error {
		key := store.Row{"kind": l.Kind, "item_id": l.ItemID}
		fields := store.Row{"space_id": l.SpaceID, "object_id": l.ObjectID, "glossa_hash": l.GlossaHash,
			"any_hash": l.AnyHash, "any_modified": l.AnyModified, "synced_at": l.SyncedAt}
		if _, err := tx.Get("any_links", key); errors.Is(err, store.ErrNotFound) {
			fields["kind"], fields["item_id"] = l.Kind, l.ItemID
			return tx.Create("any_links", fields)
		} else if err != nil {
			return err
		}
		return tx.Update("any_links", key, fields)
	})
}

// dropLink forgets a link (the page is gone from Any, or the note from Glossa).
func dropLink(ctx context.Context, env *command.Env, kind, id string) error {
	return env.Store.Write(ctx, "any.unlink", func(tx *store.Tx) error {
		err := tx.Delete("any_links", store.Row{"kind": kind, "item_id": id})
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	})
}

type defaultSpace struct {
	SpaceID string `json:"space_id"`
	Name    string `json:"name"`
}

func loadDefaultSpace(ctx context.Context, env *command.Env) (defaultSpace, error) {
	var raw string
	err := env.Store.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, spaceSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultSpace{}, nil
	}
	if err != nil {
		return defaultSpace{}, err
	}
	var d defaultSpace
	json.Unmarshal([]byte(raw), &d)
	return d, nil
}

func saveDefaultSpace(ctx context.Context, env *command.Env, d defaultSpace) error {
	raw, _ := json.Marshal(d)
	return env.Store.Write(ctx, "any.set_space", func(tx *store.Tx) error {
		k := store.Row{"key": spaceSetting}
		if _, err := tx.Get("settings", k); errors.Is(err, store.ErrNotFound) {
			return tx.Create("settings", store.Row{"key": spaceSetting, "value_json": string(raw)})
		} else if err != nil {
			return err
		}
		return tx.Update("settings", k, store.Row{"value_json": string(raw)})
	})
}
