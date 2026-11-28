package anyapp

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/connect"
	"github.com/Perlsowisdom/glossa/internal/document"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// These tests never touch the real Any app: every address is a fake on a free loopback port, or a
// port that was free a moment ago and is closed.

// deadURL is a loopback address nothing listens on.
func deadURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u := "http://" + l.Addr().String()
	l.Close()
	return u
}

// findRig is a rig whose Conn looks for Any among candidates instead of being given one address.
func findRig(t *testing.T, candidates ...string) *rig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &rig{t: t, env: &command.Env{Store: st}, keys: secrets.NewMemory(), reg: command.NewRegistry()}
	g.conn = New(WithCandidates(candidates...))
	core.Register(g.reg)
	document.Register(g.reg)
	svc := connect.Register(g.reg, g.keys, g.conn)
	g.conn.Register(g.reg, svc)
	t.Cleanup(g.reg.Close)
	g.must("connect.enable", connect.IDParams{ID: "any"}, nil)
	return g
}

func (g *rig) test() connect.TestOutput {
	g.t.Helper()
	var out connect.TestOutput
	g.must("connect.test", connect.IDParams{ID: "any"}, &out)
	return out
}

func (g *rig) address() AddressResult {
	g.t.Helper()
	var a AddressResult
	g.must("any.address", struct{}{}, &a)
	return a
}

func TestFindsThePrimaryAddressFirst(t *testing.T) {
	primary, fallback := newFake(t), newFake(t)
	primary.noKey, fallback.noKey = true, true
	g := findRig(t, primary.srv.URL, fallback.srv.URL)
	if out := g.test(); !out.OK {
		t.Fatalf("%+v", out)
	}
	if len(fallback.seen()) != 0 {
		t.Fatalf("the fallback was asked although the first answered: %v", fallback.seen())
	}
	if a := g.address(); a.Address != primary.srv.URL || a.Manual {
		t.Fatalf("remembered %+v", a)
	}
}

func TestFallsBackToTheSecondAddressAndRemembersIt(t *testing.T) {
	dead := deadURL(t)
	fallback := newFake(t)
	fallback.noKey = true
	g := findRig(t, dead, fallback.srv.URL)
	if out := g.test(); !out.OK {
		t.Fatalf("%+v", out)
	}
	if a := g.address(); a.Address != fallback.srv.URL {
		t.Fatalf("remembered %+v", a)
	}
	// Next time the remembered address is asked first, even when the usual ones are all quiet.
	g.conn.candidates = []string{dead, deadURL(t)}
	if out := g.test(); !out.OK {
		t.Fatalf("with the remembered address: %+v", out)
	}
}

func TestNeitherAddressAnswers(t *testing.T) {
	g := findRig(t, deadURL(t), deadURL(t))
	out := g.test()
	if out.OK || out.Message != notFound {
		t.Fatalf("%+v", out)
	}
	err := g.run("any.spaces", struct{}{}, nil)
	if code(err) != "unavailable" || !strings.Contains(err.Error(), notFound) {
		t.Fatalf("%v", err)
	}
	if a := g.address(); a.Address != "" {
		t.Fatalf("an address was remembered: %+v", a)
	}
}

func TestSomethingElseOnThePortIsNotAny(t *testing.T) {
	other := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(other.Close)
	g := findRig(t, other.URL)
	if out := g.test(); out.OK || out.Message != notFound {
		t.Fatalf("%+v", out)
	}
}

func TestATypedAddressIsTriedAlone(t *testing.T) {
	usual, typed := newFake(t), newFake(t)
	usual.noKey, typed.noKey = true, true
	g := findRig(t, usual.srv.URL)
	var a AddressResult
	g.must("any.set_address", SetAddressParams{Address: strings.TrimPrefix(typed.srv.URL, "http://")}, &a)
	if a.Address != typed.srv.URL || !a.Manual {
		t.Fatalf("%+v", a)
	}
	if out := g.test(); !out.OK {
		t.Fatalf("%+v", out)
	}
	if n := len(usual.seen()); n != 0 {
		t.Fatalf("the usual address was asked with one typed: %d requests", n)
	}
	// A typed address that does not answer is not replaced by finding.
	typed.srv.Close()
	if out := g.test(); out.OK || out.Message != notFound {
		t.Fatalf("%+v", out)
	}
	// Emptied, Glossa finds Any by itself again.
	g.must("any.set_address", SetAddressParams{}, &a)
	if a.Address != "" || a.Manual {
		t.Fatalf("%+v", a)
	}
	if out := g.test(); !out.OK {
		t.Fatalf("after clearing: %+v", out)
	}
}

