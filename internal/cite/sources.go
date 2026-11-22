package cite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect/csljson"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// Source kinds. A source's key is "<prefix>:<id>".
const (
	KindLibrary = "library"
	KindBible   = "bible"
	KindZotero  = "zotero"
	KindManual  = "manual"
)

// Source is a thing that can be cited, as the picker lists it.
type Source struct {
	Key    string `json:"key" desc:"Pass this as the key of a citation"`
	Kind   string `json:"kind" enum:"library,bible,zotero,manual"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty" desc:"Authors and year, or what the version is"`
	Kept   bool   `json:"kept,omitempty" desc:"A Zotero item whose copy is already kept in this library"`
}

// KindOf says what kind of source a key names.
func KindOf(key string) string {
	switch {
	case strings.HasPrefix(key, "lib:"):
		return KindLibrary
	case strings.HasPrefix(key, "bible:"):
		return KindBible
	case strings.HasPrefix(key, "zotero:"):
		return KindZotero
	case strings.HasPrefix(key, "manual:"):
		return KindManual
	}
	return ""
}

func idOf(key string) string { return key[strings.Index(key, ":")+1:] }

// Resolver finds sources and renders citations. It reaches the library, the Bibles and Zotero
// through the command registry, the same way the interface does.
type Resolver struct {
	reg        *command.Registry
	userStyles string
}

// NewResolver makes a resolver over a registry; userStyles is the folder of downloaded styles.
func NewResolver(reg *command.Registry, userStyles string) *Resolver {
	return &Resolver{reg: reg, userStyles: userStyles}
}

