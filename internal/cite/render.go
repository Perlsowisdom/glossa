package cite

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/csl"
)

// Run is a stretch of rendered text with one set of styling (what Word and PDF exports map from).
type Run struct {
	Text      string `json:"text"`
	Italic    bool   `json:"italic,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
	SmallCaps bool   `json:"small_caps,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Super     bool   `json:"super,omitempty"`
	Sub       bool   `json:"sub,omitempty"`
}

// Out is a rendered citation or bibliography entry in the forms a document needs.
type Out struct {
	Text string `json:"text"`
	HTML string `json:"html"`
	Runs []Run  `json:"runs"`
}

func outOf(o csl.Output) Out {
	out := Out{Text: o.Text, HTML: o.HTML, Runs: make([]Run, 0, len(o.Runs))}
	for _, r := range o.Runs {
		out.Runs = append(out.Runs, Run{Text: r.Text, Italic: r.Italic, Bold: r.Bold, SmallCaps: r.SmallCaps,
			Underline: r.Underline, Super: r.Super, Sub: r.Sub})
	}
	return out
}

func plainOut(s string) Out { return Out{Text: s, HTML: html.EscapeString(s), Runs: []Run{{Text: s}}} }

// Rendered is one citation, rendered.
type Rendered struct {
	Key        string `json:"key"`
	NoteNumber int    `json:"note_number,omitempty" desc:"The footnote number, for a note style"`
	Out
	Position string `json:"position,omitempty" desc:"first, subsequent, ibid or ibid-with-locator"`
	Missing  string `json:"missing,omitempty" desc:"Set when the source could not be found, and why"`
}

// BibEntry is one entry of the bibliography.
type BibEntry struct {
	Key string `json:"key"`
	Out
}

// StyleInfo describes the style used.
type StyleInfo struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Class           string `json:"class" desc:"note (footnotes) or in-text (author-date or numeric)"`
	HasBibliography bool   `json:"has_bibliography"`
}

// Result is a rendered document: its citations in order, and its bibliography.
type Result struct {
	Style        StyleInfo  `json:"style"`
	Citations    []Rendered `json:"citations"`
	Bibliography []BibEntry `json:"bibliography"`
}

// DefaultStyle is the style used when neither the document nor Settings names one.
const DefaultStyle = "society-of-biblical-literature-fullnote-bibliography"

// Render renders citations (in document order) and their bibliography in a style.
func (r *Resolver) Render(ctx context.Context, env *command.Env, styleID string, cites []Cite) (Result, error) {
	st, err := r.loadStyle(styleID)
	if err != nil {
		return Result{}, err
	}
	res := Result{Style: StyleInfo{ID: styleID, Title: st.Title, Class: st.Class, HasBibliography: st.HasBibliography()},
		Citations: make([]Rendered, 0, len(cites)), Bibliography: []BibEntry{}}
	keys := make([]string, len(cites))
	for i, c := range cites {
		keys[i] = c.Key
	}
	srcs := r.Resolve(ctx, env, keys)
	proc, err := r.processor(st, srcs)
	if err != nil {
		return Result{}, err
	}
	note := st.IsNote()
	var added []int
	prevVersion := ""
	for i, c := range cites {
		it := srcs[c.Key]
		out := Rendered{Key: c.Key}
		if note {
			out.NoteNumber = i + 1
		}
		switch {
		case it.Gone != "":
			out.Missing = it.Gone
			out.Out = plainOut("[missing source: " + it.Gone + "]")
		case it.Bible != nil:
			out.Out = plainOut(bibleText(c, *it.Bible, prevVersion, note))
			prevVersion = it.Bible.Abbreviation
		default:
			cl := csl.Cluster{ID: fmt.Sprintf("c%d", i), Items: []csl.CiteItem{citeItem(c)}}
			if note {
				cl.NoteIndex = i + 1
			}
			if err := proc.AddCluster(cl); err != nil {
				return Result{}, command.Errorf("failed", "%v", err)
			}
			added = append(added, i)
		}
		res.Citations = append(res.Citations, out)
	}
	byID := map[string]csl.Citation{}
	for _, c := range proc.Citations() {
		byID[c.ClusterID] = c
	}
	for _, i := range added {
		c := byID[fmt.Sprintf("c%d", i)]
		res.Citations[i].Out = outOf(c.Output)
		if len(c.Positions) > 0 {
			res.Citations[i].Position = c.Positions[0]
		}
	}
	for _, e := range proc.Bibliography() {
		res.Bibliography = append(res.Bibliography, BibEntry{Key: e.ID, Out: outOf(e.Output)})
	}
	return res, nil
}

