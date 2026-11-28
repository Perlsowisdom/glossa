package logosimport

import (
	"context"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
)

// Titles and authors as each source writes them, by hand (never from a real Logos library).
var sourceItems = []FreeItem{
	// Project Gutenberg: authors as "First Last", every text public domain (US).
	{Source: "gutenberg", ID: "45001", Name: "Institutes of the Christian Religion, Volume 1", About: "Jean Calvin", Language: "en", Kind: "book", PD: true},
	{Source: "gutenberg", ID: "45002", Name: "The Pilgrim's Progress", About: "John Bunyan", Language: "en", Kind: "book", PD: true},
	{Source: "gutenberg", ID: "45003", Name: "Morning and Evening: Daily Readings", About: "C. H. Spurgeon", Language: "en", Kind: "book", PD: true},
	{Source: "gutenberg", ID: "45004", Name: "The Confessions of Saint Augustine", About: "Augustine of Hippo", Language: "en", Kind: "book", PD: true},
	{Source: "gutenberg", ID: "45005", Name: "Der Römerbrief", About: "Karl Barth", Language: "de", Kind: "book", PD: true},
	{Source: "gutenberg", ID: "45006", Name: "Mere Christianity", About: "C. S. Lewis", Language: "en", Kind: "book", PD: true},
	{Source: "gutenberg", ID: "45007", Name: "Sermons", About: "John Wesley", Language: "en", Kind: "book", PD: true},
	{Source: "gutenberg", ID: "45008", Name: "Commentary on Romans", About: "Martin Luther", Language: "en", Kind: "commentary", PD: true},
	// CCEL: author's full name, no rights in the list.
	{Source: "ccel", ID: "calvin/institutes", Name: "Institutes of the Christian Religion", About: "John Calvin", Kind: "book"},
	{Source: "ccel", ID: "bunyan/pilgrim", Name: "The Pilgrim's Progress", About: "John Bunyan", Kind: "book"},
	{Source: "ccel", ID: "luther/romans", Name: "Commentary on Romans", About: "Martin Luther", Kind: "commentary"},
	// Bible modules (CrossWire): a description, not an author.
	{Source: "crosswire", ID: "MHC", Name: "Matthew Henry's Commentary on the Whole Bible", Abbreviation: "MHC", About: "Matthew Henry's Commentary", Language: "en", Kind: "commentary", PD: true},
	{Source: "crosswire", ID: "JFB", Name: "Jamieson-Fausset-Brown Bible Commentary", Abbreviation: "JFB", Language: "en", Kind: "commentary", PD: true},
	{Source: "crosswire", ID: "Thayer", Name: "Thayer's Greek Lexicon", Abbreviation: "Thayer", Language: "en", Kind: "dictionary", PD: true},
	{Source: "crosswire", ID: "Eastons", Name: "Easton's Bible Dictionary", Abbreviation: "Eastons", Language: "en", Kind: "dictionary", PD: true},
	{Source: "crosswire", ID: "StrongsGreek", Name: "Strong's Greek Dictionary", Abbreviation: "StrongsGreek", Language: "en", Kind: "dictionary", PD: true},
	{Source: "ebible", ID: "engwebp", Name: "World English Bible", Abbreviation: "WEB", Language: "en", Kind: "bible", PD: true},
	{Source: "crosswire", ID: "NIV", Name: "New International Version", Abbreviation: "NIV", Language: "en", Kind: "bible"},
}