func TestTheAddressStaysOnThisComputer(t *testing.T) {
	g := findRig(t)
	for _, bad := range []string{"http://example.com:31310", "192.168.1.20:31310", "https://any.example.org", "http://10.0.0.5"} {
		err := g.run("any.set_address", SetAddressParams{Address: bad}, nil)
		if code(err) != "not_local" || !strings.Contains(err.Error(), "not on this computer") {
			t.Errorf("%s: %v", bad, err)
		}
	}
	for _, bad := range []string{"ftp://127.0.0.1:1", "://", "not an address"} {
		if err := g.run("any.set_address", SetAddressParams{Address: bad}, nil); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if a := g.address(); a.Address != "" {
		t.Fatalf("a refused address was saved: %+v", a)
	}
	var a AddressResult
	g.must("any.set_address", SetAddressParams{Address: "localhost:31310/v1/spaces?x=1"}, &a)
	if a.Address != "http://localhost:31310" {
		t.Fatalf("%+v", a)
	}
	// A forced address off this computer is refused as well, with nothing sent.
	c := New(WithURL("http://example.com:31310"))
	out, err := c.Test(t.Context(), connect.Config{Enabled: true})
	if err != nil || out.OK || !strings.Contains(out.Message, "not on this computer") {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestNoKeyNeededWhenAnyDoesNotAskForOne(t *testing.T) {
	f := newFake(t)
	f.noKey = true
	g := findRig(t, f.srv.URL)
	out := g.test()
	want := "Connected (Any didn’t ask for a key). 2 spaces are available."
	if !out.OK || out.Auth != AuthNone || out.Message != want {
		t.Fatalf("%+v", out)
	}
	g.must("any.spaces", struct{}{}, nil)
	g.must("any.set_space", SetSpaceParams{SpaceID: spaceA}, nil)
	id := g.note("Keyless", "sent without a key", "John 3:16")
	var res SyncResult
	g.must("any.send", SendParams{Kind: "note", ID: id}, &res)
	if res.State != "created" {
		t.Fatalf("%+v", res)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.hdrs {
		if h.Get("Authorization") != "" {
			t.Fatalf("a key was sent: %v", h)
		}
	}
	if v, _ := g.keys.Get("connect.any.key"); v != "" {
		t.Fatal("a key was saved")
	}
}

func TestKeyNeededOffersSigningIn(t *testing.T) {
	f := newFake(t)
	g := findRig(t, f.srv.URL)
	out := g.test()
	if out.OK || out.Auth != AuthNeeded || !strings.Contains(out.Message, "not signed in yet") || !strings.Contains(out.Message, "Sign in to Any") {
		t.Fatalf("%+v", out)
	}
	if err := g.run("any.spaces", struct{}{}, nil); code(err) != "not_paired" {
		t.Fatalf("%v", err)
	}
	var st PairStartResult
	g.must("any.pair_start", struct{}{}, &st)
	g.must("any.pair_finish", PairFinishParams{ChallengeID: st.ChallengeID, Code: goodCode}, nil)
	if out := g.test(); !out.OK || out.Auth != AuthOK || !strings.HasPrefix(out.Message, "Connected to Any.") {
		t.Fatalf("%+v", out)
	}
	g.must("any.spaces", struct{}{}, nil)
}

func TestAStaleKeyIsSaidSoAndPairingIsOffered(t *testing.T) {
	f := newFake(t)
	g := findRig(t, f.srv.URL)
	var st PairStartResult
	g.must("any.pair_start", struct{}{}, &st)
	g.must("any.pair_finish", PairFinishParams{ChallengeID: st.ChallengeID, Code: goodCode}, nil)
	f.badKey = true
	out := g.test()
	want := "Any no longer accepts Glossa’s key. Choose “Sign in to Any” to pair again."
	if out.OK || out.Auth != AuthStale || out.Message != want {
		t.Fatalf("%+v", out)
	}
	err := g.run("any.spaces", struct{}{}, nil)
	if code(err) != "not_paired" || !strings.Contains(err.Error(), "no longer accepts") {
		t.Fatalf("%v", err)
	}
	// Pairing again works, and the key is replaced.
	f.badKey = false
	g.must("any.pair_start", struct{}{}, &st)
	g.must("any.pair_finish", PairFinishParams{ChallengeID: st.ChallengeID, Code: goodCode}, nil)
	if out := g.test(); !out.OK {
		t.Fatalf("%+v", out)
	}
}

func TestASavedKeyIsStillSentWhenAnyAsksForNone(t *testing.T) {
	f := newFake(t)
	f.noKey = true
	g := findRig(t, f.srv.URL)
	if err := g.keys.Set("connect.any.key", secretKey); err != nil {
		t.Fatal(err)
	}
	if out := g.test(); !out.OK || out.Auth != AuthOK {
		t.Fatalf("%+v", out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	last := f.hdrs[len(f.hdrs)-1]
	if last.Get("Authorization") != "Bearer "+secretKey {
		t.Fatalf("the saved key was not sent: %v", last)
	}
}
