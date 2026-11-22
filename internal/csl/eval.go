package csl

import (
	"strconv"
	"strings"
)

// citeCtx is what a cite adds to rendering: locator, position and the like.
type citeCtx struct {
	locator, label string
	prefix, suffix string
	suppressAuthor bool
	authorOnly     bool
	position       int // 0 first, 1 subsequent, 2 ibid, 3 ibid-with-locator
	nearNote       bool
	noteIndex      int
}

const (
	posFirst = iota
	posSubsequent
	posIbid
	posIbidLocator
)

// disState is how far disambiguation has gone for one item.
type disState struct {
	extraNames int            // names added beyond et-al-use-first
	given      map[string]int // per name: 1 initials (or full when the style has no initials), 2 full given name
	yearSuffix string
	condLevel  int // how many cs:if disambiguate="true" tests are satisfied
}

// renderer evaluates a layout for one item.
type renderer struct {
	p    *Processor
	loc  *Locale
	sec  *section
	bib  bool
	item *Item
	cite *citeCtx
	dis  disState

	sortMode   bool
	suppressed map[string]bool
	track      *[]string // variables rendered, recorded for substitute
	depth      int

	called, nonEmpty int // group suppression counters
	condSeen         int
	subCfg           *namesCfg // names settings a cs:names inside cs:substitute inherits

	firstNames *rich  // first names node rendered (for subsequent-author-substitute)
	keyEl      *xnode // the sort key being evaluated
}

func newRenderer(p *Processor, sec *section, bib bool, it *Item) *renderer {
	return &renderer{p: p, loc: p.loc, sec: sec, bib: bib, item: it, suppressed: map[string]bool{}}
}

func (r *renderer) term(name, form string, plural bool) string {
	s, _ := r.loc.lookup(name, form, plural)
	return s
}

// layout renders the section's layout children.
func (r *renderer) layout() []*rich {
	return r.children(r.sec.layout)
}

func (r *renderer) children(n *xnode) []*rich {
	var out []*rich
	for _, k := range n.Kids {
		out = append(out, r.render(k)...)
	}
	return out
}

func (r *renderer) render(n *xnode) []*rich {
	switch n.Name {
	case "text":
		return r.text(n)
	case "number":
		return r.number(n)
	case "label":
		return r.label(n)
	case "date":
		return r.date(n)
	case "names":
		return r.names(n, r.subCfg)
	case "group":
		return r.group(n)
	case "choose":
		return r.choose(n)
	}
	return nil
}

// join puts delim between the non-empty units.
func join(units [][]*rich, delim string) []*rich {
	var out []*rich
	first := true
	for _, u := range units {
		if allEmpty(u) {
			continue
		}
		if !first && delim != "" {
			out = append(out, leaf(delim))
		}
		out = append(out, u...)
		first = false
	}
	return out
}

func (r *renderer) group(n *xnode) []*rich {
	c0, n0 := r.called, r.nonEmpty
	r.called, r.nonEmpty = 0, 0
	var units [][]*rich
	for _, k := range n.Kids {
		units = append(units, r.units(k)...)
	}
	called, non := r.called, r.nonEmpty
	r.called, r.nonEmpty = c0+called, n0
	if called > 0 && non == 0 {
		return nil
	}
	r.nonEmpty += non
	return r.wrap(n, join(units, n.a("delimiter")))
}

// wrap applies text-case, font, quotes, display and affixes of element n to its content.
func (r *renderer) wrap(n *xnode, content []*rich) []*rich {
	if allEmpty(content) {
		return nil
	}
	node := &rich{Kids: content}
	if tc := n.a("text-case"); tc != "" && !r.sortMode {
		applyCase(node, tc, r.english())
	}
	if n.a("strip-periods") == "true" {
		stripPeriodsNode(node)
	}
	node.Style = fontValue(n.a("font-style"))
	node.Weight = fontValue(n.a("font-weight"))
	node.Variant = fontValue(n.a("font-variant"))
	node.Deco = fontValue(n.a("text-decoration"))
	node.VAlign = fontValue(n.a("vertical-align"))
	node.Quote = n.a("quotes") == "true"
	node.Display = n.a("display")
	pre, suf := n.a("prefix"), n.a("suffix")
	if !node.styled() && !node.Quote && pre == "" && suf == "" {
		return content
	}
	var out []*rich
	if pre != "" {
		out = append(out, affixLeaf(pre))
	}
	out = append(out, node)
	if suf != "" {
		out = append(out, affixLeaf(suf))
	}
	return out
}

func fontValue(v string) string {
	switch v {
	case "", "none", "baseline":
		return ""
	case "normal":
		return "normal"
	}
	return v
}

