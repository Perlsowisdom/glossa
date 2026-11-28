package anyapp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const pageTypeID = "bafy-page-type"

type obj struct {
	ID, Space, Name, Markdown string
	Rev, Version              int
	Archived                  bool // in the bin
	Type                      string
}

// fakeAny serves the part of the Any app's own API (Any API 1.0) that Glossa uses, shaped from the
// description of it, and records every request.
type fakeAny struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []string
	hdrs []http.Header

	objs   map[string]*obj
	nextID int
	rev    int

	pageCap    int  // the most records a query returns, whatever limit is asked
	badKey     bool // refuses whatever key is sent (the pairing flow, kept for now)
	noKey      bool // Any asks for no key at all
	rejectSort bool // answers 400 when a query has a sort
	noPageType bool // the types list has no page type, only others
	flatRecord bool // rows come with dotted keys instead of nested ones
	// the heading Any might put at the top of the text
	titleHeading bool
	// the next markdown write is answered 409, as if the page changed in between
	conflictNext bool
	// the answer to the spaces list, when not the usual two
	spacesBody any
	typeCalls  int
}

func newFake(t *testing.T) *fakeAny {
	f := &fakeAny{t: t, objs: map[string]*obj{}, pageCap: 100}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAny) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func (f *fakeAny) tick(o *obj) {
	f.rev++
	o.Rev = f.rev
	o.Version++
}

func (f *fakeAny) record(o *obj) map[string]any {
	cols := []any{}
	if o.Archived {
		cols = append(cols, "bin")
	}
	typ := o.Type
	if typ == "" {
		typ = pageTypeID
	}
	m := map[string]any{"id": o.ID, "modifiedAt": 1700000000000 + o.Rev}
	if f.flatRecord {
		m["any.name"], m["any.type"], m["any.collections"] = o.Name, typ, cols
		return m
	}
	m["any"] = map[string]any{"name": o.Name, "type": typ, "collections": cols}
	return m
}

func (f *fakeAny) errorOut(w http.ResponseWriter, code int, ecode, msg string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": ecode, "message": msg}})
}

func (f *fakeAny) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.Path)
	f.hdrs = append(f.hdrs, r.Header.Clone())
	w.Header().Set("Content-Type", "application/json")
	raw, _ := io.ReadAll(r.Body)
	var in map[string]any
	json.Unmarshal(raw, &in)
	p := r.URL.Path

	// The old sign-in flow, kept while the decision on pairing is open.
	switch {
	case p == "/v1/auth/challenges" && r.Method == "POST":
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"challenge_id": "chal-1"})
		return
	case p == "/v1/auth/api_keys" && r.Method == "POST":
		if in["challenge_id"] != "chal-1" || in["code"] != goodCode {
			f.errorOut(w, 400, "auth.bad_code", "wrong code")
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"api_key": secretKey})
		return
	}
	if r.Method == "DELETE" || strings.HasPrefix(p, "/v1/shutdown") || p == "/v1/auth" {
		f.t.Errorf("forbidden request: %s %s", r.Method, p)
		f.errorOut(w, 405, "method.not_allowed", "no")
		return
	}
	if !f.noKey && (f.badKey || r.Header.Get("Authorization") != "Bearer "+secretKey) {
		f.errorOut(w, 401, "auth.required", "unauthorized")
		return
	}
	parts := strings.Split(strings.Trim(p, "/"), "/") // v1 spaces {s} ...
	switch {
	case p == "/v1/spaces" && r.Method == "GET":
		if f.spacesBody != nil {
			json.NewEncoder(w).Encode(f.spacesBody)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"spaces": []any{
			map[string]any{"id": spaceA, "name": "Study", "type": "space", "spaceType": "personal", "status": "active"},
			map[string]any{"id": spaceB, "name": "Church", "type": "space", "spaceType": "shared", "status": "active"}}})
	case len(parts) == 4 && parts[3] == "types" && r.Method == "GET":
		f.typeCalls++
		types := []any{map[string]any{"id": "bafy-note-type", "name": "Note", "xKey": "note", "builtIn": true}}
		if f.noPageType {
			types = append(types, map[string]any{"id": "bafy-by-name", "name": "page", "xKey": "other"})
		} else {
			types = append(types, map[string]any{"id": pageTypeID, "name": "Page", "xKey": "page", "builtIn": true})
		}
		json.NewEncoder(w).Encode(map[string]any{"types": types})
	case len(parts) == 5 && parts[3] == "objects" && parts[4] == "query" && r.Method == "POST":
		f.query(w, parts[2], in)
	case len(parts) == 4 && parts[3] == "search" && r.Method == "POST":
		f.search(w, parts[2], in)
	case len(parts) == 4 && parts[3] == "objects" && r.Method == "POST":
		f.create(w, parts[2], in)
	case len(parts) == 5 && parts[3] == "objects" && r.Method == "GET":
		o := f.find(parts[2], parts[4])
		if o == nil {
			f.errorOut(w, 404, "object.not_found", "no such object")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"objectId": o.ID, "record": f.record(o)})
	case len(parts) == 8 && parts[3] == "objects" && parts[5] == "editor" && parts[6] == "editor_blocks" && parts[7] == "markdown":
		f.markdown(w, r, parts[2], parts[4], in)
	default:
		f.errorOut(w, 404, "route.not_found", "no route "+p)
	}
}

