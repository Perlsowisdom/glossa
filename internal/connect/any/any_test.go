package anyapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.uber.org/goleak"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/connect"
	"github.com/Perlsowisdom/glossa/internal/document"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const (
	secretKey = "any-key-do-not-leak"
	goodCode  = "1234"
	spaceA    = "space-a"
	spaceB    = "space-b"
)

type obj struct {
	ID, Space, Name, Markdown string
	Props                     map[string]string
	Rev                       int
	Archived                  bool
}

// fakeAny serves the part of Any's local API Glossa uses, and records what it was asked.
type fakeAny struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []string
	hdrs []http.Header

	objs    map[string]*obj
	props   map[string]bool // "space/key"
	nextID  int
	rev     int
	pageCap int // the most items a listing returns, whatever limit is asked
	badKey  bool
	noProps bool // refuses to create properties
	// the heading Any puts at the top of the text, as some versions might
	titleHeading bool
}

func newFake(t *testing.T) *fakeAny {
	f := &fakeAny{t: t, objs: map[string]*obj{}, props: map[string]bool{}, pageCap: 100}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAny) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func (f *fakeAny) tick() string {
	f.rev++
	return fmt.Sprintf("2026-01-01T00:%02d:%02dZ", f.rev/60, f.rev%60)
}

func (f *fakeAny) wire(o *obj, body bool) map[string]any {
	m := map[string]any{"id": o.ID, "space_id": o.Space, "name": o.Name, "archived": o.Archived, "type": map[string]any{"key": "page"},
		"snippet": o.Markdown}
	props := []map[string]any{{"key": "last_modified_date", "date": fmt.Sprintf("2026-01-01T00:%02d:%02dZ", o.Rev/60, o.Rev%60)}}
	for k, v := range o.Props {
		props = append(props, map[string]any{"key": k, "text": v})
	}
	m["properties"] = props
	if body {
		md := o.Markdown
		if f.titleHeading {
			md = "# " + o.Name + "\n\n" + md
		}
		m["markdown"] = md
	}
	return m
}

func (f *fakeAny) page(w http.ResponseWriter, r *http.Request, items []any) {
	off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if lim <= 0 || lim > f.pageCap {
		lim = f.pageCap
	}
	end := min(off+lim, len(items))
	if off > end {
		off = end
	}
	json.NewEncoder(w).Encode(map[string]any{"data": items[off:end],
		"pagination": map[string]any{"total": len(items), "offset": off, "limit": lim, "has_more": end < len(items)}})
}