func (r *renderer) english() bool {
	lang := r.item.Vars["language"]
	if lang == "" {
		lang = r.loc.Lang
	}
	return lang == "" || strings.HasPrefix(strings.ToLower(lang), "en")
}

// ---- text ----

func (r *renderer) text(n *xnode) []*rich {
	var content []*rich
	switch {
	case n.has("variable"):
		content = r.textVariable(n)
	case n.has("macro"):
		content = r.macro(n.a("macro"))
	case n.has("term"):
		content = r.termText(n)
	case n.has("value"):
		content = parseMarkup(n.a("value"))
	}
	return r.wrap(n, content)
}

func (r *renderer) macro(name string) []*rich {
	m := r.p.style.macros[name]
	if m == nil || r.depth > 40 {
		return nil
	}
	r.depth++
	defer func() { r.depth-- }()
	c0, n0 := r.called, r.nonEmpty
	r.called, r.nonEmpty = 0, 0
	out := r.children(m)
	called, non := r.called, r.nonEmpty
	r.called, r.nonEmpty = c0+called, n0
	if called > 0 && non == 0 {
		return nil
	}
	r.nonEmpty += non
	return out
}

func (r *renderer) termText(n *xnode) []*rich {
	form := n.a("form")
	plural := n.a("plural") == "true"
	s, ok := r.loc.lookup(n.a("term"), form, plural)
	if !ok {
		return nil
	}
	if strings.Contains(s, "<") {
		return parseMarkup(s)
	}
	l := leaf(s)
	l.Tag = "term"
	return []*rich{l}
}

func parseMarkupText(s string) []*rich {
	if s == "" {
		return nil
	}
	return []*rich{leaf(s)}
}

func (r *renderer) note(v string) {
	if r.track != nil {
		*r.track = append(*r.track, v)
	}
}

// textVariable renders cs:text variable=.
func (r *renderer) textVariable(n *xnode) []*rich {
	v := n.a("variable")
	r.called++
	if r.suppressed[v] {
		return nil
	}
	val := r.variable(v, n.a("form"))
	if val == "" {
		if d := r.item.Dates[v]; d != nil && !r.sortMode && false {
			return nil
		}
		return nil
	}
	r.nonEmpty++
	r.note(v)
	if r.sortMode {
		return []*rich{leaf(sortNumber(v, val))}
	}
	switch v {
	case "page", "locator":
		if v == "page" || r.cite == nil || r.cite.label == "" || r.cite.label == "page" {
			val = formatPages(val, r.rangeFormat(), r.term("page-range-delimiter", "long", false))
		}
		return []*rich{leaf(val)}
	case "citation-number", "year-suffix", "page-first", "first-reference-note-number", "citation-label":
		return []*rich{leaf(val)}
	}
	return parseMarkup(val)
}

func (r *renderer) rangeFormat() string {
	if v := r.sec.node.a("page-range-format"); v != "" {
		return v
	}
	return r.p.style.opts["page-range-format"]
}

