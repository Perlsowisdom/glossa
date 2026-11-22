package zotero

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/appdir"
	"github.com/Perlsowisdom/glossa/internal/connect"
)

// Environment variables that point Glossa at other servers. They exist for tests and for people who
// run Zotero on another port; the settings screen does not offer them.
const (
	LocalURLEnv = "GLOSSA_ZOTERO_LOCAL_URL"
	WebURLEnv   = "GLOSSA_ZOTERO_WEB_URL"
)

// Where an answer came from, as the person would say it.
const (
	viaApp = "the Zotero app on this computer"
	viaWeb = "zotero.org"
)

// Conn is the Zotero connection. It implements connect.Connection.
type Conn struct {
	localURL, webURL string
	http             *http.Client
	local            *Client

	mu   sync.Mutex
	webs map[string]*Client // one per key, so a Backoff is remembered

	sleep func(context.Context, time.Duration) error
	now   func() time.Time
}

// Option customises New, mainly for tests.
type Option func(*Conn)

func WithLocalURL(u string) Option { return func(c *Conn) { c.localURL = u } }
func WithWebURL(u string) Option   { return func(c *Conn) { c.webURL = u } }

// WithSleep replaces the wait used for Backoff and Retry-After, and the clock they are measured on.
func WithSleep(sleep func(context.Context, time.Duration) error, now func() time.Time) Option {
	return func(c *Conn) { c.sleep, c.now = sleep, now }
}

// New makes the Zotero connection. The addresses come from the environment when set, else the
// defaults (the Zotero app on port 23119, and https://api.zotero.org).
func New(opts ...Option) *Conn {
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}}
	c := &Conn{localURL: DefaultLocalURL, webURL: DefaultWebURL, http: hc, webs: map[string]*Client{},
		sleep: sleepCtx, now: time.Now}
	if u := appdir.Getenv(LocalURLEnv); u != "" {
		c.localURL = u
	}
	if u := appdir.Getenv(WebURLEnv); u != "" {
		c.webURL = u
	}
	for _, o := range opts {
		o(c)
	}
	c.local = NewLocal(c.localURL, hc)
	c.local.sleep, c.local.now = c.sleep, c.now
	return c
}

// Close releases idle connections.
func (c *Conn) Close() { c.http.CloseIdleConnections() }

func (c *Conn) ID() string   { return "zotero" }
func (c *Conn) Name() string { return "Zotero" }

func (c *Conn) SignIn() connect.SignIn { return connect.SignInApp }

func (c *Conn) Capabilities() []connect.Capability {
	return []connect.Capability{connect.CanImport, connect.CanCite}
}

func (c *Conn) Sends() string {
	return "Glossa asks the Zotero app on this computer for your sources, and if you add a key, asks zotero.org " +
		"when the app is not available (sending the key and what you search for); it only reads, and sends nothing else."
}

func (c *Conn) Key() *connect.KeyInfo {
	return &connect.KeyInfo{
		Label:    "Zotero key",
		Help:     "Only needed when the Zotero app is not on this computer. Make one at zotero.org/settings/keys with read access to your library.",
		Required: false,
	}
}

// webClient returns the zotero.org client for a key, looking up whose key it is the first time.
func (c *Conn) webClient(ctx context.Context, key string) (*Client, error) {
	c.mu.Lock()
	wc := c.webs[key]
	c.mu.Unlock()
	if wc != nil {
		return wc, nil
	}
	id, err := LookupUser(ctx, c.webURL, key, c.http)
	if err != nil {
		return nil, err
	}
	wc = NewWeb(c.webURL, key, id, c.http)
	wc.sleep, wc.now = c.sleep, c.now
	c.mu.Lock()
	c.webs[key] = wc
	c.mu.Unlock()
	return wc, nil
}

// fallback reports whether a failure of the local app is a reason to try zotero.org.
func fallback(err error) bool {
	var ae *APIError
	return errors.Is(err, ErrNotRunning) || errors.Is(err, ErrLocalOff) || errors.As(err, &ae)
}

// use runs fn against the Zotero app on this computer and, if that cannot be reached and a key is
// set, against zotero.org. It returns where the answer came from.
func use[T any](ctx context.Context, c *Conn, cfg connect.Config, fn func(*Client) (T, error)) (T, string, error) {
	v, err := fn(c.local)
	if err == nil {
		return v, viaApp, nil
	}
	var zero T
	if cfg.Key == "" || !fallback(err) {
		return zero, "", err
	}
	wc, werr := c.webClient(ctx, cfg.Key)
	if werr != nil {
		return zero, "", werr
	}
	v, err = fn(wc)
	if err != nil {
		return zero, "", err
	}
	return v, viaWeb, nil
}

// Explain says what a failure means and what to do, in plain words.
func Explain(err error) string {
	switch {
	case errors.Is(err, ErrNotRunning):
		return "Zotero is not running. Open Zotero on this computer, or add a Zotero key to use zotero.org instead."
	case errors.Is(err, ErrLocalOff):
		return "Zotero is open but is not letting other programs in. In Zotero, open Settings → Advanced and tick " +
			"“Allow other applications on this computer to communicate with Zotero”, or add a Zotero key to use zotero.org instead."
	case errors.Is(err, ErrBadKey):
		return "zotero.org did not accept this key. Check that it was copied whole and that it may read your library."
	case errors.Is(err, ErrRateLimited):
		return "zotero.org asked Glossa to slow down. Try again in a minute."
	case errors.Is(err, ErrNotFound):
		return "Zotero has no such item."
	}
	return "Zotero did not answer as expected: " + err.Error()
}

// Test checks the link: the app on this computer first, then zotero.org when a key is saved.
func (c *Conn) Test(ctx context.Context, cfg connect.Config) (connect.TestResult, error) {
	_, via, err := use(ctx, c, cfg, func(cl *Client) (ItemsPage, error) {
		return cl.Items(ctx, ItemsQuery{Limit: 1})
	})
	if err != nil {
		if ctx.Err() != nil {
			return connect.TestResult{}, ctx.Err()
		}
		return connect.TestResult{Message: Explain(err)}, nil
	}
	msg := "Connected to Zotero"
	if via == viaWeb {
		msg = "Connected through zotero.org, because the Zotero app was not available"
	}
	return connect.TestResult{OK: true, Via: via, Message: msg + "."}, nil
}