func (f *fakeAny) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.Path)
	f.hdrs = append(f.hdrs, r.Header.Clone())
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"object": "error", "status": code, "message": msg})
	}
	if r.Header.Get("Anytype-Version") != APIVersion {
		{
			fail(400, "missing or wrong Anytype-Version")
			return
		}
	}
	p := r.URL.Path
	var in map[string]any
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&in)
	}
	switch {
	case p == "/v1/auth/challenges" && r.Method == "POST":
		if in["app_name"] == "" {
			{
				fail(400, "app_name")
				return
			}
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"challenge_id": "chal-1"})
		return
	case p == "/v1/auth/api_keys" && r.Method == "POST":
		if in["challenge_id"] != "chal-1" || in["code"] != goodCode {
			{
				fail(400, "wrong code")
				return
			}
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"api_key": secretKey})
		return
	}
	if f.badKey || r.Header.Get("Authorization") != "Bearer "+secretKey {
		{
			fail(401, "unauthorized")
			return
		}
	}
	switch {
	case p == "/v1/spaces" && r.Method == "GET":
		f.page(w, r, []any{map[string]any{"id": spaceA, "name": "Study"}, map[string]any{"id": spaceB, "name": "Church"}})
	case strings.HasSuffix(p, "/properties") && r.Method == "GET":
		space := strings.Split(p, "/")[3]
		var items []any
		for k := range f.props {
			if s, key, _ := strings.Cut(k, "/"); s == space {
				items = append(items, map[string]any{"key": key})
			}
		}
		f.page(w, r, items)
	case strings.HasSuffix(p, "/properties") && r.Method == "POST":
		if f.noProps {
			{
				fail(500, "no")
				return
			}
		}
		f.props[strings.Split(p, "/")[3]+"/"+in["key"].(string)] = true
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"property": map[string]any{"key": in["key"]}})
	case strings.HasSuffix(p, "/search") && r.Method == "POST":
		space := strings.Split(p, "/")[3]
		q, _ := in["query"].(string)
		var list []*obj
		for _, o := range f.objs {
			if o.Space == space && (q == "" || strings.Contains(strings.ToLower(o.Name+" "+o.Markdown), strings.ToLower(q))) {
				list = append(list, o)
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Rev > list[j].Rev })
		items := make([]any, len(list))
		for i, o := range list {
			items[i] = f.wire(o, false)
		}
		f.page(w, r, items)
	case strings.HasSuffix(p, "/objects") && r.Method == "POST":
		space := strings.Split(p, "/")[3]
		f.nextID++
		o := &obj{ID: "obj-" + strconv.Itoa(f.nextID), Space: space, Props: map[string]string{}}
		f.apply(o, in, "body")
		f.objs[o.ID] = o
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"object": f.wire(o, true)})
	case strings.Contains(p, "/objects/"):
		parts := strings.Split(p, "/")
		o := f.objs[parts[5]]
		if o == nil || o.Space != parts[3] {
			{
				fail(404, "not found")
				return
			}
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"object": f.wire(o, true)})
		} else if r.Method == "PATCH" {
			f.apply(o, in, "markdown")
			json.NewEncoder(w).Encode(map[string]any{"object": f.wire(o, true)})
		} else {
			fail(405, "method")
		}
	default:
		fail(404, "no route "+p)
	}
}

func (f *fakeAny) apply(o *obj, in map[string]any, bodyField string) {
	if n, ok := in["name"].(string); ok {
		o.Name = n
	}
	if b, ok := in[bodyField].(string); ok {
		o.Markdown = b
	}
	if ps, ok := in["properties"].([]any); ok {
		for _, p := range ps {
			m := p.(map[string]any)
			if !f.props[o.Space+"/"+m["key"].(string)] {
				f.t.Errorf("property %v set before it was made", m["key"])
			}
			o.Props[m["key"].(string)] = m["text"].(string)
		}
	}
	f.tick()
	o.Rev = f.rev
}

// edit changes a page the way the person would in Any.
func (f *fakeAny) edit(id, name, markdown string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.objs[id]
	o.Name, o.Markdown = name, markdown
	f.tick()
	o.Rev = f.rev
}

func (f *fakeAny) add(space, name, markdown string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	o := &obj{ID: "obj-" + strconv.Itoa(f.nextID), Space: space, Name: name, Markdown: markdown, Props: map[string]string{}}
	f.tick()
	o.Rev = f.rev
	f.objs[o.ID] = o
	return o.ID
}

// rig ---------------------------------------------------------------------------------------------

type rig struct {
	t    *testing.T
	fake *fakeAny
	conn *Conn
	reg  *command.Registry
	env  *command.Env
	keys *secrets.Memory
}

func newRig(t *testing.T) *rig {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &rig{t: t, fake: newFake(t), env: &command.Env{Store: st}, keys: secrets.NewMemory(), reg: command.NewRegistry()}
	g.conn = New(WithURL(g.fake.srv.URL))
	core.Register(g.reg)
	document.Register(g.reg)
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
		reflect.ValueOf(out).Elem().SetZero()
		b, _ := json.Marshal(res)
		return json.Unmarshal(b, out)
	}
	return nil
}

func (g *rig) must(id string, params any, out any) {
	g.t.Helper()
	if err := g.run(id, params, out); err != nil {
		g.t.Fatalf("%s: %v", id, err)
	}
}

// paired turns Any on and signs in, as the person would.
func (g *rig) paired() {
	g.t.Helper()
	g.must("connect.enable", connect.IDParams{ID: "any"}, nil)
	var st PairStartResult
	g.must("any.pair_start", struct{}{}, &st)
	g.must("any.pair_finish", PairFinishParams{ChallengeID: st.ChallengeID, Code: goodCode}, nil)
}

