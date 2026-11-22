package zotero

import (
	"context"
	"errors"
	"regexp"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect"
	"github.com/Perlsowisdom/glossa/internal/connect/csljson"
)

var keyRE = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)

// Register adds the zotero.* commands. svc is the framework service that Register in package
// connect returned; it holds the switch and the key.
func (c *Conn) Register(r *command.Registry, svc *connect.Service) {
	s := &commands{c: c, svc: svc}
	r.OnClose(c.Close)
	r.Add(
		command.Define("zotero.items", "List items (books, articles, chapters...) in your Zotero library, a page at a time, as short entries. Optionally only one collection. Needs Zotero turned on in Settings → Connections.", false, s.items),
		command.Define("zotero.search", "Search your Zotero library by title, author or year. Needs Zotero turned on.", false, s.search),
		command.Define("zotero.collections", "List your Zotero collections. Needs Zotero turned on.", false, s.collections),
		command.Define("zotero.item", "Get one Zotero item as CSL-JSON. When Zotero cannot be reached, a copy kept from an earlier citation is returned if there is one.", false, s.item),
		command.Define("zotero.cite", "Keep a copy of a Zotero item in your library so a document that cites it still shows its citation when Zotero is not available. Returns the item as CSL-JSON.", true, s.cite),
		command.Define("zotero.cached", "List the Zotero items kept for citing, as CSL-JSON. Never uses the network, and works with Zotero turned off.", false, s.cached),
		command.Define("zotero.refresh", "Update the kept copies from Zotero, asking only whether the library changed. Needs Zotero turned on.", true, s.refresh),
	)
}

type commands struct {
	c   *Conn
	svc *connect.Service
}

