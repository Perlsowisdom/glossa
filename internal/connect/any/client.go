// Package anyapp connects Glossa to Any (the Any app) through the app's local API on this computer:
// send a note or sermon to a space as a page, and bring a page in as a note. It never talks to
// anything but the Any app on this computer, and only after the person turns it on.
//
// Written from the OpenAPI description the Any app publishes about itself ("Any API 1.0", served at
// /v1/openapi.json); see docs/sources/any-api.md. No code from the Any apps was read or used.
package anyapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

const (
	// DefaultURL is where the Any app listens for other programs on this computer.
	DefaultURL = "http://127.0.0.1:31310"
	// FallbackURL is the other port Glossa tries after DefaultURL.
	FallbackURL = "http://127.0.0.1:31009"

	maxBody  = 32 << 20
	pageSize = 100

	// bodyCollection is the editor collection that holds a page's text.
	bodyCollection = "editor_blocks"
)

// Errors a caller can act on.
var (
	ErrNotRunning  = errors.New("any: the Any app is not running")
	ErrNotPaired   = errors.New("any: Glossa is not paired with Any")
	ErrRefused     = errors.New("any: Any refused Glossa")
	ErrDifferent   = errors.New("any: this is a different Any account")
	ErrNotFound    = errors.New("any: no such page or space")
	ErrConflict    = errors.New("any: the page changed in Any since Glossa read it")
	ErrRateLimited = errors.New("any: Any asked to slow down")
	ErrNotLocal    = errors.New("any: the address is not on this computer")
)

// APIError is any other failure Any reported.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("any: the app said %d %s", e.Status, strings.TrimSpace(e.Message))
}

// Client talks to the Any app on this computer.
type Client struct {
	Root string
	HTTP *http.Client

	mu    sync.Mutex
	types map[string]string // space id -> id of its page type, for this session
}

// NewClient makes a client. root must be an address on this computer: Glossa never sends anything
// to Any anywhere else.
func NewClient(root string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(root)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("any: %q is not a web address", root)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, ErrNotLocal
	}
	return &Client{Root: strings.TrimRight(root, "/"), HTTP: hc}, nil
}