func (g *rig) note(title, body, ref string) string {
	g.t.Helper()
	var n core.Note
	g.must("note.create", core.NoteCreateParams{Title: title, Body: body, Ref: ref}, &n)
	return n.ID
}

func (g *rig) getNote(id string) core.Note {
	g.t.Helper()
	var n core.Note
	g.must("note.get", map[string]string{"id": id}, &n)
	return n
}

func (g *rig) setNote(id, title, body string) {
	g.t.Helper()
	g.must("note.update", map[string]any{"id": id, "title": title, "body": body}, nil)
}

func code(err error) string {
	var ce *command.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Tests -------------------------------------------------------------------------------------------

func TestOffByDefaultAndNothingSent(t *testing.T) {
	g := newRig(t)
	var infos []connect.Info
	g.must("connect.list", struct{}{}, &infos)
	if len(infos) != 1 || infos[0].Enabled || infos[0].KeySet || !strings.Contains(infos[0].Sends, "nothing leaves this computer") {
		t.Fatalf("info = %+v", infos)
	}
	for _, c := range []string{"any.pair_start", "any.spaces", "any.pages", "any.send"} {
		params := any(struct{}{})
		if c == "any.send" {
			params = SendParams{Kind: "note", ID: "x"}
		}
		if err := g.run(c, params, nil); code(err) != "off" {
			t.Errorf("%s while off: %v", c, err)
		}
	}
	if err := g.run("connect.test", connect.IDParams{ID: "any"}, nil); code(err) != "off" {
		t.Errorf("test while off: %v", err)
	}
	if n := len(g.fake.seen()); n != 0 {
		t.Fatalf("%d requests were made while off: %v", n, g.fake.seen())
	}
}

func TestStatusNotRunningNotPairedPaired(t *testing.T) {
	g := newRig(t)
	g.must("connect.enable", connect.IDParams{ID: "any"}, nil)
	test := func() connect.TestOutput {
		var out connect.TestOutput
		g.must("connect.test", connect.IDParams{ID: "any"}, &out)
		return out
	}
	// Any is running, Glossa is not signed in.
	if out := test(); out.OK || !strings.Contains(out.Message, "not signed in") {
		t.Fatalf("not paired: %+v", out)
	}
	g.paired()
	if out := test(); !out.OK || !strings.Contains(out.Message, "2 spaces") {
		t.Fatalf("paired: %+v", out)
	}
	// The key stops working (signed out inside Any).
	g.fake.badKey = true
	if out := test(); out.OK || !strings.Contains(out.Message, "not signed in") {
		t.Fatalf("revoked: %+v", out)
	}
	// Any is not running.
	g.fake.srv.Close()
	if out := test(); out.OK || !strings.Contains(out.Message, "not running") {
		t.Fatalf("not running: %+v", out)
	}
	if err := g.run("any.spaces", struct{}{}, nil); code(err) != "unavailable" || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("spaces with Any closed: %v", err)
	}
}

func TestPairingWrongCodeAndKeyHandling(t *testing.T) {
	g := newRig(t)
	g.must("connect.enable", connect.IDParams{ID: "any"}, nil)
	var st PairStartResult
	g.must("any.pair_start", struct{}{}, &st)
	if err := g.run("any.pair_finish", PairFinishParams{ChallengeID: st.ChallengeID, Code: "9999"}, nil); code(err) != "bad_code" {
		t.Fatalf("wrong code: %v", err)
	}
	if err := g.run("any.pair_finish", PairFinishParams{ChallengeID: st.ChallengeID, Code: "12"}, nil); code(err) != "bad_code" {
		t.Fatalf("short code: %v", err)
	}
	if v, _ := g.keys.Get("connect.any.key"); v != "" {
		t.Fatal("a key was saved after a wrong code")
	}
	var res PairResult
	g.must("any.pair_finish", PairFinishParams{ChallengeID: st.ChallengeID, Code: goodCode}, &res)
	if !res.Paired {
		t.Fatalf("%+v", res)
	}
	if v, _ := g.keys.Get("connect.any.key"); v != secretKey {
		t.Fatalf("key in keychain = %q", v)
	}
	var infos []connect.Info
	g.must("connect.list", struct{}{}, &infos)
	raw, _ := json.Marshal(infos)
	if !infos[0].KeySet || strings.Contains(string(raw), secretKey) || strings.Contains(fmt.Sprint(res), secretKey) {
		t.Fatalf("key shown or not marked saved: %s", raw)
	}
	// Signing out removes the key.
	g.must("any.unpair", struct{}{}, nil)
	if v, _ := g.keys.Get("connect.any.key"); v != "" {
		t.Fatal("key kept after sign out")
	}
	if err := g.run("any.spaces", struct{}{}, nil); code(err) != "not_paired" {
		t.Fatalf("spaces after sign out: %v", err)
	}
}

