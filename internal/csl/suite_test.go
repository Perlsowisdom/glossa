package csl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// The official CSL test suite (pinned copy under testdata/test-suite, see README there). The
// runner reports the pass rate overall and per category. Failures do not fail go test, except
// for the fixtures listed in testdata/test-suite/must-pass.txt: those exercise features the
// bundled styles rely on.

var sectionRE = regexp.MustCompile(`(?s)>>=+\s*([A-Z-]+)\s*=+>>\r?\n(.*?)<<=+\s*[A-Z-]+\s*=+<<`)

type fixture struct {
	name     string
	sections map[string]string
}

func parseFixture(name, text string) fixture {
	fx := fixture{name: name, sections: map[string]string{}}
	for _, m := range sectionRE.FindAllStringSubmatch(text, -1) {
		fx.sections[m[1]] = m[2]
	}
	return fx
}

func loadLocales(t testing.TB) *Locales {
	files, err := filepath.Glob("testdata/test-suite/locales/locales-*.xml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no locale files: %v", err)
	}
	set := NewLocales()
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		l, err := LoadLocale(fh)
		fh.Close()
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		set.Add(l)
	}
	return set
}

type suiteCite struct {
	ID             string `json:"id"`
	Locator        any    `json:"locator"`
	Label          string `json:"label"`
	Prefix         string `json:"prefix"`
	Suffix         string `json:"suffix"`
	SuppressAuthor bool   `json:"suppress-author"`
	AuthorOnly     bool   `json:"author-only"`
	Position       *int   `json:"position"`
	NearNote       *bool  `json:"near-note"`
}

func (c suiteCite) item() CiteItem {
	loc := ""
	switch v := c.Locator.(type) {
	case string:
		loc = v
	case float64:
		loc = fmt.Sprint(int(v))
	}
	return CiteItem{ID: c.ID, Locator: loc, Label: c.Label, Prefix: c.Prefix, Suffix: c.Suffix,
		SuppressAuthor: c.SuppressAuthor, AuthorOnly: c.AuthorOnly, Position: c.Position, NearNote: c.NearNote}
}

type suiteCitation struct {
	ID    string      `json:"citationID"`
	Items []suiteCite `json:"citationItems"`
	Props struct {
		NoteIndex int `json:"noteIndex"`
	} `json:"properties"`
}

func suiteClusters(fx fixture) (clusters []Cluster, tracking bool, err error) {
	if s, ok := fx.sections["CITATIONS"]; ok {
		var raw [][]json.RawMessage
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil, false, fmt.Errorf("CITATIONS: %w", err)
		}
		for _, step := range raw {
			var c suiteCitation
			if err := json.Unmarshal(step[0], &c); err != nil {
				return nil, false, err
			}
			cl := Cluster{ID: c.ID, NoteIndex: c.Props.NoteIndex}
			for _, ci := range c.Items {
				cl.Items = append(cl.Items, ci.item())
			}
			clusters = append(clusters, cl)
		}
		return clusters, true, nil
	}
	if s, ok := fx.sections["CITATION-ITEMS"]; ok {
		var raw [][]suiteCite
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil, false, fmt.Errorf("CITATION-ITEMS: %w", err)
		}
		for i, cites := range raw {
			cl := Cluster{ID: fmt.Sprint("C", i)}
			for _, ci := range cites {
				cl.Items = append(cl.Items, ci.item())
			}
			clusters = append(clusters, cl)
		}
		return clusters, false, nil
	}
	return nil, false, nil
}

type bibFilter struct {
	Select  []filterTerm `json:"select"`
	Include []filterTerm `json:"include"`
	Exclude []filterTerm `json:"exclude"`
	Quash   []filterTerm `json:"quash"`
}

type filterTerm struct{ Field, Value string }

func (t filterTerm) match(it *Item) bool {
	v := it.Vars[t.Field]
	if t.Field == "type" {
		v = it.Type
	}
	if t.Value == "" {
		return v == "" && it.Dates[t.Field].IsEmpty() && len(it.Names[t.Field]) == 0
	}
	return v == t.Value
}

func (f bibFilter) keep(it *Item) bool {
	ok := true
	switch {
	case len(f.Select) > 0:
		for _, t := range f.Select {
			ok = ok && t.match(it)
		}
	case len(f.Include) > 0:
		ok = false
		for _, t := range f.Include {
			ok = ok || t.match(it)
		}
	case len(f.Exclude) > 0:
		for _, t := range f.Exclude {
			ok = ok && !t.match(it)
		}
	}
	if ok && len(f.Quash) > 0 {
		all := true
		for _, t := range f.Quash {
			all = all && t.match(it)
		}
		ok = !all
	}
	return ok
}

