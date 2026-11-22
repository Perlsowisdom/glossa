package cite

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/appdir"
	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/csl"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// Downloading more citation styles is a network feature, so it is off until the person turns it on
// (ADR 0014, docs/privacy.md). It reads two public places in the CSL project's repository,
// github.com/citation-style-language/styles: the list of style names (through GitHub's tree API)
// and one raw style file. Nothing about the person, their documents or what they search for is
// sent: the words are matched on this computer against the list.
const (
	ListURLEnv = "GLOSSA_CSL_LIST_URL"
	RawURLEnv  = "GLOSSA_CSL_RAW_URL"

	defaultListURL = "https://api.github.com/repos/citation-style-language/styles/git/trees/"
	defaultRawURL  = "https://raw.githubusercontent.com/citation-style-language/styles/master/"

	// SendsWords is said beside the switch and in docs/privacy.md.
	SendsWords = "Asks GitHub (github.com/citation-style-language/styles) for the list of style names, and for a style's file when you download it. " +
		"It sends nothing about you, your documents or what you search for: the words are matched on this computer."

	switchKey   = "connect.csl-styles" // under connect. so the switch stays on this device (store/remote.go)
	maxStyle    = 2 << 20
	listMaxAge  = time.Hour
	listMaxSize = 8 << 20
)

var styleIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,120}$`)

// Downloader searches and downloads styles.
type Downloader struct {
	userStyles      string
	listURL, rawURL string
	http            *http.Client
	mu              sync.Mutex
	index           []styleName
	indexAt         time.Time
	now             func() time.Time
}

type styleName struct {
	ID        string
	Dependent bool
}

// NewDownloader makes the downloader. The addresses come from GLOSSA_CSL_LIST_URL and
// GLOSSA_CSL_RAW_URL when set (for tests), else the CSL project's repository.
func NewDownloader(userStyles string) *Downloader {
	d := &Downloader{userStyles: userStyles, listURL: defaultListURL, rawURL: defaultRawURL, now: time.Now,
		http: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, Proxy: http.ProxyFromEnvironment}}}
	if u := appdir.Getenv(ListURLEnv); u != "" {
		d.listURL = strings.TrimRight(u, "/") + "/"
	}
	if u := appdir.Getenv(RawURLEnv); u != "" {
		d.rawURL = strings.TrimRight(u, "/") + "/"
	}
	return d
}

// Close drops idle connections.
func (d *Downloader) Close() { d.http.CloseIdleConnections() }

// Online is the state of the switch.
type Online struct {
	Enabled bool   `json:"enabled"`
	Sends   string `json:"sends" desc:"Exactly what is sent and where"`
}

func (d *Downloader) enabled(ctx context.Context, env *command.Env) (bool, error) {
	if env == nil || env.Store == nil {
		return false, nil
	}
	var raw string
	err := env.Store.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, switchKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var st struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal([]byte(raw), &st)
	return st.Enabled, nil
}

func (d *Downloader) require(ctx context.Context, env *command.Env) error {
	on, err := d.enabled(ctx, env)
	if err != nil {
		return err
	}
	if !on {
		return command.Errorf("off", "Downloading styles is turned off. Turn it on in Settings → Citations first.")
	}
	return nil
}

func (d *Downloader) online(ctx context.Context, env *command.Env, _ struct{}) (Online, error) {
	on, err := d.enabled(ctx, env)
	return Online{Enabled: on, Sends: SendsWords}, err
}

func (d *Downloader) setOnline(on bool) func(context.Context, *command.Env, struct{}) (Online, error) {
	return func(ctx context.Context, env *command.Env, _ struct{}) (Online, error) {
		if env == nil || env.Store == nil {
			return Online{}, command.Errorf("unavailable", "this needs an open library")
		}
		raw, _ := json.Marshal(map[string]bool{"enabled": on})
		name := "cite.styles_disable"
		if on {
			name = "cite.styles_enable"
		}
		err := env.Store.Write(ctx, name, func(tx *store.Tx) error { return putSetting(tx, switchKey, string(raw)) })
		return Online{Enabled: on, Sends: SendsWords}, err
	}
}

func putSetting(tx *store.Tx, key, valueJSON string) error {
	k := store.Row{"key": key}
	if _, err := tx.Get("settings", k); errors.Is(err, store.ErrNotFound) {
		return tx.Create("settings", store.Row{"key": key, "value_json": valueJSON})
	} else if err != nil {
		return err
	}
	return tx.Update("settings", k, store.Row{"value_json": valueJSON})
}

// Searching -------------------------------------------------------------------------------------

func (d *Downloader) get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Glossa")
	req.Header.Set("Accept", "application/vnd.github+json, text/plain, */*")
	res, err := d.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, command.Errorf("unavailable", "Could not reach the styles site. Check the internet connection and try again.")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, command.Errorf("not_found", "That style is not in the styles repository.")
	}
	if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests {
		return nil, command.Errorf("unavailable", "The styles site is asking for a pause (too many requests). Try again in a while.")
	}
	if res.StatusCode != http.StatusOK {
		return nil, command.Errorf("unavailable", "The styles site answered with an error (%d). Try again later.", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, command.Errorf("unavailable", "The download was interrupted. Try again.")
	}
	if int64(len(b)) > limit {
		return nil, command.Errorf("failed", "The answer was larger than expected, so it was not used.")
	}
	return b, nil
}

type treeAnswer struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"tree"`
}

