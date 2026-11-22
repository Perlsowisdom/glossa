package csl

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---- text-case ----

var stopWords = map[string]bool{"a": true, "an": true, "and": true, "as": true, "at": true, "but": true,
	"by": true, "down": true, "for": true, "from": true, "in": true, "into": true, "nor": true, "of": true,
	"off": true, "on": true, "onto": true, "or": true, "out": true, "over": true, "so": true, "the": true,
	"till": true, "to": true, "up": true, "via": true, "with": true, "yet": true}

type caseState struct {
	first      bool // nothing has been written yet
	afterColon bool
}

func applyCase(n *rich, mode string, english bool) {
	st := &caseState{first: true}
	n.caseWalk(mode, english, st)
}

func (n *rich) caseWalk(mode string, english bool, st *caseState) {
	if n.NoCase {
		if !n.empty() {
			st.first = false
		}
		return
	}
	if n.Text != "" {
		n.Text = caseString(n.Text, mode, english, st)
	}
	for _, k := range n.Kids {
		k.caseWalk(mode, english, st)
	}
}

func upperFirst(s string) string {
	for i, r := range s {
		if unicode.IsLetter(r) {
			return s[:i] + string(unicode.ToUpper(r)) + s[i+utf8.RuneLen(r):]
		}
		if unicode.IsDigit(r) {
			return s
		}
	}
	return s
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isAllUpper(s string) bool {
	has := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			has = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return has
}

func caseString(s, mode string, english bool, st *caseState) string {
	switch mode {
	case "lowercase":
		return strings.ToLower(s)
	case "uppercase":
		return strings.ToUpper(s)
	case "capitalize-first":
		if st.first && hasLetter(s) {
			st.first = false
			return upperFirst(s)
		}
		return s
	case "capitalize-all":
		return mapWords(s, func(w string, _ int) string { return upperFirst(w) })
	case "title":
		if !english {
			return s
		}
		return titleCase(s, st)
	case "sentence":
		return sentenceCase(s, st)
	}
	return s
}

// mapWords applies f to each space-separated word, keeping the spaces.
func mapWords(s string, f func(w string, idx int) string) string {
	var b strings.Builder
	idx := 0
	start := -1
	flush := func(end int) {
		if start >= 0 {
			b.WriteString(f(s[start:end], idx))
			idx++
			start = -1
		}
	}
	for i, r := range s {
		if unicode.IsSpace(r) {
			flush(i)
			b.WriteRune(r)
		} else if start < 0 {
			start = i
		}
	}
	flush(len(s))
	return b.String()
}

func titleCase(s string, st *caseState) string {
	allCaps := isAllUpper(s)
	out := mapWords(s, func(w string, idx int) string {
		first := st.first && idx == 0
		after := st.afterColon && idx == 0
		defer func() { st.first = false; st.afterColon = strings.HasSuffix(w, ":") }()
		if allCaps {
			w = strings.ToLower(w)
		}
		bare := strings.ToLower(strings.Trim(w, ".,;:!?()[]\"'"))
		if stopWords[bare] && !first && !after {
			return strings.ToLower(w)
		}
		if strings.Contains(w, "-") && !first {
			parts := strings.Split(w, "-")
			for i, p := range parts {
				if stopWords[strings.ToLower(p)] && i > 0 {
					parts[i] = strings.ToLower(p)
				} else {
					parts[i] = upperFirst(p)
				}
			}
			return strings.Join(parts, "-")
		}
		return upperFirst(w)
	})
	return out
}

func sentenceCase(s string, st *caseState) string {
	if isAllUpper(s) {
		s = strings.ToLower(s)
	}
	if st.first && hasLetter(s) {
		st.first = false
		return upperFirst(s)
	}
	return s
}

func stripPeriods(s string) string { return strings.ReplaceAll(s, ".", "") }

func stripPeriodsNode(n *rich) {
	n.Text = stripPeriods(n.Text)
	for _, k := range n.Kids {
		stripPeriodsNode(k)
	}
}

// ---- numbers ----

var numTokenRE = regexp.MustCompile(`^[A-Za-z]?[0-9]+[A-Za-z]?$`)

// isNumeric implements the CSL is-numeric test: a number, or a list or range of them, each
// optionally carrying one letter before or after.
func isNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	parts := splitNumbers(s)
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !numTokenRE.MatchString(p) {
			return false
		}
	}
	return true
}

