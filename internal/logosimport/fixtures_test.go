package logosimport

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// A fake Logos folder is built by the tests: no real Logos file or note is ever kept in the repo.

const notesSchema = `
CREATE TABLE Notes (NoteId integer primary key autoincrement, ExternalId text not null, CreatedDate text not null,
	ModifiedDate text, IsDeleted bool not null default 0, IsTrashed bool not null default 0, Kind int not null,
	ContentRichText text, FoldedContent text, NoteColorId int, NoteStyleId int, AnchorsJson text, TagsJson text,
	NotebookExternalId text not null, Role int not null default 1);
CREATE TABLE Notebooks (NotebookId integer primary key autoincrement, ExternalId text not null, Title text,
	IsDeleted bool not null default 0, IsTrashed bool not null default 0, Role int not null default 1);
CREATE TABLE NoteStyles (NoteStyleId integer primary key autoincrement, Name text not null);
CREATE TABLE NoteColors (NoteColorId integer primary key autoincrement, Name text not null);
CREATE TABLE NoteAnchorFacetReferences (NoteAnchorFacetReferenceId integer primary key autoincrement, NoteId integer not null,
	AnchorIndex int not null, DataTypeId int not null, BibleBook int, SortKey blob, Reference text not null);
`

const catalogSchema = `CREATE TABLE Records (ResourceId text not null, ResourceIdAsAlternate text, Title text not null,
	UserTitle text, Type text not null);`

// logosFolder makes <root>/{Data,Documents}/<user> and returns the root.
func logosFolder(t *testing.T) (root string, docs, data string) {
	t.Helper()
	root = t.TempDir()
	docs = filepath.Join(root, "Documents", "abcd1234.xyz")
	data = filepath.Join(root, "Data", "abcd1234.xyz")
	for _, d := range []string{docs, data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, docs, data
}

// makeDB creates a SQLite file at dir/rel with the schema, runs the statements, and closes it.
func makeDB(t *testing.T, dir, rel, schema string, stmts ...string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:/"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func run(s string) string { return `<Paragraph><Run Text="` + s + `"/></Paragraph>` }

// notesFixture builds notestool.db and catalog.db with one of each case.
func notesFixture(t *testing.T) Source {
	root, docs, data := logosFolder(t)
	makeDB(t, data, fileCatalog, catalogSchema,
		`INSERT INTO Records VALUES ('LLS:ESV', NULL, 'English Standard Version', NULL, 'text.monograph.bible')`,
		`INSERT INTO Records VALUES ('LLS:COMM', NULL, 'A Commentary', NULL, 'text.monograph.commentary.bible')`,
	)
	makeDB(t, docs, fileNotes, notesSchema,
		`INSERT INTO NoteStyles VALUES (1, 'custom:YellowHighlighter'), (2, 'custom:RedUnderline'), (3, 'custom:Box'), (4, 'none')`,
		`INSERT INTO NoteColors VALUES (1, 'lightGreen')`,
		`INSERT INTO Notebooks (ExternalId, Title, Role) VALUES ('nb1', 'Romans study', 1), ('nb2', 'Shared', 3)`,
		`INSERT INTO Notebooks (ExternalId, Title, IsTrashed) VALUES ('nb3', 'Old', 1)`,
		// 1: a note on a verse, with a tag, bold text and a web link.
		`INSERT INTO Notes (ExternalId, CreatedDate, ModifiedDate, Kind, ContentRichText, AnchorsJson, TagsJson, NotebookExternalId, NoteStyleId)
		 VALUES ('n1', '2020-01-22T10:57:51Z', '2020-01-22T10:58:11Z', 0,
		 '<Paragraph><Run FontBold="True" Text="Grace"/><Run Text=" alone, see "/><UriLink Uri="https://example.org/x"><Run Text="here"/></UriLink></Paragraph>',
		 '[{"reference":{"raw":"bible+esv.66.8.28-66.8.30"}}]', '[{"plain":{"text":"Grace"}}]', 'nb1', 4)`,
		// 2: a note on a place in a commentary.
		`INSERT INTO Notes (ExternalId, CreatedDate, Kind, ContentRichText, AnchorsJson, NotebookExternalId)
		 VALUES ('n2', '2021-03-01T00:00:00Z', 0, '`+run("Check this later")+`',
		 '[{"textRange":{"resourceId":"LLS:COMM","version":"1","offset":10,"length":5}}]', 'nb1')`,
		// 3: a note with no anchor, in the shared notebook.
		`INSERT INTO Notes (ExternalId, CreatedDate, Kind, ContentRichText, NotebookExternalId, Role)
		 VALUES ('n3', '2021-03-02T00:00:00Z', 0, '`+run("Someone else wrote this")+`', 'nb2', 3)`,
		// 4: a free note, no anchor, own notebook.
		`INSERT INTO Notes (ExternalId, CreatedDate, Kind, ContentRichText, NotebookExternalId)
		 VALUES ('n4', '2021-03-03T00:00:00Z', 0, '`+run("A free thought")+`', 'nb1')`,
		// 5: empty, 6: in trashed notebook, 7: deleted.
		`INSERT INTO Notes (ExternalId, CreatedDate, Kind, ContentRichText, NotebookExternalId) VALUES ('n5', '2021-03-04T00:00:00Z', 0, '', 'nb1')`,
		`INSERT INTO Notes (ExternalId, CreatedDate, Kind, ContentRichText, NotebookExternalId) VALUES ('n6', '2021-03-04T00:00:00Z', 0, '`+run("in the trash")+`', 'nb3')`,
		`INSERT INTO Notes (ExternalId, CreatedDate, Kind, ContentRichText, NotebookExternalId, IsDeleted) VALUES ('n7', '2021-03-04T00:00:00Z', 0, '`+run("gone")+`', 'nb1', 1)`,
		// 8: a note that is on two verses.
		`INSERT INTO Notes (ExternalId, CreatedDate, Kind, ContentRichText, AnchorsJson, NotebookExternalId)
		 VALUES ('n8', '2022-01-01T00:00:00Z', 0, '`+run("Two places")+`',
		 '[{"reference":{"raw":"bible.64.3.16"}},{"reference":{"raw":"bible.66.8"}}]', 'nb1')`,
	)
	src, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func readFixture(t *testing.T, src Source) *Result {
	t.Helper()
	snap, err := src.Snapshot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	res, err := snap.Read()
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// makeFacets adds the references Logos worked out for a note's first anchor.
func makeFacets(t *testing.T, docs string, noteID int, refs ...string) {
	t.Helper()
	path := filepath.Join(docs, filepath.FromSlash(fileNotes))
	db, err := sql.Open("sqlite", "file:/"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, r := range refs {
		if _, err := db.Exec(`INSERT INTO NoteAnchorFacetReferences (NoteId, AnchorIndex, DataTypeId, Reference) VALUES (?, 0, 1, ?)`, noteID, r); err != nil {
			t.Fatal(err)
		}
	}
}
