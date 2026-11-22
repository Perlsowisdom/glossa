package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect"
	"github.com/Perlsowisdom/glossa/internal/connect/csljson"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const (
	keyA      = "AAAAAAAA"
	keyB      = "BBBBBBBB"
	keyC      = "CCCCCCCC"
	secretKey = "web-key-do-not-leak"
)

func csl(id, title, family string, year int) string {
	return fmt.Sprintf(`{"id":%q,"type":"book","title":%q,"author":[{"family":%q,"given":"D."}],"issued":{"date-parts":[[%d]]},"volume":3}`, id, title, family, year)
}

// fakeZotero serves the part of Zotero's API Glossa uses, and records what it was asked.
type fakeZotero struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []*http.Request
	version int
	// behaviour
	status      int               // when set, every request gets this status
	headers     map[string]string // added to every response
	items       []string          // CSL-JSON items, in order
	rateLimited int               // this many requests get 429 first
	local       bool
}

func newFake(t *testing.T, local bool) *fakeZotero {
	f := &fakeZotero{t: t, local: local, version: 100, items: []string{
		csl("http://zotero.org/users/local/xyz/items/"+keyA, "Romans", "Moo", 1996),
		csl("123/"+keyB, "Galatians", "Longenecker", 1990),
		csl("123/"+keyC, "Ephesians", "Hoehner", 2002),
	}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeZotero) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func (f *fakeZotero) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeZotero) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	limited := f.rateLimited > 0
	if limited {
		f.rateLimited--
	}
	f.mu.Unlock()
	for k, v := range f.headers {
		w.Header().Set(k, v)
	}
	if limited {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/keys/") {
		if strings.TrimPrefix(r.URL.Path, "/keys/") != secretKey {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"key":"x","userID":4242,"username":"someone","access":{"user":{"library":true}}}`)
		return
	}
	prefix := "/users/4242"
	if f.local {
		prefix = "/api/users/0"
	}
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Zotero-API-Version") != "3" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !f.local && r.Header.Get("Zotero-API-Key") != secretKey {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.Header().Set("Last-Modified-Version", fmt.Sprint(f.version))
	if v := r.Header.Get("If-Modified-Since-Version"); v != "" && v == fmt.Sprint(f.version) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	q := r.URL.Query()
	switch {
	case rest == "/collections":
		w.Header().Set("Total-Results", "2")
		fmt.Fprint(w, `[{"key":"COLLAAAA","data":{"name":"Paul","parentCollection":false},"meta":{"numItems":2}},
			{"key":"COLLBBBB","data":{"name":"Romans","parentCollection":"COLLAAAA"},"meta":{"numItems":1}}]`)
	case strings.HasPrefix(rest, "/items/") && rest != "/items/top":
		key := strings.TrimPrefix(rest, "/items/")
		for _, it := range f.items {
			if strings.HasSuffix(strings.Split(it, `"`)[3], key) {
				fmt.Fprintf(w, `{"items":[%s]}`, it)
				return
			}
		}
		http.NotFound(w, r)
	default: // /items, /items/top, /collections/K/items/top
		if q.Get("format") != "csljson" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var list []string
		for _, it := range f.items {
			if qq := q.Get("q"); qq != "" && !strings.Contains(strings.ToLower(it), strings.ToLower(qq)) {
				continue
			}
			if ks := q.Get("itemKey"); ks != "" {
				ok := false
				for _, k := range strings.Split(ks, ",") {
					ok = ok || strings.Contains(it, k)
				}
				if !ok {
					continue
				}
			}
			list = append(list, it)
		}
		w.Header().Set("Total-Results", fmt.Sprint(len(list)))
		start, limit := atoi(q.Get("start")), atoi(q.Get("limit"))
		if start > len(list) {
			start = len(list)
		}
		end := min(start+limit, len(list))
		fmt.Fprintf(w, `{"items":[%s]}`, strings.Join(list[start:end], ","))
	}
}

func atoi(s string) int {
	var n int
	fmt.Sscan(s, &n)
	return n
}

// clock is a fake clock: sleeping moves it forward and is recorded.
type clock struct {
	mu     sync.Mutex
	t      time.Time
	slept  []time.Duration
	notice func(time.Duration)
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slept = append(c.slept, d)
	c.t = c.t.Add(d)
	return nil
}