// sortNumber pads numeric values so they sort as numbers.
func sortNumber(v, val string) string {
	if isNumeric(val) && !strings.ContainsAny(val, ",&-–") {
		if n, err := strconv.Atoi(strings.TrimLeft(val, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")); err == nil {
			return padNum(n)
		}
	}
	return val
}

func padNum(n int) string {
	s := strconv.Itoa(n)
	if len(s) < 12 {
		s = strings.Repeat("0", 12-len(s)) + s
	}
	return s
}

// variable returns the text of a text or number variable, including the special ones.
func (r *renderer) variable(v, form string) string {
	switch v {
	case "locator":
		if r.cite != nil {
			return r.cite.locator
		}
		return ""
	case "citation-number":
		return r.p.citationNumber(r.item.ID)
	case "year-suffix":
		return r.dis.yearSuffix
	case "first-reference-note-number":
		if r.cite != nil && r.cite.position == posFirst {
			return ""
		}
		return r.p.firstNoteStr(r.item.ID)
	case "citation-label":
		return r.p.citationLabel(r.item)
	case "page-first":
		return firstPage(r.item.Vars["page"])
	case "title", "container-title":
		if form == "short" {
			if s := r.item.Vars[v+"-short"]; s != "" {
				return s
			}
		}
	}
	return r.item.Vars[v]
}

func firstPage(s string) string {
	i := strings.IndexAny(s, "-–,&")
	if i < 0 {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[:i])
}

// ---- number and label ----

func (r *renderer) number(n *xnode) []*rich {
	v := n.a("variable")
	r.called++
	if r.suppressed[v] {
		return nil
	}
	val := r.variable(v, "")
	if val == "" {
		return nil
	}
	r.nonEmpty++
	r.note(v)
	if r.sortMode {
		return []*rich{leaf(sortNumber(v, val))}
	}
	gender := r.loc.genderOf(v)
	if v == "page" {
		val = formatPages(val, r.rangeFormat(), r.term("page-range-delimiter", "long", false))
	}
	out := formatNumber(r.loc, val, n.a("form"), gender)
	return r.wrap(n, []*rich{leaf(out)})
}

func (r *renderer) label(n *xnode) []*rich {
	v := n.a("variable")
	var val string
	name := v
	if v == "locator" {
		if r.cite == nil || r.cite.locator == "" {
			return nil
		}
		val = r.cite.locator
		name = r.cite.label
		if name == "" {
			name = "page"
		}
	} else {
		val = r.variable(v, "")
		if val == "" || r.suppressed[v] {
			return nil
		}
	}
	r.called++
	r.nonEmpty++
	if r.sortMode {
		return nil
	}
	plural := labelPlural(n.a("plural"), v, val)
	s, ok := r.loc.lookup(name, n.a("form"), plural)
	if !ok || s == "" {
		return nil
	}
	return r.wrap(n, []*rich{leaf(s)})
}

func labelPlural(mode, v, val string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	switch v {
	case "number-of-pages", "number-of-volumes":
		n, err := strconv.Atoi(strings.TrimSpace(val))
		return err == nil && n > 1
	}
	if !strings.ContainsAny(val, "-–,&") && !strings.Contains(val, " and ") && !strings.Contains(val, " et ") {
		return false
	}
	if strings.Contains(val, " and ") || strings.Contains(val, " et ") {
		return true
	}
	if a, b, ok := splitRange(val); ok && isRoman(a) && isRoman(b) {
		return true
	}
	return isNumeric(val)
}

// ---- choose ----

func (r *renderer) choose(n *xnode) []*rich {
	if b := r.pick(n); b != nil {
		return r.children(b)
	}
	return nil
}

// pick chooses the branch of a cs:choose to render.
func (r *renderer) pick(n *xnode) *xnode {
	for _, b := range n.Kids {
		switch b.Name {
		case "if", "else-if":
			if r.test(b) {
				return b
			}
		case "else":
			return b
		}
	}
	return nil
}

// units renders an element as the units a group delimits. A cs:choose is transparent: what its
// chosen branch renders counts as the group's own children.
func (r *renderer) units(n *xnode) [][]*rich {
	if n.Name != "choose" {
		return [][]*rich{r.render(n)}
	}
	b := r.pick(n)
	if b == nil {
		return nil
	}
	var out [][]*rich
	for _, k := range b.Kids {
		out = append(out, r.units(k)...)
	}
	return out
}

func (r *renderer) test(b *xnode) bool {
	var results []bool
	add := func(attr string, f func(string) bool) {
		for _, v := range fields(b.a(attr)) {
			results = append(results, f(v))
		}
	}
	add("type", func(v string) bool { return r.item.Type == v })
	add("variable", r.hasVariable)
	add("is-numeric", func(v string) bool { return isNumeric(r.variable(v, "")) })
	add("is-uncertain-date", func(v string) bool { d := r.item.Dates[v]; return d != nil && d.Circa })
	add("locator", func(v string) bool {
		return r.cite != nil && r.cite.locator != "" && labelOf(r.cite) == v
	})
	add("position", r.hasPosition)
	add("disambiguate", func(v string) bool {
		sat := r.condSeen < r.dis.condLevel
		r.condSeen++
		return (v == "true") == sat
	})
	if len(results) == 0 {
		return true
	}
	match := b.a("match")
	switch match {
	case "any":
		for _, x := range results {
			if x {
				return true
			}
		}
		return false
	case "none":
		for _, x := range results {
			if x {
				return false
			}
		}
		return true
	}
	for _, x := range results {
		if !x {
			return false
		}
	}
	return true
}

func labelOf(c *citeCtx) string {
	if c.label == "" {
		return "page"
	}
	return c.label
}

func (r *renderer) hasPosition(v string) bool {
	if r.bib || r.cite == nil {
		return false
	}
	c := r.cite
	switch v {
	case "first":
		return c.position == posFirst
	case "subsequent":
		return c.position >= posSubsequent
	case "ibid":
		return c.position >= posIbid
	case "ibid-with-locator":
		return c.position == posIbidLocator
	case "near-note":
		return c.nearNote
	}
	return false
}

func (r *renderer) hasVariable(v string) bool {
	switch v {
	case "locator":
		return r.cite != nil && r.cite.locator != ""
	case "citation-number", "year-suffix", "first-reference-note-number", "page-first", "citation-label":
		return r.variable(v, "") != ""
	}
	if ns, ok := r.item.Names[v]; ok {
		return len(ns) > 0
	}
	if d, ok := r.item.Dates[v]; ok {
		return !d.IsEmpty()
	}
	return r.item.Vars[v] != ""
}
