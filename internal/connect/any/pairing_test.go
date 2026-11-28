package anyapp

import (
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/connect"
)

func (g *rig) status() AnyStatus {
	g.t.Helper()
	var s AnyStatus
	g.must("any.status", struct{}{}, &s)
	return s
}

func (g *rig) enabled() {
	g.t.Helper()
	g.must("connect.enable", connect.IDParams{ID: "any"}, nil)
}

func (g *rig) linkCount() int {
	g.t.Helper()
	var n int
	if err := g.env.Store.QueryRow(bg, `SELECT COUNT(*) FROM any_links`).Scan(&n); err != nil {
		g.t.Fatal(err)
	}
	return n
}

func TestOffByDefaultNothingAsked(t *testing.T) {
	g := newRig(t)
	for _, c := range []string{"any.status", "any.pair", "any.spaces", "any.pages", "any.send"} {
		var params any = struct{}{}
		if c == "any.send" {
			params = SendParams{Kind: "note", ID: "x"}
		}
		if err := g.run(c, params, nil); code(err) != "off" {
			t.Errorf("%s while off: %v", c, err)
		}
	}
	if n := len(g.fake.seen()); n != 0 {
		t.Fatalf("%d requests while off", n)
	}
}

func TestUnpairedOnlyStatusPairAndAddressWork(t *testing.T) {
	g := newRig(t)
	g.enabled()
	s := g.status()
	if !s.Found || s.State != "unpaired" || s.Paired || len(s.Spaces) != 2 || s.Spaces[0].Name != "Study" || s.Address != g.fake.srv.URL {
		t.Fatalf("%+v", s)
	}
	id := g.note("N", "text", "")
	for name, params := range map[string]any{
		"any.spaces": struct{}{}, "any.pages": PagesParams{SpaceID: spaceA}, "any.search": SearchParams{Query: "x", SpaceID: spaceA},
		"any.set_space": SetSpaceParams{SpaceID: spaceA}, "any.send": SendParams{Kind: "note", ID: id, SpaceID: spaceA},
		"any.import": ImportParams{SpaceID: spaceA, ObjectID: "x"},
	} {
		err := g.run(name, params, nil)
		if code(err) != "not_paired" || !strings.Contains(err.Error(), "Pair Glossa with Any first") {
			t.Errorf("%s unpaired: %v", name, err)
		}
	}
	g.must("any.address", struct{}{}, nil)
	// Unpaired, the only requests made are the probe, the health check and the list of spaces.
	for _, q := range g.fake.seen() {
		if q != "GET /v1/spaces" && q != "GET /v1/health" {
			t.Errorf("an unpaired Glossa asked Any: %s", q)
		}
	}
	// The status check does not count as pairing.
	if g.status().Paired {
		t.Fatal("status paired Glossa")
	}
}

func TestPairThenSendAndImportWork(t *testing.T) {
	g := newRig(t)
	g.enabled()
	var r PairResult
	g.must("any.pair", struct{}{}, &r)
	if !r.Paired {
		t.Fatalf("%+v", r)
	}
	if s := g.status(); !s.Paired || s.State != "paired" {
		t.Fatalf("%+v", s)
	}
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceA}, nil)
	id := g.note("Paired", "text", "")
	var res SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id}, &res)
	if res.State != "created" {
		t.Fatalf("%+v", res)
	}
	pg := g.fake.add(spaceA, "Seed", "body")
	g.must("any.import", ImportParams{ObjectID: pg}, &res)
	if res.State != "created" {
		t.Fatalf("%+v", res)
	}
	var out connect.TestOutput
	g.must("connect.test", connect.IDParams{ID: "any"}, &out)
	if !out.OK || out.Auth != AuthPaired {
		t.Fatalf("%+v", out)
	}
	// The pairing is per device and is not a secret: it is in the settings, and no key was saved.
	if v, _ := g.keys.Get("connect.any.key"); v != "" {
		t.Fatal("a key was saved")
	}
}

