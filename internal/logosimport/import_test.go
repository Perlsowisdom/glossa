package logosimport

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/folder"
	"github.com/Perlsowisdom/glossa/internal/plan"
	"github.com/Perlsowisdom/glossa/internal/reader"
	"github.com/Perlsowisdom/glossa/internal/store"
)

type harness struct {
	t   *testing.T
	r   *command.Registry
	env *command.Env
	st  *store.Store
	svc *service
	src Source
}

// newHarness builds a fake Logos folder with a bit of everything and an empty Glossa library.
func newHarness(t *testing.T) *harness {
	t.Helper()
	src := notesFixture(t)
	// Highlights, a clipping, a passage list and a plan, added to the same fixture folder.
	nd := openFixture(t, src.Documents, fileNotes)
	mustExec(t, nd, `INSERT INTO Notes (ExternalId, CreatedDate, ModifiedDate, Kind, AnchorsJson, NotebookExternalId, NoteStyleId)
		VALUES ('h1', '2019-12-17T01:21:25Z', '2019-12-17T01:21:25Z', 1, '[{"reference":{"raw":"bible+esv.64.3.16"}}]', 'nb1', 1)`)
	mustExec(t, nd, `INSERT INTO Notes (ExternalId, CreatedDate, Kind, AnchorsJson, NotebookExternalId, NoteStyleId)
		VALUES ('h2', '2019-12-17T01:21:25Z', 1, '[{"textRange":{"resourceId":"LLS:COMM","version":"1","offset":1,"length":2}}]', 'nb1', 1)`)

	makeDB(t, src.Documents, fileClippings, clippingsSchema,
		`INSERT INTO ClippingsDocuments (DocumentId, Title) VALUES (x'0102', 'Word studies')`)
	cd := openFixture(t, src.Documents, fileClippings)
	mustExec(t, cd, `INSERT INTO Clippings (ItemId, Revision, DocumentRowId, ResourceId, CreatedDate, Title, Content, Notes, Tags, Rank)
		VALUES (x'01', x'0101', 1, 'LLS:COMM', '2020-04-29T13:27:13-07:00', ?, ?, ?, '', 1)`, pack(run("agape"), false), pack(run("love of God"), true), pack("", false))

	makeDB(t, src.Documents, filePassages, `CREATE TABLE PassageLists (Id integer primary key autoincrement, DocumentGuid blob not null, Title text,
		CompressedItems blob not null, ModifiedDate text, IsDeleted bool not null default 0);`)
	pd := openFixture(t, src.Documents, filePassages)
	mustExec(t, pd, `INSERT INTO PassageLists (DocumentGuid, Title, CompressedItems, ModifiedDate) VALUES (x'01020304050607080910111213141516', 'Memory', ?, '2024-07-21T16:43:08-04:00')`,
		pack(`[{"ref":"bible+nrsv.64.3.16"},{"ref":"bible.66.8.28-66.8.30"}]`, true))

	makeDB(t, src.Documents, filePlans, plansSchema)
	rd := openFixture(t, src.Documents, filePlans)
	mustExec(t, rd, `INSERT INTO ReadingPlanDocuments (DocumentId, Title, Sessions, ModifiedDate, ScheduleId, IsDeleted)
		VALUES ('Document:ReadingPlan:aaaa', 'John', ?, '2024-02-16T11:22:49-05:00', x'F173A8243FE9D24AB337934906D5174A', 0)`,
		`[{"date":"2024-02-14","readings":[[{"resourceId":"LLS:1","reference":"bible+nrsv.64.1"}]]},
		  {"date":"2024-02-15","readings":[[{"resourceId":"LLS:1","reference":"bible+nrsv.64.2"}]]}]`)
	mustExec(t, rd, `INSERT INTO ReadingPlanStatus (SyncId, ReadSessions, IsDeleted) VALUES ('24a873f1e93f4ad2b337934906d5174a', '["2024-02-14"]', 0)`)

	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := command.NewRegistry()
	core.Register(r)
	folder.Register(r)
	plan.Register(r)
	reader.Register(r)
	Register(r)
	return &harness{t: t, r: r, env: &command.Env{Store: st}, st: st, src: src, svc: &service{r: r, tmpDir: t.TempDir()}}
}

func (h *harness) run(id string, params, out any) error {
	h.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := h.r.Run(context.Background(), h.env, id, raw)
	if err != nil {
		return err
	}
	if out != nil {
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, out); err != nil {
			h.t.Fatal(err)
		}
	}
	return nil
}

func (h *harness) must(id string, params, out any) {
	h.t.Helper()
	if err := h.run(id, params, out); err != nil {
		h.t.Fatalf("%s: %v", id, err)
	}
}

