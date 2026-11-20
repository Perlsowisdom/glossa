package logosimport

import (
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

func openFixture(t *testing.T, dir, rel string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:/"+filepath.ToSlash(filepath.Join(dir, filepath.FromSlash(rel))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestPassageAndWordLists(t *testing.T) {
	root, docs, data := logosFolder(t)
	makeDB(t, data, fileCatalog, catalogSchema)
	makeDB(t, docs, filePassages, `CREATE TABLE PassageLists (Id integer primary key autoincrement, DocumentGuid blob not null, Title text,
		CompressedItems blob not null, ModifiedDate text, IsDeleted bool not null default 0);`)
	pdb := openFixture(t, docs, filePassages)
	mustExec(t, pdb, `INSERT INTO PassageLists (DocumentGuid, Title, CompressedItems, ModifiedDate) VALUES (?, 'Memory verses', ?, '2024-07-21T16:43:08-04:00')`,
		[]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		pack(`[{"ref":"bible+nrsv.64.3.16"},{"ref":"bible.66.8.28-66.8.30"},{"ref":"gregnaz.1.29.8"}]`, true))
	mustExec(t, pdb, `INSERT INTO PassageLists (DocumentGuid, Title, CompressedItems, IsDeleted) VALUES (x'00', 'Gone', ?, 1)`, pack(`[]`, false))

	makeDB(t, docs, fileWords, `
		CREATE TABLE WordListDocuments (Id integer primary key autoincrement, DocumentId text not null, Title text not null, CreatedDate text not null,
			ModifiedDate text not null, IsDeleted bool not null);
		CREATE TABLE WordListEntries (Id integer primary key autoincrement, DocumentId blob not null, LemmaText text, LemmaReference text,
			DisplayText text not null, Gloss text not null, Section text not null);`)
	wdb := openFixture(t, docs, fileWords)
	// 2b958543-5bd9-4eca-a20f-5813a2df6641 as a .NET GUID: the first three groups reversed.
	guid, _ := hex.DecodeString("4385952BD95BCA4EA20F5813A2DF6641")
	mustExec(t, wdb, `INSERT INTO WordListDocuments (DocumentId, Title, CreatedDate, ModifiedDate, IsDeleted)
		VALUES ('Document:WordList:2b9585435bd94ecaa20f5813a2df6641', 'Greek verbs', '2022-01-01T00:00:00Z', '2022-02-01T00:00:00Z', 0)`)
	mustExec(t, wdb, `INSERT INTO WordListEntries (DocumentId, LemmaText, LemmaReference, DisplayText, Gloss, Section)
		VALUES (?, NULL, 'lemma.lbs.el.ἀποθνῄσκω', 'ἀπέθανον', 'to die', '1')`, guid)
	mustExec(t, wdb, `INSERT INTO WordListEntries (DocumentId, LemmaText, LemmaReference, DisplayText, Gloss, Section)
		VALUES (?, '', NULL, 'love', '', '1')`, guid)
	mustExec(t, wdb, `INSERT INTO WordListEntries (DocumentId, LemmaText, LemmaReference, DisplayText, Gloss, Section)
		VALUES (x'00000000000000000000000000000000', '', NULL, 'elsewhere', '', '1')`)

	src, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	res := readFixture(t, src)
	by := map[Kind]Item{}
	for _, it := range res.Items {
		by[it.Kind] = it
	}
	pl := by[KindPassageList]
	if pl.List == nil || pl.List.Kind != "verses" || pl.List.Name != "Memory verses" || len(pl.List.Entries) != 2 {
		t.Fatalf("passage list = %+v", pl)
	}
	if e := pl.List.Entries[1].Verses; e.Start != 45008028 || e.End != 45008030 {
		t.Errorf("second entry = %+v", e)
	}
	if res.Notes["passages in lists that are not Bible passages were left out"] != 1 {
		t.Errorf("notes = %v", res.Notes)
	}
	wl := by[KindWordList]
	if wl.List == nil || wl.List.Kind != "words" || len(wl.List.Entries) != 2 {
		t.Fatalf("word list = %+v", wl)
	}
	if e := wl.List.Entries[0]; e.Word != "ἀπέθανον" || e.Lemma != "ἀποθνῄσκω" || e.Language != "grc" || e.Note != "to die" {
		t.Errorf("first word = %+v", e)
	}
	if wl.Key != "word_list:2b9585435bd94ecaa20f5813a2df6641" {
		t.Errorf("key = %q", wl.Key)
	}
}

func TestPrayerLists(t *testing.T) {
	root, docs, data := logosFolder(t)
	makeDB(t, data, fileCatalog, catalogSchema)
	makeDB(t, docs, filePrayers, `
		CREATE TABLE PrayerListDocuments (Id integer primary key autoincrement, DocumentSyncId blob not null, Title text, ModifiedDate text, IsDeleted bool not null);
		CREATE TABLE Prayers (Id integer primary key autoincrement, PrayerId blob not null, DocumentSyncId blob not null, Prayer text not null);
		CREATE TABLE PrayerListStatus (Id integer primary key autoincrement, PrayerId blob not null, LastPrayed text, IsDeleted bool not null);`)
	db := openFixture(t, docs, filePrayers)
	doc := []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	p1 := []byte{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
	mustExec(t, db, `INSERT INTO PrayerListDocuments (DocumentSyncId, Title, ModifiedDate, IsDeleted) VALUES (?, 'Family', '2024-05-06', 0)`, doc)
	mustExec(t, db, `INSERT INTO PrayerListDocuments (DocumentSyncId, Title, ModifiedDate, IsDeleted) VALUES (x'03030303030303030303030303030303', 'Nothing here', '2024-05-06', 0)`)
	mustExec(t, db, `INSERT INTO Prayers (PrayerId, DocumentSyncId, Prayer) VALUES (?, ?, ?)`, p1, doc,
		`{"title":"For Sam","notes":"Healing","answer":"Better now","tags":["Health"],"schedule":{"isEnabled":true,"recurrence":"Weekly"}}`)
	mustExec(t, db, `INSERT INTO PrayerListStatus (PrayerId, LastPrayed, IsDeleted) VALUES (?, '2024-05-06', 0)`, p1)

	src, _ := Find(root)
	res := readFixture(t, src)
	if len(res.Items) != 1 {
		t.Fatalf("items = %+v", res.Items)
	}
	it := res.Items[0]
	want := "**For Sam**\nHealing\n*Answer:* Better now\n*Last prayed:* 2024-05-06\n*Repeats:* weekly"
	if it.Note == nil || it.Note.Body != want || it.Folder != "prayer-lists" || it.Note.Title != "Family" {
		t.Errorf("note = %+v", it.Note)
	}
	if strings.Join(it.Note.Tags, ",") != "from-logos,health" {
		t.Errorf("tags = %v", it.Note.Tags)
	}
	if len(res.Folders) != 1 || res.Folders[0].Name != "Prayer lists" {
		t.Errorf("folders = %+v", res.Folders)
	}
	if len(res.Unmapped) != 1 || res.Unmapped[0].Reason != "An empty prayer list" {
		t.Errorf("unmapped = %+v", res.Unmapped)
	}
}

const plansSchema = `
	CREATE TABLE ReadingPlanDocuments (Id integer primary key autoincrement, DocumentId text not null, Title text, Sessions text,
		ModifiedDate text, ScheduleId blob, IsDeleted int not null);
	CREATE TABLE ReadingPlanStatus (Id integer primary key autoincrement, SyncId text not null, ReadSessions text, IsDeleted int not null);`

func TestReadingPlans(t *testing.T) {
	root, docs, data := logosFolder(t)
	makeDB(t, data, fileCatalog, catalogSchema,
		`INSERT INTO Records VALUES ('LLS:EXPO', NULL, 'An Exposition', NULL, 'text.monograph')`)
	makeDB(t, docs, filePlans, plansSchema)
	db := openFixture(t, docs, filePlans)
	// A schedule GUID whose status id is the .NET-ordered hex.
	sched := []byte{0xF1, 0x73, 0xA8, 0x24, 0x3F, 0xE9, 0xD2, 0x4A, 0xB3, 0x37, 0x93, 0x49, 0x06, 0xD5, 0x17, 0x4A}
	bible := `[{"date":"2024-02-14","readings":[[{"resourceId":"LLS:1","reference":"bible+nrsv.64.1"}]]},
		{"date":"2024-02-15","readings":[[{"resourceId":"LLS:1","reference":"bible+nrsv.64.2"},{"resourceId":"LLS:1","reference":"bible+nrsv.64.3.1-64.3.5"}]]},
		{"date":"2024-02-16","readings":[[{"resourceId":"LLS:1","reference":"bible+nrsv.64.4"}]]}]`
	book := `[{"date":"2024-03-01","readings":[[{"resourceId":"LLS:EXPO","reference":"page.1-30"}]]},
		{"date":"2024-03-02","readings":[[{"resourceId":"LLS:1","reference":"bible+nrsv.64.1"}]]}]`
	mustExec(t, db, `INSERT INTO ReadingPlanDocuments (DocumentId, Title, Sessions, ModifiedDate, ScheduleId, IsDeleted)
		VALUES ('Document:ReadingPlan:aaaa', 'John in a week', ?, '2024-02-16T11:22:49-05:00', ?, 0)`, bible, sched)
	mustExec(t, db, `INSERT INTO ReadingPlanDocuments (DocumentId, Title, Sessions, ModifiedDate, ScheduleId, IsDeleted)
		VALUES ('Document:ReadingPlan:bbbb', 'Exposition', ?, '2024-03-02T00:00:00Z', x'00', 0)`, book)
	mustExec(t, db, `INSERT INTO ReadingPlanStatus (SyncId, ReadSessions, IsDeleted) VALUES ('24a873f1e93f4ad2b337934906d5174a', '["2024-02-14","2024-02-16"]', 0)`)

	src, _ := Find(root)
	res := readFixture(t, src)
	by := map[string]Item{}
	for _, it := range res.Items {
		by[it.Title] = it
	}
	p := by["John in a week"].Plan
	if p == nil || p.Start != "2024-02-14" || len(p.Days) != 3 {
		t.Fatalf("plan = %+v", by["John in a week"])
	}
	if got := strings.Join(p.Days[1], " + "); got != "John 2 + John 3:1-5" {
		t.Errorf("day 2 = %q", got)
	}
	if len(p.Done) != 2 || p.Done[0] != 1 || p.Done[1] != 3 {
		t.Errorf("done = %v", p.Done)
	}
	n := by["Exposition"]
	if n.Plan != nil || n.Note == nil || n.Folder != "reading-plans" {
		t.Fatalf("book plan = %+v", n)
	}
	if !strings.Contains(n.Note.Body, "- 2024-03-01: An Exposition, pages 1–30") || !strings.Contains(n.Note.Body, "- 2024-03-02: John 1") {
		t.Errorf("body = %q", n.Note.Body)
	}
}
