package csl

import (
	"fmt"
	"strconv"
	"strings"
)

// CiteItem is one cited source inside a cluster.
type CiteItem struct {
	ID             string
	Locator, Label string // Label is a locator term: page, chapter, verse, section ...
	Prefix, Suffix string
	SuppressAuthor bool
	AuthorOnly     bool
	// Position forces a position (0 first, 1 subsequent, 2 ibid, 3 ibid-with-locator).
	Position *int
	NearNote *bool
}

// Cluster is one citation: the cites that appear together (in one footnote or one pair of
// brackets).
type Cluster struct {
	ID    string
	Items []CiteItem
	// NoteIndex is the footnote number of a note style. Zero means the next number in order;
	// clusters in one note share a number.
	NoteIndex int
}

// Citation is a rendered cluster.
type Citation struct {
	ClusterID  string
	NoteNumber int // zero for in-text styles
	Output     Output
	Positions  []string // first, subsequent, ibid, ibid-with-locator, one per cite
}

// Entry is one rendered bibliography entry.
type Entry struct {
	ID     string
	Output Output
}

// Option adjusts a Processor.
type Option func(*Processor)

// WithLang sets the language for terms and dates, overriding the style's default locale.
func WithLang(lang string) Option { return func(p *Processor) { p.lang = lang } }

// WithItemPositions makes the processor skip position tracking: every cite is "first" unless it
// says otherwise. The CSL test suite uses this for its CITATION-ITEMS fixtures.
func WithoutPositionTracking() Option { return func(p *Processor) { p.noPositions = true } }

// Processor renders citations and a bibliography for a style, locales and a set of items.
type Processor struct {
	style   *Style
	locales *Locales
	lang    string
	loc     *Locale

	items    map[string]*Item
	extra    []string // registered without citing
	clusters []*Cluster

	noPositions bool

	// computed by prepare
	order     []string // registry order: citation order, then registered
	numbers   map[string]int
	firstNote map[string]int
	notes     []int
	positions [][]cstate
	dis       map[string]*disState
	coll      *collator
}

type cstate struct {
	position int
	near     bool
}

// New makes a Processor. Items may be added later with AddItems.
func New(style *Style, locales *Locales, items []*Item, opts ...Option) (*Processor, error) {
	if style == nil || style.Dependent {
		return nil, fmt.Errorf("csl: need an independent style (resolve the parent with WithParent)")
	}
	p := &Processor{style: style, locales: locales, items: map[string]*Item{}}
	for _, o := range opts {
		o(p)
	}
	if p.lang == "" {
		p.lang = style.DefaultLocale()
	}
	p.loc = locales.resolve(p.lang, style.locales)
	p.coll = newCollator()
	p.AddItems(items...)
	return p, nil
}

// AddItems makes items available for citing.
func (p *Processor) AddItems(items ...*Item) {
	for _, it := range items {
		p.items[it.ID] = it
	}
}

// Register puts items in the bibliography (and in disambiguation) without citing them.
func (p *Processor) Register(ids ...string) {
	for _, id := range ids {
		p.extra = append(p.extra, id)
	}
}

// Cite adds a cluster at the end of the document and returns its rendering. Earlier clusters
// can change when a later one is added (year suffixes, citation numbers), so call Citations
// for the whole document.
func (p *Processor) Cite(c Cluster) (Citation, error) {
	if err := p.AddCluster(c); err != nil {
		return Citation{}, err
	}
	cs := p.Citations()
	return cs[len(cs)-1], nil
}

// AddCluster appends a cluster in document order.
func (p *Processor) AddCluster(c Cluster) error {
	for _, ci := range c.Items {
		if p.items[ci.ID] == nil {
			return fmt.Errorf("csl: cite of unknown item %q", ci.ID)
		}
	}
	cc := c
	p.clusters = append(p.clusters, &cc)
	return nil
}

// Citations renders every cluster in document order.
func (p *Processor) Citations() []Citation {
	p.prepare()
	out := make([]Citation, len(p.clusters))
	for i := range p.clusters {
		out[i] = p.renderCluster(i)
	}
	return out
}