func (h *harness) count(q string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.st.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func kindOf(rep ImportResult, k Kind) KindResult {
	for _, kr := range rep.Kinds {
		if kr.Kind == k {
			return kr
		}
	}
	return KindResult{}
}

func TestPreviewChangesNothing(t *testing.T) {
	h := newHarness(t)
	var pv PreviewResult
	h.must("logos.preview", map[string]any{"folder": h.src.Root}, &pv)
	byKind := map[Kind]KindCount{}
	for _, k := range pv.Kinds {
		byKind[k.Kind] = k
	}
	if byKind[KindNote].Found != 4 || byKind[KindNote].New != 4 || len(byKind[KindNote].Samples) == 0 {
		t.Errorf("notes = %+v", byKind[KindNote])
	}
	if byKind[KindHighlight].Found != 1 || byKind[KindClipping].Found != 1 || byKind[KindPassageList].Found != 1 || byKind[KindReadingPlan].Found != 1 {
		t.Errorf("kinds = %+v", pv.Kinds)
	}
	if pv.Folders != 2 { // Romans study and Word studies
		t.Errorf("folders = %d", pv.Folders)
	}
	if len(pv.Unmapped) == 0 || pv.Unmapped[0].Reason == "" {
		t.Errorf("unmapped = %+v", pv.Unmapped)
	}
	if n := h.count(`SELECT COUNT(*) FROM notes`) + h.count(`SELECT COUNT(*) FROM logos_import_log`); n != 0 {
		t.Errorf("the preview wrote %d rows", n)
	}
}

func TestImportRerunUndo(t *testing.T) {
	h := newHarness(t)
	var rep ImportResult
	h.must("logos.import", map[string]any{"folder": h.src.Root}, &rep)
	if c := kindOf(rep, KindNote); c.Created != 4 || c.Failed != 0 {
		t.Fatalf("notes = %+v; failures %v", c, rep.Failures)
	}
	for _, k := range []Kind{KindHighlight, KindClipping, KindPassageList, KindReadingPlan} {
		if c := kindOf(rep, k); c.Created != 1 || c.Failed != 0 {
			t.Errorf("%s = %+v; failures %v", k, c, rep.Failures)
		}
	}
	if n := h.count(`SELECT COUNT(*) FROM notes WHERE deleted_at IS NULL`); n != 5 {
		t.Errorf("notes = %d, want 4 + 1 clipping", n)
	}
	// The verse note keeps its dates, verses and tag; the book note has the link and tag.
	var created, updated int64
	var body string
	var vs int
	err := h.st.QueryRow(context.Background(), `SELECT n.created_at, n.updated_at, n.body_md, a.verse_start FROM notes n JOIN anchors a ON a.id = n.anchor_id
		WHERE n.body_md LIKE '%Grace%'`).Scan(&created, &updated, &body, &vs)
	if err != nil || created != 1579690671000 || updated != 1579690691000 || vs != 45008028 {
		t.Errorf("verse note: %d %d %d, %v", created, updated, vs, err)
	}
	if h.count(`SELECT COUNT(*) FROM note_tags WHERE tag = 'from-logos'`) != 2 { // the book note and the clipping
		t.Errorf("from-logos tags = %d", h.count(`SELECT COUNT(*) FROM note_tags WHERE tag = 'from-logos'`))
	}
	if h.count(`SELECT COUNT(*) FROM logos_import_log WHERE kind = 'highlight'`) != 1 ||
		h.count(`SELECT COUNT(*) FROM logos_import_log WHERE detail_json LIKE '%YellowHighlighter%'`) != 1 {
		t.Error("the highlight's original style is not in the log")
	}
	if n := h.count(`SELECT COUNT(*) FROM list_entries`); n != 2 {
		t.Errorf("list entries = %d", n)
	}
	var hls []struct {
		Style string `json:"style"`
		Ref   string `json:"ref"`
	}
	h.must("highlight.list", map[string]any{"ref": "John 3:16"}, &hls)
	if len(hls) != 1 || hls[0].Style != "yellow" {
		t.Errorf("highlights on John 3:16 = %+v", hls)
	}
	var plans []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Days     int    `json:"days"`
		DaysDone int    `json:"days_done"`
	}
	h.must("plan.list", struct{}{}, &plans)
	if len(plans) != 1 || plans[0].Name != "John" || plans[0].Days != 2 || plans[0].DaysDone != 1 {
		t.Errorf("plans = %+v", plans)
	}
	if h.count(`SELECT COUNT(*) FROM changes WHERE command = 'logos.import'`) == 0 {
		t.Error("the import is not in the change history")
	}

	// Running it again adds nothing.
	var again ImportResult
	h.must("logos.import", map[string]any{"folder": h.src.Root}, &again)
	for _, kr := range again.Kinds {
		if kr.Created+kr.Updated+kr.Failed != 0 {
			t.Errorf("second run %s = %+v", kr.Kind, kr)
		}
	}
	if again.Folders != 0 || h.count(`SELECT COUNT(*) FROM notes WHERE deleted_at IS NULL`) != 5 {
		t.Errorf("second run made %d folders", again.Folders)
	}
	// The preview now calls them all imported.
	var pv PreviewResult
	h.must("logos.preview", map[string]any{"folder": h.src.Root}, &pv)
	for _, k := range pv.Kinds {
		if k.New != 0 || k.Changed != 0 {
			t.Errorf("preview after import: %+v", k)
		}
	}

	// A note changed in Logos is changed here, not copied; a note deleted here stays deleted.
	nd := openFixture(t, h.src.Documents, fileNotes)
	mustExec(t, nd, `UPDATE Notes SET ContentRichText = '`+run("Grace, edited")+`', ModifiedDate = '2023-01-01T00:00:00Z' WHERE ExternalId = 'n1'`)
	var gone struct{ ID string }
	if err := h.st.QueryRow(context.Background(), `SELECT id FROM notes WHERE body_md LIKE '%free thought%'`).Scan(&gone.ID); err != nil {
		t.Fatal(err)
	}
	h.must("note.delete", map[string]any{"id": gone.ID}, nil)
	var third ImportResult
	h.must("logos.import", map[string]any{"folder": h.src.Root}, &third)
	if c := kindOf(third, KindNote); c.Updated != 1 || c.Created != 0 {
		t.Errorf("third run notes = %+v", c)
	}
	if h.count(`SELECT COUNT(*) FROM notes WHERE body_md = 'Grace, edited' AND deleted_at IS NULL`) != 1 ||
		h.count(`SELECT COUNT(*) FROM notes WHERE deleted_at IS NULL`) != 4 {
		t.Error("the changed note was not updated in place, or the deleted one came back")
	}

	// Undo the first import: what it added goes to the bin and the folders it made go too.
	var runs []RunInfo
	h.must("logos.runs", struct{}{}, &runs)
	if len(runs) != 2 || runs[0].Report == nil { // the second run changed nothing, so it left no run behind
		t.Fatalf("runs = %+v", runs)
	}
	var undo UndoResult
	h.must("logos.undo", map[string]any{"run_id": rep.RunID}, &undo)
	if undo.Removed != 7 || undo.Kept != 1 || undo.Folders != 2 {
		t.Errorf("undo = %+v", undo)
	}
	for table, q := range map[string]string{
		"notes": `SELECT COUNT(*) FROM notes WHERE deleted_at IS NULL`, "highlights": `SELECT COUNT(*) FROM highlights WHERE deleted_at IS NULL`,
		"lists": `SELECT COUNT(*) FROM lists WHERE deleted_at IS NULL`, "plans": `SELECT COUNT(*) FROM reading_plans WHERE deleted_at IS NULL`,
		"folders": `SELECT COUNT(*) FROM folders WHERE deleted_at IS NULL`,
	} {
		if n := h.count(q); n != 0 {
			t.Errorf("after undo %s = %d", table, n)
		}
	}
	if err := h.run("logos.undo", map[string]any{"run_id": rep.RunID}, nil); err == nil {
		t.Error("undoing twice should fail")
	}
	// And a fresh import brings it all back, since the log forgot it.
	var fourth ImportResult
	h.must("logos.import", map[string]any{"folder": h.src.Root}, &fourth)
	if c := kindOf(fourth, KindNote); c.Created != 4 {
		t.Errorf("fourth run notes = %+v", c)
	}
}

func TestImportOnlySomeKinds(t *testing.T) {
	h := newHarness(t)
	var rep ImportResult
	h.must("logos.import", map[string]any{"folder": h.src.Root, "kinds": []string{"highlight"}}, &rep)
	if h.count(`SELECT COUNT(*) FROM notes`) != 0 || h.count(`SELECT COUNT(*) FROM highlights`) != 1 {
		t.Errorf("kinds filter ignored: %+v", rep.Kinds)
	}
	if err := h.run("logos.import", map[string]any{"folder": h.src.Root, "kinds": []string{"bogus"}}, nil); err == nil {
		t.Error("an unknown kind should be refused")
	}
	if err := h.run("logos.import", map[string]any{"folder": t.TempDir()}, nil); err == nil {
		t.Error("a folder with no Logos data should be refused")
	}
}
