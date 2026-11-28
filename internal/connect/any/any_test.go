package anyapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
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

const notFound = "Glossa couldn’t find the Any app on this computer. Open Any, or enter its address under Advanced."

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
	if out := test(); out.OK || out.Auth != AuthStale || !strings.Contains(out.Message, "no longer accepts") {
		t.Fatalf("revoked: %+v", out)
	}
	// Any is not running.
	g.fake.srv.Close()
	if out := test(); out.OK || out.Message != notFound {
		t.Fatalf("not running: %+v", out)
	}
	if err := g.run("any.spaces", struct{}{}, nil); code(err) != "unavailable" || !strings.Contains(err.Error(), notFound) {
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

func TestEveryRequestCarriesTheKeyOnlyWhenSaved(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.must("any.spaces", struct{}{}, nil)
	g.fake.mu.Lock()
	defer g.fake.mu.Unlock()
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

func TestSpacesAndDefault(t *testing.T) {
	g := newRig(t)
	g.paired()
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
	if r.State != "created" || r.ObjectID == "" {
		t.Fatalf("first send: %+v", r)
	}
	o := g.fake.objs[r.ObjectID]
	if o.Name != "Romans 8" || o.Markdown != "# Hope\n\nNo condemnation." {
		t.Fatalf("page = %+v", o)
	}
	// Nothing changed: nothing is sent.
	before := len(g.fake.seen())
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "unchanged" {
		t.Fatalf("resend unchanged: %+v", r)
	}
	for _, q := range g.fake.seen()[before:] {
		if strings.HasPrefix(q, "PUT") || strings.HasPrefix(q, "POST") {
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
	g.must("document.update", map[string]any{"id": d.ID, "body": "Saved by grace, through faith."}, nil)
	g.must("any.send", SendParams{Kind: "sermon", ID: d.ID}, &r)
	if r.State != "updated" || len(g.fake.objs) != 1 {
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
		if strings.HasPrefix(q, "PUT") {
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

func TestSendUpdateConflictAsksToTryAgain(t *testing.T) {
	g := newRig(t)
	g.paired()
	id := g.note("T", "one", "")
	var r SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id, SpaceID: spaceA}, &r)
	g.setNote(id, "T", "two")
	g.fake.conflictNext = true
	err := g.run("any.send", SendParams{Kind: "note", ID: id}, &r)
	if code(err) != "conflict" || !strings.Contains(err.Error(), "changed in Any") || !strings.Contains(err.Error(), "Try again") {
		t.Fatalf("%v", err)
	}
	g.must("any.send", SendParams{Kind: "note", ID: id}, &r)
	if r.State != "updated" || g.fake.objs[r.ObjectID].Markdown != "two" {
		t.Fatalf("%+v", r)
	}
}
