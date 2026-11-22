package anyapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Perlsowisdom/glossa/internal/appdir"
	"github.com/Perlsowisdom/glossa/internal/connect"
)

// URLEnv points Glossa at another address on this computer, for tests and for people whose Any app
// listens on another port. The address must be on this computer; the settings screen does not offer it.
const URLEnv = "GLOSSA_ANY_URL"

// Conn is the Any connection. It implements connect.Connection.
type Conn struct {
	url  string
	http *http.Client
}

// Option customises New, mainly for tests.
type Option func(*Conn)

// WithURL sets the address of the Any app.
func WithURL(u string) Option { return func(c *Conn) { c.url = u } }

// New makes the Any connection. The address comes from the environment when set, else it is the
// Any app's usual one on this computer.
func New(opts ...Option) *Conn {
	c := &Conn{url: DefaultURL, http: &http.Client{Timeout: 30 * time.Second,
		Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}}}
	if u := appdir.Getenv(URLEnv); u != "" {
		c.url = u
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Close releases idle connections.
func (c *Conn) Close() { c.http.CloseIdleConnections() }

func (c *Conn) client() (*Client, error) { return NewClient(c.url, c.http) }

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

// Key is nil: the key is not pasted in. Any hands it over when the person types the code it shows.
func (c *Conn) Key() *connect.KeyInfo { return nil }

// Explain says what a failure means and what to do, in plain words.
func Explain(err error) string {
	switch {
	case errors.Is(err, ErrNotRunning):
		return "The Any app is not running. Open Any on this computer and try again."
	case errors.Is(err, ErrNotPaired):
		return "Glossa is not signed in to Any. In Settings → Connections choose “Sign in to Any” and type the code Any shows."
	case errors.Is(err, ErrBadCode):
		return "Any did not accept that code. Check the 4 digits Any is showing, or start again."
	case errors.Is(err, ErrNotFound):
		return "Any has no such page or space. It may have been deleted."
	case errors.Is(err, ErrRateLimited):
		return "Any asked Glossa to slow down. Try again in a minute."
	case errors.Is(err, ErrNotLocal):
		return "Glossa only talks to the Any app on this computer, and the address given is not on this computer."
	}
	return "Any did not answer as expected: " + err.Error()
}

// Test checks the link: that the Any app is running, and that Glossa is signed in to it.
func (c *Conn) Test(ctx context.Context, cfg connect.Config) (connect.TestResult, error) {
	cl, err := c.client()
	if err != nil {
		return connect.TestResult{Message: Explain(err)}, nil
	}
	spaces, err := cl.Spaces(ctx, cfg.Key)
	switch {
	case ctx.Err() != nil:
		return connect.TestResult{}, ctx.Err()
	case errors.Is(err, ErrNotPaired) && cfg.Key == "":
		return connect.TestResult{Via: "the Any app on this computer",
			Message: "Any is running, but Glossa is not signed in yet. Choose “Sign in to Any” and type the code Any shows."}, nil
	case err != nil:
		return connect.TestResult{Message: Explain(err)}, nil
	}
	return connect.TestResult{OK: true, Via: "the Any app on this computer",
		Message: fmt.Sprintf("Connected to Any. %s.", countSpaces(len(spaces)))}, nil
}

func countSpaces(n int) string {
	if n == 1 {
		return "1 space is available"
	}
	return fmt.Sprintf("%d spaces are available", n)
}