func (r *Resolver) run(ctx context.Context, env *command.Env, id string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	res, err := r.reg.Run(ctx, env, id, raw)
	if err != nil {
		return err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// SearchResult is what a search finds.
type SearchResult struct {
	Sources []Source `json:"sources"`
	Notes   []string `json:"notes,omitempty" desc:"Plain words about a place that could not be searched, e.g. Zotero is not running"`
}

// Search looks for sources by words in their title or author, across the library, the installed
// Bibles, the kept and live Zotero items, and manual entries.
func (r *Resolver) Search(ctx context.Context, env *command.Env, query string, limit int) SearchResult {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	query = strings.TrimSpace(query)
	res := SearchResult{Sources: []Source{}}
	res.Sources = append(res.Sources, r.searchBibles(ctx, env, query)...)
	res.Sources = append(res.Sources, r.searchManual(ctx, env, query, limit)...)
	res.Sources = append(res.Sources, r.searchLibrary(ctx, env, query, limit)...)
	z, note := r.searchZotero(ctx, env, query, limit)
	res.Sources = append(res.Sources, z...)
	if note != "" {
		res.Notes = append(res.Notes, note)
	}
	return res
}

func words(q string) []string { return strings.Fields(strings.ToLower(q)) }

func matches(ws []string, hay ...string) bool {
	h := strings.ToLower(strings.Join(hay, " "))
	for _, w := range ws {
		if !strings.Contains(h, w) {
			return false
		}
	}
	return true
}

func (r *Resolver) searchBibles(ctx context.Context, env *command.Env, query string) []Source {
	var mods []struct {
		ID           string `json:"id"`
		Kind         string `json:"kind"`
		Name         string `json:"name"`
		Abbreviation string `json:"abbreviation"`
		Language     string `json:"language"`
	}
	if err := r.run(ctx, env, "bible.modules", map[string]any{}, &mods); err != nil {
		return nil
	}
	ws := words(query)
	var out []Source
	for _, m := range mods {
		if m.Kind != "" && m.Kind != "bible" {
			continue
		}
		if !matches(ws, m.Name, m.Abbreviation, m.ID, "bible") {
			continue
		}
		title := m.Name
		if title == "" {
			title = m.Abbreviation
		}
		detail := "Bible version"
		if m.Abbreviation != "" {
			detail = m.Abbreviation + " · Bible version"
		}
		out = append(out, Source{Key: "bible:" + m.ID, Kind: KindBible, Title: title, Detail: detail})
	}
	return out
}

func (r *Resolver) searchLibrary(ctx context.Context, env *command.Env, query string, limit int) []Source {
	seen := map[string]bool{}
	var out []Source
	for _, p := range []map[string]any{{"text": query, "limit": limit}, {"author": query, "limit": limit}} {
		if query == "" && len(out) > 0 {
			break
		}
		var got struct {
			Items []struct {
				ID      string   `json:"id"`
				Kind    string   `json:"kind"`
				Title   string   `json:"title"`
				Year    int      `json:"year"`
				Authors []string `json:"authors"`
			} `json:"items"`
		}
		if err := r.run(ctx, env, "library.items", p, &got); err != nil {
			return out
		}
		for _, it := range got.Items {
			if seen[it.ID] || it.Kind == "bible" {
				continue
			}
			seen[it.ID] = true
			out = append(out, Source{Key: "lib:" + it.ID, Kind: KindLibrary, Title: it.Title, Detail: detail(strings.Join(it.Authors, " and "), it.Year)})
		}
		if query == "" {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func detail(authors string, year int) string {
	var parts []string
	if authors != "" {
		parts = append(parts, authors)
	}
	if year > 0 {
		parts = append(parts, fmt.Sprint(year))
	}
	return strings.Join(parts, " · ")
}

type keptRow struct {
	key, conn, title string
	item             csljson.Item
}

func (r *Resolver) kept(ctx context.Context, env *command.Env, conn string) []keptRow {
	if env == nil || env.Store == nil {
		return nil
	}
	rows, err := env.Store.Query(ctx, `SELECT id, connection, title, csl_json FROM cite_items WHERE connection = ? ORDER BY title COLLATE NOCASE`, conn)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []keptRow
	for rows.Next() {
		var k keptRow
		var raw string
		if rows.Scan(&k.key, &k.conn, &k.title, &raw) != nil || json.Unmarshal([]byte(raw), &k.item) != nil {
			continue
		}
		out = append(out, k)
	}
	return out
}

func authorsOf(it csljson.Item) string {
	var fam []string
	for _, n := range it.Creators() {
		switch {
		case n.Literal != "":
			fam = append(fam, n.Literal)
		case n.Family != "":
			fam = append(fam, n.Family)
		}
	}
	switch len(fam) {
	case 0:
		return ""
	case 1:
		return fam[0]
	case 2:
		return fam[0] + " and " + fam[1]
	}
	return fam[0] + " et al."
}

func (r *Resolver) searchManual(ctx context.Context, env *command.Env, query string, limit int) []Source {
	ws := words(query)
	var out []Source
	for _, k := range r.kept(ctx, env, "manual") {
		if !matches(ws, k.item.Title, authorsOf(k.item)) {
			continue
		}
		out = append(out, Source{Key: k.key, Kind: KindManual, Title: k.title, Detail: detail(authorsOf(k.item), k.item.Year())})
		if len(out) == limit {
			break
		}
	}
	return out
}

// searchZotero lists Zotero items through the connection when it is on, and the kept copies
// otherwise, so a source cited before can be cited again with Zotero off.
func (r *Resolver) searchZotero(ctx context.Context, env *command.Env, query string, limit int) ([]Source, string) {
	keptKeys := map[string]bool{}
	ws := words(query)
	var out []Source
	for _, k := range r.kept(ctx, env, "zotero") {
		keptKeys[k.key] = true
		if matches(ws, k.item.Title, authorsOf(k.item), fmt.Sprint(k.item.Year())) {
			out = append(out, Source{Key: k.key, Kind: KindZotero, Title: k.title, Detail: detail(authorsOf(k.item), k.item.Year()), Kept: true})
		}
	}
	var got struct {
		Items []struct {
			Key     string `json:"key"`
			Title   string `json:"title"`
			Authors string `json:"authors"`
			Year    int    `json:"year"`
		} `json:"items"`
	}
	cmd, p := "zotero.items", map[string]any{"limit": limit}
	if query != "" {
		cmd, p = "zotero.search", map[string]any{"query": query, "limit": limit}
	}
	err := r.run(ctx, env, cmd, p, &got)
	var ce *command.Error
	switch {
	case err == nil:
	case errors.As(err, &ce) && ce.Code == "off":
		return out, ""
	default:
		msg := "Zotero could not be reached"
		if errors.As(err, &ce) {
			msg = ce.Message
		}
		return out, msg
	}
	have := map[string]bool{}
	for _, s := range out {
		have[s.Key] = true
	}
	for _, it := range got.Items {
		key := "zotero:" + it.Key
		if have[key] {
			continue
		}
		out = append(out, Source{Key: key, Kind: KindZotero, Title: it.Title, Detail: detail(it.Authors, it.Year), Kept: keptKeys[key]})
	}
	return out, ""
}

// Keep makes sure a cited source can be rendered later without the program it came from: a Zotero
// item is kept in cite_items. Other kinds need nothing.
func (r *Resolver) Keep(ctx context.Context, env *command.Env, key string) error {
	if KindOf(key) != KindZotero {
		return nil
	}
	return r.run(ctx, env, "zotero.cite", map[string]any{"key": idOf(key)}, &struct{}{})
}

// Item is a source for the engine: its CSL-JSON, or for a Bible the version's names.
type Item struct {
	Key   string
	CSL   map[string]any // nil for a Bible
	Bible *BibleVersion
	Gone  string // why the source could not be found, when it could not
}

// BibleVersion is an installed Bible, as cited.
type BibleVersion struct {
	Abbreviation, Name string
}

// Resolve finds each key's source. A source that cannot be found has Gone set, so a document with a
// removed book or an unreachable Zotero item still renders the rest.
func (r *Resolver) Resolve(ctx context.Context, env *command.Env, keys []string) map[string]Item {
	out := map[string]Item{}
	var mods []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Abbreviation string `json:"abbreviation"`
	}
	haveMods := false
	for _, key := range keys {
		if _, done := out[key]; done {
			continue
		}
		switch KindOf(key) {
		case KindBible:
			if !haveMods {
				_ = r.run(ctx, env, "bible.modules", map[string]any{}, &mods)
				haveMods = true
			}
			it := Item{Key: key, Gone: "this Bible is not installed"}
			for _, m := range mods {
				if m.ID == idOf(key) {
					abbr := m.Abbreviation
					if abbr == "" {
						abbr = strings.ToUpper(m.ID)
					}
					it = Item{Key: key, Bible: &BibleVersion{Abbreviation: abbr, Name: m.Name}}
				}
			}
			out[key] = it
		case KindLibrary:
			out[key] = r.libraryItem(ctx, env, key)
		case KindZotero, KindManual:
			out[key] = r.keptItem(ctx, env, key)
		default:
			out[key] = Item{Key: key, Gone: "unknown kind of source"}
		}
	}
	return out
}

func (r *Resolver) keptItem(ctx context.Context, env *command.Env, key string) Item {
	if env != nil && env.Store != nil {
		var raw string
		err := env.Store.QueryRow(ctx, `SELECT csl_json FROM cite_items WHERE id = ?`, key).Scan(&raw)
		if err == nil {
			var m map[string]any
			if json.Unmarshal([]byte(raw), &m) == nil {
				m["id"] = key
				return Item{Key: key, CSL: m}
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Item{Key: key, Gone: err.Error()}
		}
	}
	if KindOf(key) == KindZotero {
		var got struct {
			CSL map[string]any `json:"csl"`
		}
		if err := r.run(ctx, env, "zotero.item", map[string]any{"key": idOf(key)}, &got); err == nil && got.CSL != nil {
			got.CSL["id"] = key
			return Item{Key: key, CSL: got.CSL}
		}
		return Item{Key: key, Gone: "this Zotero item was not kept and Zotero is not available"}
	}
	return Item{Key: key, Gone: "this entry is gone"}
}

// libraryItem makes a library book's CSL-JSON from its catalogue details.
func (r *Resolver) libraryItem(ctx context.Context, env *command.Env, key string) Item {
	var it struct {
		Title     string `json:"title"`
		Subtitle  string `json:"subtitle"`
		Year      int    `json:"year"`
		Publisher string `json:"publisher"`
		Series    string `json:"series"`
		SeriesNo  string `json:"series_no"`
		ISBN      string `json:"isbn"`
		Language  string `json:"language"`
		People    []struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"people"`
	}
	if err := r.run(ctx, env, "library.item", map[string]any{"id": idOf(key)}, &it); err != nil {
		return Item{Key: key, Gone: "this book is no longer in the library"}
	}
	m := map[string]any{"id": key, "type": "book", "title": it.Title}
	if it.Subtitle != "" {
		m["title"] = strings.TrimRight(it.Title, ":. ") + ": " + it.Subtitle
	}
	for role, field := range map[string]string{"author": "author", "editor": "editor", "translator": "translator"} {
		var ns []map[string]string
		for _, p := range it.People {
			if strings.EqualFold(p.Role, role) || (role == "author" && p.Role == "") {
				ns = append(ns, NameOf(p.Name))
			}
		}
		if len(ns) > 0 {
			m[field] = ns
		}
	}
	if it.Year > 0 {
		m["issued"] = map[string]any{"date-parts": [][]int{{it.Year}}}
	}
	for k, v := range map[string]string{"publisher": it.Publisher, "collection-title": it.Series, "collection-number": it.SeriesNo, "ISBN": it.ISBN, "language": it.Language} {
		if v != "" {
			m[k] = v
		}
	}
	return Item{Key: key, CSL: m}
}

// NameOf reads a person's name as people type it: "Family, Given" or "Given Family". A name with
// no comma and one word, or one that looks like an organisation, is kept whole.
func NameOf(s string) map[string]string {
	s = normalSpace(s)
	if fam, given, ok := strings.Cut(s, ","); ok {
		return map[string]string{"family": strings.TrimSpace(fam), "given": strings.TrimSpace(given)}
	}
	parts := strings.Fields(s)
	if len(parts) < 2 || strings.ContainsAny(s, "&/") {
		return map[string]string{"literal": s}
	}
	cut := len(parts) - 1
	for cut > 1 && isParticle(parts[cut-1]) {
		cut--
	}
	return map[string]string{"family": strings.Join(parts[cut:], " "), "given": strings.Join(parts[:cut], " ")}
}

func isParticle(w string) bool {
	switch strings.ToLower(w) {
	case "van", "von", "de", "der", "den", "du", "la", "le", "di", "da", "del":
		return true
	}
	return false
}

// Manual entries -----------------------------------------------------------------------------

// Manual is a source typed in by hand.
type Manual struct {
	ID        string   `json:"id,omitempty" desc:"Leave out to make a new entry; give a key (manual:...) to change one"`
	Type      string   `json:"type,omitempty" enum:"book,chapter,article-journal,webpage,thesis,report" desc:"What it is (default book)"`
	Title     string   `json:"title" required:"true"`
	Authors   []string `json:"authors,omitempty" desc:"Each as \"Family, Given\" or \"Given Family\""`
	Editors   []string `json:"editors,omitempty"`
	Year      int      `json:"year,omitempty"`
	Publisher string   `json:"publisher,omitempty"`
	Place     string   `json:"place,omitempty" desc:"Where it was published"`
	Container string   `json:"container,omitempty" desc:"The journal or the book a chapter or article is in"`
	Volume    string   `json:"volume,omitempty"`
	Issue     string   `json:"issue,omitempty"`
	Pages     string   `json:"pages,omitempty"`
	Edition   string   `json:"edition,omitempty"`
	URL       string   `json:"url,omitempty"`
	DOI       string   `json:"doi,omitempty"`
}

func (m Manual) csl(key string) map[string]any {
	typ := m.Type
	if typ == "" {
		typ = "book"
	}
	out := map[string]any{"id": key, "type": typ, "title": strings.TrimSpace(m.Title)}
	names := func(in []string) []map[string]string {
		var ns []map[string]string
		for _, a := range in {
			if strings.TrimSpace(a) != "" {
				ns = append(ns, NameOf(a))
			}
		}
		return ns
	}
	if ns := names(m.Authors); len(ns) > 0 {
		out["author"] = ns
	}
	if ns := names(m.Editors); len(ns) > 0 {
		out["editor"] = ns
	}
	if m.Year > 0 {
		out["issued"] = map[string]any{"date-parts": [][]int{{m.Year}}}
	}
	for k, v := range map[string]string{"publisher": m.Publisher, "publisher-place": m.Place, "container-title": m.Container,
		"volume": m.Volume, "issue": m.Issue, "page": m.Pages, "edition": m.Edition, "URL": m.URL, "DOI": m.DOI} {
		if strings.TrimSpace(v) != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

// SaveManual makes or changes a manual entry and returns its source.
func SaveManual(ctx context.Context, env *command.Env, m Manual) (Source, error) {
	if env == nil || env.Store == nil {
		return Source{}, command.Errorf("unavailable", "keeping a source needs an open library")
	}
	if strings.TrimSpace(m.Title) == "" {
		return Source{}, command.Errorf("bad_params", "a source needs a title")
	}
	key := m.ID
	if key != "" && KindOf(key) != KindManual {
		return Source{}, command.Errorf("bad_params", "only manual entries can be changed here")
	}
	isNew := key == ""
	if isNew {
		key = "manual:" + store.NewID()
	}
	raw, err := json.Marshal(m.csl(key))
	if err != nil {
		return Source{}, err
	}
	err = env.Store.Write(ctx, "cite.manual_save", func(tx *store.Tx) error {
		k := store.Row{"id": key}
		if !isNew {
			if _, err := tx.Get("cite_items", k); errors.Is(err, store.ErrNotFound) {
				return command.Errorf("not_found", "no manual entry %s", key)
			} else if err != nil {
				return err
			}
			return tx.Update("cite_items", k, store.Row{"csl_json": string(raw), "title": strings.TrimSpace(m.Title), "cached_at": time.Now().UnixMilli()})
		}
		return tx.Create("cite_items", store.Row{"id": key, "connection": "manual", "item_key": idOf(key),
			"csl_json": string(raw), "title": strings.TrimSpace(m.Title), "cached_at": time.Now().UnixMilli()})
	})
	if err != nil {
		return Source{}, err
	}
	var it csljson.Item
	_ = json.Unmarshal(raw, &it)
	return Source{Key: key, Kind: KindManual, Title: strings.TrimSpace(m.Title), Detail: detail(authorsOf(it), it.Year())}, nil
}

// ManualEntries lists the manual entries, by title.
func ManualEntries(ctx context.Context, env *command.Env) []Source {
	out := []Source{}
	r := &Resolver{}
	for _, k := range r.kept(ctx, env, "manual") {
		out = append(out, Source{Key: k.key, Kind: KindManual, Title: k.title, Detail: detail(authorsOf(k.item), k.item.Year())})
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out
}
