package logosimport

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// A catalogue with the columns the real one has for what the book list reads. The books are made up.
const fullCatalogSchema = `CREATE TABLE Records (RecordId integer primary key autoincrement, ResourceId text not null,
	ResourceIdAsAlternate text, Type text, Title text, AbbreviatedTitle text, Authors text, PublicationDate text,
	Languages text, Copyright text, UserTitle text, UserAbbreviatedTitle text, Edition text);`

func libraryFixture(t *testing.T) string {
	t.Helper()
	root, _, data := logosFolder(t)
	ins := func(id, typ, title, abbr, authors, date, langs, copyright, userTitle string) string {
		q := func(s string) string {
			if s == "" {
				return "NULL"
			}
			return "'" + strings.ReplaceAll(s, "'", "''") + "'"
		}
		return `INSERT INTO Records (ResourceId, Type, Title, AbbreviatedTitle, Authors, PublicationDate, Languages, Copyright, UserTitle)
			VALUES (` + strings.Join([]string{q(id), q(typ), q(title), q(abbr), q(authors), q(date), q(langs), q(copyright), q(userTitle)}, ", ") + `)`
	}
	makeDB(t, data, fileCatalog, fullCatalogSchema,
		ins("LLS:KJV", "text.monograph.bible", "King James Version", "KJV", "", "1900", "en", "Public Domain", ""),
		ins("LLS:ESV", "text.monograph.bible", "English Standard Version", "ESV", "", "2016", "en", "Copyright 2016 Crossway", ""),
		ins("LLS:EASTON", "text.monograph.dictionary", "Easton’s Bible Dictionary", "", "Easton, M. G.", "1893", "en", "Public domain", ""),
		ins("LLS:BARNES-JOB1", "text.monograph.commentary.bible", "Job, Volume 1", "", "Barnes, Albert", "1847", "en", "Public Domain.", ""),
		ins("LLS:HENRY", "text.monograph.commentary.bible", "Matthew Henry’s Concise Commentary on the Bible", "", "Henry, Matthew\tScott, Thomas", "1997", "en", "Public Domain", ""),
		ins("LLS:GNT-MIXED", "text.monograph.bible", "Greek New Testament with notes", "", "", "2008", "el", "Greek Texts, Public Domain. Morphological Annotation, Copyright 2008 Lexham Press", ""),
		ins("LLS:OLDNOTE", "text.monograph", "A Sermon of Old", "", "Anon.\tOther, A.", "1850", "en", "", ""),
		ins("LLS:NEWNOTE", "text.monograph", "A Modern Study", "", "Smith, J.", "2010", "en", "", ""),
		ins("LLS:MEDIA", "lbx.media", "A Video Course", "", "", "1850", "en", "", ""),
		ins("LLS:MINE", "text.monograph", "Printed Title", "", "Who, A.", "2001", "en", "Copyright 2001 Someone", "My own title for it"),
	)
	return root
}

type libHarness struct {
	t   *testing.T
	r   *command.Registry
	env *command.Env
	st  *store.Store
	// free is what the stand-in catalogue offers; nil means the catalogues are not there.
	free []map[string]any
}

func newLibHarness(t *testing.T, free []map[string]any) *libHarness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &libHarness{t: t, r: command.NewRegistry(), env: &command.Env{Store: st}, st: st, free: free}
	Register(h.r)
	if free != nil {
		type p struct {
			Kind      string `json:"kind"`
			Supported bool   `json:"supported"`
			Limit     int    `json:"limit"`
		}
		h.r.Add(command.Define("bible.available", "stand-in", false, func(_ context.Context, _ *command.Env, q p) (map[string]any, error) {
			var items []map[string]any
			for _, it := range h.free {
				if it["kind"] == q.Kind {
					items = append(items, it)
				}
			}
			return map[string]any{"items": items}, nil
		}))
	}
	return h
}

func (h *libHarness) run(id string, params, out any) {
	h.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := h.r.Run(context.Background(), h.env, id, raw)
	if err != nil {
		h.t.Fatalf("%s: %v", id, err)
	}
	b, _ := json.Marshal(res)
	if err := json.Unmarshal(b, out); err != nil {
		h.t.Fatal(err)
	}
}

