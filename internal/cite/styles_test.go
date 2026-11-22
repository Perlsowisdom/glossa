package cite_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/cite"
	"github.com/Perlsowisdom/glossa/internal/csl"
)

const tinyStyle = `<?xml version="1.0" encoding="utf-8"?>
<style xmlns="http://purl.org/net/xbiblio/csl" class="note" version="1.0">
  <info>
    <title>Tiny notes</title>
    <id>http://www.zotero.org/styles/tiny</id>
    <author><name>Test Author</name></author>
    <updated>2020-01-01T00:00:00+00:00</updated>
    <rights license="http://creativecommons.org/licenses/by-sa/3.0/">This work is licensed under a Creative Commons Attribution-ShareAlike 3.0 License</rights>
  </info>
  <citation>
    <layout suffix=".">
      <choose>
        <if position="ibid-with-locator"><group delimiter=", "><text term="ibid"/><text variable="locator"/></group></if>
        <else-if position="ibid"><text term="ibid"/></else-if>
        <else-if position="subsequent"><group delimiter=", "><names variable="author"><name form="short"/></names><text variable="title" form="short"/></group></else-if>
        <else><group delimiter=", "><names variable="author"/><text variable="title"/></group></else>
      </choose>
    </layout>
  </citation>
</style>`

const tinyJournal = `<?xml version="1.0" encoding="utf-8"?>
<style xmlns="http://purl.org/net/xbiblio/csl" class="note" version="1.0">
  <info>
    <title>Tiny notes for a journal</title>
    <id>http://www.zotero.org/styles/tiny-journal</id>
    <link href="http://www.zotero.org/styles/tiny" rel="independent-parent"/>
    <rights license="http://creativecommons.org/licenses/by-sa/3.0/">CC BY-SA 3.0</rights>
  </info>
</style>`

// fakeStyles serves the two places the downloader reads, and records every request it gets.
type fakeStyles struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []string
	body map[string]string // raw file path -> body
}

