package gutenberg

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// A small hand-made catalogue in Project Gutenberg's pg_catalog.csv format: quoted fields, commas
// and semicolons inside them, a sound recording, a text with no title and a non-numeric number.
const catalogueCSV = `Text#,Type,Issued,Title,Language,Authors,Subjects,LoCC,Bookshelves
45001,Text,2014-01-02,"Institutes of the Christian Religion, Volume 1",en,"Calvin, Jean, 1509-1564",Theology,BX,Christianity
45002,Text,2010-05-06,The Pilgrim's Progress,en,"Bunyan, John, 1628-1688",Allegories,PR,Best Books Ever Listings
45003,Sound,2012-03-03,Morning and Evening (audio),en,"Spurgeon, C. H. (Charles Haddon), 1834-1892",Devotion,BV,
45004,Text,2001-01-01,Der Römerbrief,de,"Barth, Karl, 1886-1968",Theology,BX,
45005,Text,2005-05-05,"Commentary on Romans; Notes",en,"Henry, Matthew, 1662-1714; Scott, Thomas, 1747-1821",Bible,BS,
45006,Text,2005-05-05,,en,"Nobody, A.",,,
abc,Text,2005-05-05,Not a number,en,,,,
`

func gz(t *testing.T, s string) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(s))
	zw.Close()
	return buf.Bytes()
}

func epub() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range [][2]string{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="c.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"c.opf", `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="u"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="u">pg45002</dc:identifier><dc:title>The Pilgrim's Progress</dc:title><dc:creator>John Bunyan</dc:creator><dc:language>en</dc:language></metadata><manifest/><spine/></package>`},
	} {
		w, _ := zw.Create(e[0])
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
	cat, book := gz(t, catalogueCSV), epub()
	mux := http.NewServeMux()
	mux.HandleFunc("/cache/epub/feeds/pg_catalog.csv.gz", func(w http.ResponseWriter, r *http.Request) { w.Write(cat) })
	mux.HandleFunc("/cache/epub/45002/pg45002-images-3.epub", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(book)))
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
	h := &harness{f: newFake(t), into: t.TempDir(), t: t, now: time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)}
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
	h.must("gutenberg.settings", map[string]any{"enable": true}, nil)
}