func (d *Downloader) tree(ctx context.Context, ref string) (treeAnswer, error) {
	b, err := d.get(ctx, d.listURL+ref, listMaxSize)
	if err != nil {
		return treeAnswer{}, err
	}
	var t treeAnswer
	if err := json.Unmarshal(b, &t); err != nil {
		return treeAnswer{}, command.Errorf("failed", "The list of styles could not be read.")
	}
	return t, nil
}

func (d *Downloader) names(ctx context.Context) ([]styleName, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.index != nil && d.now().Sub(d.indexAt) < listMaxAge {
		return d.index, nil
	}
	root, err := d.tree(ctx, "master")
	if err != nil {
		return nil, err
	}
	var out []styleName
	dep := ""
	for _, e := range root.Tree {
		switch {
		case e.Type == "tree" && e.Path == "dependent":
			dep = e.SHA
		case e.Type == "blob" && strings.HasSuffix(e.Path, ".csl") && styleIDRe.MatchString(strings.TrimSuffix(e.Path, ".csl")):
			out = append(out, styleName{ID: strings.TrimSuffix(e.Path, ".csl")})
		}
	}
	if dep != "" {
		t, err := d.tree(ctx, dep)
		if err != nil {
			return nil, err
		}
		for _, e := range t.Tree {
			if e.Type == "blob" && strings.HasSuffix(e.Path, ".csl") && styleIDRe.MatchString(strings.TrimSuffix(e.Path, ".csl")) {
				out = append(out, styleName{ID: strings.TrimSuffix(e.Path, ".csl"), Dependent: true})
			}
		}
	}
	d.index, d.indexAt = out, d.now()
	return out, nil
}

// Found is a style that can be downloaded.
type Found struct {
	ID        string `json:"id"`
	Name      string `json:"name" desc:"The style's name as written in the repository"`
	Dependent bool   `json:"dependent" desc:"A journal's style that follows another (its parent is fetched with it)"`
	Have      bool   `json:"have" desc:"Already here: bundled or downloaded"`
}

// SearchParams is a search for styles to download.
type SearchParams struct {
	Query string `json:"query" required:"true" desc:"Words in the style's name, e.g. chicago author or nature"`
	Limit int    `json:"limit,omitempty" desc:"Most to return (default 40)"`
}

// SearchResult lists styles found.
type SearchStylesResult struct {
	Styles []Found `json:"styles"`
	Total  int     `json:"total"`
}

