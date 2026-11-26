package store

import (
	"context"
	"testing"
)

// The update settings (on or off, channel, last check) belong to one device: sync must not copy
// one computer's choice to another (ADR 0016).
func TestUpdateSettingsAreNotSynced(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	put := func(key string) {
		err := s.Write(ctx, "test", func(tx *Tx) error {
			return tx.Create("settings", Row{"key": key, "value_json": `{"enabled":false}`})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	put("update.settings")
	put("reading.font")
	if !perDeviceObject("settings", `["update.settings"]`) || perDeviceObject("settings", `["reading.font"]`) {
		t.Fatal("perDeviceObject does not single out update.settings")
	}
	for name, get := range map[string]func() ([]Change, error){
		"local changes": func() ([]Change, error) { return s.LocalChanges(ctx, "", 100) },
		"snapshot":      func() ([]Change, error) { return s.SnapshotChanges(ctx) },
	} {
		changes, err := get()
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, c := range changes {
			if c.ObjectType == "settings" && c.ObjectID == `["update.settings"]` {
				t.Errorf("%s include the update settings", name)
			}
			found = found || c.ObjectID == `["reading.font"]`
		}
		if !found {
			t.Errorf("%s lost an ordinary setting", name)
		}
	}
}

// Readwise (ADR 0014): the links from Readwise sources and highlights to notes travel between devices, so a
// second device that imports updates the same notes instead of making copies; the time of the last import
// is a "connect." setting and stays on its device.
func TestReadwiseLinksSyncButLastImportDoesNot(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	err := s.Write(ctx, "test", func(tx *Tx) error {
		if err := tx.Create("readwise_sources", Row{"source_id": "1", "note_id": "n", "state": "linked", "body_hash": "h", "meta_json": "{}", "synced_at": int64(1)}); err != nil {
			return err
		}
		if err := tx.Create("readwise_highlights", Row{"highlight_id": "9", "source_id": "1", "block": "> x"}); err != nil {
			return err
		}
		return tx.Create("settings", Row{"key": "connect.readwise.sync", "value_json": `{"at":"2026-01-01T00:00:00Z"}`})
	})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := s.LocalChanges(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range changes {
		got[c.ObjectType] = true
		if c.ObjectType == "settings" && c.ObjectID == `["connect.readwise.sync"]` {
			t.Error("the last import time would be sent to other devices")
		}
	}
	if !got["readwise_sources"] || !got["readwise_highlights"] {
		t.Errorf("the link tables are not synced: %v", got)
	}
}

// Obsidian (ADR 0014): a link ties a note to a file path inside the vault chosen on this computer, so it is per
// device like any_links; neither the links nor the vault setting are sent to another device.
func TestObsidianLinksAndVaultAreNotSynced(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	err := s.Write(ctx, "test", func(tx *Tx) error {
		if err := tx.Create("obsidian_links", Row{"kind": "note", "item_id": "n", "path": "Glossa/n.md", "synced_at": int64(1)}); err != nil {
			return err
		}
		if err := tx.Create("settings", Row{"key": "connect.obsidian.vault", "value_json": `{"vault":"C:\v"}`}); err != nil {
			return err
		}
		return tx.Create("settings", Row{"key": "reading.font", "value_json": `"serif"`})
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, get := range map[string]func() ([]Change, error){
		"local changes": func() ([]Change, error) { return s.LocalChanges(ctx, "", 100) },
		"snapshot":      func() ([]Change, error) { return s.SnapshotChanges(ctx) },
	} {
		changes, err := get()
		if err != nil {
			t.Fatal(err)
		}
		ordinary := false
		for _, c := range changes {
			if c.ObjectType == "obsidian_links" || c.ObjectType == "settings" && c.ObjectID == `["connect.obsidian.vault"]` {
				t.Errorf("%s include %s %s", name, c.ObjectType, c.ObjectID)
			}
			ordinary = ordinary || c.ObjectType == "settings" && c.ObjectID == `["reading.font"]`
		}
		if !ordinary {
			t.Errorf("%s lost an ordinary setting", name)
		}
	}
	if !PerDevice("obsidian_links", `["note","n"]`) {
		t.Error("obsidian_links is not per device")
	}
}
