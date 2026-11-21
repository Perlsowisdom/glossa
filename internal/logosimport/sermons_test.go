package logosimport

import (
	"fmt"
	"strings"
	"testing"
)

// The same table shapes as Sermon.db, with the columns the importer reads.
const sermonsSchema = `
CREATE TABLE Documents (Id integer primary key autoincrement, ExternalId text, IsDeleted bool not null default 0,
	ModifiedDate text not null, Title text, AuthorName text, ChurchTitle text, Series text, Date text, TagsJson text,
	Notes text, Description text, SlideGroupJson text, IsTemplate bool not null default 0);
CREATE TABLE Blocks (Id integer primary key autoincrement, DocumentId integer not null references Documents(Id),
	BlockId text not null, IsDeleted bool not null default 0, Kind text not null, Indent int not null default 0,
	Content text, PassageJson text, ClippingJson text, Rank integer not null);
`

func rich(parts ...string) string {
	return "<Paragraph>" + strings.Join(parts, "") + "</Paragraph>"
}

func runBold(s string) string { return `<Run FontBold="True" Text="` + s + `"/>` }
func runText(s string) string { return `<Run Text="` + s + `"/>` }

func sqlq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// sermonsFixture builds a Sermon.db with one of each case in a fresh fake Logos folder.
func sermonsFixture(t *testing.T) Source {
	t.Helper()
	root, docs, data := logosFolder(t)
	makeDB(t, data, fileCatalog, catalogSchema,
		`INSERT INTO Records VALUES ('LLS:ESV', NULL, 'English Standard Version', NULL, 'text.monograph.bible')`)
	var manyTags []string
	for i := 1; i <= 22; i++ {
		manyTags = append(manyTags, fmt.Sprintf(`{"raw":"bible.19.%d.1","text":"Psalm %d:1"}`, i, i))
	}
	tags1 := `{"referenceTags":[{"raw":"bible+esv.66.8.28-66.8.30","text":"Romans 8:28-30"},{"raw":"bible.64.3.16"},{"raw":"bible.66.8.28-66.8.30"}],` +
		`"topicTags":[{"raw":"x","text":"Grace"}],"miscellaneousTags":["m"]}`
	stmts := []string{
		// 1: a full sermon in a series.
		`INSERT INTO Documents (ExternalId, ModifiedDate, Title, AuthorName, Series, Date, TagsJson, Notes, Description, SlideGroupJson)
		 VALUES ('s1', '2024-03-12T10:00:00-04:00', 'Nothing can separate', 'Pastor Sam', 'Romans 8', '2024-03-10T00:00:00', ` +
			sqlq(tags1) + `, ` + sqlq(rich(runText("My own note on this sermon"))) + `, ` + sqlq(rich(runText("A sermon about love"))) + `, '[{"x":1}]')`,
		blockSQL(1, 1, "heading1", 0, rich(runText("God works for good")), ""),
		blockSQL(1, 2, "normal", 0, rich(runText("Plain "), runBold("bold"), runText(" text")), ""),
		blockSQL(1, 3, "passage", 0, "", `{"reference":{"raw":"bible+esv.66.8.28","text":"Romans 8:28"},"resource":{"id":"LLS:ESV","title":"English Standard Version","abbreviatedTitle":"ESV"}}`),
		blockSQL(1, 4, "heading2", 0, rich(runText("Called")), ""),
		blockSQL(1, 5, "bullet", 0, rich(runText("first")), ""),
		blockSQL(1, 6, "bullet", 1, rich(runText("nested")), ""),
		blockSQL(1, 7, "number", 0, rich(runText("one")), ""),
		blockSQL(1, 8, "number", 0, rich(runText("two")), ""),
		blockSQL(1, 9, "blockquote", 0, rich(runText("A quoted line")), ""),
		blockSQL(1, 10, "illustration", 0, rich(runText("A story")), ""),
		blockSQL(1, 11, "prompt", 0, rich(runText("What do you think?")), ""),
		blockSQL(1, 12, "clipping", 0, rich(runText("Clipped words")), `{"title":"Agape","resource":{"title":"A Lexicon"}}`),
		blockSQL(1, 13, "passage", 0, "", `{"reference":{"raw":"bible+nrsv.61.1.1","text":"Matthew 1:1"},"resource":{"id":"LLS:NRSV","title":"New Revised Standard Version","abbreviatedTitle":"NRSV"}}`),
		blockSQL(1, 14, "heading3", 0, rich(runText("Deep point")), ""),
		blockSQL(1, 15, "normal", 0, rich(runText("A deleted one")), ""),
		`UPDATE Blocks SET IsDeleted = 1 WHERE DocumentId = 1 AND BlockId = 'b15'`,
		// 2: no series, no tags: passages come from the quotes; a quote of a Bible Glossa lacks.
		`INSERT INTO Documents (ExternalId, ModifiedDate, Title) VALUES ('s2', '2024-04-01T00:00:00Z', 'Short one')`,
		blockSQL(2, 1, "passage", 0, "", `{"reference":{"raw":"bible+esvce.64.3.16","text":"John 3:16"},"resource":{"abbreviatedTitle":"ESVCE"}}`),
		blockSQL(2, 2, "normal", 0, "A plain text body", ""),
		// 3: a template, 4: deleted, 5: empty, 6: many passages and a tag that is not a Bible passage.
		`INSERT INTO Documents (ExternalId, ModifiedDate, Title, IsTemplate) VALUES ('s3', '2024-04-02T00:00:00Z', 'Template', 1)`,
		`INSERT INTO Documents (ExternalId, ModifiedDate, Title, IsDeleted) VALUES ('s4', '2024-04-02T00:00:00Z', 'Gone', 1)`,
		`INSERT INTO Documents (ExternalId, ModifiedDate, Title) VALUES ('s5', '2024-04-03T00:00:00Z', '')`,
		`INSERT INTO Documents (ExternalId, ModifiedDate, Title, TagsJson) VALUES ('s6', '2024-04-04T00:00:00Z', 'Psalms', ` +
			sqlq(`{"referenceTags":[`+strings.Join(manyTags, ",")+`,{"raw":"mystery.1.2"}]}`) + `)`,
	}
	makeDB(t, docs, fileSermons, sermonsSchema, stmts...)
	src, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func blockSQL(doc, n int, kind string, indent int, content, passage string) string {
	clip, pass := "NULL", "NULL"
	if kind == "clipping" {
		clip = sqlq(passage)
	} else if passage != "" {
		pass = sqlq(passage)
	}
	return fmt.Sprintf(`INSERT INTO Blocks (DocumentId, BlockId, Kind, Indent, Content, PassageJson, ClippingJson, Rank)
		VALUES (%d, 'b%d', '%s', %d, %s, %s, %s, %d)`, doc, n, kind, indent, sqlq(content), pass, clip, n)
}

func sermonItem(t *testing.T, res *Result, key string) Item {
	t.Helper()
	for _, it := range res.Items {
		if it.Kind == KindSermon && it.Key == key {
			return it
		}
	}
	t.Fatalf("no sermon %s in %+v", key, res.Items)
	return Item{}
}

func TestReadSermonsMapping(t *testing.T) {
	res := readFixture(t, sermonsFixture(t))
	var n int
	for _, it := range res.Items {
		if it.Kind == KindSermon {
			n++
		}
	}
	if n != 3 { // s1, s2, s6; a template, a deleted and an empty one are not sermons
		t.Errorf("sermons = %d", n)
	}

	s1 := sermonItem(t, res, "sermon:s1")
	d := s1.Sermon
	if d.Title != "Nothing can separate" || d.PreachedOn != "2024-03-10" || d.Theme != "Romans 8" {
		t.Errorf("sermon 1 = %+v", d)
	}
	if s1.Folder != "sermon-series:Romans 8" || s1.Changed != "2024-03-12T10:00:00-04:00" || d.Updated != 1710252000000 {
		t.Errorf("folder %q changed %q updated %d", s1.Folder, s1.Changed, d.Updated)
	}
	// The sermon's passage tags, each once.
	if strings.Join(d.Passages, "|") != "Romans 8:28-30|John 3:16" {
		t.Errorf("passages = %v", d.Passages)
	}
	wantPoints := []SermonPoint{{"God works for good", "Romans 8:28", 1}, {"Called", "Matthew 1:1", 2}, {"Deep point", "", 2}}
	if fmt.Sprint(d.Points) != fmt.Sprint(wantPoints) {
		t.Errorf("points = %v, want %v", d.Points, wantPoints)
	}
	body := d.Body(func(q SermonQuote, _ string) string { return PlainQuote(q) })
	for _, want := range []string{
		"> A sermon about love\n\n# God works for good\n\nPlain **bold** text\n\n> [Romans 8:28](#passage/Rom.8.28) (ESV)\n\n## Called\n\n- first\n  - nested\n\n1. one\n2. two\n\n> A quoted line",
		"**Illustration:** A story", "**Question:** What do you think?",
		"> Clipped words\n> — Agape, A Lexicon", "> [Matthew 1:1](#passage/Matt.1.1) (NRSV)", "### Deep point",
		"## Notes\n\nMy own note on this sermon",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "deleted one") {
		t.Error("a deleted block was imported")
	}

	s2 := sermonItem(t, res, "sermon:s2")
	if s2.Folder != "sermons" || strings.Join(s2.Sermon.Passages, "|") != "John 3:16" || s2.Sermon.Theme != "" || s2.Sermon.PreachedOn != "" {
		t.Errorf("sermon 2 = %+v folder %q", s2.Sermon, s2.Folder)
	}
	if q := s2.Sermon.Blocks[0].Quote; q == nil || q.Abbr != "ESVCE" || q.Tag != "esvce" {
		t.Errorf("quote = %+v", q)
	}
	s6 := sermonItem(t, res, "sermon:s6")
	if len(s6.Sermon.Passages) != 20 {
		t.Errorf("passages = %d, want the first 20", len(s6.Sermon.Passages))
	}

	folders := map[string]string{}
	for _, f := range res.Folders {
		folders[f.Key] = f.Name
	}
	if folders["sermons"] != SermonFolder || folders["sermon-series:Romans 8"] != "Romans 8" {
		t.Errorf("folders = %v", folders)
	}
	reasons := map[string]string{}
	for _, u := range res.Unmapped {
		if u.Kind == KindSermon {
			reasons[u.Key] = u.Reason
		}
	}
	if len(reasons) != 2 || !strings.Contains(reasons["sermon:s3"], "template") || !strings.Contains(reasons["sermon:s5"], "empty") {
		t.Errorf("unmapped = %v", reasons)
	}
	for _, want := range []string{
		"sermons with an author or church name, which Glossa sermons do not keep",
		"sermons with slide settings, which Glossa sermons do not keep",
		"sermons with topic or other tags that are not the theme, which are not kept",
		"sermons with passage tags that are not Bible passages Glossa has, left out",
		"sermons with more than 20 passages, which is all a sermon in Glossa can hold; the rest are left out",
	} {
		if res.Notes[want] != 1 {
			t.Errorf("note %q = %d; notes %v", want, res.Notes[want], res.Notes)
		}
	}
}
