package csl

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// goldenItems is a fixed set of sources covering the kinds a Bible-study writer cites.
const goldenItems = `[
 {"id":"book","type":"book","title":"Genesis 1–15","collection-title":"Word Biblical Commentary","collection-number":"1",
  "author":[{"family":"Wenham","given":"Gordon J."}],"publisher":"Word Books","publisher-place":"Waco, TX","issued":{"date-parts":[[1987]]}},
 {"id":"edited","type":"book","title":"The Cambridge Companion to Biblical Interpretation",
  "editor":[{"family":"Barton","given":"John"}],"publisher":"Cambridge University Press","publisher-place":"Cambridge","issued":{"date-parts":[[1998]]}},
 {"id":"chapter","type":"chapter","title":"Reading Genesis as Narrative","container-title":"The Cambridge Companion to Biblical Interpretation",
  "author":[{"family":"Smith","given":"Jane A."}],"editor":[{"family":"Barton","given":"John"}],"page":"13-27",
  "publisher":"Cambridge University Press","publisher-place":"Cambridge","issued":{"date-parts":[[1998]]}},
 {"id":"article","type":"article-journal","title":"Hebrew Poetry and the Psalter","container-title":"Journal of Biblical Literature",
  "author":[{"family":"Doe","given":"John"}],"volume":"135","issue":"2","page":"301-322","DOI":"10.1234/jbl.2016.135.2.301","issued":{"date-parts":[[2016]]}},
 {"id":"bible","type":"book","title":"The Holy Bible: New Revised Standard Version","publisher":"Thomas Nelson","publisher-place":"Nashville","issued":{"date-parts":[[1989]]}},
 {"id":"dict","type":"entry-dictionary","title":"Shalom","container-title":"The New Interpreter's Dictionary of the Bible",
  "author":[{"family":"Roe","given":"Alice"}],"editor":[{"family":"Sakenfeld","given":"Katharine Doob"}],"volume":"5","page":"35-37",
  "publisher":"Abingdon","publisher-place":"Nashville","issued":{"date-parts":[[2009]]}},
 {"id":"web","type":"webpage","title":"Introduction to Textual Criticism","container-title":"Bible Study Resources",
  "author":[{"family":"Poe","given":"Richard"}],"URL":"https://example.org/textual-criticism","accessed":{"date-parts":[[2024,3,5]]},"issued":{"date-parts":[[2023,6,1]]}},
 {"id":"many","type":"book","title":"Studies in Reception History",
  "author":[{"family":"Alpha","given":"Anna"},{"family":"Beta","given":"Ben"},{"family":"Gamma","given":"Cara"},{"family":"Delta","given":"Dan"},{"family":"Epsilon","given":"Eve"}],
  "publisher":"Example Press","publisher-place":"London","issued":{"date-parts":[[2005]]}}
]`

func goldenProcessor(t testing.TB, style string) *Processor {
	t.Helper()
	st, err := BundledStyle(style)
	if err != nil {
		t.Fatal(err)
	}
	items, err := ParseItems([]byte(goldenItems))
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(st, BundledLocales(), items)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// goldenClusters is a document: first cites, a locator, an ibid, a subsequent cite, a repeat.
func goldenClusters() []Cluster {
	return []Cluster{
		{ID: "c1", Items: []CiteItem{{ID: "book", Locator: "12", Label: "page"}}},
		{ID: "c2", Items: []CiteItem{{ID: "book", Locator: "14", Label: "page"}}},
		{ID: "c3", Items: []CiteItem{{ID: "book"}}},
		{ID: "c4", Items: []CiteItem{{ID: "edited"}}},
		{ID: "c5", Items: []CiteItem{{ID: "chapter", Locator: "15", Label: "page"}}},
		{ID: "c6", Items: []CiteItem{{ID: "article", Locator: "305", Label: "page"}}},
		{ID: "c7", Items: []CiteItem{{ID: "bible", Locator: "1:1", Label: "verse"}}},
		{ID: "c8", Items: []CiteItem{{ID: "dict"}}},
		{ID: "c9", Items: []CiteItem{{ID: "web"}}},
		{ID: "c10", Items: []CiteItem{{ID: "many"}}},
		{ID: "c11", Items: []CiteItem{{ID: "book", Locator: "20", Label: "page"}}},
		{ID: "c12", Items: []CiteItem{{ID: "article", Prefix: "See", Suffix: "for more"}}},
	}
}

// renderGolden renders the golden document in a bundled style as plain text lines.
func renderGolden(t testing.TB, style string) string {
	p := goldenProcessor(t, style)
	for _, c := range goldenClusters() {
		if err := p.AddCluster(c); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	for i, c := range p.Citations() {
		fmt.Fprintf(&b, "[%d] %s\n", i+1, c.Output.Text)
	}
	b.WriteString("--\n")
	for _, e := range p.Bibliography() {
		b.WriteString(e.Output.Text + "\n")
	}
	return b.String()
}

func TestGoldenPrint(t *testing.T) {
	if os.Getenv("CSL_GOLDEN") == "" {
		t.Skip("set CSL_GOLDEN=1 to print the golden outputs")
	}
	for _, s := range []string{"society-of-biblical-literature-fullnote-bibliography", "chicago-notes-bibliography", "apa"} {
		t.Logf("\n=== %s\n%s", s, renderGolden(t, s))
	}
}

var goldenStyles = []string{"society-of-biblical-literature-fullnote-bibliography", "chicago-notes-bibliography", "apa"}

// TestGolden compares the golden document with the reviewed outputs in testdata/golden. After
// changing the engine on purpose, review the new output and refresh with CSL_UPDATE=1.
func TestGolden(t *testing.T) {
	for _, s := range goldenStyles {
		t.Run(s, func(t *testing.T) {
			got := renderGolden(t, s)
			path := "testdata/golden/" + s + ".txt"
			if os.Getenv("CSL_UPDATE") != "" {
				if err := os.MkdirAll("testdata/golden", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Errorf("golden output changed for %s:\n%s", s, got)
			}
		})
	}
}

// TestGoldenBookCite pins one book cite in each style, the example in the task report.
func TestGoldenBookCite(t *testing.T) {
	want := map[string]string{
		"society-of-biblical-literature-fullnote-bibliography": "Gordon J. Wenham, <i>Genesis 1–15</i>, Word Biblical Commentary 1 (Word Books, 1987), 12.",
		"chicago-notes-bibliography":                           "Gordon J. Wenham, <i>Genesis 1–15</i>, Word Biblical Commentary 1 (Word Books, 1987), 12.",
		"apa":                                                  "(Wenham, 1987, p. 12)",
	}
	for s, w := range want {
		p := goldenProcessor(t, s)
		c, err := p.Cite(Cluster{Items: []CiteItem{{ID: "book", Locator: "12", Label: "page"}}})
		if err != nil {
			t.Fatal(err)
		}
		if c.Output.HTML != w {
			t.Errorf("%s: got %q, want %q", s, c.Output.HTML, w)
		}
	}
}
