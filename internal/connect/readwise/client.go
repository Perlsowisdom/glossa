// Package readwise connects Glossa to Readwise: it brings your highlights in through Readwise's export
// API (with your access token) as notes, one per source, attached to the library book when the title and
// author match. It only reads from Readwise, and only after the person turns it on.
//
// Written from Readwise's public API documentation (https://readwise.io/api_deets): the auth check
// GET /api/v2/auth/ and the export GET /api/v2/export/. No other code was used.
package readwise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultURL is Readwise's own address.
const DefaultURL = "https://readwise.io"

const (
	maxBody    = 64 << 20
	maxRetries = 3
	maxWait    = 90 * time.Second
	maxPages   = 2000 // a guard: far more than anyone's library
)

// Errors a caller can act on.
var (
	ErrBadToken    = errors.New("readwise: Readwise did not accept the token")
	ErrRateLimited = errors.New("readwise: Readwise asked to slow down")
)

// APIError is any other failure Readwise reported.
type APIError struct{ Status int }

func (e *APIError) Error() string { return fmt.Sprintf("readwise: Readwise answered %d", e.Status) }

// Tag is a tag as Readwise sends it: an object with a name, or a bare string.
type Tag string

func (t *Tag) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = Tag(s)
		return nil
	}
	var o struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*t = Tag(o.Name)
	return nil
}

// Highlight is one highlight of a source.
type Highlight struct {
	ID          json.Number `json:"id"`
	IsDeleted   bool        `json:"is_deleted"`
	IsDiscard   bool        `json:"is_discard"`
	Text        string      `json:"text"`
	Note        string      `json:"note"`
	Location    json.Number `json:"location"`
	LocationTyp string      `json:"location_type"`
	HighlightAt string      `json:"highlighted_at"`
	UpdatedAt   string      `json:"updated_at"`
	URL         string      `json:"url"`
	ReadwiseURL string      `json:"readwise_url"`
	Tags        []Tag       `json:"tags"`
}

// Source is a book, article or other thing the highlights came from.
type Source struct {
	ID           json.Number `json:"user_book_id"`
	IsDeleted    bool        `json:"is_deleted"`
	Title        string      `json:"title"`
	ReadableName string      `json:"readable_title"`
	Author       string      `json:"author"`
	Category     string      `json:"category"`
	SourceURL    string      `json:"source_url"`
	ReadwiseURL  string      `json:"readwise_url"`
	Tags         []Tag       `json:"book_tags"`
	DocumentNote string      `json:"document_note"`
	Highlights   []Highlight `json:"highlights"`
}

// Page is one page of the export.
type Page struct {
	Count   int      `json:"count"`
	Next    *string  `json:"nextPageCursor"`
	Results []Source `json:"results"`
}

// Client talks to Readwise.
type Client struct {
	Root  string
	Token string
	HTTP  *http.Client
	sleep func(context.Context, time.Duration) error
}

// NewClient makes a client for root.
func NewClient(root, token string, hc *http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	if sleep == nil {
		sleep = sleepCtx
	}
	return &Client{Root: strings.TrimRight(root, "/"), Token: token, HTTP: hc, sleep: sleep}, nil
}

// Loopback reports whether an address is on this computer.
func Loopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	h := u.Hostname()
	ip := net.ParseIP(h)
	return h == "localhost" || (ip != nil && ip.IsLoopback())
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// get makes one request, waiting and trying again when Readwise says 429. Errors never contain the
// token or the address.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		u := c.Root + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, errors.New("readwise: the address is not valid")
		}
		req.Header.Set("Authorization", "Token "+c.Token)
		req.Header.Set("User-Agent", "Glossa")
		res, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("readwise: could not reach Readwise")
		}
		body, rerr := io.ReadAll(io.LimitReader(res.Body, maxBody))
		res.Body.Close()
		if rerr != nil {
			return nil, errors.New("readwise: the answer from Readwise was cut off")
		}
		switch res.StatusCode {
		case http.StatusOK, http.StatusNoContent:
			return body, nil
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, ErrBadToken
		case http.StatusTooManyRequests:
			wait := 60 * time.Second // the limit is per minute
			if n, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && n >= 0 {
				wait = time.Duration(n) * time.Second
			}
			if attempt >= maxRetries || wait > maxWait {
				return nil, fmt.Errorf("%w (try again in %d seconds)", ErrRateLimited, int(wait.Seconds()))
			}
			if wait == 0 {
				wait = time.Millisecond
			}
			if err := c.sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		}
		return nil, &APIError{Status: res.StatusCode}
	}
}

// CheckToken asks Readwise whether the token is good.
func (c *Client) CheckToken(ctx context.Context) error {
	_, err := c.get(ctx, "/api/v2/auth/", nil)
	return err
}

// Export fetches every source with highlights updated after the time (all of them when it is empty),
// following the page cursor. Deleted highlights are included so an import can remove them.
func (c *Client) Export(ctx context.Context, updatedAfter string) ([]Source, error) {
	var out []Source
	cursor := ""
	for i := 0; i < maxPages; i++ {
		q := url.Values{"includeDeleted": {"true"}}
		if updatedAfter != "" {
			q.Set("updatedAfter", updatedAfter)
		}
		if cursor != "" {
			q.Set("pageCursor", cursor)
		}
		body, err := c.get(ctx, "/api/v2/export/", q)
		if err != nil {
			return nil, err
		}
		var p Page
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, errors.New("readwise: Readwise sent something Glossa could not read")
		}
		out = append(out, p.Results...)
		if p.Next == nil || *p.Next == "" {
			return out, nil
		}
		cursor = *p.Next
	}
	return nil, errors.New("readwise: Readwise has more pages than Glossa will read in one go")
}
