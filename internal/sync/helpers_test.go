package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Perlsowisdom/glossa/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var fastKDF = KDFParams{Time: 1, MemoryKiB: 8 * 1024, Threads: 1}

const password = "correct horse battery"

var baseTime = time.Now().UnixMilli()

// dev is one simulated device: its own library and its own wall clock, no shared state with the
// others except the folder.
type dev struct {
	t    *testing.T
	name string
	st   *store.Store
	mu   gosync.Mutex
	ms   int64
	eng  *Engine
}

func newDev(t *testing.T, name string) *dev {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SetDeviceName(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	d := &dev{t: t, name: name, st: st, ms: baseTime}
	st.SetNow(d.now)
	return d
}

func (d *dev) now() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ms
}

// at sets this device's wall clock to base + offset milliseconds.
func (d *dev) at(offset int64) *dev {
	d.mu.Lock()
	d.ms = baseTime + offset
	d.mu.Unlock()
	return d
}

// connect opens (initialising, if needed) the folder for this device.
func (d *dev) connect(folder string) *Engine {
	d.t.Helper()
	ctx := context.Background()
	tr := NewFolder(folder)
	if ok, err := IsInitialised(ctx, tr); err != nil {
		d.t.Fatal(err)
	} else if !ok {
		if err := Initialize(ctx, tr, password, fastKDF); err != nil {
			d.t.Fatal(err)
		}
	}
	e, err := Open(ctx, d.st, tr, password)
	if err != nil {
		d.t.Fatal(err)
	}
	e.Now = func() time.Time { return time.UnixMilli(d.now()) }
	d.eng = e
	return e
}

func (d *dev) sync() Result {
	d.t.Helper()
	r, err := d.eng.Sync(context.Background())
	if err != nil {
		d.t.Fatalf("%s: sync: %v", d.name, err)
	}
	return r
}

func (d *dev) write(cmd string, fn func(tx *store.Tx) error) {
	d.t.Helper()
	if err := d.st.Write(context.Background(), cmd, fn); err != nil {
		d.t.Fatalf("%s: %s: %v", d.name, cmd, err)
	}
}

func (d *dev) mkNote(id, body string) {
	d.t.Helper()
	d.write("note.create", func(tx *store.Tx) error {
		return tx.Create("notes", store.Row{"id": id, "title": "t " + id, "body_md": body, "created_at": int64(1), "updated_at": int64(1)})
	})
}

func (d *dev) editNote(id, body string) {
	d.t.Helper()
	d.write("note.edit", func(tx *store.Tx) error {
		return tx.Update("notes", store.ByID(id), store.Row{"body_md": body})
	})
}

func (d *dev) setNoteTitle(id, title string) {
	d.t.Helper()
	d.write("note.edit", func(tx *store.Tx) error {
		return tx.Update("notes", store.ByID(id), store.Row{"title": title})
	})
}

func (d *dev) mkList(id, name string) {
	d.t.Helper()
	d.write("list.create", func(tx *store.Tx) error {
		return tx.Create("lists", store.Row{"id": id, "kind": "verses", "name": name, "created_at": int64(1), "updated_at": int64(1)})
	})
}

func (d *dev) renameList(id, name string) {
	d.t.Helper()
	d.write("list.rename", func(tx *store.Tx) error {
		return tx.Update("lists", store.ByID(id), store.Row{"name": name})
	})
}

func (d *dev) deleteNote(id string) {
	d.t.Helper()
	d.write("note.delete", func(tx *store.Tx) error { return tx.Delete("notes", store.ByID(id)) })
}

func (d *dev) purgeNote(id string) {
	d.t.Helper()
	d.write("bin.purge", func(tx *store.Tx) error { return tx.Purge("notes", store.ByID(id)) })
}

// note reads a note: its body, whether it is in the bin, and whether the row exists at all.
func (d *dev) note(id string) (body string, deleted, exists bool) {
	d.t.Helper()
	var del sql.NullInt64
	err := d.st.QueryRow(context.Background(), `SELECT body_md, deleted_at FROM notes WHERE id = ?`, id).Scan(&body, &del)
	if err == sql.ErrNoRows {
		return "", false, false
	}
	if err != nil {
		d.t.Fatal(err)
	}
	return body, del.Valid, true
}

func (d *dev) listName(id string) (string, bool) {
	d.t.Helper()
	var n string
	err := d.st.QueryRow(context.Background(), `SELECT name FROM lists WHERE id = ?`, id).Scan(&n)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		d.t.Fatal(err)
	}
	return n, true
}

// dump is every synced row of the tables the tests use, as text, so two libraries can be compared.
func (d *dev) dump() string {
	d.t.Helper()
	var out []string
	for _, q := range []string{
		`SELECT 'note', id, title, body_md, deleted_at IS NOT NULL FROM notes`,
		`SELECT 'list', id, name, kind, deleted_at IS NOT NULL FROM lists`,
		`SELECT 'doc', id, title, body_md, deleted_at IS NOT NULL FROM documents`,
	} {
		rows, err := d.st.Query(context.Background(), q)
		if err != nil {
			d.t.Fatal(err)
		}
		for rows.Next() {
			var a, b, c, e string
			var f bool
			if err := rows.Scan(&a, &b, &c, &e, &f); err != nil {
				d.t.Fatal(err)
			}
			out = append(out, fmt.Sprintln(a, b, c, e, f))
		}
		rows.Close()
	}
	sort.Strings(out)
	return strings.Join(out, "")
}

func (d *dev) changeCount() int {
	d.t.Helper()
	var n int
	if err := d.st.QueryRow(context.Background(), `SELECT COUNT(*) FROM changes`).Scan(&n); err != nil {
		d.t.Fatal(err)
	}
	return n
}

func sameLibraries(t *testing.T, ds ...*dev) {
	t.Helper()
	for _, d := range ds[1:] {
		if a, b := ds[0].dump(), d.dump(); a != b {
			t.Fatalf("libraries differ:\n%s:\n%s\n%s:\n%s", ds[0].name, a, d.name, b)
		}
	}
}

func folderOf(t *testing.T) string { return t.TempDir() }

// readAll decrypts every file of a folder with the engine's key and returns the plain text, for
// tests that check what is in the folder.
func readAll(t *testing.T, e *Engine, folder string) string {
	t.Helper()
	ctx := context.Background()
	var sb strings.Builder
	devs, err := e.devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, dv := range devs {
		f, _ := e.listDevice(ctx, dv)
		for seq := range f.changes {
			raw, err := e.t.Read(ctx, dv+"/"+changeName(seq))
			if err != nil {
				t.Fatal(err)
			}
			p, err := open(e.key, raw, kindChange, dv, seq)
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(p)
		}
		for _, seq := range f.snaps {
			raw, err := e.t.Read(ctx, dv+"/"+snapName(seq))
			if err != nil {
				t.Fatal(err)
			}
			p, err := open(e.key, raw, kindSnapshot, dv, seq)
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(p)
		}
	}
	return sb.String()
}

func jsonOf(v any) string { b, _ := json.Marshal(v); return string(b) }