// Bibliography renders the bibliography for every cited and registered item.
func (p *Processor) Bibliography() []Entry { return p.BibliographyFiltered(nil) }

// BibliographyFiltered is Bibliography limited to the items keep accepts.
func (p *Processor) BibliographyFiltered(keep func(*Item) bool) []Entry {
	if !p.style.HasBibliography() {
		return nil
	}
	p.prepare()
	ids := p.bibOrder()
	var ents []Entry
	var nodes []*rich
	var firsts []*rich
	for _, id := range ids {
		it := p.items[id]
		if keep != nil && !keep(it) {
			continue
		}
		r := newRenderer(p, p.style.bibliography, true, it)
		r.dis = *p.disFor(id)
		root := r.layout()
		node := &rich{Kids: root}
		nodes = append(nodes, node)
		firsts = append(firsts, r.firstNames)
		ents = append(ents, Entry{ID: id})
	}
	p.substituteAuthors(firsts)
	for i, n := range nodes {
		ents[i].Output = p.finish([]*rich{n}, true)
	}
	return ents
}

// BibliographyHTML wraps the entries as the CSL HTML bibliography block.
func (p *Processor) BibliographyHTML() string {
	ents := p.Bibliography()
	var b strings.Builder
	b.WriteString("<div class=\"csl-bib-body\">\n")
	for _, e := range ents {
		b.WriteString("  <div class=\"csl-entry\">" + e.Output.HTML + "</div>\n")
	}
	b.WriteString("</div>")
	return b.String()
}

// finish tidies quotes and punctuation and produces the three output forms.
func (p *Processor) finish(root []*rich, bib bool) Output {
	q := quotes{open: p.term("open-quote"), close: p.term("close-quote"),
		openInner: p.term("open-inner-quote"), closeInner: p.term("close-inner-quote")}
	return finalize(root, q, p.loc.option("punctuation-in-quote") == "true")
}

func (p *Processor) term(name string) string {
	s, _ := p.loc.lookup(name, "long", false)
	return s
}

// ---- preparation: order, numbers, positions, disambiguation ----

func (p *Processor) prepare() {
	p.order = nil
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] && p.items[id] != nil {
			seen[id] = true
			p.order = append(p.order, id)
		}
	}
	for _, c := range p.clusters {
		for _, ci := range c.Items {
			add(ci.ID)
		}
	}
	for _, id := range p.extra {
		add(id)
	}
	p.computeNotes()
	p.computePositions()
	p.numbers = map[string]int{}
	for i, id := range p.bibOrder() {
		p.numbers[id] = i + 1
	}
	p.disambiguate()
}

func (p *Processor) computeNotes() {
	p.notes = make([]int, len(p.clusters))
	next := 0
	for i, c := range p.clusters {
		if !p.style.IsNote() {
			p.notes[i] = 0
			continue
		}
		if c.NoteIndex > 0 {
			p.notes[i] = c.NoteIndex
			if c.NoteIndex > next {
				next = c.NoteIndex
			}
		} else {
			next++
			p.notes[i] = next
		}
	}
}

func (p *Processor) nearDistance() int {
	if v := p.style.citation.node.a("near-note-distance"); v != "" {
		return atoiDefault(v, 5)
	}
	return 5
}