// Space is one of the person's spaces.
type Space struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Page is an object in a space, with its text when it was fetched whole.
type Page struct {
	ID       string `json:"id"`
	SpaceID  string `json:"space_id"`
	Name     string `json:"name"`
	Snippet  string `json:"snippet,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	Type     string `json:"type,omitempty"`
	Modified string `json:"modified,omitempty"` // the last-change time as Any gave it
	Archived bool   `json:"archived,omitempty"`
	Version  string `json:"-"` // the text's version, for the next write
}

// record helpers ----------------------------------------------------------------------------------

// A record is an object's row. The built-in values sit under the owner "any" (any.name, any.type,
// any.collections). Both the nested form and the dotted key are read.
type record map[string]any

func (r record) get(owner, name string) any {
	if m, ok := r[owner].(map[string]any); ok {
		if v, ok := m[name]; ok {
			return v
		}
	}
	return r[owner+"."+name]
}

func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	}
	return ""
}

func (r record) name() string { return strings.TrimSpace(text(r.get("any", "name"))) }
func (r record) typ() string  { return text(r.get("any", "type")) }

// modified is the last change: the first of the usual time fields the row has.
func (r record) modified() string {
	for _, k := range []string{"modifiedAt", "updatedAt"} {
		if s := text(r[k]); s != "" {
			return s
		}
		if s := text(r.get("any", k)); s != "" {
			return s
		}
	}
	return ""
}

// gone is true for a page in the bin or one marked deleted: Glossa treats both as not there.
func (r record) gone() bool {
	if _, ok := r["_deletedAt"]; ok {
		return true
	}
	cols, _ := r.get("any", "collections").([]any)
	for _, c := range cols {
		if c == "bin" {
			return true
		}
	}
	return false
}

func (r record) page(space, id string) Page {
	return Page{ID: id, SpaceID: space, Name: r.name(), Type: r.typ(), Modified: r.modified(), Archived: r.gone()}
}

// request -----------------------------------------------------------------------------------------

func (c *Client) do(ctx context.Context, method, path, key string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Root+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if notRunning(err) {
			return ErrNotRunning
		}
		return fmt.Errorf("any: %w", scrub(err, key))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return statusError(resp.StatusCode, raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("any: the app's answer was not understood: %w", err)
	}
	return nil
}

// scrub keeps a key out of an error text, in case a transport error ever quotes a header.
func scrub(err error, key string) error {
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "[key]"))
}

func notRunning(err error) bool {
	var op *net.OpError
	return errors.Is(err, syscall.ECONNREFUSED) || (errors.As(err, &op) && op.Op == "dial")
}

// statusError reads Any's error envelope, {"error": {"code", "message"}}.
func statusError(status int, raw []byte) error {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	json.Unmarshal(raw, &e)
	msg := e.Error.Message
	if msg == "" {
		msg = e.Message
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrRefused
	case http.StatusNotFound, http.StatusGone:
		return ErrNotFound
	case http.StatusTooManyRequests:
		return ErrRateLimited
	case http.StatusConflict:
		if strings.HasPrefix(e.Error.Code, "markdown.conflict") {
			return ErrConflict
		}
	}
	return &APIError{Status: status, Code: e.Error.Code, Message: msg}
}

// Probe asks Any a cheap question with no side effects and no key, and returns the status it
// answered with, or ErrNotRunning when nothing there answers like Any: 200 with a list of spaces, or
// 401 or 403 when Any refuses Glossa. It is how Glossa finds out where Any is.
func (c *Client) Probe(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.Root+"/v1/spaces", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, ErrNotRunning
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK:
		var r struct {
			Spaces *[]json.RawMessage `json:"spaces"`
		}
		if json.Unmarshal(raw, &r) != nil || r.Spaces == nil {
			return 0, ErrNotRunning // some other program answered
		}
		return resp.StatusCode, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return resp.StatusCode, nil
	}
	return 0, ErrNotRunning
}

// health ------------------------------------------------------------------------------------------

// Health returns the id of the account the Any app is signed in to (GET /v1/health, which has no side
// effects). Glossa remembers it only to notice that a different account has been opened since pairing;
// it reads nothing else from the answer. An answer with no account gives "".
func (c *Client) Health(ctx context.Context, key string) (string, error) {
	var r struct {
		Account string `json:"account"`
	}
	err := c.do(ctx, "GET", "/v1/health", key, nil, &r)
	return r.Account, err
}

// reading -----------------------------------------------------------------------------------------

// Spaces lists the spaces in use (Any leaves out deleted ones unless asked).
func (c *Client) Spaces(ctx context.Context, key string) ([]Space, error) {
	var r struct {
		Spaces []Space `json:"spaces"`
	}
	if err := c.do(ctx, "GET", "/v1/spaces", key, nil, &r); err != nil {
		return nil, err
	}
	if r.Spaces == nil {
		r.Spaces = []Space{}
	}
	return r.Spaces, nil
}

func spacePath(space string) string { return "/v1/spaces/" + url.PathEscape(space) }

// pageType finds the id of the page type in a space: the type whose xKey is "page", else the one
// named Page. It is remembered per space for this session. Any calls a plain document "page", so that
// is the answer when the list has neither.
func (c *Client) pageType(ctx context.Context, key, space string) (string, error) {
	c.mu.Lock()
	id, ok := c.types[space]
	c.mu.Unlock()
	if ok {
		return id, nil
	}
	var r struct {
		Types []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			XKey string `json:"xKey"`
		} `json:"types"`
	}
	if err := c.do(ctx, "GET", spacePath(space)+"/types", key, nil, &r); err != nil {
		return "", err
	}
	id = "page"
	byName, found := "", false
	for _, t := range r.Types {
		if t.XKey == "page" {
			id, found = t.ID, true
			break
		}
		if byName == "" && strings.EqualFold(t.Name, "page") {
			byName = t.ID
		}
	}
	if !found && byName != "" {
		id = byName
	}
	c.mu.Lock()
	if c.types == nil {
		c.types = map[string]string{}
	}
	c.types[space] = id
	c.mu.Unlock()
	return id, nil
}

// PagesResult is one page of a listing.
type PagesResult struct {
	Pages      []Page
	Total      int
	NextOffset int // 0 on the last page
}

// Pages lists or searches the pages of one space. A listing is newest change first. A search gives the
// best matches Any returns, up to 100, in its own order.
func (c *Client) Pages(ctx context.Context, key, space, query string, offset, limit int) (PagesResult, error) {
	if limit <= 0 || limit > pageSize {
		limit = pageSize
	}
	typ, err := c.pageType(ctx, key, space)
	if err != nil {
		return PagesResult{}, err
	}
	if strings.TrimSpace(query) != "" {
		return c.searchPages(ctx, key, space, typ, query, offset, limit)
	}
	body := map[string]any{"filter": map[string]any{"any.type": typ}, "sort": []string{"-modifiedAt"},
		"limit": limit, "offset": offset, "includeTotal": true}
	var r struct {
		Records []record `json:"records"`
		Total   int      `json:"total"`
		HasNext bool     `json:"hasNext"`
	}
	path := spacePath(space) + "/objects/query"
	err = c.do(ctx, "POST", path, key, body, &r)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusBadRequest {
		// The app did not take the sort: ask again without it and sort here.
		delete(body, "sort")
		err = c.do(ctx, "POST", path, key, body, &r)
	}
	if err != nil {
		return PagesResult{}, err
	}
	res := PagesResult{Total: r.Total}
	for _, rec := range r.Records {
		id := text(rec["id"])
		if id == "" || rec.gone() {
			continue
		}
		res.Pages = append(res.Pages, rec.page(space, id))
	}
	sort.SliceStable(res.Pages, func(i, j int) bool { return res.Pages[i].Modified > res.Pages[j].Modified })
	if r.HasNext && len(r.Records) > 0 {
		res.NextOffset = offset + len(r.Records)
	}
	return res, nil
}

func (c *Client) searchPages(ctx context.Context, key, space, typ, query string, offset, limit int) (PagesResult, error) {
	body := map[string]any{"query": query, "limit": min(offset+limit, pageSize), "filter": map[string]any{"any.type": typ}}
	var r struct {
		Hits []struct {
			ObjectID string `json:"objectId"`
			Data     string `json:"data"`
		} `json:"hits"`
	}
	if err := c.do(ctx, "POST", spacePath(space)+"/search", key, body, &r); err != nil {
		return PagesResult{}, err
	}
	var pages []Page
	seen := map[string]bool{}
	for _, h := range r.Hits {
		if h.ObjectID == "" || seen[h.ObjectID] {
			continue
		}
		seen[h.ObjectID] = true
		rec, err := c.record(ctx, key, space, h.ObjectID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return PagesResult{}, err
		}
		if rec.gone() {
			continue
		}
		p := rec.page(space, h.ObjectID)
		p.Snippet = strings.TrimSpace(h.Data)
		pages = append(pages, p)
	}
	res := PagesResult{Total: len(pages)}
	if offset >= len(pages) {
		return res, nil
	}
	pages = pages[offset:]
	if len(pages) > limit {
		pages, res.NextOffset = pages[:limit], offset+limit
	}
	res.Pages = pages
	return res, nil
}

func objectPath(space, id string) string { return spacePath(space) + "/objects/" + url.PathEscape(id) }

func (c *Client) record(ctx context.Context, key, space, id string) (record, error) {
	var r struct {
		Record record `json:"record"`
	}
	if err := c.do(ctx, "GET", objectPath(space, id), key, nil, &r); err != nil {
		return nil, err
	}
	if r.Record == nil {
		r.Record = record{}
	}
	return r.Record, nil
}

func (c *Client) markdown(ctx context.Context, key, space, id string) (content, version string, err error) {
	var r struct {
		Content string `json:"content"`
		Version string `json:"version"`
	}
	err = c.do(ctx, "GET", objectPath(space, id)+"/editor/"+bodyCollection+"/markdown", key, nil, &r)
	if errors.Is(err, ErrNotFound) {
		return "", "", nil // an object that has no text yet
	}
	return r.Content, r.Version, err
}

// Get fetches one page with its text as Markdown.
func (c *Client) Get(ctx context.Context, key, space, id string) (Page, error) {
	rec, err := c.record(ctx, key, space, id)
	if err != nil {
		return Page{}, err
	}
	p := rec.page(space, id)
	if p.Archived {
		return Page{}, ErrNotFound
	}
	p.Markdown, p.Version, err = c.markdown(ctx, key, space, id)
	return p, err
}

// writing -----------------------------------------------------------------------------------------

func (c *Client) putMarkdown(ctx context.Context, key, space, id, markdown, version string) error {
	body := map[string]any{"content": markdown}
	if version != "" {
		body["ifVersion"] = version
	}
	return c.do(ctx, "PUT", objectPath(space, id)+"/editor/"+bodyCollection+"/markdown", key, body, nil)
}

// Create makes a new page with a name and a Markdown body and returns it.
func (c *Client) Create(ctx context.Context, key, space, name, markdown string) (Page, error) {
	typ, err := c.pageType(ctx, key, space)
	if err != nil {
		return Page{}, err
	}
	body := map[string]any{"type": typ, "initialProperties": map[string]any{"any": map[string]any{"name": name}}}
	var r struct {
		ObjectID string `json:"objectId"`
	}
	if err := c.do(ctx, "POST", spacePath(space)+"/objects", key, body, &r); err != nil {
		return Page{}, err
	}
	if r.ObjectID == "" {
		return Page{}, errors.New("any: the app did not say which page it made")
	}
	if strings.TrimSpace(markdown) != "" {
		if err := c.putMarkdown(ctx, key, space, r.ObjectID, markdown, ""); err != nil {
			return Page{}, err
		}
	}
	return Page{ID: r.ObjectID, SpaceID: space, Name: name, Markdown: markdown, Type: typ}, nil
}

// Update replaces a page's text. It reads the text's version first and sends it back, so a change
// made in Any in between is not overwritten: that is ErrConflict.
//
// The description gives no address that renames a page, so the name is set when the page is made and
// not changed here.
func (c *Client) Update(ctx context.Context, key, space, id, markdown string) error {
	_, version, err := c.markdown(ctx, key, space, id)
	if err != nil {
		return err
	}
	return c.putMarkdown(ctx, key, space, id, markdown, version)
}