func prettyName(id string) string {
	s := strings.ReplaceAll(id, "-", " ")
	if s == "" {
		return id
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (d *Downloader) have(id string) bool {
	if _, err := csl.BundledStyle(id); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(d.userStyles, id+".csl"))
	return err == nil
}

func (d *Downloader) search(ctx context.Context, env *command.Env, p SearchParams) (SearchStylesResult, error) {
	if err := d.require(ctx, env); err != nil {
		return SearchStylesResult{}, err
	}
	names, err := d.names(ctx)
	if err != nil {
		return SearchStylesResult{}, err
	}
	ws := words(strings.ReplaceAll(p.Query, "-", " "))
	if len(ws) == 0 {
		return SearchStylesResult{}, command.Errorf("bad_params", "type some words of the style's name")
	}
	limit := p.Limit
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	var hits []styleName
	for _, n := range names {
		if matches(ws, strings.ReplaceAll(n.ID, "-", " ")) {
			hits = append(hits, n)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Dependent != hits[j].Dependent {
			return !hits[i].Dependent
		}
		if len(hits[i].ID) != len(hits[j].ID) {
			return len(hits[i].ID) < len(hits[j].ID)
		}
		return hits[i].ID < hits[j].ID
	})
	res := SearchStylesResult{Styles: []Found{}, Total: len(hits)}
	for i, h := range hits {
		if i == limit {
			break
		}
		res.Styles = append(res.Styles, Found{ID: h.ID, Name: prettyName(h.ID), Dependent: h.Dependent, Have: d.have(h.ID)})
	}
	return res, nil
}

// Downloading -----------------------------------------------------------------------------------

// DownloadParams names a style to download.
type DownloadParams struct {
	ID string `json:"id" required:"true" desc:"The style's id from the search, e.g. chicago-author-date"`
}

// Downloaded says what was saved.
type Downloaded struct {
	Style   csl.StyleEntry `json:"style"`
	Parent  string         `json:"parent,omitempty" desc:"The style it follows, downloaded with it"`
	Licence string         `json:"licence" desc:"The style's licence, kept beside it"`
}

type styleInfo struct {
	Title   string `xml:"title"`
	Rights  rights `xml:"rights"`
	Authors []struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Contributors []struct {
		Name string `xml:"name"`
	} `xml:"contributor"`
	Links []struct {
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
	} `xml:"link"`
}

type rights struct {
	License string `xml:"license,attr"`
	Text    string `xml:",chardata"`
}

func readInfo(b []byte) styleInfo {
	var doc struct {
		Info styleInfo `xml:"info"`
	}
	_ = xml.Unmarshal(b, &doc)
	return doc.Info
}

func (d *Downloader) download(ctx context.Context, env *command.Env, p DownloadParams) (Downloaded, error) {
	if err := d.require(ctx, env); err != nil {
		return Downloaded{}, err
	}
	if !styleIDRe.MatchString(p.ID) {
		return Downloaded{}, command.Errorf("bad_params", "%q is not a style id", p.ID)
	}
	names, err := d.names(ctx)
	if err != nil {
		return Downloaded{}, err
	}
	dependent, found := false, false
	for _, n := range names {
		if n.ID == p.ID {
			dependent, found = n.Dependent, true
			break
		}
	}
	if !found {
		return Downloaded{}, command.Errorf("not_found", "There is no style %q in the styles repository.", p.ID)
	}
	return d.fetch(ctx, p.ID, dependent, 0)
}

func (d *Downloader) fetch(ctx context.Context, id string, dependent bool, depth int) (Downloaded, error) {
	if depth > 2 {
		return Downloaded{}, command.Errorf("failed", "The style %q follows too many other styles.", id)
	}
	path := id + ".csl"
	if dependent {
		path = "dependent/" + path
	}
	b, err := d.get(ctx, d.rawURL+path, maxStyle)
	if err != nil {
		return Downloaded{}, err
	}
	st, err := csl.LoadStyle(strings.NewReader(string(b)))
	if err != nil {
		return Downloaded{}, command.Errorf("failed", "That file is not a usable style (%v), so it was not saved.", err)
	}
	info := readInfo(b)
	out := Downloaded{}
	if st.Dependent {
		pid := st.ParentLink[strings.LastIndex(st.ParentLink, "/")+1:]
		if !styleIDRe.MatchString(pid) {
			return Downloaded{}, command.Errorf("failed", "The style %q names a parent style that cannot be fetched.", id)
		}
		if !d.have(pid) {
			if _, err := d.fetch(ctx, pid, false, depth+1); err != nil {
				return Downloaded{}, err
			}
		}
		out.Parent = pid
	}
	if err := os.MkdirAll(d.userStyles, 0o755); err != nil {
		return Downloaded{}, err
	}
	if err := writeAtomic(filepath.Join(d.userStyles, id+".csl"), b); err != nil {
		return Downloaded{}, err
	}
	out.Licence = licenceNote(id, info, d.rawURL+path, d.now())
	if err := writeAtomic(filepath.Join(d.userStyles, id+".licence.txt"), []byte(out.Licence)); err != nil {
		return Downloaded{}, err
	}
	class := st.Class
	out.Style = csl.StyleEntry{ID: id, Title: st.Title, Class: class, Source: "added"}
	return out, nil
}

func licenceNote(id string, in styleInfo, from string, at time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Citation style: %s (%s)\n", strings.TrimSpace(in.Title), id)
	fmt.Fprintf(&b, "Downloaded from: %s on %s\n", strings.TrimSuffix(from, ".csl")+".csl", at.Format("2006-01-02"))
	lic := strings.TrimSpace(in.Rights.License)
	if lic == "" {
		lic = "not stated in the file"
	}
	fmt.Fprintf(&b, "Licence: %s\n", lic)
	if t := strings.TrimSpace(in.Rights.Text); t != "" {
		fmt.Fprintf(&b, "Rights: %s\n", normalSpace(t))
	}
	var names []string
	for _, a := range in.Authors {
		names = append(names, strings.TrimSpace(a.Name))
	}
	for _, c := range in.Contributors {
		names = append(names, strings.TrimSpace(c.Name))
	}
	if len(names) > 0 {
		fmt.Fprintf(&b, "Authors: %s\n", strings.Join(names, ", "))
	}
	b.WriteString("The style file next to this note is unchanged from the Citation Style Language project's repository, https://github.com/citation-style-language/styles.\n")
	return b.String()
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// RemoveParams names a downloaded style to remove.
type RemoveParams struct {
	ID string `json:"id" required:"true"`
}

func (d *Downloader) remove(_ context.Context, _ *command.Env, p RemoveParams) (struct{}, error) {
	if !styleIDRe.MatchString(p.ID) {
		return struct{}{}, command.Errorf("bad_params", "%q is not a style id", p.ID)
	}
	if _, err := csl.BundledStyle(p.ID); err == nil {
		return struct{}{}, command.Errorf("bad_params", "The styles that come with Glossa cannot be removed.")
	}
	if err := os.Remove(filepath.Join(d.userStyles, p.ID+".csl")); err != nil {
		if os.IsNotExist(err) {
			return struct{}{}, command.Errorf("not_found", "No downloaded style %q.", p.ID)
		}
		return struct{}{}, err
	}
	os.Remove(filepath.Join(d.userStyles, p.ID+".licence.txt"))
	return struct{}{}, nil
}
