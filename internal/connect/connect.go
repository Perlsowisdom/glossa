// Package connect is the framework for connections: optional links from Glossa to another program
// or service the person already uses (Zotero first), for importing, exporting and citing sources.
// ADR 0014 describes the design.
//
// Every connection is off until the person turns it on, and has one plain sentence saying exactly what
// it sends and where. Its secrets live in the operating system keychain (internal/integrations/secrets)
// and are never written to library.db, a file or a log, and no command returns them. The switch and
// the secrets are kept here, once, so a connection cannot forget to check them.
package connect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// SignIn is how the person signs in to a connection.
type SignIn string

const (
	SignInKey    SignIn = "key"    // a key or token they paste in
	SignInApp    SignIn = "app"    // a program running on this computer
	SignInFolder SignIn = "folder" // a folder on this computer
)

// Capability is what a connection can do.
type Capability string

const (
	CanImport Capability = "import" // bring things into Glossa
	CanExport Capability = "export" // send things out of Glossa
	CanCite   Capability = "cite"   // offer sources to cite
)

// KeyInfo describes the key a connection takes. Nil means it takes none.
type KeyInfo struct {
	Label    string `json:"label" desc:"What the key is called, in plain words"`
	Help     string `json:"help" desc:"Where the person gets it, in a sentence"`
	Required bool   `json:"required" desc:"False when the connection also works without one"`
}

// Config is what the framework knows about one connection when it calls it.
type Config struct {
	Enabled bool
	Key     string // from the keychain; empty when none is set
}

// TestResult is the outcome of a check.
type TestResult struct {
	OK      bool   `json:"ok"`
	Via     string `json:"via,omitempty" desc:"How it connected, in plain words, e.g. \"the Zotero app on this computer\""`
	Message string `json:"message" desc:"One plain sentence the person can read; says what to do when it failed"`
}

// Connection is one link to another program or service.
type Connection interface {
	// ID is a short lowercase identifier, such as "zotero". Commands and keychain names use it.
	ID() string
	// Name is what the person sees.
	Name() string
	SignIn() SignIn
	Capabilities() []Capability
	// Sends is one plain sentence saying exactly what is sent and where. It is shown beside the
	// switch, and docs/privacy.md says the same in full.
	Sends() string
	// Key describes the key it takes, or nil.
	Key() *KeyInfo
	// Test checks the link with the current settings. It makes network calls, so it is only
	// called while the connection is on. A failure the person can fix is a TestResult with
	// OK false; the error is for the unexpected.
	Test(ctx context.Context, cfg Config) (TestResult, error)
}

// Registry holds the known connections.
type Registry struct {
	mu    sync.Mutex
	conns map[string]Connection
}

func NewRegistry() *Registry { return &Registry{conns: map[string]Connection{}} }

// Add registers a connection. It panics on a duplicate or bad ID, like command registration.
func (r *Registry) Add(c Connection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := c.ID()
	if id == "" || id != strings.ToLower(id) || strings.ContainsAny(id, " ./") {
		panic(fmt.Sprintf("connect: bad ID %q", id))
	}
	if _, dup := r.conns[id]; dup {
		panic("connect: duplicate ID " + id)
	}
	r.conns[id] = c
}

// Get returns the connection with that ID.
func (r *Registry) Get(id string) (Connection, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.conns[id]
	return c, ok
}

// List returns every connection, ordered by name.
func (r *Registry) List() []Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Connection, 0, len(r.conns))
	for _, c := range r.conns {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// ErrOff means the connection's switch is off.
var ErrOff = errors.New("connect: this connection is turned off")