type rig struct {
	t     *testing.T
	local *fakeZotero
	web   *fakeZotero
	conn  *Conn
	clk   *clock
	reg   *command.Registry
	env   *command.Env
	keys  *secrets.Memory
}

func newRig(t *testing.T) *rig {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &rig{t: t, local: newFake(t, true), web: newFake(t, false), clk: &clock{t: time.Unix(1000, 0)},
		env: &command.Env{Store: st}, keys: secrets.NewMemory(), reg: command.NewRegistry()}
	g.conn = New(WithLocalURL(g.local.srv.URL), WithWebURL(g.web.srv.URL), WithSleep(g.clk.sleep, g.clk.now))
	svc := connect.Register(g.reg, g.keys, g.conn)
	g.conn.Register(g.reg, svc)
	t.Cleanup(g.reg.Close)
	return g
}

func (g *rig) run(id string, params any, out any) error {
	g.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := g.reg.Run(context.Background(), g.env, id, raw)
	if err != nil {
		return err
	}
	if out != nil {
		b, _ := json.Marshal(res)
		return json.Unmarshal(b, out)
	}
	return nil
}

func (g *rig) on() {
	g.t.Helper()
	if err := g.run("connect.enable", connect.IDParams{ID: "zotero"}, nil); err != nil {
		g.t.Fatal(err)
	}
}

func (g *rig) setKey() {
	g.t.Helper()
	if err := g.run("connect.set_key", connect.SetKeyParams{ID: "zotero", Key: secretKey}, nil); err != nil {
		g.t.Fatal(err)
	}
}

func code(err error) string {
	var ce *command.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Client ----------------------------------------------------------------------------------------

func TestLocalPagingAndQueries(t *testing.T) {
	g := newRig(t)
	c := g.conn.local
	p, err := c.Items(context.Background(), ItemsQuery{Limit: 2})
	if err != nil || len(p.Items) != 2 || p.Total != 3 || p.NextStart != 2 || p.Version != 100 {
		t.Fatalf("%+v %v", p, err)
	}
	if p.Items[0].Key != keyA || p.Items[0].Item.ID != "zotero:"+keyA {
		t.Errorf("mapped id %+v", p.Items[0])
	}
	if r := g.local.last(); r.Header.Get("Zotero-API-Key") != "" || r.URL.Path != "/api/users/0/items/top" {
		t.Errorf("local request: %s key=%q", r.URL.Path, r.Header.Get("Zotero-API-Key"))
	}
	p, err = c.Items(context.Background(), ItemsQuery{Limit: 2, Start: 2})
	if err != nil || len(p.Items) != 1 || p.NextStart != 0 {
		t.Fatalf("last page %+v %v", p, err)
	}
	p, err = c.Items(context.Background(), ItemsQuery{Query: "galat", Sort: "title", Descending: true})
	if err != nil || len(p.Items) != 1 || p.Items[0].Key != keyB {
		t.Fatalf("search %+v %v", p, err)
	}
	if q := g.local.last().URL.Query(); q.Get("q") != "galat" || q.Get("sort") != "title" || q.Get("direction") != "desc" || q.Get("limit") != "100" {
		t.Errorf("query %v", q)
	}
	if _, err = c.Items(context.Background(), ItemsQuery{Collection: "COLLAAAA"}); err != nil || g.local.last().URL.Path != "/api/users/0/collections/COLLAAAA/items/top" {
		t.Errorf("collection path %s %v", g.local.last().URL.Path, err)
	}
	e, v, err := c.Item(context.Background(), keyC)
	if err != nil || e.Item.Title != "Ephesians" || v != 100 {
		t.Fatalf("%+v %d %v", e, v, err)
	}
	if _, _, err := c.Item(context.Background(), "ZZZZZZZZ"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing item: %v", err)
	}
}

func TestLinkNextWhenNoTotal(t *testing.T) {
	g := newRig(t)
	g.local.headers = map[string]string{"Link": `<http://x/next>; rel="next"`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<http://x/next>; rel="next"`)
		fmt.Fprint(w, `[`+csl("1/"+keyA, "T", "F", 2000)+`]`)
	}))
	defer srv.Close()
	c := NewLocal(srv.URL, g.conn.http)
	p, err := c.Items(context.Background(), ItemsQuery{Limit: 1})
	if err != nil || p.Total != -1 || p.NextStart != 1 {
		t.Errorf("%+v %v", p, err)
	}
}

