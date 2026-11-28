// Package gutenberg offers public-domain books from Project Gutenberg (gutenberg.org) as free copies.
// It uses the network only after the user has turned it on (ADR 0009) and only for the requests
// docs/privacy.md lists: Gutenberg's own catalogue file at most once a week, and, for a book the user
// chose, its EPUB. Requests are made one at a time with a pause between them and the User-Agent
// "Glossa". Terms and formats are in docs/sources/gutenberg.md.
package gutenberg

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
)

const (
	DefaultBase = "https://www.gutenberg.org"
	userAgent   = "Glossa"
	// Credit is shown with every book and recorded as its attribution.
	Credit = "Project Gutenberg (gutenberg.org)"
	// LicenceName is what the book's licence is called in the interface.
	LicenceName  = "Public domain (US)"
	refreshEvery = 7 * 24 * time.Hour
	requestGap   = 5 * time.Second
	maxCatalogue = 64 << 20
	maxBook      = 512 << 20
	catalogue    = "/cache/epub/feeds/pg_catalog.csv.gz"
)

// Work is one text in the catalogue.
type Work struct {
	ID       string `json:"id" desc:"Project Gutenberg's book number"`
	Title    string `json:"title"`
	Author   string `json:"author,omitempty" desc:"Authors as First Last, separated by semicolons"`
	Language string `json:"language,omitempty" desc:"The first language the catalogue gives, e.g. en"`
}

type state struct {
	Enabled     bool     `json:"enabled"`
	RefreshedAt int64    `json:"refreshed_at,omitempty"`
	Works       []Work   `json:"works,omitempty"`
	Installed   []string `json:"installed,omitempty"`
	// Base, Into and GapMS are for tests and for people who run a mirror, not interface settings.
	Base  string `json:"base,omitempty"`
	Into  string `json:"into,omitempty"`
	GapMS *int   `json:"gap_ms,omitempty"`
}

// Service is the Project Gutenberg source.
type Service struct {
	dir  string
	base string
	http *http.Client
	now  func() time.Time
	gap  time.Duration
	mu   sync.Mutex // guards the state and the time of the last request
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

// Register adds the Gutenberg commands; the cache lives in dir.
func Register(r *command.Registry, dir string, opts ...Option) *Service {
	s := &Service{dir: dir, base: DefaultBase, http: &http.Client{Timeout: 10 * time.Minute}, now: time.Now, gap: requestGap, r: r}
	for _, o := range opts {
		o(s)
	}
	r.Add(
		command.Define("gutenberg.settings", "Show, or turn on or off, Project Gutenberg (gutenberg.org) as a source of free public-domain books. Off until turned on; nothing is sent to gutenberg.org while it is off.", true, s.settings),
		command.Define("gutenberg.refresh", "Fetch Project Gutenberg's catalogue file (at most once a week). Needs Project Gutenberg turned on.", true, s.refresh),
		command.Define("gutenberg.items", "List Project Gutenberg's texts from the cache. Never uses the network.", false, s.items),
		command.Define("gutenberg.get", "Get a book from Project Gutenberg into your library as an EPUB. With preview, shows the size and licence without downloading the book.", true, s.get),
	)
	return s
}

func (s *Service) path() string { return filepath.Join(s.dir, "gutenberg.json") }

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
			return nil, fmt.Errorf("gutenberg: %s: %w", s.path(), err)
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

var errOff = command.Errorf("off", "Project Gutenberg is turned off. Turn it on in Settings → Privacy first.")

// fetch makes one request to gutenberg.org, after checking the switch and waiting out the pause.
func (s *Service) fetch(ctx context.Context, method, url string) (*http.Response, error) {
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
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("gutenberg.org answered %s for %s", resp.Status, url)
	}
	return resp, nil
}

// SettingsParams turns Project Gutenberg on or off.
type SettingsParams struct {
	Enable *bool `json:"enable,omitempty" desc:"true turns Project Gutenberg on, false turns it off (the cached list stays); leave out to only look"`
}

// SettingsResult is the state of the source.
type SettingsResult struct {
	Enabled     bool   `json:"enabled"`
	Works       int    `json:"works" desc:"Texts in the cached list"`
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
	resp, err := s.fetch(ctx, http.MethodGet, s.base+catalogue)
	if err != nil {
		return RefreshResult{}, err
	}
	defer resp.Body.Close()
	works, err := parseCatalogue(io.LimitReader(resp.Body, maxCatalogue))
	if err != nil {
		return RefreshResult{}, err
	}
	if len(works) == 0 {
		return RefreshResult{}, errors.New("gutenberg: the catalogue had no texts in it; the file may have changed")
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
	yearsRe  = regexp.MustCompile(`,?\s*\(?\s*(?:approximately\s+)?\d{0,4}\??\s*(?:BCE|BC|CE|AD)?\s*-\s*\d{0,4}\??\s*(?:BCE|BC|CE|AD)?\)?\s*$`)
	bracketR = regexp.MustCompile(`\s*\[[^\]]*\]`)
)

// cleanAuthor turns "Calvin, Jean, 1509-1564" into "Jean Calvin"; several authors are kept, separated
// by semicolons.
func cleanAuthor(raw string) string {
	var out []string
	for _, a := range strings.Split(raw, ";") {
		a = bracketR.ReplaceAllString(strings.TrimSpace(a), "")
		a = strings.TrimSpace(yearsRe.ReplaceAllString(a, ""))
		if a == "" {
			continue
		}
		if last, first, ok := strings.Cut(a, ","); ok {
			a = strings.TrimSpace(first + " " + last)
		}
		out = append(out, a)
	}
	return strings.Join(out, "; ")
}

// parseCatalogue reads pg_catalog.csv.gz: one row per item with the columns Text#, Type, Issued,
// Title, Language, Authors, Subjects, LoCC and Bookshelves. Only texts are kept (not audio, music,
// images or other kinds); a text with no title is left out.
func parseCatalogue(r io.Reader) ([]Work, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gutenberg: the catalogue is not a gzip file: %w", err)
	}
	defer zr.Close()
	cr := csv.NewReader(zr)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("gutenberg: read the catalogue: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	idC, okID := col["text#"]
	titleC, okT := col["title"]
	if !okID || !okT {
		return nil, errors.New("gutenberg: the catalogue has no Text# and Title columns; the file may have changed")
	}
	typeC, hasType := col["type"]
	langC, authC := col["language"], col["authors"]
	get := func(rec []string, i int) string {
		if i >= 0 && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	var out []Work
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gutenberg: read the catalogue: %w", err)
		}
		if hasType && !strings.EqualFold(get(rec, typeC), "Text") {
			continue
		}
		id := get(rec, idC)
		if _, err := strconv.Atoi(id); err != nil {
			continue
		}
		title := strings.Join(strings.Fields(get(rec, titleC)), " ")
		if title == "" {
			continue
		}
		lang, _, _ := strings.Cut(get(rec, langC), ";")
		out = append(out, Work{ID: id, Title: title, Author: cleanAuthor(get(rec, authC)), Language: strings.TrimSpace(lang)})
	}
	return out, nil
}

