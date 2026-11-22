// Package anyapp connects Glossa to Any (the Any app, formerly Anytype) through the app's local API
// on this computer: send a note or sermon to a space as a page, and bring a page in as a note. It
// never talks to anything but the Any app on this computer, and only after the person turns it on.
//
// Written from Any's public developer documentation and its OpenAPI description
// (https://developers.anytype.io, https://github.com/anyproto/anytype-api, API version 2025-11-08).
// No code from the Any apps themselves was read or used.
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
	"strconv"
	"strings"
	"syscall"
)

const (
	// DefaultURL is where the Any app listens for other programs on this computer.
	DefaultURL = "http://127.0.0.1:31009"
	// APIVersion is the dated version of Any's API this code was written against. Any wants it in
	// the Anytype-Version header of every request.
	APIVersion = "2025-11-08"

	appName  = "Glossa"
	maxBody  = 32 << 20
	pageSize = 100
	maxPages = 50 // a guard: 5,000 spaces or pages in one listing is far more than anyone has
)

// Errors a caller can act on.
var (
	ErrNotRunning  = errors.New("any: the Any app is not running")
	ErrNotPaired   = errors.New("any: Glossa is not signed in to Any")
	ErrBadCode     = errors.New("any: Any did not accept that code")
	ErrNotFound    = errors.New("any: no such page or space")
	ErrRateLimited = errors.New("any: Any asked to slow down")
	ErrNotLocal    = errors.New("any: the address is not on this computer")
)

// APIError is any other failure Any reported.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("any: the app said %d %s", e.Status, strings.TrimSpace(e.Message))
}

// Client talks to the Any app on this computer.
type Client struct {
	Root string
	HTTP *http.Client
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
	Modified string `json:"modified,omitempty"` // the last-modified time as Any gave it
	Archived bool   `json:"archived,omitempty"`
}

// wire shapes -------------------------------------------------------------------------------------

type wireProp struct {
	Key  string `json:"key"`
	Date string `json:"date,omitempty"`
	Text string `json:"text,omitempty"`
}

type wireObject struct {
	ID         string     `json:"id"`
	SpaceID    string     `json:"space_id"`
	Name       string     `json:"name"`
	Snippet    string     `json:"snippet"`
	Markdown   string     `json:"markdown"`
	Archived   bool       `json:"archived"`
	Properties []wireProp `json:"properties"`
	Type       struct {
		Key string `json:"key"`
	} `json:"type"`
}

func (w wireObject) page() Page {
	p := Page{ID: w.ID, SpaceID: w.SpaceID, Name: w.Name, Snippet: w.Snippet, Markdown: w.Markdown, Type: w.Type.Key, Archived: w.Archived}
	for _, pr := range w.Properties {
		if pr.Key == "last_modified_date" {
			p.Modified = pr.Date
		}
	}
	return p
}

type pageMeta struct {
	Total   int  `json:"total"`
	HasMore bool `json:"has_more"`
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
	req.Header.Set("Anytype-Version", APIVersion)
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

func statusError(status int, raw []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	json.Unmarshal(raw, &e)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrNotPaired
	case http.StatusNotFound, http.StatusGone:
		return ErrNotFound
	case http.StatusTooManyRequests:
		return ErrRateLimited
	}
	return &APIError{Status: status, Message: e.Message}
}

// pairing -----------------------------------------------------------------------------------------

// StartPairing asks Any to show a 4-digit code and returns the challenge to finish with.
func (c *Client) StartPairing(ctx context.Context) (string, error) {
	var r struct {
		ChallengeID string `json:"challenge_id"`
	}
	if err := c.do(ctx, "POST", "/v1/auth/challenges", "", map[string]string{"app_name": appName}, &r); err != nil {
		return "", err
	}
	if r.ChallengeID == "" {
		return "", errors.New("any: the app did not start a sign-in")
	}
	return r.ChallengeID, nil
}

// FinishPairing trades the code the person typed for a key.
func (c *Client) FinishPairing(ctx context.Context, challenge, code string) (string, error) {
	var r struct {
		APIKey string `json:"api_key"`
	}
	err := c.do(ctx, "POST", "/v1/auth/api_keys", "", map[string]string{"challenge_id": challenge, "code": code}, &r)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 || errors.Is(err, ErrNotFound) {
		return "", ErrBadCode
	}
	if err != nil {
		return "", err
	}
	if r.APIKey == "" {
		return "", ErrBadCode
	}
	return r.APIKey, nil
}

// reading -----------------------------------------------------------------------------------------

func paged(offset int) string {
	return "offset=" + strconv.Itoa(offset) + "&limit=" + strconv.Itoa(pageSize)
}

