package connect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

const settingPrefix = "connect." // settings row "connect.<id>"; per device (see store/remote.go)

// Service keeps each connection's switch (in library.db) and key (in the keychain), and offers
// the connect.* commands.
type Service struct {
	secrets secrets.Store
	reg     *Registry
}

// NewService makes the service over a keychain and registry.
func NewService(s secrets.Store, reg *Registry) *Service { return &Service{secrets: s, reg: reg} }

// Registry is the set of connections the service knows.
func (s *Service) Registry() *Registry { return s.reg }

func keyName(id string) string { return "connect." + id + ".key" }

type stored struct {
	Enabled bool `json:"enabled"`
}

// Config reads a connection's switch and key. A missing library or setting means off.
func (s *Service) Config(ctx context.Context, env *command.Env, id string) (Config, error) {
	cfg := Config{}
	if env != nil && env.Store != nil {
		var raw string
		err := env.Store.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, settingPrefix+id).Scan(&raw)
		switch {
		case err == nil:
			var st stored
			if err := json.Unmarshal([]byte(raw), &st); err != nil {
				return cfg, command.Errorf("failed", "the saved setting for %s is damaged", id)
			}
			cfg.Enabled = st.Enabled
		case !errors.Is(err, sql.ErrNoRows):
			return cfg, err
		}
	}
	key, err := s.secrets.Get(keyName(id))
	if err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return cfg, command.Errorf("failed", "could not read the system keychain: %v", err)
	}
	cfg.Key = key
	return cfg, nil
}

// Require is what a connection's own commands call first: it returns the config, or an "off"
// error when the switch is off, so nothing is sent while a connection is off.
func (s *Service) Require(ctx context.Context, env *command.Env, id string) (Config, error) {
	cfg, err := s.Config(ctx, env, id)
	if err != nil {
		return cfg, err
	}
	if !cfg.Enabled {
		name := id
		if c, ok := s.reg.Get(id); ok {
			name = c.Name()
		}
		return cfg, command.Errorf("off", "%s is turned off. Turn it on in Settings → Connections first.", name)
	}
	return cfg, nil
}

func (s *Service) setEnabled(ctx context.Context, env *command.Env, id string, on bool) error {
	raw, _ := json.Marshal(stored{Enabled: on})
	name := "connect.disable"
	if on {
		name = "connect.enable"
	}
	return env.Store.Write(ctx, name, func(tx *store.Tx) error {
		k := store.Row{"key": settingPrefix + id}
		if _, err := tx.Get("settings", k); errors.Is(err, store.ErrNotFound) {
			return tx.Create("settings", store.Row{"key": settingPrefix + id, "value_json": string(raw)})
		} else if err != nil {
			return err
		}
		return tx.Update("settings", k, store.Row{"value_json": string(raw)})
	})
}

// Register adds the connect.* commands and returns the service, so connections' own commands can
// use Require.
func Register(r *command.Registry, s secrets.Store, conns ...Connection) *Service {
	reg := NewRegistry()
	for _, c := range conns {
		reg.Add(c)
	}
	svc := NewService(s, reg)
	r.Add(
		command.Define("connect.list", "List the connections (such as Zotero): what each can do, what it sends and where, whether it is on, and whether a key is saved. Never uses the network.", false, svc.list),
		command.Define("connect.enable", "Turn a connection on. Nothing is sent to it until then.", true, svc.enable),
		command.Define("connect.disable", "Turn a connection off. Nothing more is sent to it; what was cached stays.", true, svc.disable),
		command.Define("connect.set_key", "Save a connection's key in the system keychain. It is never shown again.", false, svc.setKey),
		command.Define("connect.clear_key", "Remove a connection's key from the system keychain.", false, svc.clearKey),
		command.Define("connect.test", "Check that a connection works now, and say in plain words what to do if it does not. Needs the connection turned on.", false, svc.test),
	)
	return svc
}

// Info is a connection as the interface shows it.
type Info struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	SignIn       SignIn       `json:"sign_in" enum:"key,app,folder"`
	Capabilities []Capability `json:"capabilities"`
	Sends        string       `json:"sends" desc:"One plain sentence: exactly what is sent and where"`
	Enabled      bool         `json:"enabled"`
	KeySet       bool         `json:"key_set"`
	Key          *KeyInfo     `json:"key,omitempty" desc:"Set when the connection takes a key"`
}

