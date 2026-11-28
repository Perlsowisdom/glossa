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

	g := findRig(t, primary.srv.URL, fallback.srv.URL)
	if out := g.test(); out.Via == "" {
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

	g := findRig(t, dead, fallback.srv.URL)
	if out := g.test(); out.Via == "" {
		t.Fatalf("%+v", out)
	}
	if a := g.address(); a.Address != fallback.srv.URL {
		t.Fatalf("remembered %+v", a)
	}
	// Next time the remembered address is asked first, even when the usual ones are all quiet.
	g.conn.candidates = []string{dead, deadURL(t)}
	if out := g.test(); out.Via == "" {
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

	g := findRig(t, usual.srv.URL)
	var a AddressResult
	g.must("any.set_address", SetAddressParams{Address: strings.TrimPrefix(typed.srv.URL, "http://")}, &a)
	if a.Address != typed.srv.URL || !a.Manual {
		t.Fatalf("%+v", a)
	}
	if out := g.test(); out.Via == "" {
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
	if out := g.test(); out.Via == "" {
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