func TestLocalOffAndNotRunning(t *testing.T) {
	g := newRig(t)
	g.local.status = http.StatusForbidden
	_, err := g.conn.local.Items(context.Background(), ItemsQuery{})
	if !errors.Is(err, ErrLocalOff) || !strings.Contains(Explain(err), "Advanced") {
		t.Fatalf("403: %v", err)
	}
	g.local.srv.Close()
	_, err = g.conn.local.Items(context.Background(), ItemsQuery{})
	if !errors.Is(err, ErrNotRunning) || !strings.Contains(Explain(err), "not running") {
		t.Fatalf("closed: %v", err)
	}
}

func TestBackoffAndRetryAfter(t *testing.T) {
	g := newRig(t)
	g.web.headers = map[string]string{"Backoff": "7"}
	wc := NewWeb(g.web.srv.URL, secretKey, "4242", g.conn.http)
	wc.sleep, wc.now = g.clk.sleep, g.clk.now
	for i := 0; i < 2; i++ {
		if _, err := wc.Items(context.Background(), ItemsQuery{Limit: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if len(g.clk.slept) != 1 || g.clk.slept[0] != 7*time.Second {
		t.Fatalf("the second request must wait out Backoff: %v", g.clk.slept)
	}

	// 429 with Retry-After: wait that long, then it works.
	g.web.headers = nil
	g.clk.slept = nil
	wc.backoffUntil = time.Time{}
	g.web.rateLimited = 1
	if _, err := wc.Items(context.Background(), ItemsQuery{Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if len(g.clk.slept) != 1 || g.clk.slept[0] != 3*time.Second {
		t.Errorf("Retry-After: %v", g.clk.slept)
	}

	// Still limited after the retries: give up with a plain error.
	g.clk.slept = nil
	g.web.rateLimited = 100
	_, err := wc.Items(context.Background(), ItemsQuery{Limit: 1})
	if !errors.Is(err, ErrRateLimited) || len(g.clk.slept) != maxRetries {
		t.Errorf("gave up: %v slept=%v", err, g.clk.slept)
	}

	// A long Backoff is not waited out; the person is told to try later.
	g.clk.slept = nil
	g.web.rateLimited = 0
	g.web.headers = map[string]string{"Backoff": "600"}
	wc.Items(context.Background(), ItemsQuery{Limit: 1})
	_, err = wc.Items(context.Background(), ItemsQuery{Limit: 1})
	if !errors.Is(err, ErrRateLimited) || len(g.clk.slept) != 0 {
		t.Errorf("long backoff: %v slept=%v", err, g.clk.slept)
	}
}

func TestNotModified(t *testing.T) {
	g := newRig(t)
	p, err := g.conn.local.Items(context.Background(), ItemsQuery{Keys: []string{keyA}, SinceVersion: 100})
	if err != nil || !p.NotModified || p.Version != 100 || len(p.Items) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	if g.local.last().Header.Get("If-Modified-Since-Version") != "100" {
		t.Error("header not sent")
	}
	p, err = g.conn.local.Items(context.Background(), ItemsQuery{Keys: []string{keyA}, SinceVersion: 90})
	if err != nil || p.NotModified || len(p.Items) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestCollections(t *testing.T) {
	g := newRig(t)
	cols, err := g.conn.local.Collections(context.Background())
	if err != nil || len(cols) != 2 || cols[0].Parent != "" || cols[1].Parent != "COLLAAAA" || cols[1].Name != "Romans" || cols[0].Items != 2 {
		t.Fatalf("%+v %v", cols, err)
	}
}

func TestLookupUser(t *testing.T) {
	g := newRig(t)
	id, err := LookupUser(context.Background(), g.web.srv.URL, secretKey, g.conn.http)
	if err != nil || id != "4242" {
		t.Fatalf("%q %v", id, err)
	}
	if g.web.last().Header.Get("Zotero-API-Key") != secretKey {
		t.Error("key header")
	}
	if _, err := LookupUser(context.Background(), g.web.srv.URL, "nope", g.conn.http); !errors.Is(err, ErrBadKey) {
		t.Errorf("bad key: %v", err)
	}
}

// Mapping ---------------------------------------------------------------------------------------

func TestMapAndSummarize(t *testing.T) {
	items, err := csljson.Parse([]byte(`[{"id":"http://zotero.org/users/local/abc/items/ABCD2345","type":"chapter","title":" Law ",
	  "container-title":"Dictionary","author":[{"family":"Moo","given":"D."},{"family":"Wright","given":"N."},{"literal":"IVP"}],
	  "issued":{"date-parts":[["2001","3"]]},"page":"10-12","volume":2,"number-of-volumes":"4"}]`))
	if err != nil {
		t.Fatal(err)
	}
	e := Map(items[0])
	if e.Key != "ABCD2345" || e.Item.ID != "zotero:ABCD2345" || e.Item.Title != "Law" || e.Item.Volume != "2" || e.Item.NumberOfVolumes != "4" {
		t.Errorf("%+v", e)
	}
	s := Summarize(e)
	if s.Authors != "Moo et al." || s.Year != 2001 || s.Where != "Dictionary" || s.Type != "chapter" {
		t.Errorf("%+v", s)
	}
	if got := authors(items[0].Author[:2]); got != "Moo and Wright" {
		t.Errorf("two authors: %q", got)
	}
	if Map(Map(items[0]).Item).Key != "ABCD2345" {
		t.Error("mapping twice must not change the key")
	}
	if Summarize(Entry{Key: "X"}).Title != "(untitled)" {
		t.Error("untitled")
	}
}

// Connection and commands -----------------------------------------------------------------------

func TestOffSendsNothing(t *testing.T) {
	g := newRig(t)
	for _, c := range []struct {
		id string
		p  any
	}{
		{"zotero.items", ItemsParams{}}, {"zotero.search", SearchParams{Query: "x"}}, {"zotero.collections", struct{}{}},
		{"zotero.refresh", struct{}{}}, {"connect.test", connect.IDParams{ID: "zotero"}},
	} {
		if err := g.run(c.id, c.p, nil); code(err) != "off" {
			t.Errorf("%s while off: %v", c.id, err)
		}
	}
	if err := g.run("zotero.item", KeyParams{Key: keyA}, nil); code(err) != "off" {
		t.Errorf("item while off and not kept: %v", err)
	}
	if g.local.count()+g.web.count() != 0 {
		t.Error("a request was sent while the connection was off")
	}
}

func TestBrowseThroughTheApp(t *testing.T) {
	g := newRig(t)
	g.on()
	var items ItemsResult
	if err := g.run("zotero.items", ItemsParams{Limit: 2}, &items); err != nil {
		t.Fatal(err)
	}
	if len(items.Items) != 2 || items.NextStart != 2 || items.Via != viaApp || items.Items[0].Title != "Romans" || items.Items[0].Authors != "Moo" {
		t.Errorf("%+v", items)
	}
	var found ItemsResult
	if err := g.run("zotero.search", SearchParams{Query: "ephes"}, &found); err != nil || len(found.Items) != 1 || found.Items[0].Key != keyC {
		t.Errorf("%+v %v", found, err)
	}
	var cols CollectionsResult
	if err := g.run("zotero.collections", struct{}{}, &cols); err != nil || len(cols.Collections) != 2 {
		t.Errorf("%+v %v", cols, err)
	}
	var it ItemResult
	if err := g.run("zotero.item", KeyParams{Key: keyB}, &it); err != nil || it.CSL.Title != "Galatians" || it.FromCache {
		t.Errorf("%+v %v", it, err)
	}
	if err := g.run("zotero.item", KeyParams{Key: "../etc"}, nil); code(err) != "bad_params" {
		t.Errorf("bad key: %v", err)
	}
	if err := g.run("zotero.items", ItemsParams{Collection: "a/b"}, nil); code(err) != "bad_params" {
		t.Errorf("bad collection: %v", err)
	}
	if err := g.run("zotero.item", KeyParams{Key: "ZZZZZZZZ"}, nil); code(err) != "not_found" {
		t.Errorf("missing: %v", err)
	}
	if g.web.count() != 0 {
		t.Error("the web must not be used when the app answers")
	}
}

func TestLocalOffFallsBackToWebWithKey(t *testing.T) {
	g := newRig(t)
	g.on()
	g.local.status = http.StatusForbidden
	err := g.run("zotero.items", ItemsParams{}, nil)
	if code(err) != "unavailable" || !strings.Contains(err.Error(), "Advanced") {
		t.Fatalf("without a key: %v", err)
	}
	if g.web.count() != 0 {
		t.Fatal("no key, so zotero.org must not be asked")
	}

	g.setKey()
	var items ItemsResult
	if err := g.run("zotero.items", ItemsParams{}, &items); err != nil || items.Via != viaWeb || len(items.Items) != 3 {
		t.Fatalf("%+v %v", items, err)
	}
	r := g.web.last()
	if r.URL.Path != "/users/4242/items/top" || r.Header.Get("Zotero-API-Key") != secretKey || r.Header.Get("Zotero-API-Version") != "3" {
		t.Errorf("web request: %s %v", r.URL.Path, r.Header)
	}
	n := g.web.count()
	g.run("zotero.collections", struct{}{}, nil)
	for _, r := range g.web.reqs[n:] {
		if strings.HasPrefix(r.URL.Path, "/keys/") {
			t.Error("the key must be looked up once")
		}
	}
	var res connect.TestOutput
	if err := g.run("connect.test", connect.IDParams{ID: "zotero"}, &res); err != nil || !res.OK || res.Via != viaWeb || !strings.Contains(res.Message, "zotero.org") {
		t.Errorf("%+v %v", res, err)
	}
}

func TestNotRunningMessageAndBadKey(t *testing.T) {
	g := newRig(t)
	g.on()
	g.local.srv.Close()
	var res connect.TestOutput
	if err := g.run("connect.test", connect.IDParams{ID: "zotero"}, &res); err != nil || res.OK || !strings.Contains(res.Message, "not running") {
		t.Fatalf("%+v %v", res, err)
	}
	if err := g.run("connect.set_key", connect.SetKeyParams{ID: "zotero", Key: "wrong-key"}, nil); err != nil {
		t.Fatal(err)
	}
	err := g.run("zotero.items", ItemsParams{}, nil)
	if code(err) != "unavailable" || !strings.Contains(err.Error(), "did not accept this key") {
		t.Errorf("%v", err)
	}
	if strings.Contains(fmt.Sprint(err), "wrong-key") {
		t.Error("the key is in an error")
	}
	g.web.srv.Close()
	g.setKey()
	err = g.run("zotero.items", ItemsParams{}, nil)
	if err == nil || strings.Contains(err.Error(), secretKey) {
		t.Errorf("web unreachable must fail without the key: %v", err)
	}
}

func TestTestResultOnTheApp(t *testing.T) {
	g := newRig(t)
	g.on()
	var res connect.TestOutput
	if err := g.run("connect.test", connect.IDParams{ID: "zotero"}, &res); err != nil || !res.OK || res.Via != viaApp {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestCiteKeepsACopyForOffline(t *testing.T) {
	g := newRig(t)
	g.on()
	var it ItemResult
	if err := g.run("zotero.cite", KeyParams{Key: keyA}, &it); err != nil || it.FromCache {
		t.Fatalf("%+v %v", it, err)
	}
	var kept CachedResult
	if err := g.run("zotero.cached", struct{}{}, &kept); err != nil || len(kept.Items) != 1 || kept.Items[0].Key != keyA || kept.Items[0].CSL.ID != "zotero:"+keyA || kept.Items[0].Title != "Romans" {
		t.Fatalf("%+v %v", kept, err)
	}
	var version int
	g.env.Store.QueryRow(context.Background(), `SELECT library_version FROM cite_items`).Scan(&version)
	if version != 100 {
		t.Errorf("library version kept: %d", version)
	}

	// Zotero goes away: the kept copy still answers, and so does it with the connection off.
	g.local.srv.Close()
	if err := g.run("zotero.item", KeyParams{Key: keyA}, &it); err != nil || !it.FromCache || it.CSL.Title != "Romans" {
		t.Fatalf("offline: %+v %v", it, err)
	}
	if err := g.run("zotero.item", KeyParams{Key: keyB}, nil); code(err) != "unavailable" {
		t.Errorf("not kept and offline: %v", err)
	}
	g.run("connect.disable", connect.IDParams{ID: "zotero"}, nil)
	if err := g.run("zotero.item", KeyParams{Key: keyA}, &it); err != nil || !it.FromCache {
		t.Fatalf("off: %+v %v", it, err)
	}
	if err := g.run("zotero.cached", struct{}{}, &kept); err != nil || len(kept.Items) != 1 {
		t.Errorf("cached while off: %v", err)
	}
	// The kept copy is a logged change, so it reaches other devices.
	var n int
	g.env.Store.QueryRow(context.Background(), `SELECT COUNT(*) FROM changes WHERE object_type = 'cite_items'`).Scan(&n)
	if n == 0 {
		t.Error("the kept copy was not logged")
	}
}

func TestRefreshAsksOnlyWhetherChanged(t *testing.T) {
	g := newRig(t)
	g.on()
	for _, k := range []string{keyA, keyB} {
		if err := g.run("zotero.cite", KeyParams{Key: k}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var res RefreshResult
	if err := g.run("zotero.refresh", struct{}{}, &res); err != nil || res.Kept != 2 || res.Unchanged != 2 || res.Updated != 0 {
		t.Fatalf("nothing changed: %+v %v", res, err)
	}
	if g.local.last().Header.Get("If-Modified-Since-Version") != "100" {
		t.Error("the version was not sent")
	}

	// The library moves on and one title changes: it is updated, the other is not.
	g.local.version = 101
	g.local.items[0] = csl("http://zotero.org/users/local/xyz/items/"+keyA, "Romans, second edition", "Moo", 2018)
	if err := g.run("zotero.refresh", struct{}{}, &res); err != nil || res.Updated != 1 || res.Unchanged != 1 {
		t.Fatalf("one changed: %+v %v", res, err)
	}
	var kept CachedResult
	g.run("zotero.cached", struct{}{}, &kept)
	titles := kept.Items[0].Title + "|" + kept.Items[1].Title
	if !strings.Contains(titles, "second edition") {
		t.Errorf("titles %s", titles)
	}

	// An item removed from Zotero stays in the kept copies.
	g.local.version = 102
	g.local.items = g.local.items[:1]
	if err := g.run("zotero.refresh", struct{}{}, &res); err != nil || res.Missing != 1 {
		t.Fatalf("missing: %+v %v", res, err)
	}
	g.run("zotero.cached", struct{}{}, &kept)
	if len(kept.Items) != 2 {
		t.Errorf("kept %d", len(kept.Items))
	}
}

func TestKeyNeverInResults(t *testing.T) {
	g := newRig(t)
	g.on()
	g.setKey()
	g.local.status = http.StatusForbidden
	var all strings.Builder
	for _, c := range []struct {
		id string
		p  any
	}{{"connect.list", nil}, {"zotero.items", ItemsParams{}}, {"connect.test", connect.IDParams{ID: "zotero"}}, {"zotero.cite", KeyParams{Key: keyA}}} {
		raw, _ := json.Marshal(c.p)
		res, err := g.reg.Run(context.Background(), g.env, c.id, raw)
		b, _ := json.Marshal(res)
		fmt.Fprintf(&all, "%s %v", b, err)
	}
	if strings.Contains(all.String(), secretKey) {
		t.Error("the key appeared in a result")
	}
}

func TestEnvironmentPointsElsewhere(t *testing.T) {
	g := newRig(t)
	t.Setenv(LocalURLEnv, g.local.srv.URL)
	t.Setenv(WebURLEnv, g.web.srv.URL)
	c := New()
	defer c.Close()
	if c.localURL != g.local.srv.URL || c.webURL != g.web.srv.URL || c.local.Root != g.local.srv.URL {
		t.Errorf("%s %s", c.localURL, c.webURL)
	}
	if New().Sends() == "" || len(New().Capabilities()) == 0 {
		t.Error("the connection must say what it sends")
	}
}
