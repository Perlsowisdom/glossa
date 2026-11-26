package readwise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/connect"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const token = "rw-token-do-not-leak"

// fakeHL / fakeBook are what the fake Readwise holds.
type fakeHL struct {
	ID                  int
	Text, Note, Updated string
	Loc                 int
	Discard, Deleted    bool
}

type fakeBook struct {
	ID                              int
	Title, Author, Category, Source string
	Tags                            []string
	Highlights                      []*fakeHL
}

// fakeReadwise serves the part of the Readwise API Glossa uses: GET /api/v2/auth/ and GET /api/v2/export/.
type fakeReadwise struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	books    []*fakeBook
	pageSize int
	reqs     []string
	auth     []string
	throttle int // answer 429 this many times first
	retry    string
}

func newFake(t *testing.T) *fakeReadwise {
	f := &fakeReadwise{t: t, pageSize: 2, retry: "3"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeReadwise) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func (f *fakeReadwise) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.URL.Path+"?"+r.URL.RawQuery)
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	if r.Header.Get("Authorization") != "Token "+token {
		http.Error(w, `{"detail":"Invalid token."}`, http.StatusUnauthorized)
		return
	}
	if f.throttle > 0 {
		f.throttle--
		w.Header().Set("Retry-After", f.retry)
		http.Error(w, "slow down", http.StatusTooManyRequests)
		return
	}
	switch r.URL.Path {
	case "/api/v2/auth/":
		w.WriteHeader(http.StatusNoContent)
	case "/api/v2/export/":
		f.export(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeReadwise) export(w http.ResponseWriter, r *http.Request) {
	after := r.URL.Query().Get("updatedAfter")
	deleted := r.URL.Query().Get("includeDeleted") == "true"
	var out []map[string]any
	for _, b := range f.books {
		var hls []map[string]any
		for _, h := range b.Highlights {
			if after != "" && h.Updated <= after {
				continue
			}
			if h.Deleted && !deleted {
				continue
			}
			hls = append(hls, map[string]any{
				"id": h.ID, "text": h.Text, "note": h.Note, "location": h.Loc, "location_type": "location",
				"highlighted_at": h.Updated, "updated_at": h.Updated, "is_discard": h.Discard, "is_deleted": h.Deleted,
				"readwise_url": fmt.Sprintf("https://readwise.io/open/%d", h.ID), "url": nil,
			})
		}
		if len(hls) == 0 {
			continue
		}
		var tags []map[string]any
		for _, t := range b.Tags {
			tags = append(tags, map[string]any{"id": 1, "name": t})
		}
		out = append(out, map[string]any{
			"user_book_id": b.ID, "title": b.Title, "readable_title": b.Title, "author": b.Author, "category": b.Category,
			"source": b.Source, "readwise_url": fmt.Sprintf("https://readwise.io/bookreview/%d", b.ID), "book_tags": tags,
			"highlights": hls, "is_deleted": false,
		})
	}
	start := 0
	if c := r.URL.Query().Get("pageCursor"); c != "" {
		start, _ = strconv.Atoi(c)
	}
	end := start + f.pageSize
	var next any
	if end < len(out) {
		next = strconv.Itoa(end)
	} else {
		end = len(out)
	}
	if start > end {
		start = end
	}
	json.NewEncoder(w).Encode(map[string]any{"count": len(out), "nextPageCursor": next, "results": out[start:end]})
}

func (f *fakeReadwise) book(id int) *fakeBook {
	for _, b := range f.books {
		if b.ID == id {
			return b
		}
	}
	f.t.Fatalf("no fake book %d", id)
	return nil
}

// rig ----------------------------------------------------------------------------------------------

type rig struct {
	t     *testing.T
	fake  *fakeReadwise
	env   *command.Env
	reg   *command.Registry
	keys  *secrets.Memory
	conn  *Conn
	clock time.Time
	waits []time.Duration
}

func newRig(t *testing.T) *rig {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &rig{t: t, fake: newFake(t), env: &command.Env{Store: st}, keys: secrets.NewMemory(), reg: command.NewRegistry(),
		clock: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)}
	g.conn = New(WithURL(g.fake.srv.URL), WithSleep(func(ctx context.Context, d time.Duration) error {
		g.waits = append(g.waits, d)
		return ctx.Err()
	}, func() time.Time { return g.clock }))
	core.Register(g.reg)
	svc := connect.Register(g.reg, g.keys, g.conn)
	g.conn.Register(g.reg, svc)
	t.Cleanup(g.reg.Close)
	return g
}

