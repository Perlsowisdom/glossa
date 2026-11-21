// Package ccel offers public-domain books from the Christian Classics Ethereal Library (ccel.org)
// as free copies. It uses the network only after the user has turned it on (ADR 0009) and only for
// the requests docs/privacy.md lists: one catalogue page at most once a week, and, for a book the user
// chose, its rights header and its EPUB. CCEL's robots.txt asks for ten seconds between requests; the
// service waits that long. Terms and formats are in docs/sources/ccel.md.
package ccel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
)

const (
	DefaultBase = "https://ccel.org"
	userAgent   = "Glossa"
	// Credit is shown with every book and recorded as its attribution.
	Credit       = "Christian Classics Ethereal Library (ccel.org)"
	refreshEvery = 7 * 24 * time.Hour
	crawlDelay   = 10 * time.Second
	maxIndex     = 16 << 20
	maxBook      = 512 << 20
)

// Work is one book in the catalogue.
type Work struct {
	ID      string `json:"id" desc:"author/work, as in ccel.org/ccel/author/work"`
	Title   string `json:"title"`
	Author  string `json:"author,omitempty"`
	Classic bool   `json:"classic,omitempty" desc:"CCEL marks it a classic"`
}

type state struct {
	Enabled     bool     `json:"enabled"`
	RefreshedAt int64    `json:"refreshed_at,omitempty"`
	Works       []Work   `json:"works,omitempty"`
	Installed   []string `json:"installed,omitempty"`
	// Base and Into are not settings in the interface: they are for tests and for people who run a
	// mirror. Base replaces https://ccel.org; Into is the folder books are put in.
	Base string `json:"base,omitempty"`
	Into string `json:"into,omitempty"`
	// GapMS replaces the ten seconds between requests; only for a mirror of your own.
	GapMS *int `json:"gap_ms,omitempty"`
}

// Service is the CCEL source.
type Service struct {
	dir  string
	base string
	http *http.Client
	now  func() time.Time
	gap  time.Duration
	mu   sync.Mutex // guards the state, and spaces requests
	st   *state
	last time.Time
	r    *command.Registry
}

// Option customises a Service, mainly for tests.
type Option func(*Service)

func WithBase(u string) Option             { return func(s *Service) { s.base = strings.TrimRight(u, "/") } }
func WithHTTPClient(c *http.Client) Option { return func(s *Service) { s.http = c } }
func WithGap(d time.Duration) Option       { return func(s *Service) { s.gap = d } }
func WithClock(f func() time.Time) Option  { return func(s *Service) { s.now = f } }

// Register adds the CCEL commands; the cache lives in dir.
func Register(r *command.Registry, dir string, opts ...Option) *Service {
	s := &Service{dir: dir, base: DefaultBase, http: &http.Client{Timeout: 10 * time.Minute}, now: time.Now, gap: crawlDelay, r: r}
	for _, o := range opts {
		o(s)
	}
	r.Add(
		command.Define("ccel.settings", "Show, or turn on or off, the Christian Classics Ethereal Library (ccel.org) as a source of free public-domain books. Off until turned on; nothing is sent to ccel.org while it is off.", true, s.settings),
		command.Define("ccel.refresh", "Fetch CCEL's list of books (one page, at most once a week). Needs CCEL turned on.", true, s.refresh),
		command.Define("ccel.items", "List CCEL's books from the cache. Never uses the network.", false, s.items),
		command.Define("ccel.get", "Get a public-domain book from CCEL into your library as an EPUB. With preview, shows the size and licence without downloading the book.", true, s.get),
	)
	return s
}

func (s *Service) path() string { return filepath.Join(s.dir, "ccel.json") }

// load returns the state; the caller holds s.mu.
func (s *Service) load() (*state, error) {
	if s.st != nil {
		return s.st, nil
	}
	st := &state{}
	data, err := os.ReadFile(s.path())
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, st); err != nil {
			return nil, fmt.Errorf("ccel: %s: %w", s.path(), err)
		}
	}
	if st.Base != "" && s.base == DefaultBase {
		s.base = strings.TrimRight(st.Base, "/")
	}
	if st.GapMS != nil && s.base != DefaultBase {
		s.gap = time.Duration(*st.GapMS) * time.Millisecond
	}
	s.st = st
	return st, nil
}

func (s *Service) save(st *state) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	s.st = st
	return os.Rename(tmp, s.path())
}

func (s *Service) isEnabled() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return false, err
	}
	return st.Enabled, nil
}