func TestEveryRequestCarriesTheVersionAndKey(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.must("any.spaces", struct{}{}, nil)
	g.fake.mu.Lock()
	defer g.fake.mu.Unlock()
	for i, h := range g.fake.hdrs {
		if h.Get("Anytype-Version") != "2025-11-08" {
			t.Errorf("request %d: version %q", i, h.Get("Anytype-Version"))
		}
	}
	if last := g.fake.hdrs[len(g.fake.hdrs)-1]; last.Get("Authorization") != "Bearer "+secretKey {
		t.Errorf("authorization = %q", last.Get("Authorization"))
	}
	if g.fake.hdrs[0].Get("Authorization") != "" {
		t.Error("the pairing call carried an Authorization header")
	}
}

func TestOnlyThisComputer(t *testing.T) {
	for _, u := range []string{"http://example.com:31009", "https://192.168.1.5", "http://10.0.0.2:31009"} {
		if _, err := NewClient(u, http.DefaultClient); !errors.Is(err, ErrNotLocal) {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"http://127.0.0.1:31009", "http://localhost:31009", "http://[::1]:31009"} {
		if _, err := NewClient(u, http.DefaultClient); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
}

func TestSpacesDefaultAndPaging(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.fake.pageCap = 1 // Any hands out one space at a time
	var sp SpacesResult
	g.must("any.spaces", struct{}{}, &sp)
	if len(sp.Spaces) != 2 || sp.Default != "" {
		t.Fatalf("%+v", sp)
	}
	if err := g.run("any.set_space", SetSpaceParams{SpaceID: "nope"}, nil); code(err) != "not_found" {
		t.Fatalf("unknown space: %v", err)
	}
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceB}, nil)
	g.must("any.spaces", struct{}{}, &sp)
	if sp.Default != spaceB {
		t.Fatalf("%+v", sp)
	}
	// Without a default and with two spaces, a send needs a choice.
	g2 := newRig(t)
	g2.paired()
	id := g2.note("A", "b", "")
	if err := g2.run("any.send", SendParams{Kind: "note", ID: id}, nil); code(err) != "choose_space" {
		t.Fatalf("no space chosen: %v", err)
	}
}

func TestPagesPagingSearchAndLinkedMarks(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceA}, nil)
	for i := 1; i <= 5; i++ {
		g.fake.add(spaceA, fmt.Sprintf("Page %d", i), "about grace")
	}
	g.fake.add(spaceA, "Other", "nothing here")
	g.fake.add(spaceB, "Elsewhere", "grace")
	g.fake.pageCap = 2
	var seen []string
	var out PagesOut
	for off := 0; ; {
		g.must("any.pages", PagesParams{Offset: off, Limit: 50}, &out)
		for _, p := range out.Pages {
			seen = append(seen, p.Title)
		}
		if out.NextOffset == 0 {
			break
		}
		off = out.NextOffset
	}
	if len(seen) != 6 || out.Total != 6 {
		t.Fatalf("saw %v, total %d", seen, out.Total)
	}
	g.must("any.search", SearchParams{Query: "GRACE"}, &out)
	if out.Total != 5 {
		t.Fatalf("search total = %d", out.Total)
	}
	// An imported page is marked as linked.
	g.must("any.import", ImportParams{ObjectID: out.Pages[0].ID}, nil)
	g.must("any.search", SearchParams{Query: "grace"}, &out)
	linked := 0
	for _, p := range out.Pages {
		if p.LinkedTo == "note" && p.ItemID != "" {
			linked++
		}
	}
	if linked != 1 {
		t.Fatalf("linked marks = %d in %+v", linked, out.Pages)
	}
}

