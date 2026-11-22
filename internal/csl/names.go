package csl

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// namesCfg is the cs:name, cs:et-al and cs:label a cs:names element uses; a cs:names inside
// cs:substitute inherits them from the cs:names it stands in for.
type namesCfg struct {
	name, etal, label *xnode
	labelFirst        bool
	namesEl           *xnode
}

func (r *renderer) nameAttr(c *namesCfg, key string) string {
	if c != nil && c.name != nil && c.name.has(key) {
		return c.name.a(key)
	}
	alias := key
	switch key {
	case "delimiter":
		alias = "name-delimiter"
	case "form":
		alias = "name-form"
	}
	if v, ok := r.sec.node.Attr[alias]; ok {
		return v
	}
	if v, ok := r.p.style.opts[alias]; ok {
		return v
	}
	return ""
}

func (r *renderer) etalAttr(c *namesCfg, key string) string {
	if c != nil && c.name != nil && c.name.has(key) {
		return c.name.a(key)
	}
	if v, ok := r.sec.node.Attr[key]; ok {
		return v
	}
	return r.p.style.opts[key]
}

func (r *renderer) names(n *xnode, inh *namesCfg) []*rich {
	cfg := &namesCfg{namesEl: n}
	if inh != nil {
		*cfg = *inh
		cfg.namesEl = n
	}
	if k := n.child("name"); k != nil {
		cfg.name = k
	}
	if k := n.child("et-al"); k != nil {
		cfg.etal = k
	}
	if k := n.child("label"); k != nil {
		cfg.label = k
		cfg.labelFirst = labelBeforeName(n)
	}
	vars := fields(n.a("variable"))
	r.called++
	if r.suppressAuthorNow(vars) {
		return nil
	}
	if sub := n.child("substitute"); sub != nil && len(vars) == 0 {
		return nil
	}
	units := r.nameVariables(vars, cfg)
	if allEmpty(flatten(units)) {
		if sub := n.child("substitute"); sub != nil {
			return r.substitute(n, sub, cfg)
		}
		return nil
	}
	r.nonEmpty++
	for _, v := range vars {
		r.note(v)
	}
	return r.finishNames(n, units)
}

func flatten(units [][]*rich) []*rich {
	var out []*rich
	for _, u := range units {
		out = append(out, u...)
	}
	return out
}

func (r *renderer) finishNames(n *xnode, units [][]*rich) []*rich {
	delim := n.a("delimiter")
	if !n.has("delimiter") {
		if v, ok := r.sec.node.Attr["names-delimiter"]; ok {
			delim = v
		} else if v, ok := r.p.style.opts["names-delimiter"]; ok {
			delim = v
		}
	}
	body := join(units, delim)
	out := r.wrap(n, body)
	if len(out) > 0 {
		wrapper := &rich{Kids: out, Tag: "names"}
		if r.firstNames == nil {
			r.firstNames = wrapper
		}
		return []*rich{wrapper}
	}
	return out
}

func labelBeforeName(n *xnode) bool {
	for _, k := range n.Kids {
		if k.Name == "name" {
			return false
		}
		if k.Name == "label" {
			return true
		}
	}
	return false
}

func (r *renderer) suppressAuthorNow(vars []string) bool {
	if r.cite == nil || !r.cite.suppressAuthor {
		return false
	}
	for _, v := range vars {
		if v == "author" {
			return true
		}
	}
	return false
}

func (r *renderer) substitute(n, sub *xnode, cfg *namesCfg) []*rich {
	savedCfg := r.subCfg
	r.subCfg = cfg
	defer func() { r.subCfg = savedCfg }()
	for _, k := range sub.Kids {
		var rendered []string
		saved := r.track
		r.track = &rendered
		var out []*rich
		if k.Name == "names" {
			out = r.names(k, cfg)
		} else {
			out = r.render(k)
		}
		r.track = saved
		if allEmpty(out) {
			continue
		}
		for _, v := range rendered {
			r.suppressed[v] = true
			if saved != nil {
				*saved = append(*saved, v)
			}
		}
		if k.Name != "names" {
			return r.wrapSubstituted(n, out)
		}
		return r.wrapSubstituted(n, out)
	}
	return nil
}

func (r *renderer) wrapSubstituted(n *xnode, out []*rich) []*rich {
	r.nonEmpty++
	wrapper := &rich{Kids: out, Tag: "names"}
	if r.firstNames == nil {
		r.firstNames = wrapper
	}
	return []*rich{wrapper}
}