func (s *Service) info(ctx context.Context, env *command.Env, c Connection) (Info, error) {
	cfg, err := s.Config(ctx, env, c.ID())
	if err != nil {
		return Info{}, err
	}
	return Info{ID: c.ID(), Name: c.Name(), SignIn: c.SignIn(), Capabilities: c.Capabilities(), Sends: c.Sends(),
		Enabled: cfg.Enabled, KeySet: cfg.Key != "", Key: c.Key()}, nil
}

func (s *Service) list(ctx context.Context, env *command.Env, _ struct{}) ([]Info, error) {
	out := []Info{}
	for _, c := range s.reg.List() {
		i, err := s.info(ctx, env, c)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

// IDParams names a connection.
type IDParams struct {
	ID string `json:"id" required:"true" desc:"Connection ID, from connect.list (for example zotero)"`
}

func (s *Service) get(id string) (Connection, error) {
	c, ok := s.reg.Get(id)
	if !ok {
		return nil, command.Errorf("not_found", "no connection %q", id)
	}
	return c, nil
}

func (s *Service) enable(ctx context.Context, env *command.Env, p IDParams) (Info, error) {
	return s.switchTo(ctx, env, p.ID, true)
}

func (s *Service) disable(ctx context.Context, env *command.Env, p IDParams) (Info, error) {
	return s.switchTo(ctx, env, p.ID, false)
}

func (s *Service) switchTo(ctx context.Context, env *command.Env, id string, on bool) (Info, error) {
	c, err := s.get(id)
	if err != nil {
		return Info{}, err
	}
	if err := s.setEnabled(ctx, env, id, on); err != nil {
		return Info{}, err
	}
	return s.info(ctx, env, c)
}

// SetKeyParams carries a key. It is the only place a key travels; it is stored, never returned.
type SetKeyParams struct {
	ID  string `json:"id" required:"true" desc:"Connection ID, from connect.list"`
	Key string `json:"key" required:"true" desc:"The key; it goes to the system keychain and is never shown again"`
}

func (s *Service) setKey(ctx context.Context, env *command.Env, p SetKeyParams) (Info, error) {
	c, err := s.get(p.ID)
	if err != nil {
		return Info{}, err
	}
	if c.Key() == nil {
		return Info{}, command.Errorf("bad_params", "%s does not use a key", c.Name())
	}
	key := strings.TrimSpace(p.Key)
	if key == "" {
		return Info{}, command.Errorf("bad_params", "the key is empty")
	}
	if err := s.secrets.Set(keyName(p.ID), key); err != nil {
		return Info{}, command.Errorf("failed", "could not save to the system keychain: %v", err)
	}
	return s.info(ctx, env, c)
}

func (s *Service) clearKey(ctx context.Context, env *command.Env, p IDParams) (Info, error) {
	c, err := s.get(p.ID)
	if err != nil {
		return Info{}, err
	}
	if err := s.secrets.Delete(keyName(p.ID)); err != nil {
		return Info{}, command.Errorf("failed", "could not update the system keychain: %v", err)
	}
	return s.info(ctx, env, c)
}

// TestOutput is what connect.test returns.
type TestOutput struct {
	ID string `json:"id"`
	TestResult
}

func (s *Service) test(ctx context.Context, env *command.Env, p IDParams) (TestOutput, error) {
	c, err := s.get(p.ID)
	if err != nil {
		return TestOutput{}, err
	}
	cfg, err := s.Require(ctx, env, p.ID)
	if err != nil {
		return TestOutput{}, err
	}
	res, err := c.Test(ctx, cfg)
	if err != nil {
		return TestOutput{}, command.Errorf("failed", "%s: %v", c.Name(), err)
	}
	return TestOutput{ID: p.ID, TestResult: res}, nil
}

// SaveSecret keeps a secret a connection received while signing in (not one the person pasted), for
// example the key Any hands over once its code is typed. It goes to the keychain like any key.
func (s *Service) SaveSecret(id, value string) error { return s.secrets.Set(keyName(id), value) }

// DropSecret removes that secret from the keychain.
func (s *Service) DropSecret(id string) error { return s.secrets.Delete(keyName(id)) }