// Spaces lists every space, following the paging.
func (c *Client) Spaces(ctx context.Context, key string) ([]Space, error) {
	out := []Space{}
	for i, offset := 0, 0; i < maxPages; i++ {
		var r struct {
			Data       []Space  `json:"data"`
			Pagination pageMeta `json:"pagination"`
		}
		if err := c.do(ctx, "GET", "/v1/spaces?"+paged(offset), key, nil, &r); err != nil {
			return nil, err
		}
		out = append(out, r.Data...)
		if !r.Pagination.HasMore || len(r.Data) == 0 {
			break
		}
		offset += len(r.Data)
	}
	return out, nil
}

// PagesResult is one page of a listing.
type PagesResult struct {
	Pages      []Page
	Total      int
	NextOffset int // 0 on the last page
}

// Pages lists or searches the pages of one space, newest change first.
func (c *Client) Pages(ctx context.Context, key, space, query string, offset, limit int) (PagesResult, error) {
	if limit <= 0 || limit > pageSize {
		limit = pageSize
	}
	body := map[string]any{"query": query, "types": []string{"page"},
		"sort": map[string]string{"property_key": "last_modified_date", "direction": "desc"}}
	var r struct {
		Data       []wireObject `json:"data"`
		Pagination pageMeta     `json:"pagination"`
	}
	path := fmt.Sprintf("/v1/spaces/%s/search?offset=%d&limit=%d", url.PathEscape(space), offset, limit)
	if err := c.do(ctx, "POST", path, key, body, &r); err != nil {
		return PagesResult{}, err
	}
	res := PagesResult{Total: r.Pagination.Total}
	for _, w := range r.Data {
		if !w.Archived {
			res.Pages = append(res.Pages, w.page())
		}
	}
	if r.Pagination.HasMore && len(r.Data) > 0 {
		res.NextOffset = offset + len(r.Data)
	}
	return res, nil
}

func objectPath(space, id string) string {
	return "/v1/spaces/" + url.PathEscape(space) + "/objects/" + url.PathEscape(id)
}

// Get fetches one page with its text as Markdown.
func (c *Client) Get(ctx context.Context, key, space, id string) (Page, error) {
	var r struct {
		Object wireObject `json:"object"`
	}
	if err := c.do(ctx, "GET", objectPath(space, id)+"?format=md", key, nil, &r); err != nil {
		return Page{}, err
	}
	p := r.Object.page()
	if p.Archived {
		return Page{}, ErrNotFound
	}
	if p.SpaceID == "" {
		p.SpaceID = space
	}
	return p, nil
}

// writing -----------------------------------------------------------------------------------------

// Prop is a text property to set on a page.
type Prop struct{ Key, Text string }

func wireProps(ps []Prop) []map[string]string {
	out := make([]map[string]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, map[string]string{"key": p.Key, "text": p.Text})
	}
	return out
}

// Create makes a new page and returns it.
func (c *Client) Create(ctx context.Context, key, space, name, markdown string, props []Prop) (Page, error) {
	body := map[string]any{"type_key": "page", "name": name, "body": markdown}
	if len(props) > 0 {
		body["properties"] = wireProps(props)
	}
	var r struct {
		Object wireObject `json:"object"`
	}
	if err := c.do(ctx, "POST", "/v1/spaces/"+url.PathEscape(space)+"/objects", key, body, &r); err != nil {
		return Page{}, err
	}
	return r.Object.page(), nil
}

// Update changes a page's name, text and properties.
func (c *Client) Update(ctx context.Context, key, space, id, name, markdown string, props []Prop) error {
	body := map[string]any{"name": name, "markdown": markdown}
	if len(props) > 0 {
		body["properties"] = wireProps(props)
	}
	return c.do(ctx, "PATCH", objectPath(space, id), key, body, nil)
}

// EnsureProperty finds a text property by key in a space, making it when it is not there, and
// returns whether it is usable.
func (c *Client) EnsureProperty(ctx context.Context, key, space, propKey, name string) (bool, error) {
	base := "/v1/spaces/" + url.PathEscape(space) + "/properties"
	for i, offset := 0, 0; i < maxPages; i++ {
		var r struct {
			Data []struct {
				Key string `json:"key"`
			} `json:"data"`
			Pagination pageMeta `json:"pagination"`
		}
		if err := c.do(ctx, "GET", base+"?"+paged(offset), key, nil, &r); err != nil {
			return false, err
		}
		for _, p := range r.Data {
			if p.Key == propKey {
				return true, nil
			}
		}
		if !r.Pagination.HasMore || len(r.Data) == 0 {
			break
		}
		offset += len(r.Data)
	}
	err := c.do(ctx, "POST", base, key, map[string]string{"key": propKey, "name": name, "format": "text"}, nil)
	return err == nil, err
}