func splitNumbers(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '&' || r == '-' || r == '–' })
	var out []string
	for _, p := range f {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func romanNumeral(n int) string {
	if n <= 0 || n >= 4000 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// ordinalSuffix finds the ordinal term for n following the CSL rules for ordinal-NN terms.
func ordinalSuffix(l *Locale, n int, gender string) string {
	find := func(name string, match string) (string, bool) {
		var fallback *term
		for _, t := range l.terms[name] {
			if t.Form != "long" {
				continue
			}
			m := t.Match
			if m == "" {
				m = defaultMatch(name)
			}
			if m != match {
				continue
			}
			if t.GenderForm == gender && gender != "" {
				return t.Single, true
			}
			if t.GenderForm == "" && fallback == nil {
				fallback = t
			}
		}
		if fallback != nil {
			return fallback.Single, true
		}
		return "", false
	}
	nn := func(v int) string { return "ordinal-" + twoDigits(v) }
	if n >= 0 && n < 100 {
		if s, ok := find(nn(n), "whole-number"); ok {
			return s
		}
	}
	if s, ok := find(nn(n%100), "last-two-digits"); ok {
		return s
	}
	if s, ok := find(nn(n%10), "last-digit"); ok {
		return s
	}
	for _, t := range l.terms["ordinal"] {
		if t.Form == "long" && t.GenderForm == "" {
			return t.Single
		}
	}
	return ""
}

func defaultMatch(name string) string {
	if len(name) == len("ordinal-00") {
		if name[len("ordinal-"):len("ordinal-")+1] == "0" {
			return "last-digit"
		}
		return "last-two-digits"
	}
	return "last-digit"
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func longOrdinal(l *Locale, n int, gender string) string {
	if n >= 1 && n <= 10 {
		name := "long-ordinal-" + twoDigits(n)
		for _, t := range l.terms[name] {
			if t.Form == "long" && (t.GenderForm == gender || t.GenderForm == "") {
				return t.Single
			}
		}
	}
	return strconv.Itoa(n) + ordinalSuffix(l, n, gender)
}

// formatNumber renders a numeric value (or list/range of numbers) in the requested form.
func formatNumber(l *Locale, s, form, gender string) string {
	if form == "" || form == "numeric" || !isNumeric(s) {
		return normaliseNumbers(s)
	}
	var b strings.Builder
	rest := s
	for rest != "" {
		i := strings.IndexFunc(rest, func(r rune) bool { return r >= '0' && r <= '9' })
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		rest = rest[i:]
		j := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		n, _ := strconv.Atoi(rest[:j])
		switch form {
		case "ordinal":
			b.WriteString(rest[:j] + ordinalSuffix(l, n, gender))
		case "long-ordinal":
			b.WriteString(longOrdinal(l, n, gender))
		case "roman":
			b.WriteString(romanNumeral(n))
		default:
			b.WriteString(rest[:j])
		}
		rest = rest[j:]
	}
	return normaliseNumbers(b.String())
}

// normaliseNumbers tidies the separators in a numeric list: ranges get an en dash.
func normaliseNumbers(s string) string {
	if !isNumeric(s) || !strings.ContainsAny(s, ",&-–") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ',':
			b.WriteString(", ")
		case '&':
			b.WriteString(" & ")
		case '-':
			b.WriteString("–")
		case ' ':
		default:
			b.WriteRune(r)
		}
	}
	return strings.ReplaceAll(strings.ReplaceAll(b.String(), " ,", ","), "  ", " ")
}

// ---- page ranges ----

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// formatPages applies page-range-format to a page value: lists are split at commas and
// ampersands, ranges at hyphens.
func formatPages(s, format, delim string) string {
	if format == "" {
		return s
	}
	var b strings.Builder
	rest := s
	for rest != "" {
		i := strings.IndexAny(rest, ",&")
		piece := rest
		sep := ""
		if i >= 0 {
			piece, sep, rest = rest[:i], rest[i:i+1], rest[i+1:]
		} else {
			rest = ""
		}
		b.WriteString(formatRange(piece, format, delim))
		b.WriteString(sep)
	}
	return b.String()
}

func formatRange(p, format, delim string) string {
	i := strings.IndexAny(p, "-–")
	if i < 0 {
		return p
	}
	a, c := strings.TrimSpace(p[:i]), strings.TrimSpace(p[i+len("-"):])
	if strings.HasPrefix(p[i:], "–") {
		c = strings.TrimSpace(p[i+len("–"):])
	}
	lead := p[:len(p)-len(strings.TrimLeft(p, " "))]
	if !isDigits(a) || !isDigits(c) {
		pa, na := splitPrefix(a)
		pc, nc := splitPrefix(c)
		if pa == pc && isDigits(na) && isDigits(nc) && pa != "" {
			return lead + pa + shorten(na, nc, format, delim)
		}
		return lead + a + delim + c
	}
	return lead + shorten(a, c, format, delim)
}

func splitPrefix(s string) (string, string) {
	i := 0
	for i < len(s) && (s[i] < '0' || s[i] > '9') {
		i++
	}
	return s[:i], s[i:]
}

func shorten(a, b, format, delim string) string {
	orig := b
	if len(b) < len(a) {
		b = a[:len(a)-len(b)] + b
	}
	na, _ := strconv.Atoi(a)
	nb, _ := strconv.Atoi(b)
	if nb < na || len(a) != len(b) {
		return a + delim + orig
	}
	switch format {
	case "minimal":
		return a + delim + minimalTail(a, b, 1)
	case "minimal-two":
		return a + delim + minimalTail(a, b, 2)
	case "chicago", "chicago-15", "chicago-16":
		return a + delim + chicagoTail(a, b, na)
	}
	return a + delim + b
}

// minimalTail keeps the digits of b from the first one that differs from a, and at least min.
func minimalTail(a, b string, min int) string {
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	keep := len(b) - i
	if keep < min {
		keep = min
	}
	if keep > len(b) {
		keep = len(b)
	}
	return b[len(b)-keep:]
}

func chicagoTail(a, b string, na int) string {
	if na < 100 || na%100 == 0 {
		return b
	}
	if na%100 < 10 {
		return minimalTail(a, b, 1)
	}
	return minimalTail(a, b, 2)
}