func freeItem(kind, id, name, abbr, lang, licence string, installed bool) map[string]any {
	return map[string]any{"source": "crosswire", "id": id, "name": name, "abbreviation": abbr, "language": lang, "kind": kind,
		"supported": true, "installed": installed, "licence": map[string]any{"type": licence}}
}

var stdFree = []map[string]any{
	freeItem("bible", "KJV", "King James Version (1769) with Strongs Numbers", "KJV", "en", "public_domain", false),
	freeItem("bible", "ESV", "English Standard Version", "ESV", "en", "personal_purchase", false),
	freeItem("dictionary", "Easton", "Easton's Bible Dictionary", "Easton", "en", "public_domain", true),
	freeItem("commentary", "MHC", "Matthew Henry's Concise Commentary", "MHC", "en", "public_domain", false),
	freeItem("commentary", "Barnes", "Albert Barnes' Notes on the Bible", "Barnes", "en", "public_domain", false),
	freeItem("book", "OldSermon", "A Sermon of Old", "OldSermon", "en", "creative_commons", false),
}

func byID(books []LibraryBook, id string) LibraryBook {
	for _, b := range books {
		if b.ResourceID == id {
			return b
		}
	}
	return LibraryBook{}
}

func TestRefreshAndList(t *testing.T) {
	root := libraryFixture(t)
	h := newLibHarness(t, stdFree)

	var empty LibraryResult
	h.run("logos.library", map[string]any{}, &empty)
	if empty.Refreshed || empty.All != 0 {
		t.Fatalf("before a refresh: %+v", empty)
	}

	var ref RefreshResult
	h.run("logos.library_refresh", map[string]any{"folder": root}, &ref)
	if ref.Books != 10 || ref.Added != 10 || ref.PublicDomain != 6 {
		t.Fatalf("refresh = %+v", ref)
	}

	var res LibraryResult
	h.run("logos.library", map[string]any{"limit": 100}, &res)
	if !res.Refreshed || res.All != 10 || res.Total != 10 || res.PublicDomain != 6 {
		t.Fatalf("list = %+v", res)
	}
	kjv := byID(res.Books, "LLS:KJV")
	if kjv.Title != "King James Version" || kjv.TypeLabel != "Bible" || !kjv.PublicDomain || kjv.PDBasis != "rights" || kjv.Link != "logosres:LLS:KJV" {
		t.Fatalf("KJV = %+v", kjv)
	}
	if kjv.Free == nil || kjv.Free.Item != "KJV" || kjv.Free.Confidence != Certain {
		t.Fatalf("KJV free copy = %+v", kjv.Free)
	}
	// A book you renamed shows your title; a copyrighted one has no free copy and is not public domain.
	if b := byID(res.Books, "LLS:MINE"); b.Title != "My own title for it" || b.PublicDomain || b.Free != nil {
		t.Fatalf("renamed book = %+v", b)
	}
	if b := byID(res.Books, "LLS:ESV"); b.PublicDomain || b.Free != nil {
		t.Fatalf("ESV must not be offered: %+v", b)
	}
	// A rights note that also claims a copyright is not public domain.
	if b := byID(res.Books, "LLS:GNT-MIXED"); b.PublicDomain {
		t.Fatalf("mixed rights: %+v", b)
	}
	// No rights claim and an old date: public domain by date only.
	if b := byID(res.Books, "LLS:OLDNOTE"); !b.PublicDomain || b.PDBasis != "date" || b.Authors != "Anon.; Other, A." {
		t.Fatalf("old book = %+v", b)
	}
	if b := byID(res.Books, "LLS:NEWNOTE"); b.PublicDomain {
		t.Fatalf("new book = %+v", b)
	}
	if b := byID(res.Books, "LLS:MEDIA"); b.PublicDomain && b.Free != nil {
		t.Fatalf("media must not match: %+v", b)
	}
	if h := byID(res.Books, "LLS:EASTON"); h.Free == nil || h.Free.Confidence != Certain || !h.Free.Installed {
		t.Fatalf("Easton = %+v", h.Free)
	}
	if b := byID(res.Books, "LLS:HENRY"); b.Free == nil || b.Free.Item != "MHC" {
		t.Fatalf("Henry = %+v", b.Free)
	}
	// Volume 1 of a multi-volume set is only a likely match for the whole set's free copy, if any.
	if b := byID(res.Books, "LLS:BARNES-JOB1"); b.Free != nil && b.Free.Confidence == Certain {
		t.Fatalf("volume must not be certain: %+v", b.Free)
	}
	if res.Certain < 2 || res.Certain+res.Likely > res.PublicDomain {
		t.Fatalf("counts = %+v", res)
	}
}