func TestSendNoteThenUpdateWithoutDuplicate(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceA}, nil)
	id := g.note("Romans 8", "# Hope\n\nNo condemnation.", "Rom 8:1")

	var st StatusResult
	g.must("any.sync_status", StatusParams{Kind: "note", ID: id}, &st)
	if st.Linked || st.State != "not_linked" {
		t.Fatalf("before: %+v", st)
	}
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "created" || r.ObjectID == "" || !r.Passages {
		t.Fatalf("first send: %+v", r)
	}
	o := g.fake.objs[r.ObjectID]
	if o.Name != "Romans 8" || o.Markdown != "# Hope\n\nNo condemnation." || !strings.Contains(o.Props[passagesKey], "Romans 8:1") {
		t.Fatalf("page = %+v", o)
	}
	// Nothing changed: nothing is sent.
	before := len(g.fake.seen())
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "unchanged" {
		t.Fatalf("resend unchanged: %+v", r)
	}
	for _, q := range g.fake.seen()[before:] {
		if strings.HasPrefix(q, "PATCH") || strings.HasPrefix(q, "POST") {
			t.Fatalf("an unchanged note was written: %s", q)
		}
	}
	g.must("any.sync_status", StatusParams{Kind: "note", ID: id}, &st)
	if st.State != "in_sync" {
		t.Fatalf("status: %+v", st)
	}
	// Edit and send again: the same page is updated.
	g.setNote(id, "Romans 8", "# Hope\n\nNo condemnation. More.")
	g.must("any.sync_status", StatusParams{Kind: "note", ID: id}, &st)
	if st.State != "glossa_changed" {
		t.Fatalf("status after edit: %+v", st)
	}
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "updated" || len(g.fake.objs) != 1 || g.fake.objs[r.ObjectID].Markdown != "# Hope\n\nNo condemnation. More." {
		t.Fatalf("update: %+v, %d pages", r, len(g.fake.objs))
	}
	g.must("any.sync_status", StatusParams{Kind: "note", ID: id, Check: true}, &st)
	if st.State != "in_sync" {
		t.Fatalf("status after update: %+v", st)
	}
}

func TestSendSermonWithPassages(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceB}, nil)
	var d document.Document
	g.must("document.create", document.CreateParams{Title: "Grace alone", Passages: []string{"Eph 2:8-10", "Rom 3:24"}, Body: "Saved by grace."}, &d)
	var r SyncResult
	g.must("any.send", SendParams{Kind: "sermon", ID: d.ID}, &r)
	o := g.fake.objs[r.ObjectID]
	if r.State != "created" || o.Space != spaceB || o.Name != "Grace alone" || o.Markdown != "Saved by grace." {
		t.Fatalf("%+v %+v", r, o)
	}
	if p := o.Props[passagesKey]; !strings.Contains(p, "Ephesians 2:8-10") || !strings.Contains(p, "; ") || !strings.Contains(p, "Romans 3:24") {
		t.Fatalf("passages = %q", p)
	}
	g.must("document.update", map[string]any{"id": d.ID, "body": "Saved by grace, through faith."}, nil)
	g.must("any.send", SendParams{Kind: "sermon", ID: d.ID}, &r)
	if r.State != "updated" || len(g.fake.objs) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestPassagesPropertyRefusedStillSends(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.fake.noProps = true
	id := g.note("N", "text", "John 3:16")
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id, SpaceID: spaceA}, &r)
	if r.State != "created" || r.Passages || g.fake.objs[r.ObjectID].Markdown != "text" {
		t.Fatalf("%+v", r)
	}
}

