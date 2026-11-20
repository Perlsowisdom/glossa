package logosimport

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"
)

const clippingsSchema = `
CREATE TABLE ClippingsDocuments (RowId integer primary key autoincrement, DocumentId blob not null, IsDeleted bool not null default 0,
	ModifiedDate text, Title text not null);
CREATE TABLE Clippings (RowId integer primary key autoincrement, ItemId blob not null, Revision blob not null, DocumentRowId integer not null,
	ResourceId text not null, CreatedDate text not null, Title blob not null, Content blob not null, Notes blob, Tags text, Rank integer);`

// pack makes a value the way Logos packs text: a marker, a length, then the text (gzip or plain).
func pack(s string, gz bool) []byte {
	out := []byte{2, 0, 0, 0, 0}
	body := []byte(s)
	if gz {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		w.Write(body)
		w.Close()
		out[0], body = 1, buf.Bytes()
	}
	binary.LittleEndian.PutUint32(out[1:], uint32(len(s)))
	return append(out, body...)
}

func TestClippings(t *testing.T) {
	root, docs, data := logosFolder(t)
	makeDB(t, data, fileCatalog, catalogSchema,
		`INSERT INTO Records VALUES ('LLS:BOOK1', NULL, 'Greek Lexicon', NULL, 'text.monograph.lexicon')`)
	makeDB(t, docs, fileClippings, clippingsSchema,
		`INSERT INTO ClippingsDocuments (DocumentId, Title) VALUES (x'0102', 'Word studies')`,
		`INSERT INTO ClippingsDocuments (DocumentId, Title, IsDeleted) VALUES (x'0304', 'Old', 1)`,
	)
	path := filepath.Join(docs, filepath.FromSlash(fileClippings))
	db, err := sql.Open("sqlite", "file:/"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	add := func(item byte, doc int, title, content, notes, tags string, gz bool) {
		_, err := db.Exec(`INSERT INTO Clippings (ItemId, Revision, DocumentRowId, ResourceId, CreatedDate, Title, Content, Notes, Tags, Rank)
			VALUES (?, ?, ?, 'LLS:BOOK1', '2020-04-29T13:27:13-07:00', ?, ?, ?, ?, ?)`,
			[]byte{item}, []byte{item, 9}, doc, pack(title, gz), pack(content, gz), pack(notes, gz), tags, int(item))
		if err != nil {
			t.Fatal(err)
		}
	}
	add(1, 1, run("agape"), `<Paragraph><Run FontBold="True" Text="love"/><Run Text=" of God"/></Paragraph>`, run("see John 3"), "word study\tlove", true)
	add(2, 1, "", run("No title here, just a clipping with some words"), "", "", false)
	add(3, 1, "", "", "", "", false)
	add(4, 2, run("x"), run("in a deleted folder"), "", "", false)

	src, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	res := readFixture(t, src)
	by := map[string]Item{}
	for _, it := range res.Items {
		by[it.Key] = it
	}
	c1 := by["clipping:01"]
	if c1.Note == nil || c1.Title != "agape" || c1.Folder != "clippings:0102" {
		t.Fatalf("c1 = %+v", c1)
	}
	want := "**Logos:** *Greek Lexicon* — [Open in Logos](https://ref.ly/logosres/LLS:BOOK1)\n\n**love** of God\n\n**My note:** see John 3"
	if c1.Note.Body != want {
		t.Errorf("body = %q", c1.Note.Body)
	}
	if got := strings.Join(c1.Note.Tags, ","); got != "from-logos,love,word study" {
		t.Errorf("tags = %q", got)
	}
	if c1.Changed != "0109" || c1.Note.Created == 0 {
		t.Errorf("changed/created = %q %d", c1.Changed, c1.Note.Created)
	}
	if c2 := by["clipping:02"]; c2.Title != "No title here, just a clipping with some words" {
		t.Errorf("c2 title = %q", c2.Title)
	}
	reasons := map[string]string{}
	for _, u := range res.Unmapped {
		reasons[u.Key] = u.Reason
	}
	if reasons["clipping:03"] != "An empty clipping" || !strings.Contains(reasons["clipping:04"], "deleted") {
		t.Errorf("reasons = %v", reasons)
	}
	if len(res.Folders) != 1 || res.Folders[0].Name != "Word studies" {
		t.Errorf("folders = %+v", res.Folders)
	}
}