func (f *fakeAny) find(space, id string) *obj {
	if o := f.objs[id]; o != nil && o.Space == space {
		return o
	}
	return nil
}

func (f *fakeAny) wantType(w http.ResponseWriter, filter any) bool {
	m, _ := filter.(map[string]any)
	if m["any.type"] != pageTypeID {
		f.errorOut(w, 400, "filter.bad", "expected the page type id in the filter")
		return false
	}
	return true
}

func (f *fakeAny) query(w http.ResponseWriter, space string, in map[string]any) {
	if !f.wantType(w, in["filter"]) {
		return
	}
	if s, _ := in["sort"].([]any); len(s) > 0 && s[0] != "-modifiedAt" {
		f.errorOut(w, 400, "sort.bad", "unknown sort")
		return
	}
	if _, has := in["sort"]; has && f.rejectSort {
		f.errorOut(w, 400, "sort.bad", "no sorting here")
		return
	}
	var list []*obj
	for _, o := range f.objs {
		if o.Space == space {
			list = append(list, o)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Rev > list[j].Rev })
	if _, has := in["sort"]; !has {
		sort.Slice(list, func(i, j int) bool { return list[i].Rev < list[j].Rev }) // an arbitrary order
	}
	off, _ := in["offset"].(float64)
	lim, _ := in["limit"].(float64)
	if lim <= 0 || int(lim) > f.pageCap {
		lim = float64(f.pageCap)
	}
	end := min(int(off)+int(lim), len(list))
	start := min(int(off), end)
	recs := []any{}
	for _, o := range list[start:end] {
		recs = append(recs, f.record(o))
	}
	json.NewEncoder(w).Encode(map[string]any{"records": recs, "total": len(list), "hasNext": end < len(list)})
}

func (f *fakeAny) search(w http.ResponseWriter, space string, in map[string]any) {
	q, _ := in["query"].(string)
	if q == "" {
		f.errorOut(w, 400, "request.missing_field", "query is required")
		return
	}
	if !f.wantType(w, in["filter"]) {
		return
	}
	var list []*obj
	for _, o := range f.objs {
		if o.Space == space && strings.Contains(strings.ToLower(o.Name+" "+o.Markdown), strings.ToLower(q)) {
			list = append(list, o)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	lim, _ := in["limit"].(float64)
	hits := []any{}
	for _, o := range list {
		h := map[string]any{"objectId": o.ID, "dataset": "editor_blocks", "data": o.Markdown, "score": 1.0}
		hits = append(hits, h, h) // two chunks of one page: Glossa lists it once
	}
	if lim > 0 && int(lim) < len(hits) {
		hits = hits[:int(lim)]
	}
	json.NewEncoder(w).Encode(map[string]any{"hits": hits, "mode": "hybrid"})
}

func (f *fakeAny) create(w http.ResponseWriter, space string, in map[string]any) {
	if in["type"] != pageTypeID {
		f.errorOut(w, 400, "request.invalid_field", "type must be the page type id")
		return
	}
	props, _ := in["initialProperties"].(map[string]any)
	owner, _ := props["any"].(map[string]any)
	name, _ := owner["name"].(string)
	if _, top := in["name"]; top || name == "" {
		f.errorOut(w, 400, "request.invalid_field", "the name goes in initialProperties.any.name")
		return
	}
	f.nextID++
	o := &obj{ID: "obj-" + strconv.Itoa(f.nextID), Space: space, Name: name}
	f.tick(o)
	f.objs[o.ID] = o
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]any{"objectId": o.ID})
}

func (f *fakeAny) markdown(w http.ResponseWriter, r *http.Request, space, id string, in map[string]any) {
	o := f.find(space, id)
	if o == nil {
		f.errorOut(w, 404, "object.not_found", "no such object")
		return
	}
	version := func() string { return fmt.Sprintf("v%d", o.Version) }
	switch r.Method {
	case "GET":
		md := o.Markdown
		if f.titleHeading {
			md = "# " + o.Name + "\n\n" + md
		}
		json.NewEncoder(w).Encode(map[string]any{"content": md, "version": version()})
	case "PUT":
		if v, ok := in["ifVersion"].(string); f.conflictNext || (ok && v != version()) {
			f.conflictNext = false
			w.WriteHeader(409)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "markdown.conflict", "message": "changed",
				"details": map[string]any{"content": o.Markdown, "version": version()}}})
			return
		}
		o.Markdown, _ = in["content"].(string)
		f.tick(o)
		json.NewEncoder(w).Encode(map[string]any{"version": version(), "unchanged": 0})
	default:
		f.errorOut(w, 405, "method.not_allowed", "no")
	}
}

// edit changes a page the way the person would in Any.
func (f *fakeAny) edit(id, name, markdown string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.objs[id]
	o.Name, o.Markdown = name, markdown
	f.tick(o)
}

func (f *fakeAny) add(space, name, markdown string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	o := &obj{ID: "obj-" + strconv.Itoa(f.nextID), Space: space, Name: name, Markdown: markdown}
	f.tick(o)
	f.objs[o.ID] = o
	return o.ID
}