var errOff = command.Errorf("off", "CCEL is turned off. Turn it on in Settings → Privacy first.")

// fetch makes one request to ccel.org, after checking the switch and waiting out the crawl delay.
func (s *Service) fetch(ctx context.Context, method, url, rng string) (*http.Response, error) {
	on, err := s.isEnabled()
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, errOff
	}
	s.mu.Lock()
	wait := s.gap - s.now().Sub(s.last)
	s.last = s.now().Add(max(wait, 0))
	s.mu.Unlock()
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("ccel.org answered %s for %s", resp.Status, url)
	}
	return resp, nil
}

// SettingsParams turns CCEL on or off.
type SettingsParams struct {
	Enable *bool `json:"enable,omitempty" desc:"true turns CCEL on, false turns it off (the cached list stays); leave out to only look"`
}

// SettingsResult is the state of the source.
type SettingsResult struct {
	Enabled     bool   `json:"enabled"`
	Works       int    `json:"works" desc:"Books in the cached list"`
	RefreshedAt int64  `json:"refreshed_at,omitempty" desc:"When the list was fetched, Unix milliseconds"`
	Credit      string `json:"credit"`
}

func (s *Service) settings(_ context.Context, _ *command.Env, p SettingsParams) (SettingsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return SettingsResult{}, err
	}
	if p.Enable != nil && st.Enabled != *p.Enable {
		n := *st
		n.Enabled = *p.Enable
		if err := s.save(&n); err != nil {
			return SettingsResult{}, err
		}
		st = &n
	}
	return SettingsResult{Enabled: st.Enabled, Works: len(st.Works), RefreshedAt: st.RefreshedAt, Credit: Credit}, nil
}

// RefreshParams asks for the list.
type RefreshParams struct{}

// RefreshResult says what the cache holds.
type RefreshResult struct {
	Works       int   `json:"works"`
	RefreshedAt int64 `json:"refreshed_at"`
	Fetched     bool  `json:"fetched" desc:"False when the list fetched less than a week ago was kept"`
}

func (s *Service) refresh(ctx context.Context, _ *command.Env, _ RefreshParams) (RefreshResult, error) {
	on, err := s.isEnabled()
	if err != nil {
		return RefreshResult{}, err
	}
	if !on {
		return RefreshResult{}, errOff
	}
	s.mu.Lock()
	st, err := s.load()
	if err == nil && len(st.Works) > 0 && s.now().Sub(time.UnixMilli(st.RefreshedAt)) < refreshEvery {
		r := RefreshResult{Works: len(st.Works), RefreshedAt: st.RefreshedAt}
		s.mu.Unlock()
		return r, nil
	}
	s.mu.Unlock()
	if err != nil {
		return RefreshResult{}, err
	}
	resp, err := s.fetch(ctx, http.MethodGet, s.base+"/index/title", "")
	if err != nil {
		return RefreshResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIndex))
	if err != nil {
		return RefreshResult{}, err
	}
	works := parseIndex(string(data))
	if len(works) == 0 {
		return RefreshResult{}, errors.New("ccel: the list of books had no books in it; the page may have changed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err = s.load()
	if err != nil {
		return RefreshResult{}, err
	}
	n := *st
	n.Works, n.RefreshedAt = works, s.now().UnixMilli()
	if err := s.save(&n); err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{Works: len(works), RefreshedAt: n.RefreshedAt, Fetched: true}, nil
}

var (
	entryRe  = regexp.MustCompile(`(?s)<p class="List2">\s*<a href="/ccel/([^"/]+)/([^"/]+)\.html">(.*?)</a>(.*?)</p>`)
	authorRe = regexp.MustCompile(`(?s)<span class="authorName">\s*\(<a href="[^"]*">(.*?)</a>`)
	tagRe    = regexp.MustCompile(`<[^>]*>`)
)

func clean(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(s, " "))), " ")
}

// parseIndex reads CCEL's "browse by title" page: one entry per book, the book's page
// /ccel/<author>/<work>.html, its title, the author's name and a mark for classics. Entries that are
// not single books (sections, translations of the site) are left out.
func parseIndex(page string) []Work {
	var out []Work
	seen := map[string]bool{}
	for _, m := range entryRe.FindAllStringSubmatch(page, -1) {
		author, work := m[1], m[2]
		id := author + "/" + work
		title := clean(m[3])
		if seen[id] || title == "" || author == "bible" {
			continue
		}
		seen[id] = true
		w := Work{ID: id, Title: title, Classic: strings.Contains(m[4], "(classic)")}
		if a := authorRe.FindStringSubmatch(m[4]); a != nil {
			w.Author = clean(a[1])
		}
		out = append(out, w)
	}
	return out
}

