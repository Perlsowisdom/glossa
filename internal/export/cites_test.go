package export_test

import (
	"archive/zip"
	"bytes"
	"context"

	"github.com/Perlsowisdom/glossa/internal/export"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/store"
)

const citedBody = "Grace is plain [@lib:BOOK, p. 12]. God so loved the world [@bible:kjv, John 3:16]. " +
	"Again [@lib:BOOK, p. 14] and straight after [@lib:BOOK, p. 14]. Works matter [see @zotero:ABCD2345, p. 50, for more].\n\n" +
	"<!-- bibliography -->"

const zoteroItem = `{"id":"zotero:ABCD2345","type":"article-journal","title":"Justification and Works","container-title":"Journal of Pauline Studies","volume":"12","page":"45-67","author":[{"family":"Wright","given":"N. T."}],"issued":{"date-parts":[[2009]]}}`

// citedDoc makes a note that cites a library book, a Bible, a Zotero item and the book again.
func citedDoc(t *testing.T, e *env) map[string]any {
	t.Helper()
	id := e.book
	e.must("library.update_item", map[string]any{"id": id, "year": 2001, "publisher": "Example Press", "authors": []string{"Anna Example"}}, nil)
	err := e.env.Store.Write(context.Background(), "test", func(tx *store.Tx) error {
		return tx.Create("cite_items", store.Row{"id": "zotero:ABCD2345", "connection": "zotero", "item_key": "ABCD2345",
			"csl_json": zoteroItem, "title": "Justification and Works", "cached_at": 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	var note struct{ ID string }
	e.must("note.create", map[string]any{"title": "Grace", "body": strings.ReplaceAll(citedBody, "BOOK", id)}, &note)
	return map[string]any{"kind": "note", "id": note.ID}
}

func zipPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == name {
			r, _ := f.Open()
			defer r.Close()
			b, _ := io.ReadAll(r)
			return string(b)
		}
	}
	return ""
}

const sbl = "society-of-biblical-literature-fullnote-bibliography"

func TestCitationsAsFootnotesEveryFormat(t *testing.T) {
	e := setup(t)
	src := []map[string]any{citedDoc(t, e)}
	opts := map[string]any{"cite_style": sbl}
	foot := []string{
		"Anna Example, The Long Book (Example Press, 2001), 12.",
		"John 3:16 (KJV).",
		"Example, The Long Book, 14.",
		"see N. T. Wright, “Justification and Works,” Journal of Pauline Studies 12 (2009): 50 for more.",
	}

	_, md := e.make("md", src, opts)
	for _, w := range []string{"Grace is plain[^1]. God so loved the world[^2]. Again[^3] and straight after[^4]. Works matter[^5].",
		"### Bibliography", "Example, Anna. *The Long Book*. Example Press, 2001.",
		"Wright, N. T. “Justification and Works.” *Journal of Pauline Studies* 12 (2009): 45–67.",
		"[^1]: Anna Example, *The Long Book* (Example Press, 2001), 12.", "[^2]: John 3:16 (KJV).", "[^4]: Example, *The Long Book*, 14."} {
		if !strings.Contains(string(md), w) {
			t.Errorf("Markdown lacks %q:\n%s", w, md)
		}
	}
	if strings.Contains(string(md), "@lib:") || strings.Contains(string(md), "bibliography -->") {
		t.Errorf("Markdown still holds a raw citation:\n%s", md)
	}

	_, page := e.make("html", src, opts)
	for _, w := range []string{`<sup class="xd-fn"><a id="fnref-1" href="#fn-1">1</a></sup>.`, `<section class="xd-notes"><h2>Notes</h2><ol>`,
		`<li id="fn-1" value="1" dir="auto">Anna Example, <em>The Long Book</em> (Example Press, 2001), 12.`,
		`<p class="xd-hang" dir="auto">Example, Anna. <em>The Long Book</em>. Example Press, 2001.</p>`} {
		if !bytes.Contains(page, []byte(w)) {
			t.Errorf("HTML lacks %q:\n%s", w, page)
		}
	}

	_, docx := e.make("docx", src, opts)
	body := checkDOCX(t, docx)
	if strings.Count(zipPart(t, docx, "word/document.xml"), "<w:footnoteReference") != 5 {
		t.Errorf("the Word file has five footnote marks")
	}
	notes := zipPart(t, docx, "word/footnotes.xml")
	for _, w := range foot {
		if !strings.Contains(squash(xmlText(notes)), squash(w)) {
			t.Errorf("footnotes.xml lacks %q:\n%s", w, notes)
		}
	}
	if !strings.Contains(notes, `<w:i/>`) || !strings.Contains(zipPart(t, docx, "[Content_Types].xml"), "footnotes+xml") ||
		!strings.Contains(zipPart(t, docx, "word/_rels/document.xml.rels"), "relationships/footnotes") {
		t.Errorf("the footnotes are italic where the style says, and are wired in")
	}
	if !strings.Contains(body, "Example, Anna.") || strings.Contains(body, "@lib") {
		t.Errorf("the Word text:\n%s", body)
	}

	var pv export.Preview
	e.must("export.preview", map[string]any{"sources": src, "format": "pdf", "options": opts}, &pv)
	if len(pv.Warnings) > 0 && strings.Contains(pv.Warnings[0], "no font for PDF") {
		t.Skip("no TrueType fonts on this system for the PDF test")
	}
	_, pdfData := e.make("pdf", src, opts)
	text := squash(pdfText(t, pdfData))
	for _, w := range append([]string{"Notes", "Bibliography", "Example, Anna."}, foot...) {
		if !strings.Contains(text, squash(w)) {
			t.Errorf("PDF text lacks %q:\n%s", w, text)
		}
	}
}

func TestCitationsInTextForAuthorDate(t *testing.T) {
	e := setup(t)
	src := []map[string]any{citedDoc(t, e)}
	opts := map[string]any{"cite_style": "apa"}
	_, md := e.make("md", src, opts)
	for _, w := range []string{"Grace is plain (Example, 2001, p. 12). God so loved the world (KJV, John 3:16). Again (Example, 2001, p. 14) and straight after (Example, 2001, p. 14). Works matter (see Wright, 2009, p. 50 for more).",
		"### References", "Example, A. (2001). *The Long Book*. Example Press."} {
		if !strings.Contains(string(md), w) {
			t.Errorf("Markdown lacks %q:\n%s", w, md)
		}
	}
	if strings.Contains(string(md), "[^") {
		t.Errorf("an in-text style has no footnotes:\n%s", md)
	}
	_, docx := e.make("docx", src, opts)
	checkDOCX(t, docx)
	if zipPart(t, docx, "word/footnotes.xml") != "" {
		t.Errorf("no footnotes part for in-text citations")
	}
	_, page := e.make("html", src, opts)
	if !bytes.Contains(page, []byte("(Example, 2001, p. 12)")) || bytes.Contains(page, []byte("<section class=\"xd-notes\">")) {
		t.Errorf("HTML:\n%s", page)
	}
}

func TestDocumentStyleBibliographyOptionAndOtherText(t *testing.T) {
	e := setup(t)
	id := e.book
	e.must("library.update_item", map[string]any{"id": id, "year": 2001, "authors": []string{"Anna Example"}}, nil)
	var note struct{ ID string }
	body := "A [link](https://example.org) and a chip [Rom 8:28](#passage/Rom.8.28) stay. Cited [@lib:" + id + ", p. 3]."
	e.must("note.create", map[string]any{"title": "Plain", "body": body}, &note)
	src := []map[string]any{{"kind": "note", "id": note.ID}}

	// The document's own style (set through its id) is used; with no bibliography block none is added...
	e.must("cite.set_style", map[string]any{"id": note.ID, "style": "apa"}, nil)
	_, md := e.make("md", src, nil)
	if !strings.Contains(string(md), "Cited (Example, 2001, p. 3).") || strings.Contains(string(md), "References") {
		t.Errorf("own style, no bibliography:\n%s", md)
	}
	if !strings.Contains(string(md), "[link](https://example.org)") || !strings.Contains(string(md), "[Rom 8:28](#passage/Rom.8.28)") {
		t.Errorf("links and chips are untouched:\n%s", md)
	}
	// ...until the person asks for one at the end, or says none.
	_, md = e.make("md", src, map[string]any{"bibliography": "end"})
	if !strings.Contains(string(md), "### References") {
		t.Errorf("a bibliography at the end:\n%s", md)
	}
	// A note style has footnotes; the explicit style beats the document's.
	_, md = e.make("md", src, map[string]any{"cite_style": "chicago-notes-bibliography"})
	if !strings.Contains(string(md), "Cited[^1].") || !strings.Contains(string(md), "[^1]: Anna Example") {
		t.Errorf("explicit style:\n%s", md)
	}

	// A bibliography block in a document with no citations leaves nothing behind.
	e.must("note.create", map[string]any{"title": "Empty", "body": "Text.\n\n<!-- bibliography -->"}, &note)
	_, md = e.make("md", []map[string]any{{"kind": "note", "id": note.ID}}, nil)
	if strings.Contains(string(md), "bibliography") || strings.Contains(string(md), "Bibliography") {
		t.Errorf("an empty bibliography:\n%s", md)
	}
	// And a document without any citation is exactly as before.
	e.must("note.create", map[string]any{"title": "Old", "body": "Just [a note] with [brackets] and an email me@example.org."}, &note)
	_, md = e.make("md", []map[string]any{{"kind": "note", "id": note.ID}}, nil)
	if !strings.Contains(string(md), "Just [a note] with [brackets] and an email me@example.org.") {
		t.Errorf("older text:\n%s", md)
	}
}

var xmlTag = regexp.MustCompile(`<[^>]*>`)

func xmlText(s string) string { return xmlTag.ReplaceAllString(s, "") }
