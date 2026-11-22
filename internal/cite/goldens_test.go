package cite_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/cite"
)

func texts(res cite.Result) []string {
	var out []string
	for _, c := range res.Citations {
		out = append(out, c.Text)
	}
	return out
}

func bib(res cite.Result) []string {
	var out []string
	for _, b := range res.Bibliography {
		out = append(out, b.Text)
	}
	return out
}

func sameStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

// A short document: a book, a Bible, a Zotero item, and the book three times more (the second of
// them straight after the first), in each bundled style.
func TestBundledStyleGoldens(t *testing.T) {
	g := setup(t)
	cites := []cite.Cite{
		{Key: g.book, Locator: "12"},
		{Key: "bible:kjv", Locator: "John 3:16"},
		{Key: "zotero:ABCD2345", Locator: "50"},
		{Key: g.book, Locator: "14"},
		{Key: g.book, Locator: "14"},
		{Key: g.book, Locator: "20", Prefix: "see", Suffix: "for more"},
	}
	moo := "Moo, Douglas J. The Epistle to the Romans. Eerdmans, 1996."
	wright := "Wright, N. T. “Justification and Works.” Journal of Pauline Studies 12 (2009): 45–67."
	noteCites := []string{
		"Douglas J. Moo, The Epistle to the Romans (Eerdmans, 1996), 12.",
		"John 3:16 (KJV).",
		"N. T. Wright, “Justification and Works,” Journal of Pauline Studies 12 (2009): 50.",
		"Moo, The Epistle to the Romans, 14.",
		"Moo, The Epistle to the Romans, 14.",
		"see Moo, The Epistle to the Romans, 20 for more.",
	}
	notePos := []string{"first", "", "first", "subsequent", "ibid", "ibid-with-locator"}
	noteShort := []string{"Moo, The Epistle to the Romans, 12", "John 3:16 (KJV)", "Wright, “Justification and Works,” 50"}
	for _, c := range []struct {
		style     string
		class     string
		cites     []string
		positions []string
		short     []string
		bib       []string
	}{
		{"society-of-biblical-literature-fullnote-bibliography", "note", noteCites, notePos, noteShort, []string{moo, wright}},
		{"chicago-notes-bibliography", "note", noteCites, notePos, noteShort, []string{moo, wright}},
		{"apa", "in-text", []string{
			"(Moo, 1996, p. 12)",
			"(KJV, John 3:16)",
			"(Wright, 2009, p. 50)",
			"(Moo, 1996, p. 14)",
			"(Moo, 1996, p. 14)",
			"(see Moo, 1996, p. 20 for more)",
		}, nil, []string{"(Moo, 1996, p. 12)", "John 3:16 (KJV)", "(Wright, 2009, p. 50)"},
			[]string{"Moo, D. J. (1996). The Epistle to the Romans. Eerdmans.", "Wright, N. T. (2009). Justification and Works. Journal of Pauline Studies, 12, 45–67."}},
	} {
		var res cite.Result
		g.must("cite.render", cite.RenderParams{Style: c.style, Cites: cites}, &res)
		if res.Style.Class != c.class {
			t.Errorf("%s: class %q", c.style, res.Style.Class)
		}
		sameStrings(t, c.style+" citations", texts(res), c.cites)
		sameStrings(t, c.style+" bibliography", bib(res), c.bib)
		if c.positions != nil {
			var pos []string
			for i, ct := range res.Citations {
				pos = append(pos, ct.Position)
				if ct.NoteNumber != i+1 {
					t.Errorf("%s: cite %d is note %d", c.style, i+1, ct.NoteNumber)
				}
			}
			sameStrings(t, c.style+" positions", pos, c.positions)
		} else if res.Citations[0].NoteNumber != 0 {
			t.Errorf("%s: an in-text style has no note numbers", c.style)
		}
		var sh cite.ShortResult
		g.must("cite.short", cite.RenderParams{Style: c.style, Cites: cites[:3]}, &sh)
		sameStrings(t, c.style+" short", sh.Labels, c.short)
	}
}