// ItemsParams filters the cached list.
type ItemsParams struct {
	Text  string `json:"text,omitempty" desc:"Words in the title or author"`
	Limit int    `json:"limit,omitempty" desc:"Most books to return (default 100)"`
}

// Item is a book in the cached list.
type Item struct {
	Work
	Kind      string `json:"kind" desc:"book, commentary or dictionary"`
	Installed bool   `json:"installed" desc:"Already in your library"`
}

// ItemsResult is a page of the cache.
type ItemsResult struct {
	Items   []Item `json:"items"`
	Total   int    `json:"total"`
	Enabled bool   `json:"enabled"`
}

func kindOf(title string) string {
	t := strings.ToLower(title)
	switch {
	case strings.Contains(t, "commentary"):
		return "commentary"
	case strings.Contains(t, "dictionary"), strings.Contains(t, "encyclopedia"), strings.Contains(t, "lexicon"):
		return "dictionary"
	}
	return "book"
}

func (s *Service) items(_ context.Context, _ *command.Env, p ItemsParams) (ItemsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return ItemsResult{}, err
	}
	res := ItemsResult{Items: []Item{}, Enabled: st.Enabled}
	if !st.Enabled {
		return res, nil
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	have := map[string]bool{}
	for _, id := range st.Installed {
		have[id] = true
	}
	words := strings.Fields(strings.ToLower(p.Text))
	for _, w := range st.Works {
		hay := strings.ToLower(w.Title + " " + w.Author)
		ok := true
		for _, x := range words {
			ok = ok && strings.Contains(hay, x)
		}
		if !ok {
			continue
		}
		res.Total++
		if len(res.Items) < p.Limit {
			res.Items = append(res.Items, Item{Work: w, Kind: kindOf(w.Title), Installed: have[w.ID]})
		}
	}
	return res, nil
}

// GetParams gets one book.
type GetParams struct {
	ID      string `json:"id" required:"true" desc:"author/work from ccel.items"`
	Preview bool   `json:"preview,omitempty" desc:"Check the rights and size only; do not download the book"`
	Into    string `json:"into,omitempty" desc:"Folder to put the book in; Documents/Glossa Library when omitted"`
}

// GetResult describes the book.
type GetResult struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author,omitempty"`
	Format  string `json:"format"`
	Size    int64  `json:"size" desc:"Bytes to download; 0 when CCEL does not say"`
	Rights  string `json:"rights" desc:"The rights CCEL states for the book"`
	Licence string `json:"licence" desc:"Licence it gets in your library"`
	Credit  string `json:"credit"`
	Added   bool   `json:"added,omitempty" desc:"The book is now in your library"`
	ItemID  string `json:"item_id,omitempty" desc:"The library item"`
	Summary string `json:"summary,omitempty"`
}

var rightsRe = regexp.MustCompile(`(?s)<DC\.Rights[^>]*>(.*?)</DC\.Rights>`)

func publicDomain(rights string) bool {
	r := strings.ToLower(rights)
	return strings.Contains(r, "public domain") && !strings.Contains(r, "not in the public domain") && !strings.Contains(r, "not public domain")
}

func validID(id string) (author, work string, ok bool) {
	author, work, ok = strings.Cut(id, "/")
	good := func(s string) bool {
		if s == "" || strings.HasPrefix(s, ".") {
			return false
		}
		for _, r := range s {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
				return false
			}
		}
		return true
	}
	return author, work, ok && good(author) && good(work)
}

func (s *Service) find(id string) (Work, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return Work{}, err
	}
	for _, w := range st.Works {
		if w.ID == id {
			return w, nil
		}
	}
	return Work{}, command.Errorf("not_found", "CCEL has no book %q in the cached list", id)
}

// epubURL is where CCEL keeps a book's EPUB.
func (s *Service) epubURL(author, work string) string {
	return fmt.Sprintf("%s/ccel/%s/%s/%s/cache/%s.epub", s.base, strings.ToLower(author[:1]), author, work, work)
}

// rights reads the start of the book's ThML file for its rights statement.
func (s *Service) rights(ctx context.Context, author, work string) (string, error) {
	resp, err := s.fetch(ctx, http.MethodGet, fmt.Sprintf("%s/ccel/%s/%s/%s.xml", s.base, strings.ToLower(author[:1]), author, work), "bytes=0-32767")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	m := rightsRe.FindSubmatch(data)
	if m == nil {
		return "", nil
	}
	return clean(string(m[1])), nil
}

