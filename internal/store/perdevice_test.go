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