func TestMissingSourceStillRenders(t *testing.T) {
	g := setup(t)
	var res cite.Result
	g.must("cite.render", cite.RenderParams{Style: "apa", Cites: []cite.Cite{{Key: "lib:NOSUCHBOOK"}, {Key: g.book, Locator: "3"}, {Key: "bible:nope", Locator: "Gen 1:1"}}}, &res)
	if res.Citations[0].Missing == "" || !strings.Contains(res.Citations[0].Text, "missing source") || res.Citations[2].Missing == "" {
		t.Errorf("a missing source is said: %+v", res.Citations)
	}
	if res.Citations[1].Text != "(Moo, 1996, p. 3)" || len(res.Bibliography) != 1 {
		t.Errorf("the rest renders: %q %v", res.Citations[1].Text, bib(res))
	}
}

func TestBibleCites(t *testing.T) {
	g := setup(t)
	cites := []cite.Cite{
		{Key: "bible:kjv", Locator: "gen 1:1"},
		{Key: "bible:kjv", Locator: "Romans 8:28-30", Prefix: "cf."},
		{Key: "bible:kjv", Locator: "1 cor 13"},
		{Key: "bible:kjv", Locator: "1John 4:8-5:2", Suffix: ", and more"},
	}
	var res cite.Result
	g.must("cite.render", cite.RenderParams{Style: "society-of-biblical-literature-fullnote-bibliography", Cites: cites}, &res)
	sameStrings(t, "note style", texts(res), []string{"Gen 1:1 (KJV).", "cf. Rom 8:28-30.", "1 Cor 13.", "1 John 4:8-5:2, and more."})
	g.must("cite.render", cite.RenderParams{Style: "apa", Cites: cites}, &res)
	sameStrings(t, "in-text style", texts(res), []string{"(KJV, Gen 1:1)", "(cf. KJV, Rom 8:28-30)", "(KJV, 1 Cor 13)", "(KJV, 1 John 4:8-5:2, and more)"})
	if len(res.Bibliography) != 0 {
		t.Errorf("a Bible version is not listed among the works cited: %v", bib(res))
	}
	var p cite.PassageResult
	g.must("cite.passage", cite.PassageParams{Text: "song of songs 2:4"}, &p)
	if !p.OK || p.Passage != "Song 2:4" {
		t.Errorf("passage %+v", p)
	}
	g.must("cite.passage", cite.PassageParams{Text: "page twelve"}, &p)
	if p.OK {
		t.Errorf("not a passage")
	}
}

func TestSourcesSearch(t *testing.T) {
	g := setup(t)
	var m cite.Source
	g.must("cite.manual_save", cite.Manual{Title: "A Private Letter", Authors: []string{"Smith, Jane"}, Year: 1988, Type: "book"}, &m)
	if !strings.HasPrefix(m.Key, "manual:") {
		t.Fatalf("key %q", m.Key)
	}
	find := func(q string) map[string]string {
		var r cite.SearchResult
		g.must("cite.sources", cite.SourcesParams{Query: q}, &r)
		out := map[string]string{}
		for _, s := range r.Sources {
			out[s.Key] = s.Kind
		}
		return out
	}
	if got := find("romans"); got[g.book] != "library" {
		t.Errorf("the library book: %v", got)
	}
	if got := find("moo"); got[g.book] != "library" {
		t.Errorf("by author: %v", got)
	}
	if got := find("king james"); got["bible:kjv"] != "bible" {
		t.Errorf("a Bible: %v", got)
	}
	if got := find("justification"); got["zotero:ABCD2345"] != "zotero" {
		t.Errorf("a kept Zotero item is found with Zotero off: %v", got)
	}
	if got := find("private"); got[m.Key] != "manual" {
		t.Errorf("a manual entry: %v", got)
	}
	if got := find("nothing matches this"); len(got) != 0 {
		t.Errorf("no match: %v", got)
	}
	var res cite.Result
	g.must("cite.render", cite.RenderParams{Style: "apa", Cites: []cite.Cite{{Key: m.Key, Locator: "2"}}}, &res)
	if res.Citations[0].Text != "(Smith, 1988, p. 2)" {
		t.Errorf("manual cite: %q", res.Citations[0].Text)
	}
	g.must("cite.manual_save", cite.Manual{ID: m.Key, Title: "A Private Letter", Authors: []string{"Jane Smyth"}, Year: 1989}, &m)
	g.must("cite.render", cite.RenderParams{Style: "apa", Cites: []cite.Cite{{Key: m.Key}}}, &res)
	if res.Citations[0].Text != "(Smyth, 1989)" {
		t.Errorf("changed manual cite: %q", res.Citations[0].Text)
	}
}