func (g *rig) run(id string, params, out any) error {
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

func (g *rig) must(id string, params, out any) {
	g.t.Helper()
	if err := g.run(id, params, out); err != nil {
		g.t.Fatalf("%s: %v", id, err)
	}
}

func (g *rig) on() {
	g.t.Helper()
	g.must("connect.enable", map[string]string{"id": "readwise"}, nil)
	g.must("connect.set_key", map[string]string{"id": "readwise", "key": token}, nil)
}

func (g *rig) importAll(full bool) Result {
	g.t.Helper()
	var r Result
	g.must("readwise.import", ImportParams{Full: full}, &r)
	return r
}

func (g *rig) addBook(id, title, author string) {
	g.t.Helper()
	err := g.env.Store.Write(context.Background(), "test", func(tx *store.Tx) error {
		if err := tx.Create("items", store.Row{"id": id, "kind": "monograph", "title": title, "source": "import", "created_at": int64(1), "updated_at": int64(1)}); err != nil {
			return err
		}
		for i, a := range strings.Split(author, ";") {
			pid := fmt.Sprintf("%s-p%d", id, i)
			if err := tx.Create("people", store.Row{"id": pid, "name": a, "sort_name": a}); err != nil {
				return err
			}
			if err := tx.Create("item_people", store.Row{"item_id": id, "person_id": pid, "role": "author", "position": int64(i)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		g.t.Fatal(err)
	}
}

func (g *rig) note(id string) core.Note {
	g.t.Helper()
	var n core.Note
	g.must("note.get", map[string]string{"id": id}, &n)
	return n
}

func (g *rig) count(table string) int {
	var n int
	if err := g.env.Store.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		g.t.Fatal(err)
	}
	return n
}

func (g *rig) noteOf(sid string) string {
	g.t.Helper()
	var id string
	if err := g.env.Store.QueryRow(context.Background(), `SELECT note_id FROM readwise_sources WHERE source_id = ?`, sid).Scan(&id); err != nil {
		g.t.Fatal(err)
	}
	return id
}

func hl(id int, text, updated string) *fakeHL {
	return &fakeHL{ID: id, Text: text, Updated: updated, Loc: id * 10}
}

// seed makes the usual shelf: one book in the library (matched), three that are not, one article.
func (g *rig) seed() {
	g.addBook("lib-mc", "Mere Christianity", "C. S. Lewis")
	g.fake.books = []*fakeBook{
		{ID: 1, Title: "Mere Christianity", Author: "Lewis, C.S.", Category: "books", Tags: []string{"Apologetics", "favourite"},
			Highlights: []*fakeHL{hl(11, "Pride is the complete anti-god state of mind.", "2026-02-01T00:00:00Z"), {ID: 12, Text: "Humility is not thinking less of yourself.\nIt is thinking of yourself less.", Note: "so good", Updated: "2026-02-02T00:00:00Z", Loc: 120}}},
		{ID: 2, Title: "The Pilgrim's Progress", Author: "John Bunyan", Category: "books",
			Highlights: []*fakeHL{hl(21, "I saw in my dream.", "2026-02-03T00:00:00Z")}},
		{ID: 3, Title: "Knowing God", Author: "J. I. Packer", Category: "books",
			Highlights: []*fakeHL{hl(31, "To know God is to be known.", "2026-02-04T00:00:00Z"), hl(32, "Second.", "2026-02-05T00:00:00Z")}},
		{ID: 4, Title: "On the sufficiency of Scripture", Author: "Some Blogger", Category: "articles", Source: "web_clip",
			Highlights: []*fakeHL{hl(41, "Scripture is enough.", "2026-02-06T00:00:00Z")}},
		{ID: 5, Title: "Empty", Author: "Nobody", Category: "books",
			Highlights: []*fakeHL{{ID: 51, Text: "thrown away", Updated: "2026-02-07T00:00:00Z", Discard: true}}},
	}
}

// tests ----------------------------------------------------------------------------------------------

func TestTokenCheck(t *testing.T) {
	g := newRig(t)
	g.on()
	var r connect.TestResult
	g.must("readwise.test", struct{}{}, &r)
	if !r.OK || r.Message != "Connected to Readwise." {
		t.Fatalf("test = %+v", r)
	}
	if got := g.fake.auth[0]; got != "Token "+token {
		t.Fatalf("the token travels in the Authorization header, got %q", got)
	}
	// A bad token says what to do, in plain words, and the token is not in the message.
	g.must("connect.set_key", map[string]string{"id": "readwise", "key": "wrong-token-123"}, nil)
	g.must("readwise.test", struct{}{}, &r)
	if r.OK || !strings.Contains(r.Message, "readwise.io/access_token") || strings.Contains(r.Message, "wrong-token") {
		t.Fatalf("bad token: %+v", r)
	}
}

func TestOffSendsNothing(t *testing.T) {
	g := newRig(t)
	g.fake.books = nil
	g.must("connect.set_key", map[string]string{"id": "readwise", "key": token}, nil)
	for _, id := range []string{"readwise.test", "readwise.preview", "readwise.import"} {
		err := g.run(id, struct{}{}, nil)
		var ce *command.Error
		if !errors.As(err, &ce) || ce.Code != "off" {
			t.Fatalf("%s while off: %v", id, err)
		}
	}
	if n := len(g.fake.seen()); n != 0 {
		t.Fatalf("%d requests while off", n)
	}
	var infos []connect.Info
	g.must("connect.list", struct{}{}, &infos)
	for _, i := range infos {
		if i.ID == "readwise" {
			if i.Enabled || !i.KeySet || i.SignIn != connect.SignInKey || !strings.Contains(i.Sends, "readwise.io") {
				t.Fatalf("info = %+v", i)
			}
			if b, _ := json.Marshal(i); strings.Contains(string(b), token) {
				t.Fatal("the token is in connect.list")
			}
			return
		}
	}
	t.Fatal("readwise is not in connect.list")
}

func TestNoToken(t *testing.T) {
	g := newRig(t)
	g.must("connect.enable", map[string]string{"id": "readwise"}, nil)
	err := g.run("readwise.import", struct{}{}, nil)
	var ce *command.Error
	if !errors.As(err, &ce) || ce.Code != "no_token" {
		t.Fatalf("import without a token: %v", err)
	}
}

func TestBadTokenOnImport(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.must("connect.enable", map[string]string{"id": "readwise"}, nil)
	g.must("connect.set_key", map[string]string{"id": "readwise", "key": "nope"}, nil)
	err := g.run("readwise.import", struct{}{}, nil)
	var ce *command.Error
	if !errors.As(err, &ce) || ce.Code != "bad_token" || strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v", err)
	}
	if g.count("notes") != 0 {
		t.Fatal("notes were made with a bad token")
	}
}

func TestImportMakesNotes(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	r := g.importAll(false)
	if !r.Full || r.Since != "" {
		t.Fatalf("the first import is complete: %+v", r)
	}
	// 4 sources have live highlights (the fifth was only discarded); paging with cursors gave all of them.
	if r.Sources != 4 || r.Highlights != 6 || r.Matched != 1 || r.NotesNew != 4 || r.NotesNewOnBook != 1 || r.Ambiguous != 0 {
		t.Fatalf("result = %+v", r)
	}
	reqs := g.fake.seen()
	if len(reqs) < 3 || !strings.Contains(reqs[0], "includeDeleted=true") || strings.Contains(reqs[0], "updatedAfter") || !strings.Contains(reqs[1], "pageCursor=") {
		t.Fatalf("requests = %v", reqs)
	}

	// The matched book's highlights are one note attached to the library book.
	mc := g.note(g.noteOf("1"))
	if mc.ItemID != "lib-mc" || mc.ItemTitle != "Mere Christianity" || mc.Locator["section"] != float64(1) {
		t.Fatalf("matched note = %+v", mc)
	}
	if mc.Title != "Mere Christianity — Lewis, C.S." {
		t.Fatalf("title = %q", mc.Title)
	}
	for _, want := range []string{
		"> Pride is the complete anti-god state of mind.",
		"> Humility is not thinking less of yourself.\n> It is thinking of yourself less.",
		"Note: so good",
		"Location 120 · [Open in Readwise](https://readwise.io/open/12)",
		"[From Readwise](https://readwise.io/bookreview/1)",
	} {
		if !strings.Contains(mc.Body, want) {
			t.Fatalf("body lacks %q:\n%s", want, mc.Body)
		}
	}
	if !reflect.DeepEqual(mc.Tags, []string{"apologetics", "favourite", "readwise"}) {
		t.Fatalf("tags = %v", mc.Tags)
	}
	// The others are one free-standing note per source.
	for sid, title := range map[string]string{"2": "The Pilgrim's Progress — John Bunyan", "3": "Knowing God — J. I. Packer", "4": "On the sufficiency of Scripture — Some Blogger"} {
		n := g.note(g.noteOf(sid))
		if n.ItemID != "" || n.Title != title || !contains(n.Tags, "readwise") {
			t.Fatalf("note for %s = %+v", sid, n)
		}
	}
	if g.count("notes") != 4 || g.count("readwise_sources") != 4 || g.count("readwise_highlights") != 6 {
		t.Fatalf("rows: notes %d, sources %d, highlights %d", g.count("notes"), g.count("readwise_sources"), g.count("readwise_highlights"))
	}
	// Knowing God has two highlights in order.
	kg := g.note(g.noteOf("3")).Body
	if strings.Index(kg, "To know God") > strings.Index(kg, "Second.") {
		t.Fatalf("order: %s", kg)
	}
}

func TestPreviewChangesNothing(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	var r Result
	g.must("readwise.preview", struct{}{}, &r)
	if !r.Preview || r.Sources != 4 || r.Highlights != 6 || r.Matched != 1 || r.NotesNew != 4 || r.NotesNewOnBook != 1 {
		t.Fatalf("preview = %+v", r)
	}
	if !strings.Contains(r.Message, "4 sources") || !strings.Contains(r.Message, "1 match") {
		t.Fatalf("message = %q", r.Message)
	}
	if g.count("notes") != 0 || g.count("readwise_sources") != 0 || g.count("readwise_highlights") != 0 {
		t.Fatal("a preview wrote something")
	}
	var st StatusResult
	g.must("readwise.status", struct{}{}, &st)
	if st.LastImport != nil {
		t.Fatalf("a preview counts as an import: %+v", st)
	}
	// So the import that follows is still the complete one.
	if r := g.importAll(false); !r.Full || r.NotesNew != 4 {
		t.Fatalf("import = %+v", r)
	}
}

func TestReimportChangesNothing(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.importAll(false)
	first := g.clock.Format(time.RFC3339)
	g.clock = g.clock.Add(time.Hour)
	g.fake.reqs = nil

	// Incremental: asks only for what changed after the last import, and nothing has.
	r := g.importAll(false)
	reqs := g.fake.seen()
	if r.Full || r.Since != first || len(reqs) != 1 || !strings.Contains(reqs[0], "updatedAfter="+strings.ReplaceAll(first, ":", "%3A")) {
		t.Fatalf("incremental: %+v, requests %v", r, reqs)
	}
	if r.NotesNew+r.NotesUpdated+r.HighlightsNew+r.HighlightsChanged != 0 || g.count("notes") != 4 {
		t.Fatalf("a quiet re-import changed things: %+v", r)
	}
	// Complete again: every source comes back and every note is found up to date.
	r = g.importAll(true)
	if !r.Full || r.NotesNew != 0 || r.NotesUpdated != 0 || r.NotesUnchanged != 4 || r.HighlightsNew+r.HighlightsChanged+r.HighlightsGone != 0 {
		t.Fatalf("full re-import: %+v", r)
	}
	if g.count("notes") != 4 || g.count("readwise_highlights") != 6 {
		t.Fatal("a re-import duplicated something")
	}
	var st StatusResult
	g.must("readwise.status", struct{}{}, &st)
	if st.LastImport == nil || st.Linked != 4 || len(st.Conflicts) != 0 {
		t.Fatalf("status = %+v", st)
	}
}

func TestIncrementalUpdateKeepsTheOthers(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.importAll(false)
	g.clock = g.clock.Add(time.Hour)
	// One highlight of Knowing God is edited and another is added; Readwise sends only those.
	b := g.fake.book(3)
	b.Highlights[1].Text, b.Highlights[1].Updated = "Second, corrected.", "2026-03-01T10:30:00Z"
	b.Highlights = append(b.Highlights, &fakeHL{ID: 33, Text: "Third.", Updated: "2026-03-01T10:31:00Z", Loc: 330})
	r := g.importAll(false)
	if r.Sources != 1 || r.NotesUpdated != 1 || r.HighlightsNew != 1 || r.HighlightsChanged != 1 || r.NotesNew != 0 {
		t.Fatalf("result = %+v", r)
	}
	body := g.note(g.noteOf("3")).Body
	for _, want := range []string{"To know God is to be known.", "Second, corrected.", "Third."} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "> Second.") {
		t.Fatal("the old text stayed")
	}
}

func TestDiscardedAndDeletedHighlightsLeaveTheNote(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.importAll(false)
	g.clock = g.clock.Add(time.Hour)
	b := g.fake.book(3)
	b.Highlights[0].Discard, b.Highlights[0].Updated = true, "2026-03-01T10:30:00Z"
	r := g.importAll(false)
	if r.HighlightsGone != 1 || r.NotesUpdated != 1 {
		t.Fatalf("discard: %+v", r)
	}
	if body := g.note(g.noteOf("3")).Body; strings.Contains(body, "To know God") || !strings.Contains(body, "Second.") {
		t.Fatalf("body after discard:\n%s", body)
	}
	// Deleted in Readwise: the export says so with includeDeleted.
	b.Highlights[1].Deleted, b.Highlights[1].Updated = true, "2026-03-01T11:40:00Z"
	g.clock = g.clock.Add(time.Hour)
	r = g.importAll(false)
	if r.HighlightsGone != 1 {
		t.Fatalf("delete: %+v", r)
	}
	if body := g.note(g.noteOf("3")).Body; strings.Contains(body, "Second.") || !strings.Contains(body, "No highlights are kept") {
		t.Fatalf("body after delete:\n%s", body)
	}
	if g.count("readwise_highlights") != 4 || g.count("notes") != 4 {
		t.Fatalf("rows: highlights %d, notes %d", g.count("readwise_highlights"), g.count("notes"))
	}
}

func TestCompleteImportDropsWhatReadwiseNoLongerHas(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.importAll(false)
	// Gone from Readwise altogether (not even flagged): only a complete import can notice.
	b := g.fake.book(3)
	b.Highlights = b.Highlights[:1]
	if r := g.importAll(false); r.HighlightsGone != 0 {
		t.Fatalf("incremental import guessed: %+v", r)
	}
	r := g.importAll(true)
	if r.HighlightsGone != 1 || r.NotesUpdated != 1 {
		t.Fatalf("full: %+v", r)
	}
	if strings.Contains(g.note(g.noteOf("3")).Body, "Second.") {
		t.Fatal("the highlight stayed")
	}
}

func TestEditedNoteIsNotOverwritten(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.importAll(false)
	nid := g.noteOf("3")
	mine := g.note(nid).Body + "\n\nMy own thought about this book."
	g.must("note.update", map[string]any{"id": nid, "body": mine}, nil)

	g.clock = g.clock.Add(time.Hour)
	g.fake.book(3).Highlights = append(g.fake.book(3).Highlights, &fakeHL{ID: 34, Text: "A new one.", Updated: "2026-03-01T10:30:00Z", Loc: 340})
	r := g.importAll(false)
	if len(r.Conflicts) != 1 || r.Conflicts[0].SourceID != "3" || r.Conflicts[0].NoteID != nid || r.NotesUpdated != 0 {
		t.Fatalf("result = %+v", r)
	}
	if !strings.Contains(r.Conflicts[0].YourExcerpt, "My own thought") || !strings.Contains(r.Conflicts[0].ReadwiseExcerpt, "A new one.") {
		t.Fatalf("excerpts = %+v", r.Conflicts[0])
	}
	if g.note(nid).Body != mine {
		t.Fatal("the edited note was changed")
	}
	if !strings.Contains(r.Message, "left alone") {
		t.Fatalf("message = %q", r.Message)
	}
	// It stays pending, without the network, until settled; the new highlight is already kept.
	var st StatusResult
	g.must("readwise.status", struct{}{}, &st)
	if len(st.Conflicts) != 1 || g.count("readwise_highlights") != 7 {
		t.Fatalf("status = %+v, highlights %d", st, g.count("readwise_highlights"))
	}
	g.fake.reqs = nil

	// Keep both: the person's note is untouched, and Readwise's text goes in a new linked note.
	var res ResolveResult
	g.must("readwise.resolve", ResolveParams{SourceID: "3", Resolve: "both"}, &res)
	if res.NoteID == "" || res.NoteID == nid || g.note(nid).Body != mine || !strings.Contains(g.note(res.NoteID).Body, "A new one.") {
		t.Fatalf("both: %+v", res)
	}
	if g.noteOf("3") != res.NoteID || len(g.fake.seen()) != 0 {
		t.Fatal("resolve should relink and not use the network")
	}
	g.must("readwise.status", struct{}{}, &st)
	if len(st.Conflicts) != 0 {
		t.Fatalf("still pending: %+v", st)
	}
	// ... and from now on the new note follows Readwise.
	g.clock = g.clock.Add(time.Hour)
	g.fake.book(3).Highlights[0].Text, g.fake.book(3).Highlights[0].Updated = "Changed again.", "2026-03-01T11:30:00Z"
	if r := g.importAll(false); r.NotesUpdated != 1 || len(r.Conflicts) != 0 || !strings.Contains(g.note(res.NoteID).Body, "Changed again.") {
		t.Fatalf("after both: %+v", r)
	}
}

func TestResolveReadwiseAndMine(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.importAll(false)
	edit := func(sid string) string {
		nid := g.noteOf(sid)
		g.must("note.update", map[string]any{"id": nid, "body": "Rewritten by me."}, nil)
		return nid
	}
	n3, n2 := edit("3"), edit("2")
	g.clock = g.clock.Add(time.Hour)
	g.fake.book(3).Highlights[0].Updated, g.fake.book(3).Highlights[0].Text = "2026-03-01T10:30:00Z", "Fresh 3."
	g.fake.book(2).Highlights[0].Updated, g.fake.book(2).Highlights[0].Text = "2026-03-01T10:30:00Z", "Fresh 2."
	if r := g.importAll(false); len(r.Conflicts) != 2 {
		t.Fatalf("conflicts = %+v", r.Conflicts)
	}
	g.must("readwise.resolve", ResolveParams{SourceID: "3", Resolve: "readwise"}, nil)
	if b := g.note(n3).Body; !strings.Contains(b, "Fresh 3.") || strings.Contains(b, "Rewritten") {
		t.Fatalf("readwise: %s", b)
	}
	g.must("readwise.resolve", ResolveParams{SourceID: "2", Resolve: "mine"}, nil)
	if g.note(n2).Body != "Rewritten by me." {
		t.Fatal("mine changed the note")
	}
	// A kept note is never touched again, but is counted as skipped.
	g.clock = g.clock.Add(time.Hour)
	g.fake.book(2).Highlights[0].Updated, g.fake.book(2).Highlights[0].Text = "2026-03-01T11:30:00Z", "Fresher 2."
	r := g.importAll(false)
	if r.Skipped != 1 || len(r.Conflicts) != 0 || g.note(n2).Body != "Rewritten by me." {
		t.Fatalf("after mine: %+v", r)
	}
	if err := g.run("readwise.resolve", ResolveParams{SourceID: "2", Resolve: "readwise"}, nil); err == nil {
		t.Fatal("a kept note has nothing to settle")
	}
	if err := g.run("readwise.resolve", ResolveParams{SourceID: "3", Resolve: "bogus"}, nil); err == nil {
		t.Fatal("bad choice accepted")
	}
}

func TestDeletedNoteIsNotMadeAgain(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.importAll(false)
	g.must("note.delete", map[string]string{"id": g.noteOf("2")}, nil)
	g.clock = g.clock.Add(time.Hour)
	g.fake.book(2).Highlights[0].Updated = "2026-03-01T10:30:00Z"
	r := g.importAll(false)
	if r.Skipped != 1 || r.NotesNew != 0 || r.NotesUpdated != 0 {
		t.Fatalf("result = %+v", r)
	}
	if r = g.importAll(true); r.NotesNew != 0 || r.Skipped != 1 {
		t.Fatalf("full: %+v", r)
	}
}

func TestRateLimitHonoursRetryAfter(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.fake.throttle, g.fake.retry = 2, "7"
	r := g.importAll(false)
	if r.NotesNew != 4 {
		t.Fatalf("result = %+v", r)
	}
	if len(g.waits) != 2 || g.waits[0] != 7*time.Second || g.waits[1] != 7*time.Second {
		t.Fatalf("waits = %v", g.waits)
	}
	// Asked to wait far too long, or too often: stop with a plain message, change nothing.
	g2 := newRig(t)
	g2.seed()
	g2.on()
	g2.fake.throttle, g2.fake.retry = 10, "3"
	err := g2.run("readwise.import", struct{}{}, nil)
	if err == nil || !strings.Contains(err.Error(), "slow down") || g2.count("notes") != 0 {
		t.Fatalf("err = %v", err)
	}
	g3 := newRig(t)
	g3.seed()
	g3.on()
	g3.fake.throttle, g3.fake.retry = 1, "3600"
	if err := g3.run("readwise.import", struct{}{}, nil); err == nil || len(g3.waits) != 0 {
		t.Fatalf("err = %v, waits %v", err, g3.waits)
	}
}

func TestFailedImportDoesNotAdvanceTheClock(t *testing.T) {
	g := newRig(t)
	g.seed()
	g.on()
	g.fake.throttle, g.fake.retry = 10, "1"
	if err := g.run("readwise.import", struct{}{}, nil); err == nil {
		t.Fatal("expected a failure")
	}
	var st StatusResult
	g.must("readwise.status", struct{}{}, &st)
	if st.LastImport != nil {
		t.Fatalf("last import = %+v", st.LastImport)
	}
}

func TestAmbiguousBookIsNotGuessed(t *testing.T) {
	g := newRig(t)
	g.addBook("a", "Knowing God", "J. I. Packer")
	g.addBook("b", "Knowing God", "Packer, J. I.")
	g.fake.books = []*fakeBook{{ID: 3, Title: "Knowing God", Author: "J. I. Packer", Category: "books", Highlights: []*fakeHL{hl(31, "x", "2026-02-04T00:00:00Z")}}}
	g.on()
	r := g.importAll(false)
	if r.Matched != 0 || r.Ambiguous != 1 || r.NotesNew != 1 || r.NotesNewOnBook != 0 {
		t.Fatalf("result = %+v", r)
	}
	if g.note(g.noteOf("3")).ItemID != "" {
		t.Fatal("attached to a guess")
	}
}

func TestOnlyBooksAreMatched(t *testing.T) {
	g := newRig(t)
	g.addBook("lib", "Mere Christianity", "C. S. Lewis")
	g.fake.books = []*fakeBook{{ID: 9, Title: "Mere Christianity", Author: "C. S. Lewis", Category: "articles", Highlights: []*fakeHL{hl(91, "x", "2026-02-04T00:00:00Z")}}}
	g.on()
	if r := g.importAll(false); r.Matched != 0 {
		t.Fatalf("an article matched a book: %+v", r)
	}
}

func TestEnvironmentAddressOnlyForThisComputer(t *testing.T) {
	t.Setenv(URLEnv, "https://evil.example.com")
	if c := New(); c.url != DefaultURL {
		t.Fatalf("url = %q: the token would go there", c.url)
	}
	t.Setenv(URLEnv, "http://127.0.0.1:5555")
	if c := New(); c.url != "http://127.0.0.1:5555" {
		t.Fatalf("url = %q", c.url)
	}
	t.Setenv(URLEnv, "http://localhost:5555")
	if c := New(); c.url != "http://localhost:5555" {
		t.Fatalf("url = %q", c.url)
	}
	t.Setenv(URLEnv, "http://127.0.0.1.evil.example.com")
	if c := New(); c.url != DefaultURL {
		t.Fatalf("url = %q", c.url)
	}
}

func TestLinkChangesAreLogged(t *testing.T) {
	// The link tables travel between devices (the notes do, and a second device must find the same
	// notes); the last import time is a "connect." setting, which stays on its device.
	g := newRig(t)
	g.seed()
	g.on()
	g.importAll(false)
	rows, err := g.env.Store.Query(context.Background(), `SELECT DISTINCT object_type FROM changes WHERE command LIKE 'readwise.%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		types = append(types, s)
	}
	if !contains(types, "readwise_sources") || !contains(types, "readwise_highlights") {
		t.Fatalf("logged types = %v", types)
	}
}
