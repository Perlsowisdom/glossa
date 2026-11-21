package ccel

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// The fake server stands in for ccel.org: a small "browse by title" page, ThML headers and EPUBs.
const indexPage = `<html><body><ul class="nav-list">
<p class="List2"><a href="/ccel/calvin/institutes.html">Institutes of the Christian Religion
</a><span class="authorName">(<a href="/ccel/calvin">John Calvin</a>)</span><b>(classic)</b></p>
<p class="List2"><a href="/ccel/henry/mhc1.html">Matthew Henry&#039;s Commentary on the Whole Bible, Vol. 1
</a><span class="authorName">(<a href="/ccel/henry">Matthew Henry</a>)</span></p>
<p class="List2"><a href="/ccel/spurgeon/morning.html">Morning and Evening
</a><span class="authorName">(<a href="/ccel/spurgeon">Charles Haddon Spurgeon</a>)</span></p>
<p class="List2"><a href="/ccel/modern/book.html">A Modern Book</a><span class="authorName">(<a href="/ccel/modern">Some Author</a>)</span></p>
<p class="List2"><a href="/contrib/cn">Christian Classics Ethereal Library in Chinese</a></p>
<p class="List2"><a href="/ccel/bible/kjv.html">King James Version</a></p>
<p class="List2"><a href="/ccel/calvin/sermons/">Three Volumes of Sermons</a></p>
</ul></body></html>`

func thml(rights string) string {
	return `<?xml version="1.0"?><ThML><ThML.head><generalInfo><DC.Title>X</DC.Title><DC.Rights>` + rights + `</DC.Rights></generalInfo></ThML.head>`
}

