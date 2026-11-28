package anyapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Perlsowisdom/glossa/internal/appdir"
	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect"
)

// URLEnv forces one address on this computer and turns off finding, for tests. It must be on this
// computer.
const URLEnv = "GLOSSA_ANY_URL"

// CandidatesEnv replaces the addresses tried when none is saved (comma separated, in order), for tests,
// so a test never reaches the real Any app's ports.
const CandidatesEnv = "GLOSSA_ANY_CANDIDATES"

// probeTimeout is how long Glossa waits for each address to answer while looking for Any.
const probeTimeout = time.Second

// Conn is the Any connection. It implements connect.Connection.
type Conn struct {
	url        string   // when set, the only address used
	candidates []string // tried in order when no address is saved
	timeout    time.Duration
	http       *http.Client
}

// Option customises New, mainly for tests.
type Option func(*Conn)

// WithURL sets the only address of the Any app, and turns off finding.
func WithURL(u string) Option { return func(c *Conn) { c.url = u } }

// WithCandidates sets the addresses tried, in order, when none is saved.
func WithCandidates(u ...string) Option { return func(c *Conn) { c.candidates = u } }

// New makes the Any connection. With no address forced, it looks for the Any app on this computer at
// its usual ports (31310, then 31009), unless the person saved an address.
func New(opts ...Option) *Conn {
	c := &Conn{candidates: []string{DefaultURL, FallbackURL}, timeout: probeTimeout,
		http: &http.Client{Timeout: 30 * time.Second,
			Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}}}
	if u := appdir.Getenv(URLEnv); u != "" {
		c.url = u
	}
	if l := appdir.Getenv(CandidatesEnv); l != "" {
		c.candidates = nil
		for _, u := range strings.Split(l, ",") {
			if u = strings.TrimSpace(u); u != "" {
				c.candidates = append(c.candidates, u)
			}
		}
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Close releases idle connections.
func (c *Conn) Close() { c.http.CloseIdleConnections() }

func (c *Conn) ID() string   { return "any" }
func (c *Conn) Name() string { return "Any" }

func (c *Conn) SignIn() connect.SignIn { return connect.SignInApp }

func (c *Conn) Capabilities() []connect.Capability {
	return []connect.Capability{connect.CanImport, connect.CanExport}
}

func (c *Conn) Sends() string {
	return "Glossa talks only to the Any app on this computer: it sends the notes and sermons you choose, " +
		"and asks for the pages you pick; nothing leaves this computer."
}

// Key is nil: there is no key to paste. Glossa asks the person to pair it instead (see pairing.go).
func (c *Conn) Key() *connect.KeyInfo { return nil }

// Disabled is called when the person turns Use Any off: that also unpairs, keeping the links.
func (c *Conn) Disabled(ctx context.Context, env *command.Env) error {
	if env == nil || env.Store == nil {
		return nil
	}
	return forgetPairing(ctx, env)
}

// Explain says what a failure means and what to do, in plain words.
func Explain(err error) string {
	switch {
	case errors.Is(err, ErrNotRunning):
		return "Glossa couldn’t find the Any app on this computer. Open Any, or enter its address under Advanced."
	case errors.Is(err, ErrNotPaired):
		return "Pair Glossa with Any first. In Settings → Connections choose “Pair with Any”."
	case errors.Is(err, ErrDifferent):
		return "This is a different Any account; pair again."
	case errors.Is(err, ErrRefused):
		return "Any refused Glossa. Check Any’s own settings, then try again."
	case errors.Is(err, ErrNotFound):
		return "Any has no such page or space. It may have been deleted."
	case errors.Is(err, ErrConflict):
		return "That page changed in Any since Glossa read it. Try again."
	case errors.Is(err, ErrRateLimited):
		return "Any asked Glossa to slow down. Try again in a minute."
	case errors.Is(err, ErrBadAddress):
		return "That is not an address. Enter it like 127.0.0.1:31310."
	case errors.Is(err, ErrNotLocal):
		return "Glossa only talks to the Any app on this computer, and the address given is not on this computer."
	}
	return "Any did not answer as expected: " + err.Error()
}

// Test checks the link: that the Any app can be found, and whether Glossa is paired with it.
func (c *Conn) Test(ctx context.Context, cfg connect.Config) (connect.TestResult, error) {
	return c.TestEnv(ctx, nil, cfg)
}

// Pairing states a test reports, for the interface.
const (
	AuthPaired    = "paired"    // Glossa is paired with the Any that is open
	AuthUnpaired  = "unpaired"  // Any is running and Glossa is not paired with it yet
	AuthDifferent = "different" // another Any account is open than the one Glossa was paired with
	AuthRefused   = "refused"   // Any refused Glossa
)

// TestEnv is Test with the library, where the saved address and the pairing live.
func (c *Conn) TestEnv(ctx context.Context, env *command.Env, cfg connect.Config) (connect.TestResult, error) {
	f, err := c.find(ctx, env)
	if err != nil {
		if ctx.Err() != nil {
			return connect.TestResult{}, ctx.Err()
		}
		return connect.TestResult{Message: Explain(err)}, nil
	}
	via := "the Any app on this computer"
	if f.status != http.StatusOK {
		return connect.TestResult{Via: via, Auth: AuthRefused, Message: Explain(ErrRefused)}, nil
	}
	key, err := gate(ctx, env, f.account)
	spaces, serr := f.cl.Spaces(ctx, key)
	switch {
	case ctx.Err() != nil:
		return connect.TestResult{}, ctx.Err()
	case errors.Is(serr, ErrRefused):
		return connect.TestResult{Via: via, Auth: AuthRefused, Message: Explain(serr)}, nil
	case serr != nil:
		return connect.TestResult{Message: Explain(serr)}, nil
	case errors.Is(err, ErrNotPaired):
		return connect.TestResult{Via: via, Auth: AuthUnpaired,
			Message: fmt.Sprintf("Any is running (%s), but Glossa is not paired with it yet.", countSpaces(len(spaces)))}, nil
	case errors.Is(err, ErrDifferent):
		return connect.TestResult{Via: via, Auth: AuthDifferent, Message: Explain(err)}, nil
	case err != nil:
		return connect.TestResult{}, err
	}
	return connect.TestResult{OK: true, Via: via, Auth: AuthPaired,
		Message: fmt.Sprintf("Connected to Any. %s.", countSpaces(len(spaces)))}, nil
}

func countSpaces(n int) string {
	if n == 1 {
		return "1 space is available"
	}
	return fmt.Sprintf("%d spaces are available", n)
}