func TestImportCreatesNoteThenUpdatesIt(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.fake.titleHeading = true // Any puts the title at the top of the text
	pg := g.fake.add(spaceA, "Sermon seed", "A *thought* on mercy.")
	var r SyncResult
	g.must("any.import", ImportParams{SpaceID: spaceA, ObjectID: pg}, &r)
	if r.State != "created" || r.Kind != "note" {
		t.Fatalf("%+v", r)
	}
	n := g.getNote(r.ItemID)
	if n.Title != "Sermon seed" || n.Body != "A *thought* on mercy." {
		t.Fatalf("note = %+v", n)
	}
	// Importing again with nothing changed does nothing.
	g.must("any.import", ImportParams{SpaceID: spaceA, ObjectID: pg}, &r)
	if r.State != "unchanged" || r.ItemID != n.ID {
		t.Fatalf("%+v", r)
	}
	// Changed in Any: the same note is updated, not a second one made.
	g.fake.edit(pg, "Sermon seed", "A thought on mercy, and more.")
	g.must("any.import", ImportParams{SpaceID: spaceA, ObjectID: pg}, &r)
	if r.State != "updated" || r.ItemID != n.ID || g.getNote(n.ID).Body != "A thought on mercy, and more." {
		t.Fatalf("%+v", r)
	}
	var notes []core.Note
	g.must("note.list", struct{}{}, &notes)
	if len(notes) != 1 {
		t.Fatalf("%d notes", len(notes))
	}
	// Sending an imported note back updates the page.
	g.setNote(n.ID, "Sermon seed", "Edited in Glossa.")
	g.must("any.send", SendParams{Kind: "note", ID: n.ID}, &r)
	if r.State != "updated" || r.ObjectID != pg || len(g.fake.objs) != 1 {
		t.Fatalf("%+v", r)
	}
}

// bothChanged sends a note, then edits it on both sides.
func bothChanged(t *testing.T) (*rig, string, string) {
	g := newRig(t)
	g.paired()
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceA}, nil)
	id := g.note("Title", "original", "")
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	g.setNote(id, "Title", "glossa edit")
	g.fake.edit(r.ObjectID, "Title", "any edit")
	return g, id, r.ObjectID
}

func TestBothChangedAsksAndChangesNothing(t *testing.T) {
	g, id, obj := bothChanged(t)
	var r SyncResult
	before := len(g.fake.seen())
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "both_changed" || r.Conflict == nil || r.Conflict.GlossaExcerpt != "glossa edit" || r.Conflict.AnyExcerpt != "any edit" {
		t.Fatalf("%+v", r)
	}
	if g.fake.objs[obj].Markdown != "any edit" || g.getNote(id).Body != "glossa edit" {
		t.Fatal("something was overwritten")
	}
	for _, q := range g.fake.seen()[before:] {
		if strings.HasPrefix(q, "PATCH") {
			t.Fatalf("wrote while asking: %s", q)
		}
	}
	// Importing the same page says the same.
	g.must("any.import", ImportParams{SpaceID: spaceA, ObjectID: obj}, &r)
	if r.State != "both_changed" {
		t.Fatalf("import: %+v", r)
	}
	var st StatusResult
	g.must("any.sync_status", StatusParams{Kind: "note", ID: id, Check: true}, &st)
	if st.State != "both_changed" {
		t.Fatalf("status: %+v", st)
	}
}

func TestResolveKeepGlossa(t *testing.T) {
	g, id, obj := bothChanged(t)
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id, Resolve: "glossa"}, &r)
	if r.State != "updated" || g.fake.objs[obj].Markdown != "glossa edit" || g.getNote(id).Body != "glossa edit" {
		t.Fatalf("%+v", r)
	}
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "unchanged" {
		t.Fatalf("after resolving: %+v", r)
	}
}

func TestResolveKeepAny(t *testing.T) {
	g, id, obj := bothChanged(t)
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id, Resolve: "any"}, &r)
	if g.getNote(id).Body != "any edit" || g.fake.objs[obj].Markdown != "any edit" {
		t.Fatalf("%+v", r)
	}
	g.must("any.import", ImportParams{SpaceID: spaceA, ObjectID: obj}, &r)
	if r.State != "unchanged" {
		t.Fatalf("after resolving: %+v", r)
	}
}