func TestSourcesTable(t *testing.T) {
	mono, comm, dict, bible := "text.monograph", "text.monograph.commentary.bible", "text.monograph.lexicon", "text.monograph.bible"
	pd := func(b Book) Book { b.PublicDomain = true; return b }
	for _, c := range []struct {
		name   string
		book   Book
		source string // "" for no free copy
		id     string
		conf   string
	}{
		// Title and author.
		{"author written Last, First", Book{Title: "The Pilgrim’s Progress", Authors: "Bunyan, John", Type: mono}, "ccel", "bunyan/pilgrim", Certain},
		{"author written with initials in front", Book{Title: "Morning and Evening", Authors: "C. H. Spurgeon", Type: mono}, "gutenberg", "45003", Certain},
		{"author written Last, F. M.", Book{Title: "Morning and Evening", Authors: "Spurgeon, Charles H.", Type: mono}, "gutenberg", "45003", Certain},
		{"a subtitle after a colon is set aside", Book{Title: "Morning and Evening: A Devotional", Authors: "Spurgeon, C. H.", Type: mono}, "gutenberg", "45003", Certain},
		{"a leading article and case", Book{Title: "the CONFESSIONS of saint augustine", Authors: "Augustine of Hippo", Type: mono}, "gutenberg", "45004", Certain},
		{"Vol. and Volume are the same mark, never certain", Book{Title: "Institutes of the Christian Religion, Vol. 1", Authors: "Calvin, John", Type: mono}, "ccel", "calvin/institutes", Likely},
		{"a volume is never certain", Book{Title: "Institutes of the Christian Religion, Volume 1", Authors: "Calvin, John", Type: mono}, "gutenberg", "45001", Likely},
		{"same title, another author", Book{Title: "The Pilgrim’s Progress", Authors: "Doe, Jane", Type: mono}, "", "", ""},
		{"a one-word title with no author is not shown", Book{Title: "Sermons", Type: mono}, "", "", ""},
		{"a one-word title needs the author, and is then certain", Book{Title: "Sermons", Authors: "Wesley, John", Type: mono}, "gutenberg", "45007", Certain},
		{"title only, long and close, is a possible match", Book{Title: "The Confessions of Saint Augustine", Type: mono}, "gutenberg", "45004", Likely},
		{"another language is another book", Book{Title: "Der Römerbrief", Authors: "Barth, Karl", Type: mono, Languages: "en"}, "", "", ""},
		{"the same language matches", Book{Title: "Der Römerbrief", Authors: "Barth, Karl", Type: mono, Languages: "de"}, "gutenberg", "45005", Certain},
		// Source order.
		{"CCEL before Gutenberg when both are certain", Book{Title: "Institutes of the Christian Religion", Authors: "Calvin, John", Type: mono}, "ccel", "calvin/institutes", Certain},
		{"CCEL before Gutenberg for Luther", Book{Title: "Commentary on Romans", Authors: "Luther, Martin", Type: comm}, "ccel", "luther/romans", Certain},
		{"Gutenberg before a module", Book{Title: "Matthew Henry’s Commentary on the Whole Bible", Authors: "Henry, Matthew", Type: comm}, "crosswire", "MHC", Certain},
		// Modules.
		{"a commentary module", Book{Title: "Jamieson, Fausset and Brown Commentary", Abbreviation: "JFB", Type: comm}, "crosswire", "JFB", Certain},
		{"a lexicon module", Book{Title: "Thayer’s Greek Lexicon", Type: dict, Authors: "Thayer, Joseph Henry"}, "crosswire", "Thayer", Certain},
		{"a dictionary module", Book{Title: "Easton’s Bible Dictionary", Type: "text.monograph.dictionary", Authors: "Easton, M. G."}, "crosswire", "Eastons", Certain},
		{"Strong's", Book{Title: "Strong’s Greek Dictionary", Type: "text.monograph.dictionary"}, "crosswire", "StrongsGreek", Certain},
		{"a Bible module from eBible", Book{Title: "World English Bible", Type: bible}, "ebible", "engwebp", Certain},
		{"a module that is not free is never offered", Book{Title: "New International Version", Abbreviation: "NIV", Type: bible}, "crosswire", "NIV", Certain},
		// The modern-work guard.
		{"a modern copyrighted work is not matched on title alone", Book{Title: "Mere Christianity", Authors: "Lewis, C. S.", Type: mono, Published: "1952", Rights: "Copyright 1952 Example Press"}, "gutenberg", "45006", Certain},
		{"a modern work with only a possible match gets none", Book{Title: "Mere Christianity", Type: mono, Published: "1952", Rights: "Copyright 1952 Example Press"}, "", "", ""},
		{"a modern work is not matched to a CCEL copy", Book{Title: "The Pilgrim’s Progress", Authors: "Bunyan, John", Type: mono, Published: "1985", Rights: "Copyright 1985 Example Press"}, "gutenberg", "", Certain},
		{"a Logos edition that says public domain is matched", pd(Book{Title: "The Pilgrim’s Progress", Authors: "Bunyan, John", Type: mono, Published: "2005"}), "ccel", "bunyan/pilgrim", Certain},
		{"an old book with a copyright note for the edition", Book{Title: "Morning and Evening", Authors: "Spurgeon, C. H.", Type: mono, Published: "1866", Rights: "Copyright 1866 Example"}, "gutenberg", "45003", Certain},
	} {
		ms := MatchAllSources([]Book{c.book}, sourceItems)[0]
		var got *Match
		for i := range ms {
			if c.source == "" || ms[i].Item.Source == c.source {
				got = &ms[i]
				break
			}
		}
		if c.source == "" {
			if len(ms) > 0 {
				t.Errorf("%s: matched %s/%s, want none", c.name, ms[0].Item.Source, ms[0].Item.ID)
			}
			continue
		}
		if c.id == "" {
			// The modern pilgrim's progress: CCEL says nothing about rights, so only Gutenberg may match.
			for _, m := range ms {
				if m.Item.Source == "ccel" {
					t.Errorf("%s: matched CCEL %s for a modern work", c.name, m.Item.ID)
				}
			}
			continue
		}
		if got == nil {
			t.Errorf("%s: no match from %s, want %s", c.name, c.source, c.id)
			continue
		}
		if got.Item.ID != c.id || got.Confidence != c.conf {
			t.Errorf("%s: %s %s, want %s %s", c.name, got.Item.ID, got.Confidence, c.id, c.conf)
		}
	}
}

