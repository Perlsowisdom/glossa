package csl

import (
	"fmt"
	"strconv"
	"strings"
)

// datePart is one date-part of a date format, with the attributes that apply to it.
type datePart struct {
	name string // year, month, day
	el   *xnode
}

func (r *renderer) date(n *xnode) []*rich {
	v := n.a("variable")
	r.called++
	d := r.item.Dates[v]
	if r.suppressed[v] || d.IsEmpty() {
		return nil
	}
	r.note(v)
	if r.sortMode {
		r.nonEmpty++
		return []*rich{leaf(sortDate(d))}
	}
	if d.Literal != "" && d.Y == 0 {
		r.nonEmpty++
		return r.wrap(n, []*rich{leaf(d.Literal)})
	}
	parts := r.dateParts(n)
	suffix := ""
	if v == "issued" && !r.p.style.usesSuffix {
		suffix = r.dis.yearSuffix
	}
	delim := n.a("delimiter")
	if delim == "" && n.has("form") {
		delim = r.loc.dates[n.a("form")].a("delimiter")
	}
	out := r.wrap(n, r.formatDate(d, parts, delim, suffix))
	if !allEmpty(out) {
		r.nonEmpty++
	}
	return out
}

// dateParts finds the parts to render, from the style or the locale's date format.
func (r *renderer) dateParts(n *xnode) []datePart {
	want := map[string]bool{"year": true, "month": true, "day": true}
	if dp := n.a("date-parts"); dp != "" {
		want = map[string]bool{}
		for _, p := range strings.Split(dp, "-") {
			want[p] = true
		}
	}
	var out []datePart
	if form := n.a("form"); form != "" {
		own := map[string]*xnode{}
		for _, p := range n.children("date-part") {
			own[p.a("name")] = p
		}
		for _, p := range r.loc.dates[form].children("date-part") {
			name := p.a("name")
			if !want[name] {
				continue
			}
			el := p
			if o := own[name]; o != nil {
				merged := &xnode{Name: "date-part", Attr: map[string]string{}}
				for k, v := range p.Attr {
					merged.Attr[k] = v
				}
				for k, v := range o.Attr {
					if k != "form" {
						merged.Attr[k] = v
					}
				}
				el = merged
			}
			out = append(out, datePart{name, el})
		}
		return out
	}
	for _, p := range n.children("date-part") {
		if want[p.a("name")] {
			out = append(out, datePart{p.a("name"), p})
		}
	}
	return out
}

func (r *renderer) partValue(p datePart, y, m, d, season int) string {
	form := p.el.a("form")
	switch p.name {
	case "year":
		if y == 0 {
			return ""
		}
		return r.yearText(y, form)
	case "month":
		if m == 0 {
			if season > 0 {
				return r.term("season-"+twoDigits(season), "long", false)
			}
			return ""
		}
		switch form {
		case "numeric":
			return strconv.Itoa(m)
		case "numeric-leading-zeros":
			return twoDigits(m)
		case "short":
			return r.term("month-"+twoDigits(m), "short", false)
		}
		return r.term("month-"+twoDigits(m), "long", false)
	case "day":
		if d == 0 {
			return ""
		}
		switch form {
		case "numeric-leading-zeros":
			return twoDigits(d)
		case "ordinal":
			if r.loc.option("limit-day-ordinals-to-day-1") == "true" && d != 1 {
				return strconv.Itoa(d)
			}
			return strconv.Itoa(d) + ordinalSuffix(r.loc, d, r.loc.genderOf("day"))
		}
		return strconv.Itoa(d)
	}
	return ""
}

func (r *renderer) yearText(y int, form string) string {
	switch {
	case y < 0:
		return strconv.Itoa(-y) + " " + r.term("bc", "long", false)
	case y < 1000 && y > 0:
		if form == "short" {
			return fmt.Sprintf("%02d", y%100)
		}
		return strconv.Itoa(y) + " " + r.term("ad", "long", false)
	case form == "short":
		return fmt.Sprintf("%02d", y%100)
	}
	return strconv.Itoa(y)
}

// renderParts renders the parts with the date values, one unit per part. dropLastSuffix removes
// the suffix of the final part (used for the start of a range).
func (r *renderer) renderParts(parts []datePart, y, m, d, season int, yearSuffix string, dropLastSuffix bool) [][]*rich {
	var out [][]*rich
	lastIdx := -1
	vals := make([]string, len(parts))
	for i, p := range parts {
		vals[i] = r.partValue(p, y, m, d, season)
		if vals[i] != "" {
			lastIdx = i
		}
	}
	for i, p := range parts {
		if vals[i] == "" {
			continue
		}
		val := vals[i]
		if p.name == "year" && yearSuffix != "" {
			val += yearSuffix
		}
		el := p.el
		if dropLastSuffix && i == lastIdx && el.a("suffix") != "" {
			c := &xnode{Name: el.Name, Attr: map[string]string{}}
			for k, v := range el.Attr {
				c.Attr[k] = v
			}
			delete(c.Attr, "suffix")
			el = c
		}
		out = append(out, r.wrap(el, []*rich{leaf(val)}))
	}
	return out
}

func (r *renderer) formatDate(d *Date, parts []datePart, delim, suffix string) []*rich {
	whole := func() []*rich {
		return join(r.renderParts(parts, d.Y, d.M, d.D, d.Season, suffix, false), delim)
	}
	if !d.HasEnd {
		return whole()
	}
	rd := "–"
	if d.EndY == 0 {
		return append(whole(), leaf(r.rangeDelim(parts, true, true, rd)))
	}
	diffY := d.Y != d.EndY
	diffM := diffY || d.M != d.EndM || d.Season != d.EndSeason
	diffD := diffM || d.D != d.EndD
	if !diffD {
		return whole()
	}
	in := func(name string) bool {
		switch name {
		case "year":
			return diffY
		case "month":
			return diffM
		}
		return diffD
	}
	first, last := -1, -1
	for i, p := range parts {
		if in(p.name) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return whole()
	}
	rd = r.rangeDelim(parts, diffY, diffM, rd)
	startSuffix := ""
	if diffY {
		startSuffix = suffix
	}
	lead := r.renderParts(parts[:first], d.Y, d.M, d.D, d.Season, suffix, false)
	start := r.renderParts(parts[first:last+1], d.Y, d.M, d.D, d.Season, startSuffix, true)
	end := r.renderParts(parts[first:last+1], d.EndY, d.EndM, d.EndD, d.EndSeason, suffix, false)
	trail := r.renderParts(parts[last+1:], d.EndY, d.EndM, d.EndD, d.EndSeason, suffix, false)
	out := join(append(lead, start...), delim)
	out = append(out, leaf(rd))
	return append(out, join(append(end, trail...), delim)...)
}

func (r *renderer) rangeDelim(parts []datePart, diffY, diffM bool, def string) string {
	name := "day"
	if diffY {
		name = "year"
	} else if diffM {
		name = "month"
	}
	for _, p := range parts {
		if p.name == name && p.el.has("range-delimiter") {
			return p.el.a("range-delimiter")
		}
	}
	return def
}

func sortDate(d *Date) string {
	enc := func(y, m, dd int) string { return fmt.Sprintf("%07d%02d%02d", y+100000, m, dd) }
	s := enc(d.Y, d.M, d.D)
	if d.HasEnd {
		s += "-" + enc(d.EndY, d.EndM, d.EndD)
	}
	return s
}
