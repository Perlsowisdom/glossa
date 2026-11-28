package anyapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

const addressSetting = "connect.any.address" // per device, like every connect.* setting

// savedAddress is where Glossa last found Any, or the address the person typed.
type savedAddress struct {
	Address string `json:"address"`
	Manual  bool   `json:"manual"` // typed by the person: tried alone, never replaced by finding
}

func loadAddress(ctx context.Context, env *command.Env) (savedAddress, error) {
	if env == nil || env.Store == nil {
		return savedAddress{}, nil
	}
	var raw string
	err := env.Store.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, addressSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return savedAddress{}, nil
	}
	if err != nil {
		return savedAddress{}, err
	}
	var a savedAddress
	json.Unmarshal([]byte(raw), &a)
	return a, nil
}

func saveAddress(ctx context.Context, env *command.Env, a savedAddress) error {
	k := store.Row{"key": addressSetting}
	return env.Store.Write(ctx, "any.set_address", func(tx *store.Tx) error {
		if a.Address == "" {
			if err := tx.Delete("settings", k); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			return nil
		}
		raw, _ := json.Marshal(a)
		if _, err := tx.Get("settings", k); errors.Is(err, store.ErrNotFound) {
			return tx.Create("settings", store.Row{"key": addressSetting, "value_json": string(raw)})
		} else if err != nil {
			return err
		}
		return tx.Update("settings", k, store.Row{"value_json": string(raw)})
	})
}

// ErrBadAddress is an address that is not a web address at all.
var ErrBadAddress = errors.New("any: that is not an address")

// CleanAddress turns what the person typed (127.0.0.1:31310, localhost:31310, http://...) into
// scheme://host:port, and refuses anything not on this computer.
func CleanAddress(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrBadAddress
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrBadAddress
	}
	root := u.Scheme + "://" + u.Host
	if _, err := NewClient(root, nil); err != nil {
		return "", err
	}
	return root, nil
}

// found is the Any app that answered: a client for it, the status the probe got (200, or 401 or 403 when
// Any refuses Glossa) and the id of the account open in it ("" when Any did not say).
type found struct {
	cl      *Client
	status  int
	account string
}

// find looks for the Any app and returns it. A forced address or a typed one is tried
// alone. Otherwise the remembered address goes first, then each usual one in order; the one that
// answers is remembered for next time.
func (c *Conn) find(ctx context.Context, env *command.Env) (found, error) {
	var tries []string
	remember := false
	saved, err := loadAddress(ctx, env)
	if err != nil {
		return found{}, err
	}
	switch {
	case c.url != "":
		tries = []string{c.url}
	case saved.Manual && saved.Address != "":
		tries = []string{saved.Address}
	default:
		remember = env != nil && env.Store != nil
		if saved.Address != "" {
			tries = append(tries, saved.Address)
		}
		for _, u := range c.candidates {
			if u != saved.Address {
				tries = append(tries, u)
			}
		}
	}
	for _, u := range tries {
		cl, err := NewClient(u, c.http)
		if err != nil {
			return found{}, err
		}
		pctx, cancel := context.WithTimeout(ctx, c.timeout)
		status, err := cl.Probe(pctx)
		cancel()
		if ctx.Err() != nil {
			return found{}, ctx.Err()
		}
		if err != nil {
			continue
		}
		if remember && cl.Root != saved.Address {
			if err := saveAddress(ctx, env, savedAddress{Address: cl.Root}); err != nil {
				return found{}, err
			}
		}
		f := found{cl: cl, status: status}
		if status == 200 {
			hctx, hcancel := context.WithTimeout(ctx, c.timeout)
			f.account, _ = cl.Health(hctx, "")
			hcancel()
			if ctx.Err() != nil {
				return found{}, ctx.Err()
			}
		}
		return f, nil
	}
	return found{}, ErrNotRunning
}
