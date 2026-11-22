// Package zotero connects Glossa to Zotero, so sources in a person's Zotero library can be browsed,
// searched and cited. It prefers the local API of the Zotero 7 app on this computer and falls back to
// the Zotero web API (v3) with the person's own key. Written from Zotero's public documentation
// (https://www.zotero.org/support/dev/web_api/v3/basics and
// https://www.zotero.org/support/dev/zotero_7_for_developers). It only ever reads: nothing here
// changes a Zotero library.
package zotero

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
	"sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/connect/csljson"
)

const (
	// DefaultLocalURL is where Zotero 7 listens for other programs on this computer, once the
	// person has allowed it in Zotero's settings.
	DefaultLocalURL = "http://localhost:23119"
	// DefaultWebURL is the Zotero web API.
	DefaultWebURL = "https://api.zotero.org"

	maxBody    = 64 << 20
	pageLimit  = 100 // the web API's most per request
	maxWait    = 30 * time.Second
	maxRetries = 2
)

// Errors a caller can act on.
var (
	ErrNotRunning  = errors.New("zotero: the Zotero app is not running")
	ErrLocalOff    = errors.New("zotero: the Zotero app does not allow other programs yet")
	ErrBadKey      = errors.New("zotero: zotero.org did not accept the key")
	ErrNotFound    = errors.New("zotero: not found")
	ErrRateLimited = errors.New("zotero: zotero.org asked to slow down")
)

// APIError is any other failure reported by Zotero.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("zotero: server said %d %s", e.Status, strings.TrimSpace(e.Body))
}

// Client reads one library, either through the local app or through zotero.org.
type Client struct {
	// Root is the server, such as http://localhost:23119 or https://api.zotero.org.
	Root string
	// Library is the path of the library under Root: "/api/users/0" locally, "/users/<id>" on the web.
	Library string
	// Key is the web API key; empty for the local app.
	Key   string
	Local bool
	HTTP  *http.Client

	sleep func(context.Context, time.Duration) error
	now   func() time.Time

	mu           sync.Mutex
	backoffUntil time.Time
}

// NewLocal makes a client for the Zotero app on this computer.
func NewLocal(root string, hc *http.Client) *Client {
	return &Client{Root: strings.TrimRight(root, "/"), Library: "/api/users/0", Local: true, HTTP: hc, sleep: sleepCtx, now: time.Now}
}

