package folder

import (
	"context"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/document"
	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/lists"
	"github.com/Perlsowisdom/glossa/internal/plan"
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