// Short gives each citation as a short label for a chip in the editor: a note style's short form (as
// in a later note), an author-date style's citation, a Bible's passage and version. It never numbers.
func (r *Resolver) Short(ctx context.Context, env *command.Env, styleID string, cites []Cite) ([]string, error) {
	st, err := r.loadStyle(styleID)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(cites))
	for i, c := range cites {
		keys[i] = c.Key
	}
	srcs := r.Resolve(ctx, env, keys)
	proc, err := r.processor(st, srcs)
	if err != nil {
		return nil, err
	}
	note := st.IsNote()
	var asked []int
	for i, c := range cites {
		it := srcs[c.Key]
		if it.Gone != "" || it.Bible != nil {
			continue
		}
		if note {
			// The same source cited twice, far apart, so the second is in its short form.
			_ = proc.AddCluster(csl.Cluster{ID: fmt.Sprintf("a%d", i), NoteIndex: 10*i + 1, Items: []csl.CiteItem{citeItem(c)}})
			_ = proc.AddCluster(csl.Cluster{ID: fmt.Sprintf("b%d", i), NoteIndex: 10*i + 5, Items: []csl.CiteItem{citeItem(c)}})
		} else {
			_ = proc.AddCluster(csl.Cluster{ID: fmt.Sprintf("b%d", i), Items: []csl.CiteItem{citeItem(c)}})
		}
		asked = append(asked, i)
	}
	byID := map[string]string{}
	for _, c := range proc.Citations() {
		byID[c.ClusterID] = c.Output.Text
	}
	out := make([]string, len(cites))
	for i, c := range cites {
		it := srcs[c.Key]
		switch {
		case it.Gone != "":
			out[i] = "Missing source"
		case it.Bible != nil:
			out[i] = strings.TrimSuffix(bibleShort(c, *it.Bible), ".")
		}
	}
	for _, i := range asked {
		out[i] = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(byID[fmt.Sprintf("b%d", i)]), "."))
	}
	return out, nil
}

func (r *Resolver) loadStyle(id string) (*csl.Style, error) {
	if id == "" {
		id = DefaultStyle
	}
	return csl.StyleByID(r.userStyles, id)
}

func (r *Resolver) processor(st *csl.Style, srcs map[string]Item) (*csl.Processor, error) {
	var items []*csl.Item
	for _, it := range srcs {
		if it.CSL == nil {
			continue
		}
		raw, err := json.Marshal(it.CSL)
		if err != nil {
			continue
		}
		ci, err := csl.ParseItem(raw)
		if err != nil {
			continue
		}
		ci.ID = it.Key
		items = append(items, ci)
	}
	proc, err := csl.New(st, csl.BundledLocales(), items)
	if err != nil {
		return nil, command.Errorf("failed", "%v", err)
	}
	return proc, nil
}

func citeItem(c Cite) csl.CiteItem {
	label := c.Label
	if label == "" && c.Locator != "" {
		label = "page"
	}
	return csl.CiteItem{ID: c.Key, Locator: c.Locator, Label: label, Prefix: c.Prefix, Suffix: c.Suffix, SuppressAuthor: c.SuppressAuthor}
}

// Bible cites -------------------------------------------------------------------------------

func passageOf(c Cite) string {
	if p, ok := Passage(c.Locator); ok {
		return p
	}
	return strings.TrimSpace(c.Locator)
}

func withAffixes(c Cite, s string) string {
	if c.Prefix != "" {
		s = c.Prefix + " " + s
	}
	if suf := c.Suffix; suf != "" {
		if strings.ContainsAny(suf[:1], ",;:.)") {
			s += suf
		} else {
			s += " " + suf
		}
	}
	return s
}

// bibleText renders a Bible cite for a document. A note style gives the passage with the version
// in parentheses when it is the first Bible cite or the version changed, ending with a full stop
// ("Gen 1:1 (NRSV)."); an in-text style gives "(NRSV, Gen 1:1)".
func bibleText(c Cite, v BibleVersion, prevVersion string, note bool) string {
	p := passageOf(c)
	if note {
		s := p
		switch {
		case s == "":
			s = v.Abbreviation
		case prevVersion != v.Abbreviation:
			s += " (" + v.Abbreviation + ")"
		}
		return withAffixes(c, s) + "."
	}
	if p == "" {
		return withAffixes(c, "("+v.Abbreviation+")")
	}
	return "(" + withAffixes(c, v.Abbreviation+", "+p) + ")"
}

func bibleShort(c Cite, v BibleVersion) string {
	p := passageOf(c)
	if p == "" {
		return v.Abbreviation
	}
	return p + " (" + v.Abbreviation + ")"
}
