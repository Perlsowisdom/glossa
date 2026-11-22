package csl

import (
	"sort"
	"strings"
)

// ---- disambiguation ----

func (p *Processor) newDis() {
	p.dis = map[string]*disState{}
	for _, id := range p.order {
		p.dis[id] = &disState{}
	}
}

// disKey is how an item reads as a cite, for comparing with others: its first cite, no
// locator, in the current disambiguation state.
func (p *Processor) disKey(id string) string {
	r := newRenderer(p, p.style.citation, false, p.items[id])
	r.cite = &citeCtx{label: "page"}
	r.dis = *p.disFor(id)
	return strings.TrimSpace(plainOf(r.layout()))
}

// ambiguous returns the groups of different items whose cites read the same.
func (p *Processor) ambiguous() [][]string {
	by := map[string][]string{}
	var keys []string
	for _, id := range p.order {
		k := p.disKey(id)
		if k == "" {
			continue
		}
		if _, ok := by[k]; !ok {
			keys = append(keys, k)
		}
		by[k] = append(by[k], id)
	}
	var out [][]string
	for _, k := range keys {
		if len(by[k]) > 1 {
			out = append(out, by[k])
		}
	}
	return out
}

func flattenIDs(groups [][]string) []string {
	var ids []string
	for _, g := range groups {
		ids = append(ids, g...)
	}
	return ids
}

func (p *Processor) disambiguate() {
	p.newDis()
	addNames := optBool(p.cOpt("disambiguate-add-names"), false)
	addGiven := optBool(p.cOpt("disambiguate-add-givenname"), false)
	addSuffix := optBool(p.cOpt("disambiguate-add-year-suffix"), false)
	if !(addNames || addGiven || addSuffix || p.style.usesDisamb) {
		return
	}
	if addNames {
		p.phaseNames()
	}
	if addGiven {
		p.phaseGiven()
	}
	if p.style.usesDisamb {
		p.phaseCondition()
	}
	if addSuffix {
		p.phaseSuffix()
	}
}

func maxNames(it *Item) int {
	n := 0
	for _, l := range it.Names {
		if len(l) > n {
			n = len(l)
		}
	}
	return n
}