// ItemsParams filters the cached list.
type ItemsParams struct {
	Text  string `json:"text,omitempty" desc:"Words in the title or author"`
	Limit int    `json:"limit,omitempty" desc:"Most texts to return (default 100)"`
}

// Item is a text in the cached list.
type Item struct {
	Work
	Installed bool `json:"installed" desc:"Already in your library"`
}

// ItemsResult is a page of the cache.
type ItemsResult struct {
	Items   []Item `json:"items"`
	Total   int    `json:"total"`
	Enabled bool   `json:"enabled"`
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
		if len(words) > 0 {
			hay := strings.ToLower(w.Title + " " + w.Author)
			ok := true
			for _, x := range words {
				ok = ok && strings.Contains(hay, x)
			}
			if !ok {
				continue
			}
		}
		res.Total++
		if len(res.Items) < p.Limit {
			res.Items = append(res.Items, Item{Work: w, Installed: have[w.ID]})
		}
	}
	return res, nil
}

// GetParams gets one book.
type GetParams struct {
	ID      string `json:"id" required:"true" desc:"The book number from gutenberg.items"`
	Preview bool   `json:"preview,omitempty" desc:"Check the size only; do not download the book"`
	Into    string `json:"into,omitempty" desc:"Folder to put the book in; Documents/Glossa Library when omitted"`
}

// GetResult describes the book.
type GetResult struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author,omitempty"`
	Format  string `json:"format"`
	Size    int64  `json:"size" desc:"Bytes to download; 0 when Project Gutenberg does not say"`
	Licence string `json:"licence" desc:"Licence it gets in your library"`
	Credit  string `json:"credit"`
	Added   bool   `json:"added,omitempty" desc:"The book is now in your library"`
	ItemID  string `json:"item_id,omitempty" desc:"The library item"`
	Summary string `json:"summary,omitempty"`
}

func validID(id string) bool {
	if id == "" || len(id) > 9 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
	return Work{}, command.Errorf("not_found", "Project Gutenberg has no book %q in the cached list", id)
}

// epubURL is where Project Gutenberg keeps a book's EPUB 3 with images.
func (s *Service) epubURL(id string) string {
	return fmt.Sprintf("%s/cache/epub/%s/pg%s-images-3.epub", s.base, id, id)
}

func (s *Service) get(ctx context.Context, env *command.Env, p GetParams) (GetResult, error) {
	if !validID(p.ID) {
		return GetResult{}, command.Errorf("bad_params", "gutenberg.get: %q is not a Project Gutenberg book number", p.ID)
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
	res := GetResult{ID: w.ID, Title: w.Title, Author: w.Author, Format: "EPUB", Licence: LicenceName, Credit: Credit}
	if p.Into == "" {
		s.mu.Lock()
		if st, err := s.load(); err == nil {
			p.Into = st.Into
		}
		s.mu.Unlock()
	}
	url := s.epubURL(w.ID)
	if p.Preview {
		head, err := s.fetch(ctx, http.MethodHead, url)
		if err != nil {
			return res, err
		}
		res.Size = max(head.ContentLength, 0)
		head.Body.Close()
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

// The book is added as Project Gutenberg sent it, with its own licence text left in: its terms let a
// copy keep that text, and removing it is only allowed if every mention of Project Gutenberg goes too.
func (s *Service) download(ctx context.Context, env *command.Env, p GetParams, w Work, url string, res GetResult) (GetResult, error) {
	resp, err := s.fetch(ctx, http.MethodGet, url)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	res.Size = max(resp.ContentLength, 0)
	tmp, err := os.MkdirTemp("", "glossa-gutenberg-")
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
		return res, errors.New("gutenberg: the book is larger than Glossa accepts")
	}
	if !isZip(dest) {
		return res, errors.New("gutenberg: what gutenberg.org sent is not an EPUB file")
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
				"rights_holder": "Project Gutenberg (public domain in the US)", "url": s.base + "/ebooks/" + w.ID, "attribution": Credit}, nil); err != nil {
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
		res.Summary = fmt.Sprintf("Added %s to your library (public domain in the US, from %s).", w.Title, Credit)
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