func TestParseCatalogue(t *testing.T) {
	got, err := parseCatalogue(bytes.NewReader(gz(t, catalogueCSV)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("works: %+v", got)
	}
	w := got[0]
	if w.ID != "45001" || w.Title != "Institutes of the Christian Religion, Volume 1" || w.Author != "Jean Calvin" || w.Language != "en" {
		t.Fatalf("first: %+v", w)
	}
	if got[1].Author != "John Bunyan" || got[2].Language != "de" || got[2].Author != "Karl Barth" {
		t.Fatalf("authors and languages: %+v", got)
	}
	if got[3].Author != "Matthew Henry; Thomas Scott" {
		t.Fatalf("two authors: %+v", got[3])
	}
	for _, w := range got {
		if strings.Contains(w.Title, "audio") || w.ID == "abc" || w.Title == "" {
			t.Fatalf("should be left out: %+v", w)
		}
	}
	if _, err := parseCatalogue(strings.NewReader("not gzip")); err == nil {
		t.Fatal("not gzip accepted")
	}
	if _, err := parseCatalogue(bytes.NewReader(gz(t, "A,B\n1,2\n"))); err == nil {
		t.Fatal("a file without the columns accepted")
	}
}

func TestCleanAuthor(t *testing.T) {
	for in, want := range map[string]string{
		"Calvin, Jean, 1509-1564":  "Jean Calvin",
		"Anonymous":                "Anonymous",
		"Plato, 427? BCE-347? BCE": "Plato",
		"Henry, Matthew, 1662-1714; Scott, Thomas, 1747-1821": "Matthew Henry; Thomas Scott",
	} {
		if got := cleanAuthor(in); got != want {
			t.Errorf("cleanAuthor(%q) = %q, want %q", in, got, want)
		}
	}
}

// Nothing is sent while the switch is off.
func TestOffByDefaultSendsNothing(t *testing.T) {
	h := setup(t)
	var s SettingsResult
	h.must("gutenberg.settings", struct{}{}, &s)
	if s.Enabled {
		t.Fatal("on by default")
	}
	if err := h.do("gutenberg.refresh", struct{}{}, nil); err == nil {
		t.Fatal("refresh worked while off")
	}
	if err := h.do("gutenberg.get", map[string]any{"id": "45002", "preview": true}, nil); err == nil {
		t.Fatal("get worked while off")
	}
	var items ItemsResult
	h.must("gutenberg.items", struct{}{}, &items)
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
	h.must("gutenberg.refresh", struct{}{}, &r)
	if !r.Fetched || r.Works != 4 || h.f.count() != 1 {
		t.Fatalf("first: %+v hits %v", r, h.f.hits)
	}
	h.now = h.now.Add(6 * 24 * time.Hour)
	h.must("gutenberg.refresh", struct{}{}, &r)
	if r.Fetched || h.f.count() != 1 {
		t.Fatalf("within a week: %+v hits %v", r, h.f.hits)
	}
	h.now = h.now.Add(25 * time.Hour)
	h.must("gutenberg.refresh", struct{}{}, &r)
	if !r.Fetched || h.f.count() != 2 {
		t.Fatalf("after a week: %+v hits %v", r, h.f.hits)
	}
	if !h.f.uas["Glossa"] || len(h.f.uas) != 1 {
		t.Fatalf("user agents: %v", h.f.uas)
	}
	h.must("gutenberg.settings", map[string]any{"enable": false}, nil)
	var items ItemsResult
	h.must("gutenberg.items", struct{}{}, &items)
	if len(items.Items) != 0 {
		t.Fatal("items while off")
	}
	h.must("gutenberg.settings", map[string]any{"enable": true}, nil)
	h.must("gutenberg.items", map[string]any{"text": "bunyan"}, &items)
	if items.Total != 1 || items.Items[0].ID != "45002" {
		t.Fatalf("search: %+v", items)
	}
}

func TestPreviewThenGetAddsTheBook(t *testing.T) {
	h := setup(t)
	h.on()
	h.must("gutenberg.refresh", struct{}{}, nil)
	var g GetResult
	h.must("gutenberg.get", map[string]any{"id": "45002", "preview": true}, &g)
	if g.Size == 0 || g.Format != "EPUB" || g.Credit != Credit || g.Licence != "Public domain (US)" || g.Added {
		t.Fatalf("preview: %+v", g)
	}
	for _, p := range h.f.hits {
		if strings.HasSuffix(p, ".epub") && !strings.HasPrefix(p, "HEAD") {
			t.Fatalf("preview downloaded the book: %v", h.f.hits)
		}
	}
	if err := h.do("gutenberg.get", map[string]any{"id": "../etc/passwd"}, nil); err == nil {
		t.Fatal("bad id accepted")
	}
	if err := h.do("gutenberg.get", map[string]any{"id": "99999"}, nil); err == nil {
		t.Fatal("a book not in the list accepted")
	}
	h.must("gutenberg.get", map[string]any{"id": "45002", "into": h.into}, &g)
	if !g.Added || g.ItemID == "" || !strings.Contains(g.Summary, "Project Gutenberg") {
		t.Fatalf("get: %+v", g)
	}
	var item struct {
		Licence struct {
			Type        string `json:"type"`
			Attribution string `json:"attribution"`
			URL         string `json:"url"`
		} `json:"licence"`
	}
	h.must("library.item", map[string]any{"id": g.ItemID}, &item)
	if item.Licence.Type != "public_domain" || item.Licence.Attribution != Credit || !strings.HasSuffix(item.Licence.URL, "/ebooks/45002") {
		t.Fatalf("licence: %+v", item)
	}
	var items ItemsResult
	h.must("gutenberg.items", map[string]any{"text": "pilgrim"}, &items)
	if len(items.Items) != 1 || !items.Items[0].Installed {
		t.Fatalf("not marked installed: %+v", items)
	}
	g = GetResult{}
	h.must("gutenberg.get", map[string]any{"id": "45002", "into": h.into}, &g)
	if g.Added || !strings.Contains(g.Summary, "already") {
		t.Fatalf("again: %+v", g)
	}
}

func TestPauseBetweenRequestsIsKept(t *testing.T) {
	h := setup(t)
	WithGap(60 * time.Millisecond)(h.svc)
	h.on()
	start := time.Now()
	h.must("gutenberg.refresh", struct{}{}, nil)
	h.must("gutenberg.get", map[string]any{"id": "45002", "preview": true}, nil)
	if d := time.Since(start); d < 50*time.Millisecond {
		t.Fatalf("two requests in %v", d)
	}
}
