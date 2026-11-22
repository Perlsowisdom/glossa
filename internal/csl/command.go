package csl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
)

// Register adds the citation commands. userStyles is the folder holding styles the user has
// added or downloaded (it need not exist); locales come from the bundled en-US and en-GB.
func Register(r *command.Registry, userStyles string) {
	s := &service{userStyles: userStyles}
	r.Add(
		command.Define("csl.styles", "List the citation styles available: the ones that ship with Glossa and any the "+
			"user has added.", false, s.styles),
		command.Define("csl.render", "Render citations and a bibliography in a citation style, for sources given as "+
			"CSL-JSON and citations given in document order. Returns the text or HTML of each citation (a note "+
			"style gives each a note number) and of each bibliography entry.", false, s.render),
	)
}

type service struct{ userStyles string }

// StylesResult is what csl.styles returns.
type StylesResult struct {
	Styles []StyleEntry `json:"styles" desc:"The available styles, by title"`
}

// StyleEntry is one style in the list.
type StyleEntry struct {
	ID     string `json:"id" desc:"Pass this as the style to csl.render"`
	Title  string `json:"title"`
	Class  string `json:"class" desc:"note (footnotes) or in-text (author-date or numeric)"`
	Source string `json:"source" desc:"bundled or added"`
}

func (s *service) styles(_ context.Context, _ *command.Env, _ struct{}) (StylesResult, error) {
	res := StylesResult{Styles: []StyleEntry{}}
	for _, b := range BundledStyles() {
		res.Styles = append(res.Styles, StyleEntry{ID: b.ID, Title: b.Title, Class: b.Class, Source: "bundled"})
	}
	ents, _ := os.ReadDir(s.userStyles)
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".csl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".csl")
		st, err := s.loadFile(id)
		if err != nil {
			continue
		}
		res.Styles = append(res.Styles, StyleEntry{ID: id, Title: st.Title, Class: st.Class, Source: "added"})
	}
	sort.SliceStable(res.Styles, func(i, j int) bool { return res.Styles[i].Title < res.Styles[j].Title })
	return res, nil
}

// loadFile loads a style from the user's folder, resolving a dependent style's parent.
func (s *service) loadFile(id string) (*Style, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return nil, fmt.Errorf("bad style id %q", id)
	}
	b, err := os.ReadFile(filepath.Join(s.userStyles, id+".csl"))
	if err != nil {
		return nil, err
	}
	st, err := LoadStyle(strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	if st.Dependent {
		parent, err := s.loadParent(st)
		if err != nil {
			return nil, err
		}
		return st.WithParent(parent), nil
	}
	return st, nil
}

func (s *service) loadParent(st *Style) (*Style, error) {
	pid := st.ParentLink[strings.LastIndex(st.ParentLink, "/")+1:]
	if p, err := BundledStyle(pid); err == nil {
		return p, nil
	}
	if p, err := s.loadFile(pid); err == nil {
		return p, nil
	}
	return nil, fmt.Errorf("the parent style %q of %q is not available", pid, st.Title)
}

func (s *service) style(id string) (*Style, error) {
	if st, err := BundledStyle(id); err == nil {
		return st, nil
	}
	if st, err := s.loadFile(id); err == nil {
		return st, nil
	}
	return nil, command.Errorf("not_found", "no citation style %q (see csl.styles)", id)
}

// RenderParams are the parameters of csl.render.
type RenderParams struct {
	Style        string           `json:"style" required:"true" desc:"A style id from csl.styles"`
	Items        []map[string]any `json:"items" required:"true" desc:"The sources, as CSL-JSON items (id, type, title, author ...)"`
	Clusters     []ClusterParam   `json:"clusters,omitempty" desc:"The citations, in document order"`
	Format       string           `json:"format,omitempty" enum:"text,html" desc:"text (default) or html"`
	Bibliography *bool            `json:"bibliography,omitempty" desc:"Include the bibliography (default true)"`
	Lang         string           `json:"lang,omitempty" desc:"Language for terms and dates, e.g. en-GB (default: the style's)"`
}