// nameVariables renders each variable's names (with its label) as one unit.
func (r *renderer) nameVariables(vars []string, cfg *namesCfg) [][]*rich {
	var units [][]*rich
	var live []string
	for _, v := range vars {
		if v == "editor-translator" {
			units = append(units, r.editorTranslator(cfg))
			continue
		}
		if r.suppressed[v] {
			continue
		}
		if len(r.item.Names[v]) > 0 {
			live = append(live, v)
		}
	}
	if len(live) == 2 && live[0] == "editor" && live[1] == "translator" && sameNames(r.item.Names["editor"], r.item.Names["translator"]) {
		units = append(units, r.nameList("editor", "editortranslator", cfg))
		return units
	}
	for _, v := range live {
		units = append(units, r.nameList(v, v, cfg))
	}
	return units
}

// editorTranslator handles the editor-translator pseudo-variable some styles use: it renders the
// names once, with the combined label, when the editors and translators are the same people, and
// leaves the separate variables to render themselves otherwise.
func (r *renderer) editorTranslator(cfg *namesCfg) []*rich {
	ed, tr := r.item.Names["editor"], r.item.Names["translator"]
	if len(ed) == 0 || !sameNames(ed, tr) || r.suppressed["editor"] || r.suppressed["translator"] {
		return nil
	}
	out := r.nameList("editor", "editor-translator", cfg)
	if !allEmpty(out) {
		r.suppressed["editor"], r.suppressed["translator"] = true, true
	}
	return out
}

func sameNames(a, b []Name) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nameList renders one variable's list: the names, then or before them the label.
func (r *renderer) nameList(v, labelTerm string, cfg *namesCfg) []*rich {
	list := r.item.Names[v]
	var body []*rich
	if r.sortMode {
		body = r.sortNames(list)
	} else {
		body = r.renderNames(list, cfg)
	}
	if cfg.name != nil && cfg.name.a("form") == "count" || r.nameAttr(cfg, "form") == "count" {
		return body
	}
	lab := r.nameLabel(labelTerm, len(list), cfg)
	if len(lab) == 0 {
		return body
	}
	if cfg.labelFirst {
		return append(lab, body...)
	}
	return append(body, lab...)
}

func (r *renderer) nameLabel(term string, count int, cfg *namesCfg) []*rich {
	if cfg.label == nil || r.sortMode {
		return nil
	}
	l := cfg.label
	plural := false
	switch l.a("plural") {
	case "always":
		plural = true
	case "never":
	default:
		plural = count > 1
	}
	s, ok := r.loc.lookup(term, l.a("form"), plural)
	if !ok || s == "" {
		return nil
	}
	return r.wrap(l, []*rich{leaf(s)})
}

// ---- et al. and the list ----

type nameOpts struct {
	form        string
	and         string
	delim       string
	precLast    string
	precEtAl    string
	initWith    string
	hasInitWith bool
	initialize  bool
	hyphen      bool
	sortOrder   string
	sortSep     string
	demote      bool
	givenEl     *xnode
	familyEl    *xnode
}

func (r *renderer) options(cfg *namesCfg) nameOpts {
	o := nameOpts{form: r.nameAttr(cfg, "form"), and: r.nameAttr(cfg, "and"), delim: ", ",
		precLast: r.nameAttr(cfg, "delimiter-precedes-last"), precEtAl: r.nameAttr(cfg, "delimiter-precedes-et-al"),
		sortOrder: r.nameAttr(cfg, "name-as-sort-order"), sortSep: ", ", initialize: true, hyphen: true}
	if o.form == "" {
		o.form = "long"
	}
	if cfg != nil && cfg.name != nil && cfg.name.has("delimiter") {
		o.delim = cfg.name.a("delimiter")
	} else if v, ok := r.sec.node.Attr["name-delimiter"]; ok {
		o.delim = v
	} else if v, ok := r.p.style.opts["name-delimiter"]; ok {
		o.delim = v
	}
	if v := r.nameAttr(cfg, "sort-separator"); v != "" || r.hasAttr(cfg, "sort-separator") {
		o.sortSep = v
	}
	if r.hasAttr(cfg, "initialize-with") {
		o.initWith, o.hasInitWith = r.nameAttr(cfg, "initialize-with"), true
	}
	if v := r.nameAttr(cfg, "initialize"); v == "false" {
		o.initialize = false
	}
	if v := r.p.style.opts["initialize-with-hyphen"]; v == "false" {
		o.hyphen = false
	}
	dem := r.p.style.opts["demote-non-dropping-particle"]
	o.demote = dem == "" || dem == "display-and-sort"
	if cfg != nil && cfg.name != nil {
		for _, k := range cfg.name.Kids {
			if k.Name == "name-part" {
				switch k.a("name") {
				case "given":
					o.givenEl = k
				case "family":
					o.familyEl = k
				}
			}
		}
	}
	return o
}