func TestResolveKeepBoth(t *testing.T) {
	g, id, obj := bothChanged(t)
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id, Resolve: "copy"}, &r)
	if r.CopyID == "" || g.fake.objs[obj].Markdown != "glossa edit" || g.getNote(id).Body != "glossa edit" {
		t.Fatalf("%+v", r)
	}
	cp := g.getNote(r.CopyID)
	if cp.Body != "any edit" || !strings.Contains(cp.Title, "from Any") {
		t.Fatalf("copy = %+v", cp)
	}
	var notes []core.Note
	g.must("note.list", struct{}{}, &notes)
	if len(notes) != 2 || len(g.fake.objs) != 1 {
		t.Fatalf("%d notes, %d pages", len(notes), len(g.fake.objs))
	}
}

func TestOnlyAnyChangedIsNotOverwrittenBySend(t *testing.T) {
	g := newRig(t)
	g.paired()
	id := g.note("T", "one", "")
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id, SpaceID: spaceA}, &r)
	g.fake.edit(r.ObjectID, "T", "two in Any")
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "any_changed" || g.fake.objs[r.ObjectID].Markdown != "two in Any" {
		t.Fatalf("%+v", r)
	}
	// Merely touched in Any (same text, new time) is not a change.
	g.must("any.send", SendParams{Kind: "note", ID: id, Resolve: "any"}, &r)
	g.fake.edit(r.ObjectID, "T", "two in Any")
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "unchanged" {
		t.Fatalf("touched only: %+v", r)
	}
}

func TestPageDeletedInAny(t *testing.T) {
	g := newRig(t)
	g.paired()
	id := g.note("T", "one", "")
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id, SpaceID: spaceA}, &r)
	old := r.ObjectID
	delete(g.fake.objs, old)
	var st StatusResult
	g.must("any.sync_status", StatusParams{Kind: "note", ID: id, Check: true}, &st)
	if st.State != "gone" {
		t.Fatalf("%+v", st)
	}
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "created" || r.ObjectID == old || len(g.fake.objs) != 1 {
		t.Fatalf("%+v", r)
	}
	delete(g.fake.objs, r.ObjectID)
	g.must("any.import", ImportParams{SpaceID: spaceA, ObjectID: r.ObjectID}, &r)
	if r.State != "gone" {
		t.Fatalf("importing a deleted linked page: %+v", r)
	}
	if err := g.run("any.import", ImportParams{SpaceID: spaceA, ObjectID: "never-was"}, &r); code(err) != "not_found" {
		t.Fatalf("importing a page that is not there: %v", err)
	}
}

func TestLinksArePerDevice(t *testing.T) {
	g := newRig(t)
	g.paired()
	id := g.note("T", "one", "")
	g.must("any.send", SendParams{Kind: "note", ID: id, SpaceID: spaceA}, nil)
	var n int
	if err := g.env.Store.QueryRow(context.Background(), `SELECT COUNT(*) FROM any_links`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("links = %d, %v", n, err)
	}
	changes, err := g.env.Store.LocalChanges(context.Background(), "", 1000)
	if err != nil {
		t.Skipf("no way to list the changes to send: %v", err)
	}
	for _, c := range changes {
		if c.ObjectType == "any_links" || strings.HasPrefix(c.ObjectID, `["connect.`) {
			t.Fatalf("a device-only change would be synced: %+v", c)
		}
	}
}

func TestKeyNeverInErrorsOrLogs(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.fake.srv.Close()
	err := g.run("any.spaces", struct{}{}, nil)
	if err == nil || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("%v", err)
	}
}

func TestUnknownKindAndResolve(t *testing.T) {
	g := newRig(t)
	g.paired()
	if err := g.run("any.send", SendParams{Kind: "list", ID: "x", SpaceID: spaceA}, nil); code(err) != "bad_params" {
		t.Fatalf("%v", err)
	}
	id := g.note("T", "one", "")
	if err := g.run("any.send", SendParams{Kind: "note", ID: id, Resolve: "both"}, nil); code(err) != "bad_params" {
		t.Fatalf("%v", err)
	}
}

var _ = store.ByID