// ClusterParam is one citation: the cites that appear together.
type ClusterParam struct {
	ID        string      `json:"id,omitempty" desc:"Your name for this citation"`
	NoteIndex int         `json:"note_index,omitempty" desc:"Footnote number for note styles; omit to number them in order"`
	Cites     []CiteParam `json:"cites" required:"true"`
}

// CiteParam is one cited source.
type CiteParam struct {
	Item           string `json:"item" required:"true" desc:"The id of a source in items"`
	Locator        string `json:"locator,omitempty" desc:"A page, chapter, verse or section number"`
	Label          string `json:"label,omitempty" desc:"What the locator is: page (default), chapter, verse, section, ..."`
	Prefix         string `json:"prefix,omitempty"`
	Suffix         string `json:"suffix,omitempty"`
	SuppressAuthor bool   `json:"suppress_author,omitempty" desc:"Leave out the author (when the text names them)"`
}

// RenderResult is what csl.render returns.
type RenderResult struct {
	Style        StyleEntry    `json:"style"`
	Citations    []CitationOut `json:"citations"`
	Bibliography []BibEntryOut `json:"bibliography"`
	Format       string        `json:"format"`
}

// CitationOut is one rendered citation.
type CitationOut struct {
	ID         string   `json:"id"`
	NoteNumber int      `json:"note_number,omitempty" desc:"The footnote number, for note styles"`
	Output     string   `json:"output" desc:"The citation in the requested format"`
	Positions  []string `json:"positions" desc:"first, subsequent, ibid or ibid-with-locator, one per cite"`
}

// BibEntryOut is one rendered bibliography entry.
type BibEntryOut struct {
	ID     string `json:"id"`
	Output string `json:"output"`
}

func (s *service) render(_ context.Context, _ *command.Env, p RenderParams) (RenderResult, error) {
	st, err := s.style(p.Style)
	if err != nil {
		return RenderResult{}, err
	}
	raw, err := json.Marshal(p.Items)
	if err != nil {
		return RenderResult{}, command.Errorf("bad_params", "items: %v", err)
	}
	items, err := ParseItems(raw)
	if err != nil {
		return RenderResult{}, command.Errorf("bad_params", "%v", err)
	}
	var opts []Option
	if p.Lang != "" {
		opts = append(opts, WithLang(p.Lang))
	}
	proc, err := New(st, BundledLocales(), items, opts...)
	if err != nil {
		return RenderResult{}, command.Errorf("failed", "%v", err)
	}
	format := p.Format
	if format == "" {
		format = "text"
	}
	pick := func(o Output) string {
		if format == "html" {
			return o.HTML
		}
		return o.Text
	}
	for i, c := range p.Clusters {
		cl := Cluster{ID: c.ID, NoteIndex: c.NoteIndex}
		if cl.ID == "" {
			cl.ID = fmt.Sprintf("cite-%d", i+1)
		}
		for _, ci := range c.Cites {
			cl.Items = append(cl.Items, CiteItem{ID: ci.Item, Locator: ci.Locator, Label: ci.Label, Prefix: ci.Prefix,
				Suffix: ci.Suffix, SuppressAuthor: ci.SuppressAuthor})
		}
		if err := proc.AddCluster(cl); err != nil {
			return RenderResult{}, command.Errorf("bad_params", "%v", err)
		}
	}
	res := RenderResult{Style: StyleEntry{ID: p.Style, Title: st.Title, Class: st.Class}, Format: format,
		Citations: []CitationOut{}, Bibliography: []BibEntryOut{}}
	for _, c := range proc.Citations() {
		res.Citations = append(res.Citations, CitationOut{ID: c.ClusterID, NoteNumber: c.NoteNumber, Output: pick(c.Output), Positions: c.Positions})
	}
	if p.Bibliography == nil || *p.Bibliography {
		if len(p.Clusters) == 0 {
			for _, it := range items {
				proc.Register(it.ID)
			}
		}
		for _, e := range proc.Bibliography() {
			res.Bibliography = append(res.Bibliography, BibEntryOut{ID: e.ID, Output: pick(e.Output)})
		}
	}
	return res, nil
}
