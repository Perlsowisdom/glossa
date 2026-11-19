package secrets

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func exercise(t *testing.T, s Store) {
	t.Helper()
	if _, err := s.Get("k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get("k"); err != nil || v != "v" {
		t.Fatalf("got %q, %v", v, err)
	}
	if err := s.Delete("k"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("k"); err != nil {
		t.Fatalf("deleting a missing secret should succeed: %v", err)
	}
	if _, err := s.Get("k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestMemory(t *testing.T) { exercise(t, NewMemory()) }

// The OS store runs against the keyring library's in-memory mock, never the real keychain.
func TestOSStoreWithMock(t *testing.T) {
	keyring.MockInit()
	exercise(t, OS())
}

func TestMigratingReadsLegacyThenSavesUnderNewName(t *testing.T) {
	cur, old := NewMemory(), NewMemory()
	old.Set("assistant.key", "sk-old")
	s := Migrating(cur, old)
	v, err := s.Get("assistant.key")
	if err != nil || v != "sk-old" {
		t.Fatalf("got %q, %v", v, err)
	}
	if got, err := cur.Get("assistant.key"); err != nil || got != "sk-old" {
		t.Fatalf("not saved under the new name: %q, %v", got, err)
	}
	// The new name wins once it has a value.
	cur.Set("assistant.key", "sk-new")
	if v, _ := s.Get("assistant.key"); v != "sk-new" {
		t.Fatalf("got %q", v)
	}
	if _, err := s.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestMigratingWritesNewAndDeletesBoth(t *testing.T) {
	cur, old := NewMemory(), NewMemory()
	old.Set("k", "old")
	s := Migrating(cur, old)
	s.Set("k", "new")
	if v, _ := old.Get("k"); v != "old" {
		t.Fatal("legacy entry must not be rewritten")
	}
	if err := s.Delete("k"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted secret came back: %v", err)
	}
}