func newFakeStyles(t *testing.T) *fakeStyles {
	f := &fakeStyles{body: map[string]string{"tiny.csl": tinyStyle, "dependent/tiny-journal.csl": tinyJournal, "dependent/broken.csl": "not xml at all"}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.reqs = append(f.reqs, r.URL.String())
		f.mu.Unlock()
		switch p := r.URL.Path; {
		case p == "/tree/master":
			fmt.Fprint(w, `{"tree":[{"path":"tiny.csl","type":"blob"},{"path":"apa.csl","type":"blob"},{"path":"README.md","type":"blob"},{"path":"dependent","type":"tree","sha":"DEP"}]}`)
		case p == "/tree/DEP":
			fmt.Fprint(w, `{"tree":[{"path":"tiny-journal.csl","type":"blob"},{"path":"broken.csl","type":"blob"}]}`)
		case strings.HasPrefix(p, "/raw/"):
			if b, ok := f.body[strings.TrimPrefix(p, "/raw/")]; ok {
				fmt.Fprint(w, b)
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	t.Setenv(cite.ListURLEnv, f.srv.URL+"/tree")
	t.Setenv(cite.RawURLEnv, f.srv.URL+"/raw")
	return f
}

func (f *fakeStyles) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func TestStyleDownloadIsOffUntilTurnedOn(t *testing.T) {
	f := newFakeStyles(t)
	g := setup(t)
	var on cite.Online
	g.must("cite.styles_online", struct{}{}, &on)
	if on.Enabled || !strings.Contains(on.Sends, "github.com/citation-style-language/styles") || !strings.Contains(on.Sends, "nothing about you") {
		t.Errorf("off, and says what it sends: %+v", on)
	}
	for _, c := range []struct {
		cmd string
		p   any
	}{{"cite.styles_search", cite.SearchParams{Query: "tiny"}}, {"cite.styles_download", cite.DownloadParams{ID: "tiny"}}} {
		err := g.run(c.cmd, c.p, nil)
		if err == nil || !strings.Contains(err.Error(), "off") {
			t.Errorf("%s while off: %v", c.cmd, err)
		}
	}
	if n := f.count(); n != 0 {
		t.Fatalf("%d requests were sent while the switch was off", n)
	}
	g.must("cite.styles_enable", struct{}{}, &on)
	if !on.Enabled {
		t.Fatal("not on")
	}
	g.must("cite.styles_online", struct{}{}, &on)
	if !on.Enabled {
		t.Fatal("the switch is kept")
	}
	g.must("cite.styles_disable", struct{}{}, &on)
	if err := g.run("cite.styles_search", cite.SearchParams{Query: "tiny"}, nil); err == nil {
		t.Error("off again")
	}
}

func TestStyleSearchAndDownload(t *testing.T) {
	f := newFakeStyles(t)
	g := setup(t)
	g.must("cite.styles_enable", struct{}{}, nil)

	var found cite.SearchStylesResult
	g.must("cite.styles_search", cite.SearchParams{Query: "tiny"}, &found)
	if found.Total != 2 || found.Styles[0].ID != "tiny" || found.Styles[1].ID != "tiny-journal" || !found.Styles[1].Dependent || found.Styles[0].Have {
		t.Errorf("search: %+v", found)
	}
	g.must("cite.styles_search", cite.SearchParams{Query: "apa"}, &found)
	if len(found.Styles) != 1 || !found.Styles[0].Have {
		t.Errorf("a bundled style is marked as here: %+v", found)
	}
	// The words searched for stay on this computer.
	g.must("cite.styles_search", cite.SearchParams{Query: "zebra secret words"}, &found)
	for _, r := range f.reqs {
		if strings.Contains(r, "zebra") || strings.Contains(r, "secret") {
			t.Errorf("the search words were sent: %s", r)
		}
	}

	var got cite.Downloaded
	g.must("cite.styles_download", cite.DownloadParams{ID: "tiny-journal"}, &got)
	if got.Parent != "tiny" || got.Style.ID != "tiny-journal" || got.Style.Class != "note" {
		t.Errorf("download: %+v", got)
	}
	styles := filepath.Join(g.data, "styles")
	for _, name := range []string{"tiny.csl", "tiny-journal.csl", "tiny.licence.txt", "tiny-journal.licence.txt"} {
		if _, err := os.Stat(filepath.Join(styles, name)); err != nil {
			t.Errorf("not saved: %s", name)
		}
	}
	saved, _ := os.ReadFile(filepath.Join(styles, "tiny.csl"))
	if string(saved) != tinyStyle {
		t.Error("the style file is kept unchanged")
	}
	lic, _ := os.ReadFile(filepath.Join(styles, "tiny.licence.txt"))
	for _, w := range []string{"http://creativecommons.org/licenses/by-sa/3.0/", "Test Author", "github.com/citation-style-language/styles", "Tiny notes"} {
		if !strings.Contains(string(lic), w) {
			t.Errorf("the licence note lacks %q:\n%s", w, lic)
		}
	}
	for _, r := range f.reqs {
		if strings.Contains(r, "?") {
			t.Errorf("a request carries a query: %s", r)
		}
	}

	// csl.styles lists them as added, and the dependent one renders through its parent: ibid. and all.
	var list csl.StylesResult
	g.must("csl.styles", struct{}{}, &list)
	added := map[string]string{}
	for _, s := range list.Styles {
		if s.Source == "added" {
			added[s.ID] = s.Title
		}
	}
	if added["tiny"] != "Tiny notes" || added["tiny-journal"] != "Tiny notes for a journal" {
		t.Errorf("listed: %v", added)
	}
	var res cite.Result
	g.must("cite.render", cite.RenderParams{Style: "tiny-journal", Cites: []cite.Cite{
		{Key: g.book, Locator: "1"}, {Key: g.book, Locator: "1"}, {Key: g.book, Locator: "2"}, {Key: "zotero:ABCD2345"}, {Key: g.book, Locator: "9"},
	}}, &res)
	sameStrings(t, "ibid. from a downloaded style", texts(res), []string{
		"Douglas J. Moo, The Epistle to the Romans.", "Ibid.", "Ibid., 2.", "N. T. Wright, Justification and Works.", "Moo, The Epistle to the Romans.",
	})

	// A document can use it, and it can be removed again.
	g.must("cite.set_style", cite.SetStyleParams{ID: "d1", Style: "tiny"}, nil)
	g.must("cite.styles_remove", cite.RemoveParams{ID: "tiny-journal"}, nil)
	if _, err := os.Stat(filepath.Join(styles, "tiny-journal.csl")); err == nil {
		t.Error("not removed")
	}
	if err := g.run("cite.styles_remove", cite.RemoveParams{ID: "apa"}, nil); err == nil {
		t.Error("a bundled style stays")
	}
}

func TestStyleDownloadFailures(t *testing.T) {
	newFakeStyles(t)
	g := setup(t)
	g.must("cite.styles_enable", struct{}{}, nil)
	styles := filepath.Join(g.data, "styles")
	for id, want := range map[string]string{"nonesuch": "not_found", "../escape": "bad_params", "Tiny": "bad_params", "broken": "not a usable style"} {
		err := g.run("cite.styles_download", cite.DownloadParams{ID: id}, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", id, err, want)
		}
	}
	if ents, _ := os.ReadDir(styles); len(ents) != 0 {
		t.Errorf("a failed download saved something: %v", ents)
	}
}

// The server being out of reach is said in plain words, without the address.
func TestStyleSiteUnreachable(t *testing.T) {
	f := newFakeStyles(t)
	f.srv.Close()
	g := setup(t)
	g.must("cite.styles_enable", struct{}{}, nil)
	err := g.run("cite.styles_search", cite.SearchParams{Query: "other"}, nil)
	if err == nil || strings.Contains(err.Error(), "127.0.0.1") || !strings.Contains(err.Error(), "internet connection") {
		t.Errorf("unreachable: %v", err)
	}
}
