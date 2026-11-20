package logosimport

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/verse"
)

func TestToMarkdown(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain text", "just words", "just words"},
		{"bold italic underline", `<Paragraph><Run FontBold="True" Text="B"/><Run Text=" and "/><Run FontItalic="True" Text="I"/><Run Text=" "/><Run HasUnderline="True" Text="U"/></Paragraph>`,
			"**B** and *I* <u>U</u>"},
		{"adjacent runs join", `<Paragraph><Run FontBold="True" Text="Ab"/><Run FontBold="True" Text="cd "/><Run Text="x"/></Paragraph>`, "**Abcd** x"},
		{"spaces stay outside the marks", `<Paragraph><Run FontBold="True" Text=" word "/></Paragraph>`, "**word**"},
		{"two paragraphs", `<Paragraph><Run Text="One"/></Paragraph><Paragraph><Run Text="Two"/></Paragraph>`, "One\n\nTwo"},
		{"web link", `<Paragraph><UriLink Uri="https://a.example/b"><Run Text="site"/></UriLink></Paragraph>`, "[site](https://a.example/b)"},
		{"other links keep only the text", `<Paragraph><UriLink Uri="logosres:x"><Run Text="there"/></UriLink><Reference Reference="bible.64.3.16"><Run Text="Jn 3:16"/></Reference></Paragraph>`, "thereJn 3:16"},
		{"bullets", `<List Kind="Disc"><ListItem><Paragraph><Run Text="a"/></Paragraph></ListItem><ListItem><Paragraph><Run Text="b"/></Paragraph></ListItem></List>`, "- a\n- b"},
		{"numbers", `<List Kind="Decimal"><ListItem><Paragraph><Run Text="a"/></Paragraph></ListItem><ListItem><Paragraph><Run Text="b"/></Paragraph></ListItem></List>`, "1. a\n2. b"},
		{"line break", `<Paragraph><Run Text="a"/><LineBreak/><Run Text="b"/></Paragraph>`, "a\nb"},
		{"entities", `<Paragraph><Run Text="a &amp; b &lt;c&gt;"/></Paragraph>`, "a & b <c>"},
		{"table", `<Table><TableRow><TableCell><Paragraph><Run Text="h1"/></Paragraph></TableCell><TableCell><Paragraph><Run Text="h2"/></Paragraph></TableCell></TableRow><TableRow><TableCell><Paragraph><Run Text="a"/></Paragraph></TableCell><TableCell><Paragraph><Run Text="b"/></Paragraph></TableCell></TableRow></Table>`,
			"<!-- table header=no -->\n| h1 | h2 |\n| --- | --- |\n| a | b |"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToMarkdown(c.in); got != strings.TrimSpace(c.want) && got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	if got := PlainText(`<Paragraph><Run FontBold="True" Text="Hi"/><Run Text=" there"/></Paragraph>`); got != "Hi there" {
		t.Errorf("PlainText = %q", got)
	}
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		ref        string
		start, end verse.ID
		bad        bool
	}{
		{"bible+esv.64.3.16", 43003016, 43003016, false},
		{"bible.66.8.28-66.8.30", 45008028, 45008030, false},
		{"bible.64.3", 43003001, 43003999, false},
		{"bible.64.3.16-64.4", 43003016, 43004999, false},
		{"bible.61.1.1", 40001001, 40001001, false},
		{"bible.1.1.1", 1001001, 1001001, false},
		{"bible.87.22", 66022001, 66022999, false},
		{"bible.40.2.1", 67002001, 67002001, false}, // Tobit
		{"bible.42.1.1", 0, 0, true},                // a book Glossa does not number
		{"gregnaz.1.29.8", 0, 0, true},
		{"bible.64.99.1", 0, 0, true},
	}
	for _, c := range cases {
		r, err := ParseRef(c.ref)
		if c.bad {
			if err == nil {
				t.Errorf("%s: want an error, got %v", c.ref, r)
			}
			continue
		}
		if err != nil || r.Start != c.start || r.End != c.end {
			t.Errorf("%s: got %v %v, want %d-%d", c.ref, r, err, c.start, c.end)
		}
	}
}

