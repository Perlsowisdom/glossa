package connect

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

type fake struct {
	id      string
	key     *KeyInfo
	tested  int
	gotCfg  Config
	outcome TestResult
}

func (f *fake) ID() string                 { return f.id }
func (f *fake) Name() string               { return "Fake " + f.id }
func (f *fake) SignIn() SignIn             { return SignInKey }
func (f *fake) Capabilities() []Capability { return []Capability{CanCite} }
func (f *fake) Sends() string              { return "Sends nothing, ever." }
func (f *fake) Key() *KeyInfo              { return f.key }
func (f *fake) Test(_ context.Context, cfg Config) (TestResult, error) {
	f.tested++
	f.gotCfg = cfg
	return f.outcome, nil
}

type rig struct {
	t    *testing.T
	reg  *command.Registry
	env  *command.Env
	keys *secrets.Memory
	svc  *Service
	f    *fake
}

func newRig(t *testing.T) *rig {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fake{id: "fake", key: &KeyInfo{Label: "Key", Help: "Get one.", Required: true}, outcome: TestResult{OK: true, Message: "ok"}}
	g := &rig{t: t, reg: command.NewRegistry(), env: &command.Env{Store: st}, keys: secrets.NewMemory(), f: f}
	g.svc = Register(g.reg, g.keys, f)
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

func TestOffByDefaultAndSwitch(t *testing.T) {
	g := newRig(t)
	var list []Info
	if err := g.run("connect.list", nil, &list); err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
	if list[0].Enabled || list[0].KeySet || list[0].Sends == "" || list[0].Key == nil {
		t.Errorf("%+v", list[0])
	}
	var err *command.Error
	e := g.run("connect.test", IDParams{ID: "fake"}, nil)
	if !asErr(e, &err) || err.Code != "off" {
		t.Fatalf("test while off: %v", e)
	}
	if g.f.tested != 0 {
		t.Error("a connection that is off must not be called")
	}
	var info Info
	if err := g.run("connect.enable", IDParams{ID: "fake"}, &info); err != nil || !info.Enabled {
		t.Fatalf("%v %+v", err, info)
	}
	var res TestOutput
	if err := g.run("connect.test", IDParams{ID: "fake"}, &res); err != nil || !res.OK || g.f.tested != 1 || !g.f.gotCfg.Enabled {
		t.Fatalf("%v %+v", err, res)
	}
	if err := g.run("connect.disable", IDParams{ID: "fake"}, &info); err != nil || info.Enabled {
		t.Fatalf("%v %+v", err, info)
	}
}

func asErr(err error, out **command.Error) bool {
	ce, ok := err.(*command.Error)
	*out = ce
	return ok
}

func TestKeyGoesToKeychainOnly(t *testing.T) {
	g := newRig(t)
	const secret = "s3cr3t-key-value"
	var info Info
	if err := g.run("connect.set_key", SetKeyParams{ID: "fake", Key: " " + secret + " "}, &info); err != nil || !info.KeySet {
		t.Fatalf("%v %+v", err, info)
	}
	if v, _ := g.keys.Get("connect.fake.key"); v != secret {
		t.Errorf("keychain holds %q", v)
	}
	b, _ := json.Marshal(info)
	if strings.Contains(string(b), secret) {
		t.Error("the key came back in a result")
	}
	g.run("connect.enable", IDParams{ID: "fake"}, nil)
	var raw string
	g.env.Store.QueryRow(context.Background(), `SELECT group_concat(value_json) FROM settings`).Scan(&raw)
	if strings.Contains(raw, secret) {
		t.Error("the key reached library.db")
	}
	g.run("connect.test", IDParams{ID: "fake"}, nil)
	if g.f.gotCfg.Key != secret {
		t.Errorf("the connection got %q", g.f.gotCfg.Key)
	}
	if err := g.run("connect.clear_key", IDParams{ID: "fake"}, &info); err != nil || info.KeySet {
		t.Fatalf("%v %+v", err, info)
	}
	if _, err := g.keys.Get("connect.fake.key"); err == nil {
		t.Error("key still stored")
	}
}

func TestBadRequests(t *testing.T) {
	g := newRig(t)
	for _, c := range []struct {
		id string
		p  any
	}{
		{"connect.enable", IDParams{ID: "nope"}},
		{"connect.set_key", SetKeyParams{ID: "fake", Key: "  "}},
		{"connect.set_key", SetKeyParams{ID: "nope", Key: "x"}},
	} {
		if err := g.run(c.id, c.p, nil); err == nil {
			t.Errorf("%s %v should fail", c.id, c.p)
		}
	}
	g.f.key = nil
	if err := g.run("connect.set_key", SetKeyParams{ID: "fake", Key: "x"}, nil); err == nil {
		t.Error("a connection with no key must refuse one")
	}
}

func TestRequire(t *testing.T) {
	g := newRig(t)
	if _, err := g.svc.Require(context.Background(), g.env, "fake"); err == nil {
		t.Fatal("off must refuse")
	}
	g.run("connect.enable", IDParams{ID: "fake"}, nil)
	if cfg, err := g.svc.Require(context.Background(), g.env, "fake"); err != nil || !cfg.Enabled {
		t.Fatalf("%v %v", cfg, err)
	}
	// No library open: off.
	if _, err := g.svc.Require(context.Background(), &command.Env{}, "fake"); err == nil {
		t.Error("no library means off")
	}
}

func TestRegistryRejectsBadIDs(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic")
		}
	}()
	NewRegistry().Add(&fake{id: "Bad ID"})
}
