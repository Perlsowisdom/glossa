package logosimport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/bible"
	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/document"
	"github.com/Perlsowisdom/glossa/internal/folder"
	"github.com/Perlsowisdom/glossa/internal/licence"
	"github.com/Perlsowisdom/glossa/internal/store"
	"github.com/Perlsowisdom/glossa/internal/verse"
)

// newSermonHarness is a Glossa library with the sermon commands and stand-in Bibles ("ESV" may not
// be copied, "NRSV" is public domain here) and a fake Logos folder with sermons only.
func newSermonHarness(t *testing.T, installed ...string) *harness {
	t.Helper()
	src := sermonsFixture(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := command.NewRegistry()
	pd, _ := licence.Defaults(licence.PublicDomain, "")
	live, _ := licence.Defaults(licence.CreativeCommons, "CC-BY-4.0")
	live.Store, live.RightsHolder = false, "A Publisher"
	all := map[string]bible.Meta{
		"nrsv": {ID: "nrsv", Abbreviation: "NRSV", Name: "New Revised Standard Version", Licence: pd},
		"esv":  {ID: "esv", Abbreviation: "ESV", Name: "English Standard Version", Licence: live},
	}
	r.Add(command.Define("bible.modules", "stand-in", false, func(context.Context, *command.Env, struct{}) ([]bible.Meta, error) {
		var out []bible.Meta
		for _, id := range installed {
			out = append(out, all[id])
		}
		return out, nil
	}))
	type readParams struct {
		Module string `json:"module"`
		Ref    string `json:"ref"`
	}
	r.Add(command.Define("bible.read", "stand-in", false, func(_ context.Context, _ *command.Env, p readParams) (bible.Passage, error) {
		rg, err := verse.Parse(p.Ref)
		if err != nil {
			return bible.Passage{}, err
		}
		out := bible.Passage{Module: p.Module, Ref: rg.String()}
		for id := rg.Start; id <= rg.End; id++ {
			out.Verses = append(out.Verses, bible.ReadVerse{ID: id, Ref: id.String(), Text: "Verse text " + id.String()})
		}
		return out, nil
	}))
	core.Register(r)
	folder.Register(r)
	document.Register(r)
	Register(r)
	return &harness{t: t, r: r, env: &command.Env{Store: st}, st: st, src: src, svc: &service{r: r, tmpDir: t.TempDir()}}
}

type docOut struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Passages []struct {
		Display string `json:"display"`
	} `json:"passages"`
	Points []struct {
		H     string `json:"h"`
		Ref   string `json:"ref"`
		Level int    `json:"level"`
	} `json:"points"`
	Body       string `json:"body"`
	PreachedOn string `json:"preached_on"`
	Theme      string `json:"theme"`
	FolderID   string `json:"folder_id"`
	UpdatedAt  int64  `json:"updated_at"`
}

func (h *harness) sermon(title string) docOut {
	h.t.Helper()
	var list []docOut
	h.must("document.list", map[string]any{"kind": "sermon"}, &list)
	for _, d := range list {
		if d.Title == title {
			var one docOut
			h.must("document.get", map[string]any{"id": d.ID}, &one)
			return one
		}
	}
	h.t.Fatalf("no sermon %q among %d", title, len(list))
	return docOut{}
}

func TestSermonPreviewChangesNothing(t *testing.T) {
	h := newSermonHarness(t, "nrsv")
	var pv PreviewResult
	h.must("logos.preview", map[string]any{"folder": h.src.Root}, &pv)
	var c KindCount
	for _, k := range pv.Kinds {
		if k.Kind == KindSermon {
			c = k
		}
	}
	if c.Found != 3 || c.New != 3 || len(c.Samples) != 3 || c.Samples[0].Title == "" {
		t.Errorf("sermons = %+v", c)
	}
	if pv.Folders != 2 { // Romans 8 and Sermons from Logos
		t.Errorf("folders = %d", pv.Folders)
	}
	if !strings.Contains(strings.Join(pv.Notes, "\n"), "ESV is not installed in Glossa (1)") {
		t.Errorf("notes = %v", pv.Notes)
	}
	var unmapped int
	for _, u := range pv.Unmapped {
		if u.Kind == KindSermon {
			unmapped += u.Count
		}
	}
	if unmapped != 2 {
		t.Errorf("unmapped sermons = %d", unmapped)
	}
	if n := h.count(`SELECT COUNT(*) FROM documents`) + h.count(`SELECT COUNT(*) FROM logos_import_log`); n != 0 {
		t.Errorf("the preview wrote %d rows", n)
	}
}