func epub(t *testing.T) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range [][2]string{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="c.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"c.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="u"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="u">ccel:institutes</dc:identifier><dc:title>Institutes of the Christian Religion</dc:title><dc:creator>John Calvin</dc:creator><dc:language>en</dc:language></metadata><manifest/><spine/></package>`},
	} {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e[1]))
	}
	zw.Close()
	return buf.Bytes()
}

type fake struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []string
	uas  map[string]bool
}

func newFake(t *testing.T) *fake {
	f := &fake{uas: map[string]bool{}}
	book := epub(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/index/title", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(indexPage)) })
	mux.HandleFunc("/ccel/c/calvin/institutes.xml", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(thml("Public Domain"))) })
	mux.HandleFunc("/ccel/h/henry/mhc1.xml", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(thml("Public Domain"))) })
	mux.HandleFunc("/ccel/m/modern/book.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(thml("Used by permission of Some Publisher. Copyright 1999.")))
	})
	mux.HandleFunc("/ccel/c/calvin/institutes/cache/institutes.epub", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", itoa(len(book)))
		if r.Method != http.MethodHead {
			w.Write(book)
		}
	})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.Method+" "+r.URL.Path)
		f.uas[r.Header.Get("User-Agent")] = true
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hits)
}

type harness struct {
	r    *command.Registry
	env  *command.Env
	f    *fake
	svc  *Service
	into string
	t    *testing.T
	now  time.Time
}

func setup(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &harness{f: newFake(t), into: t.TempDir(), t: t, now: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}
	h.r = command.NewRegistry()
	library.Register(h.r)
	h.svc = Register(h.r, t.TempDir(), WithBase(h.f.srv.URL), WithGap(0), WithClock(func() time.Time { return h.now }))
	h.env = &command.Env{Store: st}
	return h
}

func (h *harness) do(id string, params, out any) error {
	h.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := h.r.Run(context.Background(), h.env, id, raw)
	if err != nil {
		return err
	}
	if out != nil {
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, out); err != nil {
			h.t.Fatal(err)
		}
	}
	return nil
}

func (h *harness) must(id string, params, out any) {
	h.t.Helper()
	if err := h.do(id, params, out); err != nil {
		h.t.Fatalf("%s: %v", id, err)
	}
}

func (h *harness) on() {
	h.t.Helper()
	h.must("ccel.settings", map[string]any{"enable": true}, nil)
}

func TestParseIndex(t *testing.T) {
	got := parseIndex(indexPage)
	if len(got) != 4 {
		t.Fatalf("works: %+v", got)
	}
	w := got[0]
	if w.ID != "calvin/institutes" || w.Title != "Institutes of the Christian Religion" || w.Author != "John Calvin" || !w.Classic {
		t.Fatalf("first: %+v", w)
	}
	if got[1].Title != "Matthew Henry's Commentary on the Whole Bible, Vol. 1" || got[1].Classic {
		t.Fatalf("entities and marks: %+v", got[1])
	}
	for _, w := range got {
		if strings.HasPrefix(w.ID, "bible/") || strings.Contains(w.ID, "sermons") {
			t.Fatalf("should be left out: %+v", w)
		}
	}
	if kindOf(got[1].Title) != "commentary" || kindOf(got[0].Title) != "book" || kindOf("Easton's Bible Dictionary") != "dictionary" {
		t.Fatal("kinds")
	}
}

func TestOffByDefaultSendsNothing(t *testing.T) {
	h := setup(t)
	var s SettingsResult
	h.must("ccel.settings", struct{}{}, &s)
	if s.Enabled {
		t.Fatal("on by default")
	}
	if err := h.do("ccel.refresh", struct{}{}, nil); err == nil {
		t.Fatal("refresh worked while off")
	}
	if err := h.do("ccel.get", map[string]any{"id": "calvin/institutes", "preview": true}, nil); err == nil {
		t.Fatal("get worked while off")
	}
	var items ItemsResult
	h.must("ccel.items", struct{}{}, &items)
	if items.Enabled || len(items.Items) != 0 {
		t.Fatalf("items while off: %+v", items)
	}
	if n := h.f.count(); n != 0 {
		t.Fatalf("%d requests while off: %v", n, h.f.hits)
	}
}

func TestRefreshCachesForAWeek(t *testing.T) {
	h := setup(t)
	h.on()
	var r RefreshResult
	h.must("ccel.refresh", struct{}{}, &r)
	if !r.Fetched || r.Works != 4 || h.f.count() != 1 {
		t.Fatalf("first: %+v hits %v", r, h.f.hits)
	}
	h.now = h.now.Add(6 * 24 * time.Hour)
	h.must("ccel.refresh", struct{}{}, &r)
	if r.Fetched || h.f.count() != 1 {
		t.Fatalf("within a week: %+v hits %v", r, h.f.hits)
	}
	h.now = h.now.Add(25 * time.Hour)
	h.must("ccel.refresh", struct{}{}, &r)
	if !r.Fetched || h.f.count() != 2 {
		t.Fatalf("after a week: %+v hits %v", r, h.f.hits)
	}
	if !h.f.uas["Glossa"] || len(h.f.uas) != 1 {
		t.Fatalf("user agents: %v", h.f.uas)
	}
	// Turning it off keeps the cache but stops everything.
	h.must("ccel.settings", map[string]any{"enable": false}, nil)
	var items ItemsResult
	h.must("ccel.items", struct{}{}, &items)
	if len(items.Items) != 0 {
		t.Fatal("items while off")
	}
	h.must("ccel.settings", map[string]any{"enable": true}, nil)
	h.must("ccel.items", map[string]any{"text": "calvin"}, &items)
	if items.Total != 1 || items.Items[0].Kind != "book" {
		t.Fatalf("search: %+v", items)
	}
}

func TestPreviewChecksRightsAndSize(t *testing.T) {
	h := setup(t)
	h.on()
	h.must("ccel.refresh", struct{}{}, nil)
	var g GetResult
	h.must("ccel.get", map[string]any{"id": "calvin/institutes", "preview": true}, &g)
	if g.Rights != "Public Domain" || g.Size == 0 || g.Format != "EPUB" || g.Credit != Credit || g.Added {
		t.Fatalf("preview: %+v", g)
	}
	for _, p := range h.f.hits {
		if strings.HasSuffix(p, ".epub") && !strings.HasPrefix(p, "HEAD") {
			t.Fatalf("preview downloaded the book: %v", h.f.hits)
		}
	}
	// A book CCEL does not call public domain is refused, and its EPUB is never asked for.
	err := h.do("ccel.get", map[string]any{"id": "modern/book"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not public domain") {
		t.Fatalf("modern: %v", err)
	}
	for _, p := range h.f.hits {
		if strings.Contains(p, "modern/book/cache") {
			t.Fatalf("asked for the EPUB: %v", h.f.hits)
		}
	}
	if err := h.do("ccel.get", map[string]any{"id": "../etc/passwd"}, nil); err == nil {
		t.Fatal("bad id accepted")
	}
}

func TestGetAddsTheBookWithItsLicence(t *testing.T) {
	h := setup(t)
	h.on()
	h.must("ccel.refresh", struct{}{}, nil)
	var g GetResult
	h.must("ccel.get", map[string]any{"id": "calvin/institutes", "into": h.into}, &g)
	if !g.Added || g.ItemID == "" || !strings.Contains(g.Summary, "Christian Classics Ethereal Library") {
		t.Fatalf("get: %+v", g)
	}
	var item struct {
		Licence struct {
			Type        string `json:"type"`
			Attribution string `json:"attribution"`
			URL         string `json:"url"`
		} `json:"licence"`
		Title string `json:"title"`
	}
	h.must("library.item", map[string]any{"id": g.ItemID}, &item)
	if item.Licence.Type != "public_domain" || item.Licence.Attribution != Credit || !strings.HasSuffix(item.Licence.URL, "/ccel/calvin/institutes") {
		t.Fatalf("licence: %+v", item)
	}
	var items ItemsResult
	h.must("ccel.items", struct{}{}, &items)
	var found bool
	for _, it := range items.Items {
		if it.ID == "calvin/institutes" {
			found = it.Installed
		}
	}
	if !found {
		t.Fatalf("not marked installed: %+v", items)
	}
	// Getting it again adds nothing.
	g = GetResult{}
	h.must("ccel.get", map[string]any{"id": "calvin/institutes", "into": h.into}, &g)
	if g.Added || !strings.Contains(g.Summary, "already") {
		t.Fatalf("again: %+v", g)
	}
}

func TestCrawlDelayIsKept(t *testing.T) {
	h := setup(t)
	WithGap(60 * time.Millisecond)(h.svc)
	h.on()
	start := time.Now()
	h.must("ccel.refresh", struct{}{}, nil)
	h.must("ccel.get", map[string]any{"id": "calvin/institutes", "preview": true}, nil) // ThML header, then HEAD
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Fatalf("three requests in %v", d)
	}
}
