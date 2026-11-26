package readwise

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Perlsowisdom/glossa/internal/appdir"
	"github.com/Perlsowisdom/glossa/internal/connect"
)

// URLEnv points Glossa at another address, for tests. It is honoured only for an address on this
// computer; anything else is ignored, so the token can never be sent somewhere the person did not agree to.
const URLEnv = "GLOSSA_READWISE_URL"

// Conn is the Readwise connection. It implements connect.Connection.
type Conn struct {
	url   string
	http  *http.Client
	sleep func(context.Context, time.Duration) error
	now   func() time.Time
}

// Option customises New, mainly for tests.
type Option func(*Conn)

// WithURL sets the address (tests; any address is accepted here, unlike the environment).
func WithURL(u string) Option { return func(c *Conn) { c.url = u } }

// WithSleep replaces the wait used for Retry-After, and the clock that stamps a sync.
func WithSleep(sleep func(context.Context, time.Duration) error, now func() time.Time) Option {
	return func(c *Conn) {
		if sleep != nil {
			c.sleep = sleep
		}
		if now != nil {
			c.now = now
		}
	}
}

// New makes the Readwise connection.
func New(opts ...Option) *Conn {
	c := &Conn{url: DefaultURL, sleep: sleepCtx, now: time.Now,
		http: &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}}}
	if u := appdir.Getenv(URLEnv); u != "" && Loopback(u) {
		c.url = u
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Close releases idle connections.
func (c *Conn) Close() { c.http.CloseIdleConnections() }

func (c *Conn) client(token string) (*Client, error) {
	return NewClient(c.url, token, c.http, c.sleep)
}

func (c *Conn) ID() string   { return "readwise" }
func (c *Conn) Name() string { return "Readwise" }

func (c *Conn) SignIn() connect.SignIn { return connect.SignInKey }

func (c *Conn) Capabilities() []connect.Capability { return []connect.Capability{connect.CanImport} }

func (c *Conn) Sends() string {
	return "Glossa sends your Readwise token to readwise.io to check it and to ask for your highlights (after the first import, with the time of the last one, so only newer highlights come back); " +
		"it only reads, and nothing from your library, books or notes is sent."
}

func (c *Conn) Key() *connect.KeyInfo {
	return &connect.KeyInfo{
		Label:    "Readwise token",
		Help:     "Copy your access token from readwise.io/access_token.",
		Required: true,
	}
}

// Explain says what a failure means and what to do, in plain words.
func Explain(err error) string {
	var ae *APIError
	switch {
	case errors.Is(err, ErrBadToken):
		return "Readwise did not accept this token. Copy it again from readwise.io/access_token."
	case errors.Is(err, ErrRateLimited):
		return "Readwise asked Glossa to slow down. Try again in a minute."
	case errors.As(err, &ae):
		return fmt.Sprintf("Readwise did not answer as expected (it said %d). Try again later.", ae.Status)
	}
	return err.Error()
}

// Test checks the token with Readwise.
func (c *Conn) Test(ctx context.Context, cfg connect.Config) (connect.TestResult, error) {
	if cfg.Key == "" {
		return connect.TestResult{Message: "Paste your Readwise token first, below."}, nil
	}
	cl, err := c.client(cfg.Key)
	if err != nil {
		return connect.TestResult{Message: Explain(err)}, nil
	}
	if err := cl.CheckToken(ctx); err != nil {
		if ctx.Err() != nil {
			return connect.TestResult{}, ctx.Err()
		}
		return connect.TestResult{Message: Explain(err)}, nil
	}
	return connect.TestResult{OK: true, Via: "readwise.io", Message: "Connected to Readwise."}, nil
}