func TestSermonImportRerunUndo(t *testing.T) {
	h := newSermonHarness(t, "nrsv", "esv")
	var rep ImportResult
	h.must("logos.import", map[string]any{"folder": h.src.Root, "kinds": []string{"sermon"}}, &rep)
	if c := kindOf(rep, KindSermon); c.Created != 3 || c.Failed != 0 {
		t.Fatalf("sermons = %+v; failures %v", c, rep.Failures)
	}
	if rep.Folders != 2 {
		t.Errorf("folders = %d", rep.Folders)
	}

	d := h.sermon("Nothing can separate")
	if d.Kind != "sermon" || d.PreachedOn != "2024-03-10" || d.Theme != "Romans 8" || d.FolderID == "" || d.UpdatedAt != 1710252000000 {
		t.Errorf("sermon = %+v", d)
	}
	if len(d.Passages) != 2 || d.Passages[0].Display != "Romans 8:28-30" || d.Passages[1].Display != "John 3:16" {
		t.Errorf("passages = %+v", d.Passages)
	}
	if len(d.Points) != 3 || d.Points[0].H != "God works for good" || d.Points[0].Ref != "Romans 8:28" || d.Points[1].Level != 2 {
		t.Errorf("points = %+v", d.Points)
	}
	// ESV is installed but its licence does not let Glossa keep its text: a plain quote. NRSV's is
	// copied with its credit line.
	for _, want := range []string{
		"> [Romans 8:28](#passage/Rom.8.28) (ESV)\n", // plain
		"> **1** Verse text ",
		"([NRSV](#bible/nrsv))",
		"# God works for good", "## Notes\n\nMy own note on this sermon",
	} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, d.Body)
		}
	}
	notes := strings.Join(rep.Notes, "\n")
	if !strings.Contains(notes, "the licence of ESV does not allow quoting that text here (1)") ||
		!strings.Contains(notes, "ESVCE is not installed in Glossa (1)") {
		t.Errorf("notes = %v", rep.Notes)
	}
	// In its series folder, and the plain sermon in the default one.
	var series, plain string
	var folders []struct{ ID, Name string }
	h.must("folder.list", struct{}{}, &folders)
	for _, f := range folders {
		switch f.Name {
		case "Romans 8":
			series = f.ID
		case SermonFolder:
			plain = f.ID
		}
	}
	if series == "" || plain == "" || d.FolderID != series || h.sermon("Short one").FolderID != plain {
		t.Errorf("folders = %+v", folders)
	}
	if h.count(`SELECT COUNT(*) FROM changes WHERE command = 'logos.import'`) == 0 {
		t.Error("the import is not in the change history")
	}

	// Again: nothing new.
	var again ImportResult
	h.must("logos.import", map[string]any{"folder": h.src.Root, "kinds": []string{"sermon"}}, &again)
	if c := kindOf(again, KindSermon); c.Created+c.Updated != 0 || c.Skipped != 3 || h.count(`SELECT COUNT(*) FROM documents WHERE deleted_at IS NULL`) != 3 {
		t.Errorf("second run = %+v", c)
	}

	// A sermon changed in Logos is changed here; one deleted here stays deleted.
	sd := openFixture(t, h.src.Documents, fileSermons)
	mustExec(t, sd, `UPDATE Documents SET Title = 'Nothing can separate us', ModifiedDate = '2025-01-01T00:00:00Z' WHERE ExternalId = 's1'`)
	short := h.sermon("Short one")
	h.must("document.delete", map[string]any{"id": short.ID}, nil)
	mustExec(t, sd, `UPDATE Documents SET ModifiedDate = '2025-01-02T00:00:00Z' WHERE ExternalId = 's2'`)
	var third ImportResult
	h.must("logos.import", map[string]any{"folder": h.src.Root, "kinds": []string{"sermon"}}, &third)
	if c := kindOf(third, KindSermon); c.Updated != 1 || c.Created != 0 {
		t.Errorf("third run = %+v", c)
	}
	if h.count(`SELECT COUNT(*) FROM documents WHERE title = 'Nothing can separate us' AND deleted_at IS NULL`) != 1 ||
		h.count(`SELECT COUNT(*) FROM documents WHERE deleted_at IS NULL`) != 2 {
		t.Error("the changed sermon was not updated in place, or the deleted one came back")
	}

	// Undo the first import: the sermons it made go to the bin.
	var undo UndoResult
	h.must("logos.undo", map[string]any{"run_id": rep.RunID}, &undo)
	if undo.Removed != 2 || undo.Kept != 1 {
		t.Errorf("undo = %+v", undo)
	}
	if n := h.count(`SELECT COUNT(*) FROM documents WHERE deleted_at IS NULL`); n != 0 {
		t.Errorf("after undo documents = %d", n)
	}
}