func TestUnpairStopsCallsAndKeepsLinks(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceA}, nil)
	id := g.note("Linked", "text", "")
	g.must("any.send", SendParams{Kind: "note", ID: id}, nil)
	g.must("any.unpair", struct{}{}, nil)
	if g.linkCount() != 1 {
		t.Fatal("unpairing dropped the link")
	}
	before := len(g.fake.seen())
	if err := g.run("any.send", SendParams{Kind: "note", ID: id}, nil); code(err) != "not_paired" {
		t.Fatalf("send after unpair: %v", err)
	}
	if err := g.run("any.spaces", struct{}{}, nil); code(err) != "not_paired" {
		t.Fatalf("spaces after unpair: %v", err)
	}
	for _, q := range g.fake.seen()[before:] {
		if q != "GET /v1/spaces" && q != "GET /v1/health" {
			t.Errorf("a call after unpairing: %s", q)
		}
	}
	// The chosen space is forgotten; pairing again finds the same page through the kept link.
	if d, _ := loadDefaultSpace(bg, g.env); d.SpaceID != "" {
		t.Fatalf("space kept: %+v", d)
	}
	g.must("any.pair", struct{}{}, nil)
	var st StatusResult
	g.must("any.sync_status", StatusParams{Kind: "note", ID: id, Check: true}, &st)
	if !st.Linked || st.State != "in_sync" {
		t.Fatalf("%+v", st)
	}
}

func TestTurningAnyOffUnpairs(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.must("connect.disable", connect.IDParams{ID: "any"}, nil)
	g.enabled()
	if s := g.status(); s.Paired || s.State != "unpaired" {
		t.Fatalf("still paired after turning off and on: %+v", s)
	}
}

func TestADifferentAccountUnpairs(t *testing.T) {
	g := newRig(t)
	g.paired()
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceA}, nil)
	id := g.note("N", "x", "")
	g.must("any.send", SendParams{Kind: "note", ID: id}, nil)
	g.fake.account = "acct-2"
	err := g.run("any.send", SendParams{Kind: "note", ID: id}, nil)
	if code(err) != "not_paired" || !strings.Contains(err.Error(), "different Any account; pair again") {
		t.Fatalf("%v", err)
	}
	if g.linkCount() != 1 {
		t.Fatal("the link was dropped")
	}
	if s := g.status(); s.Paired || s.State != "unpaired" {
		t.Fatalf("%+v", s)
	}
	// Pairing with the new account works.
	g.must("any.pair", struct{}{}, nil)
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceA}, nil)
	// And a test says so when the account changes again.
	g.fake.account = "acct-3"
	var out connect.TestOutput
	g.must("connect.test", connect.IDParams{ID: "any"}, &out)
	if out.OK || out.Auth != AuthDifferent || !strings.Contains(out.Message, "different Any account") {
		t.Fatalf("%+v", out)
	}
}

func TestAnyThatRefusesIsSaidSo(t *testing.T) {
	g := newRig(t)
	g.enabled()
	g.fake.refuse = true
	var out connect.TestOutput
	g.must("connect.test", connect.IDParams{ID: "any"}, &out)
	if out.OK || out.Auth != AuthRefused || out.Message != "Any refused Glossa. Check Any’s own settings, then try again." {
		t.Fatalf("%+v", out)
	}
	if err := g.run("any.pair", struct{}{}, nil); code(err) != "refused" {
		t.Fatalf("%v", err)
	}
	if s := g.status(); s.State != "refused" {
		t.Fatalf("%+v", s)
	}
}

func TestAnOldSavedKeyIsDeletedQuietly(t *testing.T) {
	g := newRig(t)
	g.keys.Set("connect.any.key", "an-old-key-do-not-leak")
	// The next start registers the commands again over the same keychain.
	g2 := newRigOver(t, g.keys)
	if v, _ := g2.keys.Get("connect.any.key"); v != "" {
		t.Fatalf("the old key is still there: %q", v)
	}
	g2.paired()
	for _, h := range g2.fake.hdrs {
		if h.Get("Authorization") != "" {
			t.Fatal("a key was sent")
		}
	}
}

func TestStatusMessagesWhenNotFound(t *testing.T) {
	g := newRig(t)
	g.enabled()
	g.fake.srv.Close()
	s := g.status()
	if s.Found || s.State != "not_found" || s.Message != notFound {
		t.Fatalf("%+v", s)
	}
	var out connect.TestOutput
	g.must("connect.test", connect.IDParams{ID: "any"}, &out)
	if out.OK || out.Message != notFound {
		t.Fatalf("%+v", out)
	}
}