func TestMapStyle(t *testing.T) {
	cases := []struct {
		style, colour, want string
		exact               bool
	}{
		{"custom:YellowHighlighter", "", "yellow", true},
		{"custom:GreenHighlighter", "", "green", true},
		{"custom:RedUnderline", "", "pink-underline", false},
		{"custom:OrangeHighlighter", "", "yellow", false},
		{"custom:BlueUnderline", "", "blue-underline", true},
		{"custom:PencilUnderline", "", "underline", true},
		{"custom:Box", "", "yellow", false},
		{"none", "lightGreen", "green", true},
		{"none", "yellow", "yellow", true},
		{"", "", "yellow", false},
	}
	for _, c := range cases {
		got, exact := mapStyle(c.style, c.colour)
		if got != c.want || exact != c.exact {
			t.Errorf("mapStyle(%q, %q) = %q, %v; want %q, %v", c.style, c.colour, got, exact, c.want, c.exact)
		}
	}
}

func TestNotesMapping(t *testing.T) {
	src := notesFixture(t)
	res := readFixture(t, src)

	byKey := map[string]Item{}
	for _, it := range res.Items {
		byKey[it.Key] = it
	}
	// A note on verses: Markdown, tag, dates, verses, notebook folder.
	n1 := byKey["note:n1"].Note
	if n1 == nil {
		t.Fatalf("n1 missing; items: %+v", res.Items)
	}
	if n1.Body != "**Grace** alone, see [here](https://example.org/x)" {
		t.Errorf("body = %q", n1.Body)
	}
	if n1.Verses == nil || n1.Verses.Start != 45008028 || n1.Verses.End != 45008030 {
		t.Errorf("verses = %+v", n1.Verses)
	}
	if len(n1.Tags) != 1 || n1.Tags[0] != "grace" {
		t.Errorf("tags = %v", n1.Tags)
	}
	if n1.Created == 0 || n1.Updated <= n1.Created {
		t.Errorf("dates = %d %d", n1.Created, n1.Updated)
	}
	if byKey["note:n1"].Folder != "notebook:nb1" {
		t.Errorf("folder = %q", byKey["note:n1"].Folder)
	}
	// A note on a book: reference line with a link, from-logos tag, no verses.
	n2 := byKey["note:n2"].Note
	if n2 == nil || n2.Verses != nil {
		t.Fatalf("n2 = %+v", n2)
	}
	if !strings.HasPrefix(n2.Body, "**Logos:** *A Commentary* — [Open in Logos](https://ref.ly/logosres/LLS:COMM)\n\nCheck this later") {
		t.Errorf("body = %q", n2.Body)
	}
	if len(n2.Tags) != 1 || n2.Tags[0] != FromLogosTag {
		t.Errorf("tags = %v", n2.Tags)
	}
	// A free note, and one on two places (the first becomes the anchor).
	if byKey["note:n4"].Note == nil || byKey["note:n4"].Note.Verses != nil {
		t.Errorf("n4 = %+v", byKey["note:n4"])
	}
	n8 := byKey["note:n8"].Note
	if n8 == nil || n8.Verses.Start != 43003016 || !strings.HasSuffix(n8.Body, "Also on: Romans 8") {
		t.Errorf("n8 = %+v", n8)
	}
	// Not imported, each with a reason.
	reasons := map[string]string{}
	for _, u := range res.Unmapped {
		reasons[u.Key] = u.Reason
	}
	for key, want := range map[string]string{
		"note:n3": "Shared with you",
		"note:n5": "An empty note",
		"note:n6": "In the Logos trash",
	} {
		if !strings.HasPrefix(reasons[key], want) {
			t.Errorf("%s: reason %q, want %q", key, reasons[key], want)
		}
	}
	if _, ok := reasons["note:n7"]; ok || byKey["note:n7"].Key != "" {
		t.Errorf("a deleted note should be left out entirely")
	}
	if len(res.Folders) != 1 || res.Folders[0].Name != "Romans study" {
		t.Errorf("folders = %+v", res.Folders)
	}
}

func TestSnapshotLeavesLogosAlone(t *testing.T) {
	src := notesFixture(t)
	path := filepath.Join(src.Documents, filepath.FromSlash(fileNotes))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	snap, err := src.Snapshot(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snap.Read(); err != nil {
		t.Fatal(err)
	}
	// The copy cannot be written to.
	db, err := snap.open("notes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM Notes`); err == nil {
		t.Error("the copy accepted a write")
	}
	snap.Close()
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("the Logos file changed")
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Errorf("the temporary copy was not deleted: %v", entries)
	}
}

func TestFind(t *testing.T) {
	if _, err := Find(t.TempDir()); err != ErrNotFound {
		t.Errorf("an empty folder: err = %v", err)
	}
	root, _, _ := logosFolder(t)
	src, err := Find(root)
	if err != nil || !strings.Contains(src.Documents, "abcd1234.xyz") {
		t.Errorf("Find = %+v, %v", src, err)
	}
}