func (p *Processor) computePositions() {
	p.positions = make([][]cstate, len(p.clusters))
	p.firstNote = map[string]int{}
	lastNote := map[string]int{}
	seen := map[string]bool{}
	type prevCite struct {
		id, loc, label string
		cluster, note  int
		clusterSize    int
	}
	var prev *prevCite
	for ci, c := range p.clusters {
		p.positions[ci] = make([]cstate, len(c.Items))
		for ii, it := range c.Items {
			st := cstate{}
			note := p.notes[ci]
			if !p.noPositions && seen[it.ID] {
				st.position = posSubsequent
				if prev != nil && prev.id == it.ID && p.ibidEligible(prev.cluster == ci, prev.clusterSize, prev.note, note) {
					st.position = ibidPosition(it, prev.loc, prev.label)
				}
				if d := lastNote[it.ID]; p.style.IsNote() && note-d <= p.nearDistance() {
					st.near = true
				}
			}
			if it.Position != nil {
				st.position = *it.Position
			}
			if it.NearNote != nil {
				st.near = *it.NearNote
			}
			p.positions[ci][ii] = st
			seen[it.ID] = true
			lastNote[it.ID] = note
			if _, ok := p.firstNote[it.ID]; !ok {
				p.firstNote[it.ID] = note
			}
			prev = &prevCite{id: it.ID, loc: it.Locator, label: labelDefault(it.Locator, it.Label), cluster: ci, note: note, clusterSize: len(c.Items)}
		}
	}
}

func labelDefault(loc, label string) string {
	if loc != "" && label == "" {
		return "page"
	}
	return label
}

func (p *Processor) ibidEligible(sameCluster bool, prevSize, prevNote, note int) bool {
	if sameCluster {
		return true
	}
	if prevSize != 1 {
		return false
	}
	if p.style.IsNote() && note != prevNote && note != prevNote+1 {
		return false
	}
	return true
}

func ibidPosition(it CiteItem, prevLoc, prevLabel string) int {
	label := labelDefault(it.Locator, it.Label)
	switch {
	case it.Locator == "" && prevLoc == "":
		return posIbid
	case it.Locator == "":
		return posSubsequent
	case prevLoc == "" || prevLoc != it.Locator || prevLabel != label:
		return posIbidLocator
	}
	return posIbid
}

func (p *Processor) citationNumber(id string) string {
	if n, ok := p.numbers[id]; ok {
		return strconv.Itoa(n)
	}
	return ""
}

func (p *Processor) firstNoteStr(id string) string {
	if n := p.firstNote[id]; n > 0 {
		return strconv.Itoa(n)
	}
	return ""
}

func (p *Processor) disFor(id string) *disState {
	if d := p.dis[id]; d != nil {
		return d
	}
	return &disState{}
}

// ---- rendering a cluster ----

type renderedCite struct {
	idx        int
	nodes      []*rich
	id         string
	number     int
	hasAffix   bool
	authKey    string
	yearKey    string
	suffix     string
	item       CiteItem
	suppressed []*rich // rendering without the author, for collapsing
	year       []*rich
}

func (p *Processor) citeRenderer(ci int, ii int, it CiteItem, suppressAuthor bool) *renderer {
	item := p.items[it.ID]
	r := newRenderer(p, p.style.citation, false, item)
	st := p.positions[ci][ii]
	r.cite = &citeCtx{locator: it.Locator, label: labelDefault(it.Locator, it.Label), prefix: it.Prefix, suffix: it.Suffix,
		suppressAuthor: it.SuppressAuthor || suppressAuthor, authorOnly: it.AuthorOnly,
		position: st.position, nearNote: st.near, noteIndex: p.notes[ci]}
	r.dis = *p.disFor(it.ID)
	return r
}

func (p *Processor) renderCite(ci, ii int, suppressAuthor bool) renderedCite {
	cl := p.clusters[ci]
	it := cl.Items[ii]
	r := p.citeRenderer(ci, ii, it, suppressAuthor)
	nodes := r.layout()
	rc := renderedCite{id: it.ID, item: it}
	if r.firstNames != nil {
		rc.authKey = r.firstNames.plain()
	}
	rc.suffix = r.dis.yearSuffix
	if len(nodes) > 0 && !allEmpty(nodes) {
		pre, suf := it.Prefix, it.Suffix
		var u []*rich
		if pre != "" {
			u = append(u, leaf(spaced(pre, true)))
		}
		u = append(u, nodes...)
		if suf != "" {
			u = append(u, leaf(spaced(suf, false)))
		}
		nodes = u
	}
	rc.nodes = nodes
	rc.hasAffix = it.Prefix != "" || it.Suffix != "" || it.Locator != ""
	if n, err := strconv.Atoi(p.citationNumber(it.ID)); err == nil {
		rc.number = n
	}
	return rc
}