// The preferred source is first, the others follow, and a certain match comes before a possible one.
func TestSourceOrder(t *testing.T) {
	book := Book{Title: "Institutes of the Christian Religion", Authors: "Calvin, John", Type: "text.monograph", PublicDomain: true}
	ms := MatchAllSources([]Book{book}, sourceItems)[0]
	var order []string
	for _, m := range ms {
		order = append(order, m.Item.Source)
	}
	if strings.Join(order, ",") != "ccel,gutenberg" {
		t.Fatalf("order = %v", order)
	}
	// A possible CCEL match does not beat a certain Gutenberg one.
	items := []FreeItem{
		{Source: "ccel", ID: "x/y", Name: "Institutes of the Christian Religion, Volume 1", About: "John Calvin", Kind: "book"},
		{Source: "gutenberg", ID: "1", Name: "Institutes of the Christian Religion", About: "Jean Calvin", Language: "en", Kind: "book", PD: true},
	}
	ms = MatchAllSources([]Book{book}, items)[0]
	if len(ms) != 2 || ms[0].Item.Source != "gutenberg" || ms[0].Confidence != Certain || ms[1].Confidence != Likely {
		t.Fatalf("certain first: %+v", ms)
	}
}

func TestBookYear(t *testing.T) {
	for _, c := range []struct {
		b    Book
		want int
	}{
		{Book{Published: "1847"}, 1847},
		{Book{Published: "1847", Rights: "Copyright 2005 Example"}, 2005},
		{Book{Published: "1847", Rights: "Public Domain", PublicDomain: true}, 1847},
		{Book{Published: "n.d."}, 0},
	} {
		if got := bookYear(c.b); got != c.want {
			t.Errorf("bookYear(%+v) = %d, want %d", c.b, got, c.want)
		}
	}
}

func TestSurnames(t *testing.T) {
	for in, want := range map[string]string{
		"Barnes, Albert":           "barnes",
		"C. H. Spurgeon":           "spurgeon",
		"Spurgeon, C. H.":          "spurgeon",
		"Augustine of Hippo":       "augustine",
		"Anon.\tOther, A.":         "anon,other",
		"Smith-Jones, Mary":        "smithjones",
		"Henry, Matthew; Scott, T": "henry,scott",
	} {
		if got := strings.Join(surnames(in), ","); got != want {
			t.Errorf("surnames(%q) = %q, want %q", in, got, want)
		}
	}
}