func TestListSearchSortFilter(t *testing.T) {
	root := libraryFixture(t)
	h := newLibHarness(t, stdFree)
	h.run("logos.library_refresh", map[string]any{"folder": root}, &RefreshResult{})

	titles := func(p map[string]any) []string {
		var res LibraryResult
		h.run("logos.library", p, &res)
		var out []string
		for _, b := range res.Books {
			out = append(out, b.ResourceID)
		}
		return out
	}
	eq := func(got, want []string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	eq(titles(map[string]any{"text": "easton"}), []string{"LLS:EASTON"})
	eq(titles(map[string]any{"text": "barnes job"}), []string{"LLS:BARNES-JOB1"}) // author and title words together
	eq(titles(map[string]any{"text": "kjv"}), []string{"LLS:KJV"})                // the short name
	eq(titles(map[string]any{"text": "lls:media"}), []string{"LLS:MEDIA"})        // the ID
	eq(titles(map[string]any{"text": "nothing like this"}), nil)
	eq(titles(map[string]any{"type": "Bible", "sort": "title"}), []string{"LLS:ESV", "LLS:GNT-MIXED", "LLS:KJV"})
	eq(titles(map[string]any{"type": "Bible", "sort": "title", "desc": true}), []string{"LLS:KJV", "LLS:GNT-MIXED", "LLS:ESV"})
	// Sorting by author puts books without one last, either way.
	a := titles(map[string]any{"sort": "author"})
	if a[0] != "LLS:OLDNOTE" || a[len(a)-1] == "LLS:OLDNOTE" {
		t.Fatalf("by author: %v", a)
	}
	d := titles(map[string]any{"sort": "author", "desc": true})
	if d[0] != "LLS:MINE" {
		t.Fatalf("by author, reversed: %v", d)
	}
	eq(titles(map[string]any{"sort": "published"})[:2], []string{"LLS:BARNES-JOB1", "LLS:OLDNOTE"})
	// The filters.
	if n := len(titles(map[string]any{"public_domain": true})); n != 6 {
		t.Fatalf("public domain: %d", n)
	}
	free := titles(map[string]any{"free_copy": true})
	if len(free) < 4 {
		t.Fatalf("with a free copy: %v", free)
	}
	// Paging.
	var page LibraryResult
	h.run("logos.library", map[string]any{"limit": 3, "offset": 9}, &page)
	if page.Total != 10 || len(page.Books) != 1 {
		t.Fatalf("page: %+v", page)
	}
}

func TestRefreshUpdatesInPlace(t *testing.T) {
	root := libraryFixture(t)
	h := newLibHarness(t, nil)
	var ref RefreshResult
	h.run("logos.library_refresh", map[string]any{"folder": root}, &ref)
	again := RefreshResult{}
	h.run("logos.library_refresh", map[string]any{"folder": root}, &again)
	if again.Added != 0 || again.Changed != 0 || again.Removed != 0 || again.Books != 10 {
		t.Fatalf("an unchanged catalogue changed the list: %+v", again)
	}

	// A book added, one renamed and one removed in Logos.
	src, _ := Find(root)
	db := openFixture(t, src.Data, fileCatalog)
	mustExec(t, db, `INSERT INTO Records (ResourceId, Type, Title, Authors, PublicationDate, Languages, Copyright) VALUES ('LLS:NEW', 'text.monograph', 'Brand New', 'Writer, A.', '2024', 'en', 'Copyright 2024 X')`)
	mustExec(t, db, `UPDATE Records SET Title = 'King James Bible' WHERE ResourceId = 'LLS:KJV'`)
	mustExec(t, db, `DELETE FROM Records WHERE ResourceId = 'LLS:MEDIA'`)
	db.Close()
	h.run("logos.library_refresh", map[string]any{"folder": root}, &again)
	if again.Added != 1 || again.Changed != 1 || again.Removed != 1 || again.Books != 10 {
		t.Fatalf("refresh = %+v", again)
	}
	var res LibraryResult
	h.run("logos.library", map[string]any{"limit": 100}, &res)
	if byID(res.Books, "LLS:KJV").Title != "King James Bible" || byID(res.Books, "LLS:MEDIA").ResourceID != "" || byID(res.Books, "LLS:NEW").Title != "Brand New" {
		t.Fatalf("list after refresh: %+v", res.Books)
	}
	// Without the catalogues there are no free copies, and the list still works.
	if res.Certain+res.Likely != 0 {
		t.Fatalf("matches without catalogues: %+v", res)
	}
	// The change is in the history, so it can be seen and undone like any other.
	var n int
	if err := h.st.QueryRow(context.Background(), `SELECT COUNT(*) FROM changes WHERE command = 'logos.library_refresh'`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("changes logged: %d %v", n, err)
	}
}

func TestRefreshCatalogueMissingOrBare(t *testing.T) {
	h := newLibHarness(t, nil)
	// No Logos folder at all.
	raw, _ := json.Marshal(map[string]any{"folder": t.TempDir()})
	if _, err := h.r.Run(context.Background(), h.env, "logos.library_refresh", raw); err == nil {
		t.Fatal("expected an error with no Logos data")
	}
	// A catalogue with only the columns an older Logos had still lists its books.
	root, _, data := logosFolder(t)
	makeDB(t, data, fileCatalog, catalogSchema, `INSERT INTO Records VALUES ('LLS:A', NULL, 'Only A Title', NULL, 'text.monograph')`)
	var ref RefreshResult
	h.run("logos.library_refresh", map[string]any{"folder": root}, &ref)
	var res LibraryResult
	h.run("logos.library", map[string]any{}, &res)
	if ref.Books != 1 || len(res.Books) != 1 || res.Books[0].Title != "Only A Title" || res.Books[0].PublicDomain {
		t.Fatalf("bare catalogue: %+v %+v", ref, res)
	}
}

func TestBookLink(t *testing.T) {
	for id, want := range map[string]string{
		"LLS:ESV":           "logosres:LLS:ESV",
		"CV:HEBREW-TREES":   "logosres:CV:HEBREW-TREES",
		"LLS:A B;C(1)":      "logosres:LLS:A%20B%3BC%281%29",
		"LLS:NAÏVE":         "logosres:LLS:NA%C3%8FVE",
		"LLS:QUINT1-3INTRO": "logosres:LLS:QUINT1-3INTRO",
	} {
		if got := bookLink(id); got != want {
			t.Errorf("bookLink(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestClassifyRights(t *testing.T) {
	for _, c := range []struct {
		rights, date string
		pd           bool
		basis        string
	}{
		{"Public Domain", "1997", true, "rights"},
		{"Public domain", "", true, "rights"},
		{"Public Domain.", "1873", true, "rights"},
		{" public domain ", "", true, "rights"},
		{"In the public domain", "", true, "rights"},
		{"Greek Texts, Public Domain. Morphological Annotation, Copyright 2008 Lexham Press", "2008", false, ""},
		{"Public Domain. Copyright 2001 Someone", "1850", false, ""},
		{"Copyright 1995 Rosalie J. Slater", "1828", false, ""}, // a modern edition of an old book
		{"", "1847", true, "date"},
		{"", "1870–1872", true, "date"},
		{"", "1999", false, ""},
		{"", "1901", false, ""},
		{"", "n.d.", false, ""},
		{"", "", false, ""},
		{"All rights reserved", "1850", false, ""},
	} {
		pd, basis := classifyRights(c.rights, c.date)
		if pd != c.pd || basis != c.basis {
			t.Errorf("classifyRights(%q, %q) = %v %q, want %v %q", c.rights, c.date, pd, basis, c.pd, c.basis)
		}
	}
}