// spaced makes a cite prefix end in a space and a suffix start with one, unless it already
// does or the punctuation does not need it.
func spaced(s string, prefix bool) string {
	if prefix {
		if strings.HasSuffix(s, " ") || strings.HasSuffix(s, " ") {
			return s
		}
		return s + " "
	}
	if s == "" {
		return s
	}
	c := s[0]
	if c == ' ' || c == ',' || c == '.' || c == ';' || c == ':' || c == '!' || c == '?' || c == ')' || c == ']' {
		return s
	}
	return " " + s
}

func (p *Processor) renderCluster(ci int) Citation {
	cl := p.clusters[ci]
	lay := p.style.citation.layout
	cites := make([]renderedCite, len(cl.Items))
	for i := range cl.Items {
		cites[i] = p.renderCite(ci, i, false)
	}
	order := p.sortCites(ci, cl)
	sorted := make([]renderedCite, 0, len(cites))
	for _, i := range order {
		sorted = append(sorted, cites[i])
	}
	sorted = dropEmpty(sorted)
	units := p.collapse(ci, cl, order, sorted)
	delim := lay.a("delimiter")
	body := join(units, delim)
	node := &rich{Kids: body}
	wrapped := p.wrapLayout(lay, node)
	pos := make([]string, len(cl.Items))
	for i, st := range p.positions[ci] {
		pos[i] = positionName(st.position)
	}
	return Citation{ClusterID: cl.ID, NoteNumber: p.notes[ci], Output: p.finish(wrapped, false), Positions: pos}
}

func positionName(n int) string {
	return [...]string{"first", "subsequent", "ibid", "ibid-with-locator"}[n]
}

func dropEmpty(cs []renderedCite) []renderedCite {
	out := cs[:0:0]
	for _, c := range cs {
		if !allEmpty(c.nodes) {
			out = append(out, c)
		}
	}
	return out
}

// wrapLayout applies the layout element's own affixes and font to the whole citation.
func (p *Processor) wrapLayout(lay *xnode, node *rich) []*rich {
	if allEmpty([]*rich{node}) {
		return nil
	}
	node.Style = fontValue(lay.a("font-style"))
	node.Weight = fontValue(lay.a("font-weight"))
	node.Variant = fontValue(lay.a("font-variant"))
	node.Deco = fontValue(lay.a("text-decoration"))
	node.VAlign = fontValue(lay.a("vertical-align"))
	var out []*rich
	if s := lay.a("prefix"); s != "" {
		out = append(out, leaf(s))
	}
	out = append(out, node)
	if s := lay.a("suffix"); s != "" {
		out = append(out, leaf(s))
	}
	return out
}

func (p *Processor) cOpt(key string) string {
	if v, ok := p.style.citation.node.Attr[key]; ok {
		return v
	}
	return p.style.opts[key]
}

func (p *Processor) disambRule() string {
	if v := p.cOpt("givenname-disambiguation-rule"); v != "" {
		return v
	}
	return "all-names-with-initials"
}

func (p *Processor) citationLabel(it *Item) string {
	names := it.Names["author"]
	if len(names) == 0 {
		names = it.Names["editor"]
	}
	if len(names) == 0 {
		names = it.Names["translator"]
	}
	var s string
	switch {
	case len(names) == 1:
		f := names[0].Family
		if len(f) > 4 {
			f = f[:4]
		}
		s = f
	case len(names) >= 2 && len(names) <= 4:
		for _, n := range names {
			if n.Family != "" {
				s += n.Family[:1]
			}
		}
	case len(names) > 4:
		for _, n := range names[:4] {
			if n.Family != "" {
				s += n.Family[:1]
			}
		}
		s += "+"
	}
	if d := it.Dates["issued"]; d != nil && d.Y != 0 {
		s += fmt.Sprintf("%02d", ((d.Y%100)+100)%100)
	}
	return s
}
