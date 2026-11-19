// Package secrets keeps API keys and tokens in the operating system keychain (Windows Credential
// Manager, macOS Keychain, Secret Service on Linux). Nothing here writes to a database or file.
package secrets

import (
	"errors"
	"sync"

	"github.com/Perlsowisdom/glossa/internal/appdir"
	"github.com/zalando/go-keyring"
)

// Service is the keychain service name all Glossa secrets are filed under. The interface tests
// set GLOSSA_KEYRING_SERVICE to file theirs under another name, apart from the person's own.
const Service = "glossa"

// ServiceEnv names the environment variable that overrides Service.
const ServiceEnv = "GLOSSA_KEYRING_SERVICE"

// ErrNotFound means no secret is stored under that name.
var ErrNotFound = errors.New("secrets: not found")

// Store is a keychain. Names are short identifiers such as "apibible.key".
type Store interface {
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}

type osStore struct{ service string }

// OS returns the operating system keychain.
func OS() Store {
	if s := appdir.Getenv(ServiceEnv); s != "" {
		return osStore{s}
	}
	return Migrating(osStore{Service}, osStore{LegacyService})
}

// LegacyService is the service name Xegesis filed secrets under, before the rename.
const LegacyService = "xegesis"

// Migrating returns a Store that reads from current and, when a name is not there, from legacy:
// the value found is saved to current, so the next read does not need legacy. Writes go to current
// only; Delete removes the name from both, so a deleted secret does not come back from legacy.
func Migrating(current, legacy Store) Store { return migrating{current, legacy} }

type migrating struct{ cur, old Store }

func (m migrating) Get(name string) (string, error) {
	v, err := m.cur.Get(name)
	if !errors.Is(err, ErrNotFound) {
		return v, err
	}
	v, err = m.old.Get(name)
	if err != nil {
		return "", err
	}
	// Best effort: if saving fails, the value is still returned and found again next time.
	_ = m.cur.Set(name, v)
	return v, nil
}

func (m migrating) Set(name, value string) error { return m.cur.Set(name, value) }

func (m migrating) Delete(name string) error {
	if err := m.cur.Delete(name); err != nil {
		return err
	}
	return m.old.Delete(name)
}

func (o osStore) Get(name string) (string, error) {
	v, err := keyring.Get(o.service, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func (o osStore) Set(name, value string) error { return keyring.Set(o.service, name, value) }

func (o osStore) Delete(name string) error {
	err := keyring.Delete(o.service, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Memory is an in-memory Store for tests.
type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

func (s *Memory) Get(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *Memory) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[name] = value
	return nil
}

func (s *Memory) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
	return nil
}