func TestDocumentStyleAndDefault(t *testing.T) {
	g := setup(t)
	var s cite.StyleResult
	g.must("cite.style", cite.StyleParams{ID: "doc1"}, &s)
	if s.Style != cite.DefaultStyle || s.Own {
		t.Errorf("starts with the default: %+v", s)
	}
	g.must("cite.set_style", cite.SetStyleParams{Style: "apa"}, &s)
	g.must("cite.style", cite.StyleParams{ID: "doc1"}, &s)
	if s.Style != "apa" || s.Own || s.Default != "apa" {
		t.Errorf("the default changed: %+v", s)
	}
	g.must("cite.set_style", cite.SetStyleParams{ID: "doc1", Style: "chicago-notes-bibliography"}, &s)
	if s.Style != "chicago-notes-bibliography" || !s.Own || s.Default != "apa" {
		t.Errorf("its own: %+v", s)
	}
	var res cite.Result
	g.must("cite.document", cite.DocumentParams{ID: "doc1", Markdown: "Text " + cite.Format(cite.Cite{Key: g.book, Locator: "9"}) + "."}, &res)
	if res.Style.ID != "chicago-notes-bibliography" || len(res.Citations) != 1 || res.Citations[0].NoteNumber != 1 {
		t.Errorf("document uses its style: %+v", res)
	}
	g.must("cite.set_style", cite.SetStyleParams{ID: "doc1"}, &s)
	if s.Own || s.Style != "apa" {
		t.Errorf("back to the default: %+v", s)
	}
	if err := g.run("cite.set_style", cite.SetStyleParams{Style: "no-such-style"}, nil); err == nil {
		t.Error("an unknown style is refused")
	}
}

func TestMarkdownRoundTrip(t *testing.T) {
	back := "\x5c"
	for _, c := range []cite.Cite{
		{Key: "lib:01HXABC"},
		{Key: "lib:01HXABC", Locator: "12"},
		{Key: "lib:01HXABC", Locator: "12-14"},
		{Key: "zotero:ABCD2345", Label: "chapter", Locator: "3"},
		{Key: "zotero:ABCD2345", Label: "verse", Locator: "5", Prefix: "see also", Suffix: "for more, and more"},
		{Key: "manual:01HX", SuppressAuthor: true},
		{Key: "manual:01HX", Suffix: "and later"},
		{Key: "bible:kjv", Locator: "Rom 8:28-30"},
		{Key: "bible:kjv", Locator: "Gen 1:1", Prefix: "cf."},
		{Key: "lib:01HXABC", Locator: "7", Prefix: "[odd] @ words", Suffix: "a [bracket] and a back" + back + "slash"},
	} {
		md := cite.Format(c)
		got, n, ok := cite.ParseInline(md + " after")
		if !ok || n != len(md) {
			t.Errorf("%q: not read (%v, %d of %d)", md, ok, n, len(md))
			continue
		}
		want := c
		if want.Locator != "" && want.Label == "" && !want.IsBible() {
			want.Label = "page"
		}
		if got != want {
			t.Errorf("%q:\n got %+v\nwant %+v", md, got, want)
		}
	}
	// Ordinary links and brackets are left alone.
	for _, s := range []string{"[text](#passage/Rom.8.28)", "[@nokind:abc]", "[see @lib:ABC](http://x)", "[plain]", "[ @lib:ABC]", "[@lib:]"} {
		if _, _, ok := cite.ParseInline(s); ok {
			t.Errorf("%q is not a citation", s)
		}
	}
	md := "One [@lib:AAA, p. 3] and two [see @zotero:BBB]. A [link](http://x) too. [@bible:kjv, John 3:16]"
	got := cite.Extract(md)
	if len(got) != 3 || got[0].Key != "lib:AAA" || got[1].Prefix != "see" || got[2].Locator != "John 3:16" {
		t.Errorf("extract: %+v", got)
	}
	if got := cite.Format(cite.Cite{Key: "lib:A", Locator: "1,3"}); got != "[@lib:A, pp. 1,3]" {
		t.Errorf("a list of pages is plural: %s", got)
	}
}