func fail(err error) error {
	var ce *command.Error
	switch {
	case errors.As(err, &ce):
		return err
	case errors.Is(err, ErrNotFound):
		return command.Errorf("not_found", "%s", Explain(err))
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return command.Errorf("unavailable", "%s", Explain(err))
}

// ItemsParams chooses what to list.
type ItemsParams struct {
	Collection string `json:"collection,omitempty" desc:"A collection key, from zotero.collections; leave out for the whole library"`
	Query      string `json:"query,omitempty" desc:"Words to find in titles, authors and years"`
	Start      int    `json:"start,omitempty" desc:"Where to start; use next_start from the last page"`
	Limit      int    `json:"limit,omitempty" desc:"Most items to return, up to 100 (default 50)"`
	Sort       string `json:"sort,omitempty" enum:"title,creator,date,dateAdded,dateModified"`
	Descending bool   `json:"descending,omitempty"`
}

// ItemsResult is one page.
type ItemsResult struct {
	Items     []Summary `json:"items"`
	Total     int       `json:"total" desc:"Items in the whole result, or -1 when Zotero did not say"`
	NextStart int       `json:"next_start,omitempty" desc:"Pass as start for the next page; absent on the last page"`
	Via       string    `json:"via" desc:"Where the answer came from"`
}

func (s *commands) items(ctx context.Context, env *command.Env, p ItemsParams) (ItemsResult, error) {
	return s.list(ctx, env, p)
}

// SearchParams is a search.
type SearchParams struct {
	Query string `json:"query" required:"true" desc:"Words to find in titles, authors and years"`
	Start int    `json:"start,omitempty"`
	Limit int    `json:"limit,omitempty" desc:"Most items to return, up to 100 (default 50)"`
}

func (s *commands) search(ctx context.Context, env *command.Env, p SearchParams) (ItemsResult, error) {
	return s.list(ctx, env, ItemsParams{Query: p.Query, Start: p.Start, Limit: p.Limit})
}

func (s *commands) list(ctx context.Context, env *command.Env, p ItemsParams) (ItemsResult, error) {
	cfg, err := s.svc.Require(ctx, env, "zotero")
	if err != nil {
		return ItemsResult{}, err
	}
	if p.Collection != "" && !keyRE.MatchString(p.Collection) {
		return ItemsResult{}, command.Errorf("bad_params", "collection is not a Zotero key")
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	q := ItemsQuery{Collection: p.Collection, Query: p.Query, Start: p.Start, Limit: p.Limit, Sort: p.Sort, Descending: p.Descending}
	page, via, err := use(ctx, s.c, cfg, func(cl *Client) (ItemsPage, error) { return cl.Items(ctx, q) })
	if err != nil {
		return ItemsResult{}, fail(err)
	}
	out := ItemsResult{Items: make([]Summary, 0, len(page.Items)), Total: page.Total, NextStart: page.NextStart, Via: via}
	for _, e := range page.Items {
		out.Items = append(out.Items, Summarize(e))
	}
	return out, nil
}

// CollectionsResult lists collections.
type CollectionsResult struct {
	Collections []Collection `json:"collections"`
	Via         string       `json:"via"`
}

func (s *commands) collections(ctx context.Context, env *command.Env, _ struct{}) (CollectionsResult, error) {
	cfg, err := s.svc.Require(ctx, env, "zotero")
	if err != nil {
		return CollectionsResult{}, err
	}
	cols, via, err := use(ctx, s.c, cfg, func(cl *Client) ([]Collection, error) { return cl.Collections(ctx) })
	if err != nil {
		return CollectionsResult{}, fail(err)
	}
	if cols == nil {
		cols = []Collection{}
	}
	return CollectionsResult{Collections: cols, Via: via}, nil
}

// KeyParams names an item.
type KeyParams struct {
	Key string `json:"key" required:"true" desc:"The item's Zotero key (8 letters and digits), from zotero.items"`
}

// ItemResult is one item.
type ItemResult struct {
	Key       string       `json:"key"`
	CSL       csljson.Item `json:"csl" desc:"The item as CSL-JSON; its id is zotero:<key>"`
	Via       string       `json:"via,omitempty" desc:"Where it came from; empty when it came from the kept copy only"`
	FromCache bool         `json:"from_cache" desc:"True when Zotero was not asked or could not be reached and the kept copy was used"`
}

// fetch gets an item live when Zotero is on, else (or when Zotero cannot be reached) from the kept copy.
func (s *commands) fetch(ctx context.Context, env *command.Env, key string) (ItemResult, int, error) {
	if !keyRE.MatchString(key) {
		return ItemResult{}, 0, command.Errorf("bad_params", "%q is not a Zotero key (8 letters and digits)", key)
	}
	cfg, err := s.svc.Config(ctx, env, "zotero")
	if err != nil {
		return ItemResult{}, 0, err
	}
	var liveErr error
	if cfg.Enabled {
		type got struct {
			e Entry
			v int
		}
		g, via, err := use(ctx, s.c, cfg, func(cl *Client) (got, error) {
			e, v, err := cl.Item(ctx, key)
			return got{e, v}, err
		})
		if err == nil {
			return ItemResult{Key: g.e.Key, CSL: g.e.Item, Via: via}, g.v, nil
		}
		if errors.Is(err, ErrNotFound) || ctx.Err() != nil {
			return ItemResult{}, 0, fail(err)
		}
		liveErr = err
	}
	if c, ok := loadCache(ctx, env, key); ok {
		return ItemResult{Key: key, CSL: c.Item, FromCache: true}, c.Version, nil
	}
	if liveErr != nil {
		return ItemResult{}, 0, fail(liveErr)
	}
	return ItemResult{}, 0, command.Errorf("off", "Zotero is turned off and this item has not been kept. Turn Zotero on in Settings → Connections first.")
}

func (s *commands) item(ctx context.Context, env *command.Env, p KeyParams) (ItemResult, error) {
	r, _, err := s.fetch(ctx, env, p.Key)
	return r, err
}

func (s *commands) cite(ctx context.Context, env *command.Env, p KeyParams) (ItemResult, error) {
	r, version, err := s.fetch(ctx, env, p.Key)
	if err != nil {
		return ItemResult{}, err
	}
	if !r.FromCache {
		if _, err := saveCache(ctx, env, Entry{Key: r.Key, Item: r.CSL}, version); err != nil {
			return ItemResult{}, err
		}
	}
	return r, nil
}

// CachedItem is a kept copy.
type CachedItem struct {
	Summary
	CSL      csljson.Item `json:"csl"`
	CachedAt int64        `json:"cached_at" desc:"Unix milliseconds"`
}

// CachedResult lists kept copies.
type CachedResult struct {
	Items []CachedItem `json:"items"`
}

func (s *commands) cached(ctx context.Context, env *command.Env, _ struct{}) (CachedResult, error) {
	items, err := listCache(ctx, env)
	if err != nil {
		return CachedResult{}, err
	}
	out := CachedResult{Items: make([]CachedItem, 0, len(items))}
	for _, c := range items {
		out.Items = append(out.Items, CachedItem{Summary: Summarize(Entry{Key: c.Key, Item: c.Item}), CSL: c.Item, CachedAt: c.CachedAt})
	}
	return out, nil
}

// RefreshResult says what a refresh did.
type RefreshResult struct {
	Kept      int    `json:"kept" desc:"Copies kept before"`
	Updated   int    `json:"updated" desc:"Copies replaced because the item changed in Zotero"`
	Unchanged int    `json:"unchanged" desc:"Copies Zotero said had not changed"`
	Missing   int    `json:"missing" desc:"Copies whose item Zotero no longer has; they are kept"`
	Via       string `json:"via,omitempty"`
}

const refreshBatch = 50

func (s *commands) refresh(ctx context.Context, env *command.Env, _ struct{}) (RefreshResult, error) {
	cfg, err := s.svc.Require(ctx, env, "zotero")
	if err != nil {
		return RefreshResult{}, err
	}
	items, err := listCache(ctx, env)
	if err != nil {
		return RefreshResult{}, err
	}
	res := RefreshResult{Kept: len(items)}
	for start := 0; start < len(items); start += refreshBatch {
		batch := items[start:min(start+refreshBatch, len(items))]
		keys, since := make([]string, len(batch)), batch[0].Version
		have := map[string]bool{}
		for i, c := range batch {
			keys[i] = c.Key
			since = min(since, c.Version)
		}
		page, via, err := use(ctx, s.c, cfg, func(cl *Client) (ItemsPage, error) {
			return cl.Items(ctx, ItemsQuery{Keys: keys, Limit: refreshBatch, SinceVersion: since})
		})
		if err != nil {
			return res, fail(err)
		}
		res.Via = via
		if page.NotModified {
			res.Unchanged += len(batch)
			continue
		}
		for _, e := range page.Items {
			have[e.Key] = true
			changed, err := saveCache(ctx, env, e, page.Version)
			if err != nil {
				return res, err
			}
			if changed {
				res.Updated++
			} else {
				res.Unchanged++
			}
		}
		for _, c := range batch {
			if !have[c.Key] {
				res.Missing++
			}
		}
	}
	return res, nil
}