func (r *renderer) hasAttr(cfg *namesCfg, key string) bool {
	if cfg != nil && cfg.name != nil && cfg.name.has(key) {
		return true
	}
	if r.sec.node.has(key) {
		return true
	}
	return r.p.style.root.has(key)
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

// etAlLimits decides how many names to show; shown == len(list) means no et al.
func (r *renderer) etAlLimits(cfg *namesCfg, n int) (shown int, useLast bool) {
	minK, firstK := "et-al-min", "et-al-use-first"
	if r.cite != nil && r.cite.position >= posSubsequent {
		if r.hasAttr(cfg, "et-al-subsequent-min") {
			minK = "et-al-subsequent-min"
		}
		if r.hasAttr(cfg, "et-al-subsequent-use-first") {
			firstK = "et-al-subsequent-use-first"
		}
	}
	min := atoiDefault(r.etalAttr(cfg, minK), 0)
	if min == 0 || n < min {
		return n, false
	}
	use := atoiDefault(r.etalAttr(cfg, firstK), 1)
	if use < 1 {
		use = 1
	}
	use += r.dis.extraNames
	if use >= n {
		return n, false
	}
	useLast = r.etalAttr(cfg, "et-al-use-last") == "true" && n-use >= 2
	if useLast && use >= n-1 {
		useLast = false
	}
	return use, useLast
}

func (r *renderer) renderNames(list []Name, cfg *namesCfg) []*rich {
	o := r.options(cfg)
	n := len(list)
	shown, useLast := r.etAlLimits(cfg, n)
	if o.form == "count" {
		cnt := shown
		if useLast {
			cnt++
		}
		if cnt == 0 {
			return nil
		}
		return []*rich{leaf(strconv.Itoa(cnt))}
	}
	var items []*rich
	inverted := make([]bool, 0, n)
	idx := list
	if useLast {
		idx = append(append([]Name{}, list[:shown]...), list[n-1])
	} else {
		idx = list[:shown]
	}
	for i, nm := range idx {
		inv := o.sortOrder == "all" || (o.sortOrder == "first" && i == 0)
		it := r.renderName(nm, o, i, inv)
		if it.empty() {
			continue
		}
		it = r.nameFont(cfg, it)
		items = append(items, it)
		inverted = append(inverted, inv && !nm.isLiteral())
	}
	if len(items) == 0 {
		return nil
	}
	truncated := shown < n
	var out []*rich
	count := len(items)
	for i, it := range items {
		if i > 0 {
			out = append(out, leaf(r.separator(o, i, count, inverted, truncated, useLast)))
			if useLast && i == count-1 {
				out = append(out, leaf("… "))
			}
		}
		out = append(out, it)
	}
	if truncated && !useLast {
		out = append(out, r.etAl(cfg, o, count, inverted)...)
	}
	holder := &rich{Kids: out}
	holder.nameParts = items
	res := []*rich{holder}
	if cfg != nil && cfg.name != nil {
		if p := cfg.name.a("prefix"); p != "" {
			res = append([]*rich{leaf(p)}, res...)
		}
		if p := cfg.name.a("suffix"); p != "" {
			res = append(res, leaf(p))
		}
	}
	return res
}

func (nm Name) isLiteral() bool { return nm.Literal != "" }

// nameFont applies the font attributes of cs:name to one rendered name.
func (r *renderer) nameFont(cfg *namesCfg, it *rich) *rich {
	if cfg == nil || cfg.name == nil {
		return it
	}
	el := cfg.name
	if !el.has("font-style") && !el.has("font-weight") && !el.has("font-variant") && !el.has("text-decoration") && !el.has("vertical-align") {
		return it
	}
	w := &rich{Kids: []*rich{it}, Style: fontValue(el.a("font-style")), Weight: fontValue(el.a("font-weight")),
		Variant: fontValue(el.a("font-variant")), Deco: fontValue(el.a("text-decoration")), VAlign: fontValue(el.a("vertical-align"))}
	return w
}

// separator is the text before the i-th name (i >= 1) of count rendered names.
func (r *renderer) separator(o nameOpts, i, count int, inverted []bool, truncated, useLast bool) string {
	last := i == count-1
	if useLast && last {
		return o.delim
	}
	if o.and != "" && last && !truncated {
		andTerm := r.andText(o)
		precede := false
		switch o.precLast {
		case "always":
			precede = true
		case "never":
		case "after-inverted-name":
			precede = inverted[i-1]
		default:
			precede = count > 2
		}
		if precede {
			return o.delim + andTerm + " "
		}
		return " " + andTerm + " "
	}
	return o.delim
}

func (r *renderer) andText(o nameOpts) string {
	if o.and == "symbol" {
		return "&"
	}
	return r.term("and", "long", false)
}

func (r *renderer) etAl(cfg *namesCfg, o nameOpts, count int, inverted []bool) []*rich {
	term := "et-al"
	if cfg != nil && cfg.etal != nil && cfg.etal.a("term") != "" {
		term = cfg.etal.a("term")
	}
	s := r.term(term, "long", false)
	if s == "" {
		return nil
	}
	precede := false
	switch o.precEtAl {
	case "always":
		precede = true
	case "never":
	case "after-inverted-name":
		precede = len(inverted) > 0 && inverted[len(inverted)-1]
	default:
		precede = count >= 2
	}
	sep := " "
	if precede {
		sep = o.delim
	}
	var lab []*rich
	if cfg != nil && cfg.etal != nil {
		lab = r.wrap(cfg.etal, []*rich{leaf(s)})
	} else {
		lab = []*rich{leaf(s)}
	}
	return append([]*rich{leaf(sep)}, lab...)
}

// ---- one name ----

func (r *renderer) renderName(nm Name, o nameOpts, idx int, inverted bool) *rich {
	if nm.Literal != "" {
		return leaf(nm.Literal)
	}
	form := o.form
	initWith, hasInit, initialize := o.initWith, o.hasInitWith, o.initialize
	if g := r.dis.given[nameKey(nm)]; g > 0 {
		if form == "short" {
			form = "long"
		}
		if g == 2 || !hasInit {
			initialize, hasInit = false, false
		}
	}
	given := nm.Given
	if given != "" && hasInit && nm.Family != "" {
		given = initials(given, initWith, initialize, o.hyphen)
	}
	family := nm.Family
	if form == "short" {
		return r.familyOnly(nm, o)
	}
	return r.assemble(nm, given, family, o, inverted)
}

func (r *renderer) familyOnly(nm Name, o nameOpts) *rich {
	f := leaf(nm.Family)
	if nm.NonDropping != "" {
		f = leaf(joinParticle(nm.NonDropping, nm.Family))
	}
	return r.partWrap(o.familyEl, f)
}

func joinParticle(p, rest string) string {
	if p == "" {
		return rest
	}
	if rest == "" {
		return p
	}
	if strings.HasSuffix(p, "'") || strings.HasSuffix(p, "’") || strings.HasSuffix(p, "-") {
		return p + rest
	}
	return p + " " + rest
}

func (r *renderer) partWrap(el *xnode, n *rich) *rich {
	if el == nil || n.empty() {
		return n
	}
	out := r.wrap(el, []*rich{n})
	if len(out) == 1 {
		return out[0]
	}
	return &rich{Kids: out}
}

func (r *renderer) assemble(nm Name, given, family string, o nameOpts, inverted bool) *rich {
	sufSep := " "
	if nm.CommaSuffix {
		sufSep = ", "
	}
	fam := family
	if nm.NonDropping != "" && !(inverted && o.demote) {
		fam = joinParticle(nm.NonDropping, family)
	}
	famNode := r.partWrap(o.familyEl, leaf(fam))
	if inverted {
		return r.assembleInverted(nm, given, famNode, o)
	}
	givenFull := given
	if nm.Dropping() != "" {
		givenFull = strings.TrimSpace(given + " " + nm.DroppingParticle)
	}
	var kids []*rich
	givenNode := r.partWrap(o.givenEl, leaf(strings.TrimRight(givenFull, " ")))
	if !givenNode.empty() {
		kids = append(kids, givenNode)
		if !famNode.empty() {
			kids = append(kids, leaf(spaceAfter(givenFull)))
		}
	}
	kids = append(kids, famNode)
	if nm.Suffix != "" {
		kids = append(kids, leaf(sufSep+nm.Suffix))
	}
	return &rich{Kids: kids}
}

func spaceAfter(s string) string {
	if strings.HasSuffix(s, " ") {
		return ""
	}
	return " "
}

// Dropping reports the dropping particle.
func (nm Name) Dropping() string { return nm.DroppingParticle }

func (r *renderer) assembleInverted(nm Name, given string, fam *rich, o nameOpts) *rich {
	var kids []*rich
	kids = append(kids, fam)
	tail := given
	if nm.DroppingParticle != "" {
		tail = strings.TrimSpace(tail + " " + nm.DroppingParticle)
	}
	if o.demote && nm.NonDropping != "" {
		tail = strings.TrimSpace(tail + " " + nm.NonDropping)
	}
	tail = strings.TrimRight(tail, " ")
	if tail != "" {
		kids = append(kids, leaf(o.sortSep), r.partWrap(o.givenEl, leaf(tail)))
	}
	if nm.Suffix != "" {
		kids = append(kids, leaf(o.sortSep+nm.Suffix))
	}
	return &rich{Kids: kids}
}

type namePiece struct {
	text    string
	initial bool
	hyphen  bool
}

// initials reduces given names to initials followed by with ("." gives J.P., ". " gives J. P.).
// With initialize false, only parts that already are initials are normalised.
func initials(given, with string, initialize, hyphen bool) string {
	var ps []namePiece
	var cur strings.Builder
	prevHyphen := false
	flush := func(hadPeriod bool) {
		if cur.Len() == 0 {
			return
		}
		w := cur.String()
		cur.Reset()
		ps = append(ps, makePiece(w, initialize, hadPeriod, prevHyphen))
		prevHyphen = false
	}
	for _, c := range given {
		switch c {
		case ' ', ' ':
			flush(false)
		case '-':
			flush(false)
			prevHyphen = true
		case '.':
			flush(true)
		default:
			cur.WriteRune(c)
		}
	}
	flush(false)
	var b strings.Builder
	for i, p := range ps {
		if i > 0 {
			prev := ps[i-1]
			switch {
			case p.hyphen && prev.initial && p.initial && hyphen:
				trimmed := strings.TrimRight(b.String(), " ")
				b.Reset()
				b.WriteString(trimmed + "-")
			case p.hyphen && prev.initial && p.initial:
				if !strings.HasSuffix(b.String(), " ") && strings.HasSuffix(with, " ") {
					b.WriteString(" ")
				}
			case strings.HasSuffix(b.String(), " "):
			case prev.initial && p.initial && !strings.HasSuffix(with, " "):
			default:
				b.WriteString(" ")
			}
		}
		if p.initial {
			b.WriteString(p.text + with)
		} else {
			b.WriteString(p.text)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func makePiece(w string, initialize, hadPeriod, hyphen bool) namePiece {
	r, _ := utf8.DecodeRuneInString(w)
	switch {
	case hadPeriod:
		return namePiece{w, true, hyphen}
	case utf8.RuneCountInString(w) == 1 && !unicode.IsLower(r):
		return namePiece{w, true, hyphen}
	case initialize && unicode.IsLetter(r) && !unicode.IsLower(r):
		return namePiece{string(r), true, hyphen}
	case initialize && hyphen && unicode.IsLower(r):
		return namePiece{"", false, true} // a lowercase part after a hyphen adds nothing
	}
	return namePiece{w, false, hyphen}
}

// ---- names in sort keys ----

// sortNames renders names for use as a sort key: "family given" for each name, with et-al rules
// left to the sort key's own names-min settings.
func (r *renderer) sortNames(list []Name) []*rich {
	if k := r.keyEl; k != nil {
		if min := atoiDefault(k.a("names-min"), 0); min > 0 && len(list) >= min {
			list = list[:atoiDefault(k.a("names-use-first"), 1)]
		}
	}
	var parts []string
	for _, nm := range list {
		if nm.Literal != "" {
			parts = append(parts, nm.Literal)
			continue
		}
		fam := joinParticle(nm.NonDropping, nm.Family)
		if r.p.style.opts["demote-non-dropping-particle"] != "never" && nm.NonDropping != "" {
			fam = nm.Family
		}
		s := fam + " " + nm.Given
		if nm.DroppingParticle != "" {
			s += " " + nm.DroppingParticle
		}
		if r.p.style.opts["demote-non-dropping-particle"] != "never" && nm.NonDropping != "" {
			s += " " + nm.NonDropping
		}
		if nm.Suffix != "" {
			s += " " + nm.Suffix
		}
		parts = append(parts, strings.TrimSpace(s))
	}
	return []*rich{leaf(strings.Join(parts, "   "))}
}

// nameKey identifies a person's name for disambiguation.
func nameKey(nm Name) string { return nm.Family + "|" + nm.Given + "|" + nm.NonDropping }