// runFixture renders a fixture and returns the output to compare with RESULT.
func runFixture(fx fixture, locs *Locales) (string, error) {
	style, err := LoadStyle(strings.NewReader(fx.sections["CSL"]))
	if err != nil {
		return "", err
	}
	items, err := ParseItems([]byte(fx.sections["INPUT"]))
	if err != nil {
		return "", err
	}
	clusters, tracking, err := suiteClusters(fx)
	if err != nil {
		return "", err
	}
	var opts []Option
	if !tracking {
		opts = append(opts, WithoutPositionTracking())
	}
	p, err := New(style, locs, items, opts...)
	if err != nil {
		return "", err
	}
	mode := strings.TrimSpace(fx.sections["MODE"])
	if mode == "citation" && len(clusters) == 0 {
		var cl Cluster
		for _, it := range items {
			cl.Items = append(cl.Items, CiteItem{ID: it.ID})
		}
		clusters = []Cluster{cl}
	}
	for _, c := range clusters {
		if err := p.AddCluster(c); err != nil {
			return "", err
		}
	}
	if mode == "citation" {
		var lines []string
		for _, c := range p.Citations() {
			lines = append(lines, c.Output.HTML)
		}
		return strings.Join(lines, "\n"), nil
	}
	ids := suiteBibEntries(fx, items)
	p.Register(ids...)
	var keep func(*Item) bool
	if s := strings.TrimSpace(fx.sections["BIBSECTION"]); s != "" {
		var f bibFilter
		if err := json.Unmarshal([]byte(s), &f); err != nil {
			return "", fmt.Errorf("BIBSECTION: %w", err)
		}
		keep = f.keep
	}
	var b strings.Builder
	b.WriteString("<div class=\"csl-bib-body\">\n")
	for _, e := range p.BibliographyFiltered(keep) {
		b.WriteString("  <div class=\"csl-entry\">" + e.Output.HTML + "</div>\n")
	}
	b.WriteString("</div>")
	return b.String(), nil
}

func suiteBibEntries(fx fixture, items []*Item) []string {
	var ids []string
	if s, ok := fx.sections["BIBENTRIES"]; ok {
		var lists [][]string
		if json.Unmarshal([]byte(s), &lists) == nil && len(lists) > 0 {
			return lists[len(lists)-1]
		}
	}
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

var resultPrefixRE = regexp.MustCompile(`(?m)^(\.\.|>>)\[\d+\] `)

func expectedResult(fx fixture) string {
	s := strings.ReplaceAll(fx.sections["RESULT"], "\r\n", "\n")
	if _, ok := fx.sections["CITATIONS"]; ok {
		s = resultPrefixRE.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(s)
}

// runWithTimeout renders a fixture, recovering from panics and giving up on a hang.
func runWithTimeout(fx fixture, locs *Locales) (got string, err error) {
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- res{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		s, err := runFixture(fx, locs)
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-time.After(10 * time.Second):
		return "", fmt.Errorf("timeout")
	}
}

func readMustPass(t *testing.T) map[string]bool {
	b, err := os.ReadFile("testdata/test-suite/must-pass.txt")
	if err != nil {
		t.Fatal(err)
	}
	must := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			must[l] = true
		}
	}
	return must
}

func TestCSLSuite(t *testing.T) {
	files, _ := filepath.Glob("testdata/test-suite/humans/*.txt")
	if len(files) == 0 {
		t.Skip("test suite not present; run go generate")
	}
	locs := loadLocales(t)
	must := readMustPass(t)
	type stat struct{ pass, total int }
	cats := map[string]*stat{}
	var failed []string
	passed := 0
	mustFailed := 0
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".txt")
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fx := parseFixture(name, string(b))
		got, err := runWithTimeout(fx, locs)
		want := expectedResult(fx)
		ok := err == nil && strings.TrimSpace(got) == want
		cat := name
		if i := strings.IndexByte(name, '_'); i > 0 {
			cat = name[:i]
		}
		if cats[cat] == nil {
			cats[cat] = &stat{}
		}
		cats[cat].total++
		if ok {
			cats[cat].pass++
			passed++
		} else {
			detail := fmt.Sprintf("%s\n  want: %q\n  got:  %q", name, want, got)
			if err != nil {
				detail += "\n  err: " + err.Error()
			}
			failed = append(failed, detail)
			if must[name] {
				mustFailed++
				t.Errorf("must-pass fixture failed: %s", detail)
			}
		}
	}
	var names []string
	for c := range cats {
		names = append(names, c)
	}
	sort.Strings(names)
	var sb strings.Builder
	fmt.Fprintf(&sb, "CSL test suite: %d of %d fixtures pass (%.1f%%); %d must-pass\n", passed, len(files),
		100*float64(passed)/float64(len(files)), len(must))
	for _, c := range names {
		fmt.Fprintf(&sb, "  %-14s %3d/%3d\n", c, cats[c].pass, cats[c].total)
	}
	t.Log("\n" + sb.String())
	if out := os.Getenv("CSL_SUITE_FAILURES"); out != "" {
		_ = os.WriteFile(out, []byte(sb.String()+"\n"+strings.Join(failed, "\n")), 0o644)
	}
}