func (s *Service) get(ctx context.Context, env *command.Env, p GetParams) (GetResult, error) {
	author, work, ok := validID(p.ID)
	if !ok {
		return GetResult{}, command.Errorf("bad_params", "ccel.get: %q is not a CCEL book id", p.ID)
	}
	on, err := s.isEnabled()
	if err != nil {
		return GetResult{}, err
	}
	if !on {
		return GetResult{}, errOff
	}
	w, err := s.find(p.ID)
	if err != nil {
		return GetResult{}, err
	}
	res := GetResult{ID: w.ID, Title: w.Title, Author: w.Author, Format: "EPUB", Licence: "Public domain", Credit: Credit}
	if res.Rights, err = s.rights(ctx, author, work); err != nil {
		return res, err
	}
	if !publicDomain(res.Rights) {
		why := "CCEL does not say this book is in the public domain"
		if res.Rights != "" {
			why = fmt.Sprintf("CCEL gives its rights as %q, not public domain", res.Rights)
		}
		return res, command.Errorf("not_public_domain", "%s, so Glossa will not download it.", why)
	}
	if p.Into == "" {
		s.mu.Lock()
		if st, err := s.load(); err == nil {
			p.Into = st.Into
		}
		s.mu.Unlock()
	}
	url := s.epubURL(author, work)
	if head, err := s.fetch(ctx, http.MethodHead, url, ""); err == nil {
		res.Size = max(head.ContentLength, 0)
		head.Body.Close()
	} else if p.Preview {
		return res, err
	}
	if p.Preview {
		return res, nil
	}
	return s.download(ctx, env, p, w, url, res)
}

func fileName(w Work) string {
	n := w.Title
	if w.Author != "" {
		n = w.Author + " - " + w.Title
	}
	n = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return ' '
		}
		return r
	}, n)
	n = strings.Join(strings.Fields(n), " ")
	if len(n) > 120 {
		n = strings.TrimSpace(n[:120])
	}
	return n + ".epub"
}

func (s *Service) download(ctx context.Context, env *command.Env, p GetParams, w Work, url string, res GetResult) (GetResult, error) {
	resp, err := s.fetch(ctx, http.MethodGet, url, "")
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	tmp, err := os.MkdirTemp("", "glossa-ccel-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(tmp)
	dest := filepath.Join(tmp, fileName(w))
	f, err := os.Create(dest)
	if err != nil {
		return res, err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxBook+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return res, err
	}
	if n > maxBook {
		return res, errors.New("ccel: the book is larger than Glossa accepts")
	}
	if !isZip(dest) {
		return res, errors.New("ccel: what ccel.org sent is not an EPUB file")
	}
	var add struct {
		Added int   `json:"added"`
		Since int64 `json:"since"`
	}
	if err := s.run(ctx, env, "library.add_files", map[string]any{"files": []string{dest}, "into": p.Into}, &add); err != nil {
		return res, err
	}
	if add.Added > 0 {
		var items struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := s.run(ctx, env, "library.items", map[string]any{"added_since": add.Since}, &items); err != nil {
			return res, err
		}
		if len(items.Items) > 0 {
			ids := make([]string, len(items.Items))
			for i, it := range items.Items {
				ids[i] = it.ID
			}
			sort.Strings(ids)
			res.ItemID = ids[0]
			if err := s.run(ctx, env, "library.set_licence", map[string]any{"item_ids": ids, "type": "public_domain",
				"rights_holder": "Christian Classics Ethereal Library", "url": s.base + "/ccel/" + w.ID, "attribution": Credit}, nil); err != nil {
				return res, err
			}
		}
	}
	s.mu.Lock()
	if st, err := s.load(); err == nil {
		n := *st
		n.Installed = append(append([]string{}, st.Installed...), w.ID)
		_ = s.save(&n)
	}
	s.mu.Unlock()
	res.Added = add.Added > 0
	if res.Added {
		res.Summary = fmt.Sprintf("Added %s to your library (public domain, from %s).", w.Title, Credit)
	} else {
		res.Summary = fmt.Sprintf("%s was already in your library.", w.Title)
	}
	return res, nil
}

func isZip(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 2)
	_, err = io.ReadFull(f, b)
	return err == nil && b[0] == 'P' && b[1] == 'K'
}

func (s *Service) run(ctx context.Context, env *command.Env, id string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	res, err := s.r.Run(ctx, env, id, raw)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
