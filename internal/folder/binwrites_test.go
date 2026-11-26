package folder

import (
	"context"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/document"
	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/lists"
	"github.com/Perlsowisdom/glossa/internal/plan"
	"github.com/Perlsowisdom/glossa/internal/store"
)

func (e *env) changeCount() int {
	e.t.Helper()
	var n int
	if err := e.st.QueryRow(context.Background(), `SELECT COUNT(*) FROM changes`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Nothing that is in the bin may be written to: every command refuses with an error and changes nothing.
func TestNothingInTheBinCanBeWrittenTo(t *testing.T) {
	e := setup(t)
	plan.Register(e.r)
	lists.Register(e.r)
	library.Register(e.r)

	var p plan.Plan
	e.must("plan.create", map[string]any{"template": "gospels-30"}, &p)
	var n core.Note
	e.must("note.create", map[string]any{"body": "text"}, &n)
	var d document.Document
	e.must("document.create", map[string]any{"title": "Sermon"}, &d)
	var f Folder
	e.must("folder.create", map[string]any{"name": "Series"}, &f)
	var f2 Folder
	e.must("folder.create", map[string]any{"name": "Other"}, &f2)
	var l lists.List
	e.must("list.create", map[string]any{"name": "Verses", "kind": "verses"}, &l)
	var entry lists.Entry
	e.must("list.add", map[string]any{"list": l.ID, "ref": "John 3:16"}, &entry)
	var c library.CollectionResult
	e.must("collection.create", map[string]any{"name": "Mine"}, &c)
	var c2 library.CollectionResult
	e.must("collection.create", map[string]any{"name": "Inner"}, &c2)

	e.must("plan.delete", map[string]any{"id": p.ID}, nil)
	e.must("note.delete", map[string]any{"id": n.ID}, nil)
	e.must("document.delete", map[string]any{"id": d.ID}, nil)
	e.must("folder.delete", map[string]any{"id": f.ID}, nil)
	e.must("list.delete", map[string]any{"id": l.ID}, nil)
	e.must("collection.delete", map[string]any{"id": c.ID}, nil)

	writes := []struct {
		id     string
		params map[string]any
	}{
		{"plan.mark", map[string]any{"plan_id": p.ID, "days": []int{1}}},
		{"plan.mark", map[string]any{"plan_id": p.ID}},
		{"plan.unmark", map[string]any{"plan_id": p.ID, "day": 1}},
		{"plan.reschedule", map[string]any{"plan_id": p.ID}},
		{"plan.delete", map[string]any{"id": p.ID}},
		{"note.update", map[string]any{"id": n.ID, "body": "changed"}},
		{"note.move", map[string]any{"id": n.ID, "free": true}},
		{"note.move", map[string]any{"id": n.ID, "folder_id": f2.ID}},
		{"note.delete", map[string]any{"id": n.ID}},
		{"document.update", map[string]any{"id": d.ID, "title": "Changed"}},
		{"document.move", map[string]any{"id": d.ID, "folder_id": f2.ID}},
		{"document.delete", map[string]any{"id": d.ID}},
		{"folder.rename", map[string]any{"id": f.ID, "name": "Changed"}},
		{"folder.move", map[string]any{"id": f.ID, "parent_id": f2.ID}},
		{"folder.delete", map[string]any{"id": f.ID}},
		{"list.rename", map[string]any{"id": l.ID, "name": "Changed"}},
		{"list.add", map[string]any{"list": l.ID, "ref": "John 3:17"}},
		{"list.remove", map[string]any{"id": entry.ID}},
		{"list.reorder", map[string]any{"list": l.ID, "ids": []string{entry.ID}}},
		{"list.delete", map[string]any{"id": l.ID}},
		{"collection.update", map[string]any{"id": c.ID, "name": "Changed"}},
		{"collection.add", map[string]any{"collection_id": c.ID, "item_ids": []string{"x"}}},
		{"collection.remove", map[string]any{"collection_id": c.ID, "item_ids": []string{"x"}}},
		{"collection.delete", map[string]any{"id": c.ID}},
		{"collection.create", map[string]any{"name": "Inside", "parent_id": c.ID}},
		{"collection.update", map[string]any{"id": c2.ID, "parent_id": c.ID}},
		{"note.create", map[string]any{"body": "into the bin", "folder_id": f.ID}},
		{"document.create", map[string]any{"title": "Into the bin", "folder_id": f.ID}},
		{"folder.create", map[string]any{"name": "Inside", "parent_id": f.ID}},
	}
	for _, w := range writes {
		before := e.changeCount()
		err := e.run(w.id, w.params, nil)
		if err == nil {
			t.Errorf("%s %v on something in the bin did not fail", w.id, w.params)
		}
		if after := e.changeCount(); after != before {
			t.Errorf("%s %v on something in the bin wrote %d changes before it failed", w.id, w.params, after-before)
		}
	}
}

func TestCollectionGoesToBinRestoresAndPurges(t *testing.T) {
	e := setup(t)
	library.Register(e.r)
	ctx := context.Background()
	var book string
	if err := e.st.Write(ctx, "test.seed", func(tx *store.Tx) error {
		book = store.NewID()
		return tx.Create("items", store.Row{"id": book, "kind": "book", "title": "A book", "source": "import", "created_at": int64(1), "updated_at": int64(1)})
	}); err != nil {
		t.Fatal(err)
	}
	var top, inner, alone library.CollectionResult
	e.must("collection.create", map[string]any{"name": "Reformed", "item_ids": []string{book}}, &top)
	e.must("collection.create", map[string]any{"name": "Puritans", "parent_id": top.ID, "item_ids": []string{book}}, &inner)
	e.must("collection.create", map[string]any{"name": "Alone"}, &alone)
	e.must("collection.delete", map[string]any{"id": alone.ID}, nil)
	e.must("collection.delete", map[string]any{"id": top.ID}, nil)

	bin := binOf(e)
	if len(bin) != 2 {
		t.Fatalf("bin = %+v", bin)
	}
	var got BinEntry
	for _, b := range bin {
		if b.ID == top.ID {
			got = b
		}
	}
	if got.Kind != "collection" || got.Title != "Reformed" || got.Collections != 1 {
		t.Fatalf("the bin entry for the collection = %+v", got)
	}

	var back Restored
	e.must("bin.restore", map[string]any{"kind": "collection", "id": top.ID}, &back)
	if back.Collections != 2 {
		t.Fatalf("restored = %+v", back)
	}
	var tree library.ListResult
	e.must("collection.list", map[string]any{}, &tree)
	var names []string
	var walk func(ns []*library.Node)
	walk = func(ns []*library.Node) {
		for _, n := range ns {
			if n.Kind != "builtin" {
				names = append(names, n.Name)
				if n.Name == "Reformed" && n.Count != 1 {
					t.Errorf("the books stayed in the restored collection: %d", n.Count)
				}
			}
			walk(n.Children)
		}
	}
	walk(tree.Tree)
	if len(names) != 2 {
		t.Fatalf("collections after restore = %v", names)
	}
	if bin = binOf(e); len(bin) != 1 || bin[0].ID != alone.ID {
		t.Fatalf("bin after restore = %+v", bin)
	}

	// Delete for good removes the collection, the ones inside it and what they held, with their history values.
	e.must("collection.delete", map[string]any{"id": top.ID}, nil)
	var gone Purged
	e.must("bin.purge", map[string]any{"kind": "collection", "id": top.ID}, &gone)
	if gone.Collections != 2 {
		t.Fatalf("purged = %+v", gone)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM collections WHERE id IN ('` + top.ID + `', '` + inner.ID + `')`,
		`SELECT COUNT(*) FROM collection_items WHERE collection_id IN ('` + top.ID + `', '` + inner.ID + `')`,
		`SELECT COUNT(*) FROM changes WHERE object_type IN ('collections', 'collection_items') AND object_id LIKE '%` + top.ID + `%' AND (before_json IS NOT NULL OR after_json IS NOT NULL)`,
	} {
		var c int
		if err := e.st.QueryRow(ctx, q).Scan(&c); err != nil || c != 0 {
			t.Fatalf("%s = %d, %v", q, c, err)
		}
	}
	if err := e.run("bin.purge", map[string]any{"kind": "collection", "id": alone.ID}, &gone); err != nil {
		t.Fatal(err)
	}
	var live library.CollectionResult
	e.must("collection.create", map[string]any{"name": "Live"}, &live)
	if err := e.run("bin.purge", map[string]any{"kind": "collection", "id": live.ID}, nil); code(err) != "bad_params" {
		t.Fatalf("purge a live collection: %v", err)
	}
}

// set changes columns of one row, as a merge from another device would.
func (e *env) set(table, id string, fields store.Row) {
	e.t.Helper()
	if err := e.st.Write(context.Background(), "test.set", func(tx *store.Tx) error { return tx.Update(table, store.ByID(id), fields) }); err != nil {
		e.t.Fatal(err)
	}
}

// seedNote writes a note straight into a folder with a given deleted_at and batch, like a row that
// arrived from another device or from before deletion batches existed.
func seedNote(t *testing.T, e *env, folder string, deletedAt int64, batch any) string {
	t.Helper()
	id := store.NewID()
	row := store.Row{"id": id, "body_md": "seeded", "folder_id": folder, "created_at": int64(1), "updated_at": int64(1), "deleted_at": deletedAt}
	if batch != nil {
		row["deletion_batch"] = batch
	}
	if err := e.st.Write(context.Background(), "test.seed", func(tx *store.Tx) error { return tx.Create("notes", row) }); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRestoreOfAFolderBringsBackExactlyItsBatch(t *testing.T) {
	e := setup(t)
	var f Folder
	e.must("folder.create", map[string]any{"name": "Series"}, &f)
	var n core.Note
	e.must("note.create", map[string]any{"body": "inside", "folder_id": f.ID}, &n)
	e.must("folder.delete", map[string]any{"id": f.ID}, nil)
	// A note in the folder deleted on its own, stamped later than the folder (as a merge from another
	// device can leave it): the time rule would bring it back with the folder.
	apart := seedNote(t, e, f.ID, 9_000_000_000_000, nil)

	bin := binOf(e)
	var entry BinEntry
	for _, b := range bin {
		if b.ID == f.ID {
			entry = b
		}
	}
	if entry.Notes != 1 {
		t.Fatalf("the folder's bin entry counts %d notes, want only its batch (1): %+v", entry.Notes, bin)
	}
	var back Restored
	e.must("bin.restore", map[string]any{"kind": "folder", "id": f.ID}, &back)
	if back.Notes != 1 || back.Folders != 1 {
		t.Fatalf("restored = %+v", back)
	}
	for _, b := range binOf(e) {
		if b.ID == n.ID {
			t.Fatalf("the note deleted with the folder is still in the bin")
		}
	}
	var deletedAt, batch any
	if err := e.st.QueryRow(context.Background(), `SELECT deleted_at, deletion_batch FROM notes WHERE id = ?`, n.ID).Scan(&deletedAt, &batch); err != nil || deletedAt != nil || batch != nil {
		t.Fatalf("restored note: deleted_at=%v batch=%v err=%v", deletedAt, batch, err)
	}
	stay := binOf(e)
	if len(stay) != 1 || stay[0].ID != apart {
		t.Fatalf("the note deleted separately should still be in the bin: %+v", stay)
	}
}

func TestRestoreOfAnOldFolderFallsBackToTheTimeRule(t *testing.T) {
	e := setup(t)
	var f Folder
	e.must("folder.create", map[string]any{"name": "Old"}, &f)
	var n core.Note
	e.must("note.create", map[string]any{"body": "inside", "folder_id": f.ID}, &n)
	e.must("folder.delete", map[string]any{"id": f.ID}, nil)
	// Make everything look as it did before batches: no ids anywhere.
	e.set("folders", f.ID, store.Row{"deletion_batch": nil})
	e.set("notes", n.ID, store.Row{"deletion_batch": nil})
	early := seedNote(t, e, f.ID, 1, nil)
	bin := binOf(e)
	if len(bin) != 1 || bin[0].Kind != "folder" {
		t.Fatalf("bin = %+v", bin)
	}
	for _, b := range bin {
		if b.Kind == "folder" && b.Notes != 1 {
			t.Fatalf("old data: the folder should count the note deleted at or after it: %+v", b)
		}
	}
	var back Restored
	e.must("bin.restore", map[string]any{"kind": "folder", "id": f.ID}, &back)
	if back.Notes != 1 {
		t.Fatalf("restored = %+v", back)
	}
	stay := binOf(e)
	if len(stay) != 1 || stay[0].ID != early {
		t.Fatalf("a note deleted before the folder stays in the bin: %+v", stay)
	}
}

func TestCollectionRestoreBringsBackExactlyItsBatch(t *testing.T) {
	e := setup(t)
	library.Register(e.r)
	var top, inner, early library.CollectionResult
	e.must("collection.create", map[string]any{"name": "Top"}, &top)
	e.must("collection.create", map[string]any{"name": "Inner", "parent_id": top.ID}, &inner)
	e.must("collection.create", map[string]any{"name": "Early", "parent_id": top.ID}, &early)
	e.must("collection.delete", map[string]any{"id": early.ID}, nil)
	e.must("collection.delete", map[string]any{"id": top.ID}, nil)
	// Make the one deleted on its own look as if it was stamped at the same moment or later.
	e.set("collections", early.ID, store.Row{"deleted_at": int64(9_000_000_000_000)})
	var back Restored
	e.must("bin.restore", map[string]any{"kind": "collection", "id": top.ID}, &back)
	if back.Collections != 2 {
		t.Fatalf("restored = %+v, want Top and Inner only", back)
	}
	bin := binOf(e)
	if len(bin) != 1 || bin[0].ID != early.ID {
		t.Fatalf("bin = %+v", bin)
	}
}