func (p *Processor) phaseNames() {
	for round := 0; round < 30; round++ {
		amb := flattenIDs(p.ambiguous())
		if len(amb) == 0 {
			return
		}
		changed := false
		for _, id := range amb {
			d := p.dis[id]
			if d.extraNames < maxNames(p.items[id]) {
				d.extraNames++
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for _, id := range flattenIDs(p.ambiguous()) {
		p.dis[id].extraNames = 0
	}
}

func (p *Processor) phaseGiven() {
	rule := p.disambRule()
	maxLevel := 2
	if strings.HasSuffix(rule, "with-initials") {
		maxLevel = 1
	}
	for level := 1; level <= maxLevel; level++ {
		amb := flattenIDs(p.ambiguous())
		if len(amb) == 0 {
			return
		}
		for _, id := range amb {
			d := p.dis[id]
			d.givenLevel = level
			d.primaryOnly = strings.HasPrefix(rule, "primary-name")
		}
	}
	for _, id := range flattenIDs(p.ambiguous()) {
		d := p.dis[id]
		d.givenLevel, d.primaryOnly = 0, false
	}
}

func (p *Processor) phaseCondition() {
	amb := flattenIDs(p.ambiguous())
	if len(amb) == 0 {
		return
	}
	for _, id := range amb {
		p.dis[id].tryCond = true
	}
	for _, id := range flattenIDs(p.ambiguous()) {
		p.dis[id].tryCond = false
	}
}

func (p *Processor) phaseSuffix() {
	groups := p.ambiguous()
	if len(groups) == 0 {
		return
	}
	pos := map[string]int{}
	for i, id := range p.bibOrder() {
		pos[id] = i
	}
	for _, g := range groups {
		ids := append([]string{}, g...)
		sort.SliceStable(ids, func(i, j int) bool { return pos[ids[i]] < pos[ids[j]] })
		for n, id := range ids {
			p.dis[id].yearSuffix = suffixLetters(n)
		}
	}
}

func suffixLetters(n int) string {
	if n < 26 {
		return string(rune('a' + n))
	}
	return string(rune('a'+n/26-1)) + string(rune('a'+n%26))
}

// ---- subsequent-author-substitute ----

type namesSnapshot struct {
	all   string
	parts []string
}

func findHolder(n *rich) *rich {
	if n == nil {
		return nil
	}
	if n.nameParts != nil {
		return n
	}
	for _, k := range n.Kids {
		if h := findHolder(k); h != nil {
			return h
		}
	}
	return nil
}

func snapshot(n *rich) namesSnapshot {
	h := findHolder(n)
	if h == nil {
		return namesSnapshot{all: n.plain()}
	}
	s := namesSnapshot{all: h.plain()}
	for _, part := range h.nameParts {
		s.parts = append(s.parts, part.plain())
	}
	return s
}

func (p *Processor) substituteAuthors(firsts []*rich) {
	sec := p.style.bibliography
	sub, ok := sec.node.Attr["subsequent-author-substitute"]
	if !ok {
		return
	}
	rule := sec.node.a("subsequent-author-substitute-rule")
	if rule == "" {
		rule = "complete-all"
	}
	var prev *namesSnapshot
	for _, cur := range firsts {
		if cur == nil {
			prev = nil
			continue
		}
		snap := snapshot(cur)
		if prev != nil {
			substitute(cur, snap, *prev, sub, rule)
		}
		prev = &snap
	}
}

func substitute(cur *rich, now, prev namesSnapshot, sub, rule string) {
	h := findHolder(cur)
	set := func(n *rich) { n.Text, n.Kids, n.nameParts = sub, nil, nil }
	if h == nil {
		if now.all == prev.all {
			set(cur)
			cur.Kids = nil
		}
		return
	}
	switch rule {
	case "complete-all":
		if now.all == prev.all {
			set(h)
		}
	case "complete-each":
		if len(now.parts) == len(prev.parts) && sameStrings(now.parts, prev.parts) {
			for _, part := range h.nameParts {
				part.Text, part.Kids = sub, nil
			}
		}
	case "partial-each":
		for i, part := range h.nameParts {
			if i >= len(prev.parts) || now.parts[i] != prev.parts[i] {
				break
			}
			part.Text, part.Kids = sub, nil
		}
	case "partial-first":
		if len(h.nameParts) > 0 && len(prev.parts) > 0 && now.parts[0] == prev.parts[0] {
			h.nameParts[0].Text, h.nameParts[0].Kids = sub, nil
		}
	}
}

func sameStrings(a, b []string) bool {
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

// ---- collapsing ----

func (p *Processor) collapse(ci int, cl *Cluster, order []int, cites []renderedCite) [][]*rich {
	for i := range cites {
		cites[i].idx = indexOf(cl, cites[i].item, order, i)
	}
	mode := p.cOpt("collapse")
	switch mode {
	case "citation-number":
		return p.collapseNumbers(cites)
	case "year", "year-suffix", "year-suffix-ranged":
		return p.collapseYears(ci, cites, mode)
	}
	units := make([][]*rich, len(cites))
	for i, c := range cites {
		units[i] = c.nodes
	}
	return units
}

func indexOf(cl *Cluster, it CiteItem, order []int, i int) int {
	_ = cl
	_ = it
	if i < len(order) {
		return order[i]
	}
	return i
}

func (p *Processor) collapseNumbers(cites []renderedCite) [][]*rich {
	var units [][]*rich
	for i := 0; i < len(cites); {
		j := i
		for j+1 < len(cites) && cites[j+1].number == cites[j].number+1 && cites[j].number > 0 &&
			!cites[j].hasAffix && !cites[j+1].hasAffix {
			j++
		}
		if j-i >= 2 {
			u := append([]*rich{}, cites[i].nodes...)
			u = append(u, leaf("–"))
			u = append(u, cites[j].nodes...)
			units = append(units, u)
			i = j + 1
			continue
		}
		units = append(units, cites[i].nodes)
		i++
	}
	return units
}

func (p *Processor) collapseYears(ci int, cites []renderedCite, mode string) [][]*rich {
	groupDelim := ", "
	if v, ok := p.style.citation.node.Attr["cite-group-delimiter"]; ok {
		groupDelim = v
	}
	suffixDelim := p.style.citation.node.a("year-suffix-delimiter")
	var units [][]*rich
	for i := 0; i < len(cites); {
		j := i
		for j+1 < len(cites) && cites[i].authKey != "" && cites[j+1].authKey == cites[i].authKey {
			j++
		}
		if j == i {
			units = append(units, cites[i].nodes)
			i++
			continue
		}
		parts := [][]*rich{cites[i].nodes}
		for k := i + 1; k <= j; k++ {
			if mode != "year" && p.sameYear(cites[k-1].id, cites[k].id) && cites[k].suffix != "" {
				parts = append(parts, []*rich{leaf(cites[k].suffix)})
				continue
			}
			rc := p.renderCite(ci, cites[k].idx, true)
			parts = append(parts, rc.nodes)
		}
		units = append(units, p.joinGroup(parts, groupDelim, suffixDelim, mode, cites[i:j+1]))
		i = j + 1
	}
	return units
}

func (p *Processor) sameYear(a, b string) bool {
	da, db := p.items[a].Dates["issued"], p.items[b].Dates["issued"]
	return da != nil && db != nil && da.Y == db.Y
}

func (p *Processor) joinGroup(parts [][]*rich, groupDelim, suffixDelim, mode string, cs []renderedCite) []*rich {
	var out []*rich
	for i, part := range parts {
		if i > 0 {
			d := groupDelim
			if len(part) == 1 && part[0].Text != "" && len(part[0].Text) <= 2 && cs[i].suffix == part[0].Text && p.sameYear(cs[i-1].id, cs[i].id) && suffixDelim != "" {
				d = suffixDelim
			}
			out = append(out, leaf(d))
		}
		out = append(out, part...)
	}
	return out
}