// NewWeb makes a client for one user's library on zotero.org. Find userID with LookupUser.
func NewWeb(root, key, userID string, hc *http.Client) *Client {
	return &Client{Root: strings.TrimRight(root, "/"), Library: "/users/" + url.PathEscape(userID), Key: key, HTTP: hc, sleep: sleepCtx, now: time.Now}
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

type response struct {
	body        []byte
	total       int // Total-Results, or -1
	version     int // Last-Modified-Version, or 0
	notModified bool
	linkNext    bool
}

func (c *Client) get(ctx context.Context, root, path string, q url.Values, sinceVersion int) (*response, error) {
	for attempt := 0; ; attempt++ {
		if err := c.waitBackoff(ctx); err != nil {
			return nil, err
		}
		resp, wait, err := c.once(ctx, root, path, q, sinceVersion)
		if err == nil || wait == 0 {
			return resp, err
		}
		if attempt >= maxRetries || wait > maxWait {
			return nil, fmt.Errorf("%w (try again in %d seconds)", ErrRateLimited, int(wait.Seconds()))
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// waitBackoff honours a Backoff header from an earlier response: no request until it has passed.
func (c *Client) waitBackoff(ctx context.Context) error {
	c.mu.Lock()
	wait := c.backoffUntil.Sub(c.now())
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	if wait > maxWait {
		return fmt.Errorf("%w (try again in %d seconds)", ErrRateLimited, int(wait.Seconds()))
	}
	return c.sleep(ctx, wait)
}

// once makes one request. A non-zero wait with an error means the server asked to be left alone for
// that long (429 or 503 with Retry-After).
func (c *Client) once(ctx context.Context, root, path string, q url.Values, sinceVersion int) (*response, time.Duration, error) {
	u := root + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Zotero-API-Version", "3")
	req.Header.Set("User-Agent", "Glossa")
	if c.Key != "" {
		req.Header.Set("Zotero-API-Key", c.Key)
	}
	if sinceVersion > 0 {
		req.Header.Set("If-Modified-Since-Version", strconv.Itoa(sinceVersion))
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, c.transportError(err)
	}
	defer res.Body.Close()
	if b := res.Header.Get("Backoff"); b != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(b)); err == nil && n > 0 {
			c.mu.Lock()
			c.backoffUntil = c.now().Add(time.Duration(n) * time.Second)
			c.mu.Unlock()
		}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, 0, err
	}
	switch res.StatusCode {
	case http.StatusOK:
		r := &response{body: body, total: -1}
		if n, err := strconv.Atoi(res.Header.Get("Total-Results")); err == nil {
			r.total = n
		}
		r.version, _ = strconv.Atoi(res.Header.Get("Last-Modified-Version"))
		r.linkNext = strings.Contains(res.Header.Get("Link"), `rel="next"`)
		return r, 0, nil
	case http.StatusNotModified:
		v, _ := strconv.Atoi(res.Header.Get("Last-Modified-Version"))
		return &response{notModified: true, total: -1, version: v}, 0, nil
	case http.StatusNotFound:
		return nil, 0, ErrNotFound
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		wait := 5 * time.Second
		if n, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && n >= 0 {
			wait = time.Duration(n) * time.Second
		}
		if wait == 0 {
			wait = time.Millisecond
		}
		return nil, wait, ErrRateLimited
	case http.StatusForbidden, http.StatusUnauthorized:
		if c.Local {
			return nil, 0, ErrLocalOff
		}
		return nil, 0, ErrBadKey
	}
	return nil, 0, &APIError{Status: res.StatusCode, Body: clip(string(body), 200)}
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// transportError turns "connection refused" from the local app into ErrNotRunning. The address is
// removed from other errors: it is not useful, and the key is part of one of them.
func (c *Client) transportError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var op *net.OpError
	if c.Local && errors.As(err, &op) && op.Op == "dial" {
		return ErrNotRunning
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("zotero: could not reach the server: %w", err)
}

// Entry is one item with its Zotero key.
type Entry struct {
	Key  string       `json:"key"`
	Item csljson.Item `json:"item"`
}

// ItemsQuery chooses which items to list.
type ItemsQuery struct {
	Collection string // a collection key; empty for the whole library
	Query      string // quick search: titles, creators and years
	Keys       []string
	Start      int
	Limit      int    // at most 100
	Sort       string // title, creator, date, dateAdded, dateModified
	Descending bool
	// SinceVersion asks for nothing when the library has not changed since that version.
	SinceVersion int
}

// ItemsPage is one page of items.
type ItemsPage struct {
	Items       []Entry
	Total       int // items in the whole result; -1 when the server did not say
	NextStart   int // where the next page starts; 0 when this was the last
	Version     int // the library version this answer is from
	NotModified bool
}

// Items lists regular items (books, articles, ...; not notes or attachments) as CSL-JSON.
func (c *Client) Items(ctx context.Context, q ItemsQuery) (ItemsPage, error) {
	limit := q.Limit
	if limit <= 0 || limit > pageLimit {
		limit = pageLimit
	}
	v := url.Values{"format": {"csljson"}, "limit": {strconv.Itoa(limit)}}
	if q.Start > 0 {
		v.Set("start", strconv.Itoa(q.Start))
	}
	if q.Query != "" {
		v.Set("q", q.Query)
	}
	if q.Sort != "" {
		v.Set("sort", q.Sort)
		if q.Descending {
			v.Set("direction", "desc")
		} else {
			v.Set("direction", "asc")
		}
	}
	path := c.Library + "/items/top"
	switch {
	case len(q.Keys) > 0:
		path = c.Library + "/items"
		v.Set("itemKey", strings.Join(q.Keys, ","))
	case q.Collection != "":
		path = c.Library + "/collections/" + url.PathEscape(q.Collection) + "/items/top"
	}
	r, err := c.get(ctx, c.Root, path, v, q.SinceVersion)
	if err != nil {
		return ItemsPage{}, err
	}
	if r.notModified {
		return ItemsPage{NotModified: true, Version: r.version, Total: -1}, nil
	}
	items, err := csljson.Parse(r.body)
	if err != nil {
		return ItemsPage{}, err
	}
	page := ItemsPage{Total: r.total, Version: r.version}
	for _, it := range items {
		e := Map(it)
		page.Items = append(page.Items, e)
	}
	next := q.Start + limit
	if (r.total >= 0 && next < r.total) || (r.total < 0 && r.linkNext) {
		page.NextStart = next
	}
	return page, nil
}

// Item fetches one item by key.
func (c *Client) Item(ctx context.Context, key string) (Entry, int, error) {
	r, err := c.get(ctx, c.Root, c.Library+"/items/"+url.PathEscape(key), url.Values{"format": {"csljson"}}, 0)
	if err != nil {
		return Entry{}, 0, err
	}
	items, err := csljson.Parse(r.body)
	if err != nil {
		return Entry{}, 0, err
	}
	if len(items) == 0 {
		return Entry{}, 0, ErrNotFound
	}
	return Map(items[0]), r.version, nil
}

// Collection is a folder of items in Zotero.
type Collection struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty" desc:"Key of the collection this one is inside; empty at the top"`
	Items  int    `json:"items" desc:"How many items are directly in it"`
}

// Collections lists every collection (up to 5,000).
func (c *Client) Collections(ctx context.Context) ([]Collection, error) {
	var out []Collection
	for start := 0; start < 5000; start += pageLimit {
		v := url.Values{"format": {"json"}, "limit": {strconv.Itoa(pageLimit)}, "start": {strconv.Itoa(start)}}
		r, err := c.get(ctx, c.Root, c.Library+"/collections", v, 0)
		if err != nil {
			return nil, err
		}
		var raw []struct {
			Key  string `json:"key"`
			Data struct {
				Name   string          `json:"name"`
				Parent json.RawMessage `json:"parentCollection"`
			} `json:"data"`
			Meta struct {
				NumItems int `json:"numItems"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(r.body, &raw); err != nil {
			return nil, fmt.Errorf("zotero: collections: %w", err)
		}
		for _, x := range raw {
			col := Collection{Key: x.Key, Name: x.Data.Name, Items: x.Meta.NumItems}
			var p string
			if json.Unmarshal(x.Data.Parent, &p) == nil {
				col.Parent = p // false (no parent) does not decode as a string
			}
			out = append(out, col)
		}
		if len(raw) < pageLimit && !r.linkNext {
			break
		}
	}
	return out, nil
}

// LookupUser asks zotero.org which user a key belongs to (GET /keys/<key>).
func LookupUser(ctx context.Context, root, key string, hc *http.Client) (string, error) {
	c := &Client{Root: strings.TrimRight(root, "/"), Key: key, HTTP: hc, sleep: sleepCtx, now: time.Now}
	r, err := c.get(ctx, c.Root, "/keys/"+url.PathEscape(key), nil, 0)
	if errors.Is(err, ErrNotFound) {
		return "", ErrBadKey
	}
	if err != nil {
		return "", err
	}
	var out struct {
		UserID json.Number `json:"userID"`
	}
	if err := json.Unmarshal(r.body, &out); err != nil || out.UserID.String() == "" || out.UserID.String() == "0" {
		return "", ErrBadKey
	}
	return out.UserID.String(), nil
}
