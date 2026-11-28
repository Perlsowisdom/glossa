package anyapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// Pairing. Any's local API checks nobody yet, so Glossa asks the person once: until they pair Glossa
// with Any, nothing is read from it or sent to it beyond finding it and listing its spaces. The state is
// kept per device in the setting connect.any.pairing (it is not a secret). Everything that decides
// whether Glossa may use Any is in this file: gate.

const pairingSetting = "connect.any.pairing"

type pairingState struct {
	Paired  bool   `json:"paired"`
	Account string `json:"account,omitempty"` // the Any account that was open when the person paired
	// Key is empty today. It is where a credential issued by Any itself would go, if Any ever hands
	// them out; the client sends it as a bearer key when it is set.
	Key string `json:"key,omitempty"`
}

func loadPairing(ctx context.Context, env *command.Env) (pairingState, error) {
	if env == nil || env.Store == nil {
		return pairingState{}, nil
	}
	var raw string
	err := env.Store.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, pairingSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return pairingState{}, nil
	}
	if err != nil {
		return pairingState{}, err
	}
	var p pairingState
	json.Unmarshal([]byte(raw), &p)
	return p, nil
}

func savePairing(ctx context.Context, env *command.Env, p pairingState) error {
	return env.Store.Write(ctx, "any.pairing", func(tx *store.Tx) error {
		k := store.Row{"key": pairingSetting}
		if !p.Paired {
			if err := tx.Delete("settings", k); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			return nil
		}
		raw, _ := json.Marshal(p)
		if _, err := tx.Get("settings", k); errors.Is(err, store.ErrNotFound) {
			return tx.Create("settings", store.Row{"key": pairingSetting, "value_json": string(raw)})
		} else if err != nil {
			return err
		}
		return tx.Update("settings", k, store.Row{"value_json": string(raw)})
	})
}

// forgetPairing clears the pairing and the chosen space. The links to pages are kept, so pairing again
// finds the same pages.
func forgetPairing(ctx context.Context, env *command.Env) error {
	if err := savePairing(ctx, env, pairingState{}); err != nil {
		return err
	}
	return env.Store.Write(ctx, "any.set_space", func(tx *store.Tx) error {
		err := tx.Delete("settings", store.Row{"key": spaceSetting})
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	})
}

// gate is the one place that says whether Glossa may use the Any it found: it must be paired, and with
// the account that is open now. A different account undoes the pairing. It returns the credential to
// send, which is empty today.
func gate(ctx context.Context, env *command.Env, account string) (string, error) {
	p, err := loadPairing(ctx, env)
	if err != nil {
		return "", err
	}
	if !p.Paired {
		return "", ErrNotPaired
	}
	if account != "" && p.Account != "" && account != p.Account {
		if err := forgetPairing(ctx, env); err != nil {
			return "", err
		}
		return "", ErrDifferent
	}
	return p.Key, nil
}