// Whole-library behaviour: states, other sources, the archive link and the switches. The stand-in
// commands answer as the cached lists do.
func TestFreeSourcesInTheList(t *testing.T) {
	root := libraryFixture(t)
	gOn, cOn := true, true
	h := newLibHarness(t, stdFree)
	h.r.Add(command.Define("gutenberg.items", "stand-in", false, func(_ context.Context, _ *command.Env, _ struct {
		Limit int `json:"limit"`
	}) (map[string]any, error) {
		if !gOn {
			return map[string]any{"items": []any{}}, nil
		}
		return map[string]any{"items": []map[string]any{
			{"id": "1", "title": "A Sermon of Old", "author": "Anon.", "language": "en", "installed": false},
			{"id": "2", "title": "A Modern Study", "author": "Pat Other", "language": "en", "installed": false},
		}}, nil
	}))
	h.r.Add(command.Define("ccel.items", "stand-in", false, func(_ context.Context, _ *command.Env, _ struct {
		Limit int `json:"limit"`
	}) (map[string]any, error) {
		if !cOn {
			return map[string]any{"items": []any{}}, nil
		}
		return map[string]any{"items": []map[string]any{
			{"id": "sermons/old", "title": "A Sermon of Old", "author": "Anon.", "kind": "book"},
		}}, nil
	}))
	h.r.Add(command.Define("library.items", "stand-in", false, func(_ context.Context, _ *command.Env, q struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}) (map[string]any, error) {
		if q.Offset > 0 {
			return map[string]any{"items": []any{}}, nil
		}
		return map[string]any{"items": []map[string]any{{"id": "L1", "title": "Notes on the Whole Bible", "authors": []string{"Barnes, Albert"}, "language": "en"}}}, nil
	}))
	h.run("logos.library_refresh", map[string]any{"folder": root}, &RefreshResult{})

	var res LibraryResult
	h.run("logos.library", map[string]any{"limit": 100}, &res)
	old := byID(res.Books, "LLS:OLDNOTE")
	if old.State != "free" || old.Free == nil || old.Free.Source != "ccel" || len(old.Others) == 0 || old.Others[0].Source != "gutenberg" {
		t.Fatalf("old sermon: %+v free %+v others %+v", old.State, old.Free, old.Others)
	}
	if e := byID(res.Books, "LLS:EASTON"); e.State != "already" {
		t.Fatalf("Easton (module installed): %+v", e.State)
	}
	if b := byID(res.Books, "LLS:BARNES-JOB1"); b.State == "already" || b.InLibrary != nil {
		t.Fatalf("a different Barnes book is not in the library: %+v", b.InLibrary)
	}
	m := byID(res.Books, "LLS:NEWNOTE")
	if m.State != "none" || m.Free != nil || !strings.HasPrefix(m.ArchiveLink, "https://archive.org/search?query=A+Modern+Study+Smith%2C+J.") {
		t.Fatalf("modern study: %+v %q", m.State, m.ArchiveLink)
	}
	if res.FreeCopies == 0 || res.Already == 0 || res.NotFree == 0 || res.FreeCopies+res.NotFree+res.Already < res.All {
		t.Fatalf("counts: %+v", res)
	}

	// The filters.
	for state, id := range map[string]string{"free": "LLS:OLDNOTE", "already": "LLS:EASTON", "none": "LLS:NEWNOTE"} {
		var r LibraryResult
		h.run("logos.library", map[string]any{"state": state, "limit": 100}, &r)
		if byID(r.Books, id).ResourceID == "" {
			t.Errorf("state %s lacks %s", state, id)
		}
		for _, b := range r.Books {
			if b.State != state {
				t.Errorf("state %s lists %s as %s", state, b.ResourceID, b.State)
			}
		}
	}

	// Both lists empty (the switches are off): only the modules and the library remain.
	gOn, cOn = false, false
	res = LibraryResult{}
	h.run("logos.library", map[string]any{"limit": 100}, &res)
	old = byID(res.Books, "LLS:OLDNOTE")
	if old.Free != nil && (old.Free.Source == "ccel" || old.Free.Source == "gutenberg") {
		t.Fatalf("a closed source was offered: %+v", old.Free)
	}
}

// A book the user already has in the library, from any source, shows as already in Glossa.
func TestAlreadyInTheLibrary(t *testing.T) {
	root := libraryFixture(t)
	h := newLibHarness(t, nil)
	h.r.Add(command.Define("library.items", "stand-in", false, func(_ context.Context, _ *command.Env, q struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}) (map[string]any, error) {
		if q.Offset > 0 {
			return map[string]any{"items": []any{}}, nil
		}
		return map[string]any{"items": []map[string]any{{"id": "L1", "title": "Easton’s Bible Dictionary", "authors": []string{"M. G. Easton"}, "language": "en"}}}, nil
	}))
	h.run("logos.library_refresh", map[string]any{"folder": root}, &RefreshResult{})
	var res LibraryResult
	h.run("logos.library", map[string]any{"limit": 100}, &res)
	e := byID(res.Books, "LLS:EASTON")
	if e.State != "already" || e.InLibrary == nil || e.InLibrary.Item != "L1" {
		t.Fatalf("Easton: %+v %+v", e.State, e.InLibrary)
	}
}
