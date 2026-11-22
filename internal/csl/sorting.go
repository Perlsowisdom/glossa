package csl

import (
	"sort"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

type collator struct{ c *collate.Collator }

func newCollator() *collator { return &collator{c: collate.New(language.English)} }

func (c *collator) compare(a, b string) int { return c.c.CompareString(a, b) }

// sortKey is one evaluated cs:key.
type sortKey struct {
	val  string
	desc bool
}

// keysFor evaluates the sort keys of a section for an item.
func (p *Processor) keysFor(sec *section, it *Item, cite *citeCtx, bib bool) []sortKey {
	if sec == nil || sec.sort == nil {
		return nil
	}
	var keys []sortKey
	for _, k := range sec.sort.children("key") {
		keys = append(keys, sortKey{val: p.keyValue(sec, k, it, cite, bib), desc: k.a("sort") == "descending"})
	}
	return keys
}

func (p *Processor) keyValue(sec *section, k *xnode, it *Item, cite *citeCtx, bib bool) string {
	r := newRenderer(p, sec, bib, it)
	r.sortMode = true
	r.cite = cite
	r.dis = *p.disFor(it.ID)
	r.keyEl = k
	if v := k.a("macro"); v != "" {
		return strings.TrimSpace(plainOf(r.macro(v)))
	}
	v := k.a("variable")
	switch {
	case v == "citation-number":
		return padNumStr(p.citationNumber(it.ID))
	case len(it.Names[v]) > 0:
		return strings.TrimSpace(plainOf(r.sortNames(it.Names[v])))
	case it.Dates[v] != nil && !it.Dates[v].IsEmpty():
		return sortDate(it.Dates[v])
	}
	return sortNumber(v, it.Vars[v])
}

func padNumStr(s string) string {
	if n := atoiDefault(s, -1); n >= 0 {
		return padNum(n)
	}
	return s
}

func (p *Processor) lessKeys(a, b []sortKey) int {
	for i := range a {
		if i >= len(b) {
			break
		}
		x, y := a[i], b[i]
		switch {
		case x.val == "" && y.val == "":
			continue
		case x.val == "":
			return 1
		case y.val == "":
			return -1
		}
		c := p.coll.compare(x.val, y.val)
		if x.desc {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	return 0
}

// bibOrder is the bibliography order of the registry.
func (p *Processor) bibOrder() []string {
	ids := append([]string{}, p.order...)
	sec := p.style.bibliography
	if sec == nil || sec.sort == nil {
		return ids
	}
	keys := map[string][]sortKey{}
	for _, id := range ids {
		keys[id] = p.keysFor(sec, p.items[id], nil, true)
	}
	sort.SliceStable(ids, func(i, j int) bool { return p.lessKeys(keys[ids[i]], keys[ids[j]]) < 0 })
	return ids
}

// sortCites returns the order in which a cluster's cites are shown.
func (p *Processor) sortCites(ci int, cl *Cluster) []int {
	idx := make([]int, len(cl.Items))
	for i := range idx {
		idx[i] = i
	}
	sec := p.style.citation
	if sec.sort == nil {
		return idx
	}
	keys := make([][]sortKey, len(cl.Items))
	for i, it := range cl.Items {
		st := p.positions[ci][i]
		cc := &citeCtx{locator: it.Locator, label: labelDefault(it.Locator, it.Label), position: st.position, noteIndex: p.notes[ci]}
		keys[i] = p.keysFor(sec, p.items[it.ID], cc, false)
	}
	sort.SliceStable(idx, func(a, b int) bool { return p.lessKeys(keys[idx[a]], keys[idx[b]]) < 0 })
	return idx
}
