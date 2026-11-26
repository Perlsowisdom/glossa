package obsidian

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// The vault folder and subfolder, the time of the last export and import, and the link table (migration 0019).
// All of it is per device: the "connect." settings are never synced and obsidian_links is a per-device table
// (store/remote.go), because the paths belong to the vault on this computer.

const (
	configSetting = "connect.obsidian.config"
	syncSetting   = "connect.obsidian.sync"
)

type config struct {
	Vault     string `json:"vault"`
	Subfolder string `json:"subfolder"`
}

func displaySub(s string) string {
	if s == "" {
		return DefaultSubfolder
	}
	if s == "." {
		return "(the vault itself)"
	}
	return s
}

// sub is the subfolder to use: the default when none was chosen, "" for the vault itself.
func (c config) sub() string {
	switch c.Subfolder {
	case "":
		return DefaultSubfolder
	case ".":
		return ""
	}
	return c.Subfolder
}

func loadConfig(ctx context.Context, env *command.Env) (config, error) {
	var c config
	err := loadSetting(ctx, env, configSetting, &c)
	return c, err
}

func loadSetting(ctx context.Context, env *command.Env, key string, into any) error {
	var raw string
	err := env.Store.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = json.Unmarshal([]byte(raw), into) // damaged: start again from nothing
	return nil
}

func saveSetting(ctx context.Context, env *command.Env, name, key string, v any) error {
	raw, _ := json.Marshal(v)
	return env.Store.Write(ctx, name, func(tx *store.Tx) error {
		k := store.Row{"key": key}
		if _, err := tx.Get("settings", k); errors.Is(err, store.ErrNotFound) {
			return tx.Create("settings", store.Row{"key": key, "value_json": string(raw)})
		} else if err != nil {
			return err
		}
		return tx.Update("settings", k, store.Row{"value_json": string(raw)})
	})
}

// cleanSub checks a subfolder the person typed: inside the vault, nothing odd. "" means the default and "." the vault itself.
func cleanSub(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "." {
		return s, nil
	}
	rel, err := cleanRel(s)
	if err != nil {
		return "", command.Errorf("bad_params", "The folder must be inside the vault, for example “Glossa” or “Notes/Glossa”.")
	}
	if rel == "" {
		return ".", nil
	}
	for _, p := range strings.Split(rel, "/") {
		if strings.HasPrefix(p, ".") || safeName(p) != p {
			return "", command.Errorf("bad_params", "“%s” cannot be used as a folder name. Choose a plain name.", p)
		}
	}
	return rel, nil
}

// Last is what the last export or import left behind.
type Last struct {
	At        string `json:"at" desc:"When it finished (UTC)"`
	Written   int    `json:"written,omitempty" desc:"Export: files written or updated"`
	Created   int    `json:"created,omitempty" desc:"Import: notes made"`
	Updated   int    `json:"updated,omitempty" desc:"Import: notes updated"`
	Unchanged int    `json:"unchanged"`
}

type syncInfo struct {
	Export *Last `json:"export,omitempty"`
	Import *Last `json:"import,omitempty"`
}

func loadSync(ctx context.Context, env *command.Env) (syncInfo, error) {
	var s syncInfo
	err := loadSetting(ctx, env, syncSetting, &s)
	return s, err
}

func saveSync(ctx context.Context, env *command.Env, export, imp *Last) error {
	s, err := loadSync(ctx, env)
	if err != nil {
		return err
	}
	if export != nil {
		s.Export = export
	}
	if imp != nil {
		s.Import = imp
	}
	return saveSetting(ctx, env, "obsidian.sync", syncSetting, s)
}

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// Links --------------------------------------------------------------------------------------------------

// link ties one note or sermon to one file. The two hashes are what each side looked like at the last sync ("" when
// that is not known, for example when a file was first found by its glossa_id).
type link struct {
	Kind, ItemID, Path, GlossaHash, FileHash string
}

const linkCols = `kind, item_id, path, glossa_hash, file_hash`

func scanLink(r interface{ Scan(...any) error }) (link, error) {
	var l link
	err := r.Scan(&l.Kind, &l.ItemID, &l.Path, &l.GlossaHash, &l.FileHash)
	return l, err
}

func linkOfItem(ctx context.Context, env *command.Env, kind, id string) (*link, error) {
	l, err := scanLink(env.Store.QueryRow(ctx, `SELECT `+linkCols+` FROM obsidian_links WHERE kind = ? AND item_id = ?`, kind, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &l, err
}

func linkOfPath(ctx context.Context, env *command.Env, rel string) (*link, error) {
	l, err := scanLink(env.Store.QueryRow(ctx, `SELECT `+linkCols+` FROM obsidian_links WHERE path = ? COLLATE NOCASE`, rel))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &l, err
}

func allLinks(ctx context.Context, env *command.Env) ([]link, error) {
	rows, err := env.Store.Query(ctx, `SELECT `+linkCols+` FROM obsidian_links ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []link
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func saveLink(ctx context.Context, env *command.Env, l link) error {
	return env.Store.Write(ctx, "obsidian.link", func(tx *store.Tx) error {
		key := store.Row{"kind": l.Kind, "item_id": l.ItemID}
		fields := store.Row{"path": l.Path, "glossa_hash": l.GlossaHash, "file_hash": l.FileHash, "synced_at": time.Now().UnixMilli()}
		if _, err := tx.Get("obsidian_links", key); errors.Is(err, store.ErrNotFound) {
			fields["kind"], fields["item_id"] = l.Kind, l.ItemID
			return tx.Create("obsidian_links", fields)
		} else if err != nil {
			return err
		}
		return tx.Update("obsidian_links", key, fields)
	})
}

// forgetVault drops every link: they point into the vault that was just replaced. The files stay where they are.
func forgetLinks(ctx context.Context, env *command.Env) error {
	ls, err := allLinks(ctx, env)
	if err != nil {
		return err
	}
	return env.Store.Write(ctx, "obsidian.unlink", func(tx *store.Tx) error {
		for _, l := range ls {
			if err := tx.Delete("obsidian_links", store.Row{"kind": l.Kind, "item_id": l.ItemID}); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		return nil
	})
}
