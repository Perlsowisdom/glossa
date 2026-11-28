package anyapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// clientFor is a client for a fake that asks for no key.
func clientFor(t *testing.T) (*Client, *fakeAny) {
	t.Helper()
	f := newFake(t)
	hc := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	c, err := NewClient(f.srv.URL, hc)
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

var bg = context.Background()

func TestSpacesReadFromTheSpacesList(t *testing.T) {
	c, _ := clientFor(t)
	sp, err := c.Spaces(bg, "")
	if err != nil || len(sp) != 2 || sp[0].ID != spaceA || sp[0].Name != "Study" {
		t.Fatalf("%+v %v", sp, err)
	}
}

func TestListingNewestFirstSkipsBinned(t *testing.T) {
	c, f := clientFor(t)
	f.add(spaceA, "Old", "x")
	mid := f.add(spaceA, "Middle", "x")
	f.add(spaceA, "New", "x")
	f.add(spaceB, "Other space", "x")
	binned := f.add(spaceA, "Binned", "x")
	f.objs[binned].Archived = true
	f.edit(mid, "Middle", "touched last")
	r, err := c.Pages(bg, "", spaceA, "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range r.Pages {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "Middle,New,Old" {
		t.Fatalf("pages = %s", got)
	}
	if r.Pages[0].Modified == "" {
		t.Fatal("no last-change time")
	}
}

func TestListingPagesThroughAndSortFallback(t *testing.T) {
	c, f := clientFor(t)
	for _, n := range []string{"A", "B", "C", "D", "E"} {
		f.add(spaceA, n, "x")
	}
	f.pageCap = 2
	var seen []string
	for off := 0; ; {
		r, err := c.Pages(bg, "", spaceA, "", off, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range r.Pages {
			seen = append(seen, p.Name)
		}
		if r.NextOffset == 0 {
			break
		}
		off = r.NextOffset
	}
	if len(seen) != 5 {
		t.Fatalf("saw %v", seen)
	}
	// An app that refuses the sort is asked again without it, and the pages are sorted here.
	f.rejectSort = true
	f.pageCap = 100
	r, err := c.Pages(bg, "", spaceA, "", 0, 10)
	if err != nil || len(r.Pages) != 5 || r.Pages[0].Name != "E" {
		t.Fatalf("%+v %v", r.Pages, err)
	}
}

func TestRecordsWithDottedKeysAreRead(t *testing.T) {
	c, f := clientFor(t)
	f.flatRecord = true
	id := f.add(spaceA, "Flat", "text")
	pg, err := c.Get(bg, "", spaceA, id)
	if err != nil || pg.Name != "Flat" || pg.Type != pageTypeID || pg.Markdown != "text" {
		t.Fatalf("%+v %v", pg, err)
	}
}

func TestSearchFindsPagesOnceEach(t *testing.T) {
	c, f := clientFor(t)
	f.add(spaceA, "Grace", "about grace")
	f.add(spaceA, "Mercy", "about mercy")
	gone := f.add(spaceA, "Gracious bin", "grace")
	f.objs[gone].Archived = true
	f.add(spaceB, "Grace elsewhere", "grace")
	r, err := c.Pages(bg, "", spaceA, "grace", 0, 10)
	if err != nil || r.Total != 1 || len(r.Pages) != 1 || r.Pages[0].Name != "Grace" || r.Pages[0].Snippet != "about grace" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestGetReturnsMarkdown(t *testing.T) {
	c, f := clientFor(t)
	id := f.add(spaceA, "Page", "# Heading\n\nBody *text*.")
	pg, err := c.Get(bg, "", spaceA, id)
	if err != nil || pg.Markdown != "# Heading\n\nBody *text*." || pg.Version == "" || pg.SpaceID != spaceA {
		t.Fatalf("%+v %v", pg, err)
	}
}

func TestMissingAndBinnedPagesAreNotFound(t *testing.T) {
	c, f := clientFor(t)
	if _, err := c.Get(bg, "", spaceA, "never-was"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	id := f.add(spaceA, "Binned", "x")
	f.objs[id].Archived = true
	if _, err := c.Get(bg, "", spaceA, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("binned: %v", err)
	}
	// A page of another space is not there either.
	other := f.add(spaceB, "Elsewhere", "x")
	if _, err := c.Get(bg, "", spaceA, other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other space: %v", err)
	}
}

func TestCreateUsesTheTypeFromTheTypesList(t *testing.T) {
	c, f := clientFor(t)
	pg, err := c.Create(bg, "", spaceA, "New page", "Some *text*")
	if err != nil || pg.ID == "" {
		t.Fatalf("%+v %v", pg, err)
	}
	o := f.objs[pg.ID]
	if o == nil || o.Name != "New page" || o.Markdown != "Some *text*" {
		t.Fatalf("%+v", o)
	}
	// The type list was asked once, and the answer kept for the session.
	if _, err := c.Create(bg, "", spaceA, "Second", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pages(bg, "", spaceA, "", 0, 10); err != nil {
		t.Fatal(err)
	}
	if f.typeCalls != 1 {
		t.Fatalf("types asked %d times", f.typeCalls)
	}
}

func TestPageTypeFallsBackToTheName(t *testing.T) {
	c, f := clientFor(t)
	f.noPageType = true
	typ, err := c.pageType(bg, "", spaceA)
	if err != nil || typ != "bafy-by-name" {
		t.Fatalf("%q %v", typ, err)
	}
}

func TestPageTypeIsPlainPageWhenTheListHasNone(t *testing.T) {
	c, f := clientFor(t)
	f.realTypes = true
	typ, err := c.pageType(bg, "", spaceA)
	if err != nil || typ != "page" {
		t.Fatalf("%q %v", typ, err)
	}
}

func TestRealRowShapeDatesAndBin(t *testing.T) {
	r := record{
		"id":         "x",
		"any":        map[string]any{"name": " Notes ", "type": "page", "collections": []any{}},
		"modifiedAt": map[string]any{"$date": "2026-11-28T10:00:00.123Z"},
	}
	if r.name() != "Notes" || r.typ() != "page" || r.modified() != "2026-11-28T10:00:00.123Z" || r.gone() {
		t.Fatalf("%q %q %q %v", r.name(), r.typ(), r.modified(), r.gone())
	}
	r["bin"] = map[string]any{"movedAt": map[string]any{"$date": "2026-11-28T11:00:00Z"}, "movedBy": "u"}
	if !r.gone() {
		t.Fatal("a row with a bin value is in the bin")
	}
}

func TestUpdateSendsTheVersionAndTurnsAConflictIntoAPlainError(t *testing.T) {
	c, f := clientFor(t)
	id := f.add(spaceA, "Page", "one")
	if err := c.Update(bg, "", spaceA, id, "two"); err != nil {
		t.Fatal(err)
	}
	if f.objs[id].Markdown != "two" {
		t.Fatalf("%+v", f.objs[id])
	}
	f.conflictNext = true
	err := c.Update(bg, "", spaceA, id, "three")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("%v", err)
	}
	if f.objs[id].Markdown != "two" {
		t.Fatal("a conflicting write went through")
	}
	if got := Explain(err); !strings.Contains(got, "changed in Any") || !strings.Contains(got, "Try again") {
		t.Fatalf("%q", got)
	}
	if err := c.Update(bg, "", spaceA, "never-was", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestErrorEnvelopeAndStatusMapping(t *testing.T) {
	for status, want := range map[int]error{401: ErrRefused, 403: ErrRefused, 404: ErrNotFound, 410: ErrNotFound, 429: ErrRateLimited} {
		if err := statusError(status, []byte(`{"error":{"code":"x","message":"m"}}`)); !errors.Is(err, want) {
			t.Errorf("%d: %v", status, err)
		}
	}
	if err := statusError(409, []byte(`{"error":{"code":"markdown.conflict","message":"m"}}`)); !errors.Is(err, ErrConflict) {
		t.Errorf("409: %v", err)
	}
	var ae *APIError
	err := statusError(409, []byte(`{"error":{"code":"object.ambiguous_id","message":"two match"}}`))
	if !errors.As(err, &ae) || ae.Code != "object.ambiguous_id" || !strings.Contains(err.Error(), "two match") {
		t.Errorf("other 409: %v", err)
	}
}

func TestNoKeyIsSentWhenNoneIsSaved(t *testing.T) {
	c, f := clientFor(t)
	id := f.add(spaceA, "P", "x")
	c.Spaces(bg, "")
	c.Pages(bg, "", spaceA, "", 0, 10)
	c.Pages(bg, "", spaceA, "x", 0, 10)
	c.Get(bg, "", spaceA, id)
	c.Create(bg, "", spaceA, "N", "y")
	c.Update(bg, "", spaceA, id, "z")
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, h := range f.hdrs {
		if h.Get("Authorization") != "" {
			t.Errorf("request %d carried a key", i)
		}
	}
	if len(f.hdrs) == 0 {
		t.Fatal("nothing was asked")
	}
}

func TestProbeRejectsAReplyThatIsNotAny(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"an Anytype-shaped list": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":[],"pagination":{}}`)) },
		"a web page":             func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`<html>hello</html>`)) },
		"a null list":            func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"spaces":null}`)) },
		"an error":               func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
	} {
		srv := httptest.NewServer(h)
		c, _ := NewClient(srv.URL, &http.Client{Transport: &http.Transport{DisableKeepAlives: true}})
		if _, err := c.Probe(bg); !errors.Is(err, ErrNotRunning) {
			t.Errorf("%s was taken for Any: %v", name, err)
		}
		srv.Close()
	}
	// An empty list of spaces is still Any.
	c, f := clientFor(t)
	f.spacesBody = map[string]any{"spaces": []any{}}
	if st, err := c.Probe(bg); err != nil || st != 200 {
		t.Fatalf("%d %v", st, err)
	}
}

// Glossa never asks Any to sign in, shut down, or delete anything.
func TestNeverCallsAuthShutdownOrDelete(t *testing.T) {
	c, f := clientFor(t)
	id := f.add(spaceA, "P", "x")
	c.Probe(bg)
	c.Spaces(bg, "")
	c.Pages(bg, "", spaceA, "", 0, 10)
	c.Pages(bg, "", spaceA, "x", 0, 10)
	c.Get(bg, "", spaceA, id)
	pg, _ := c.Create(bg, "", spaceA, "N", "y")
	c.Update(bg, "", spaceA, pg.ID, "z")
	for _, q := range f.seen() {
		method, path, _ := strings.Cut(q, " ")
		if method == "DELETE" || strings.Contains(path, "/auth") || strings.Contains(path, "/shutdown") {
			t.Errorf("forbidden request: %s", q)
		}
	}
	// And the client's code builds no such request: the only methods it names are these.
	src, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`"DELETE"`, "/shutdown", `"/v1/auth"`} {
		if strings.Contains(string(src), bad) {
			t.Errorf("client.go mentions %s", bad)
		}
	}
}
