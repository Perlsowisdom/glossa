package csl

import (
	"io"
	"strings"
)

// term is one entry of a locale's terms.
type term struct {
	Name, Form, Gender, GenderForm, Match string
	Single, Multiple                      string
}

// Locale is a CSL locale: terms, date formats and style options for one language. A locale can
// be a file (LoadLocale) or the <locale> element inside a style.
type Locale struct {
	Lang  string
	terms map[string][]*term
	dates map[string]*xnode // "text", "numeric"
	opts  map[string]string // style-options attributes
}

func newLocale(lang string) *Locale {
	return &Locale{Lang: lang, terms: map[string][]*term{}, dates: map[string]*xnode{}, opts: map[string]string{}}
}

// LoadLocale reads a CSL locale file.
func LoadLocale(r io.Reader) (*Locale, error) {
	root, err := parseXML(r)
	if err != nil {
		return nil, err
	}
	return localeFromNode(root), nil
}

func localeFromNode(n *xnode) *Locale {
	l := newLocale(n.a("xml:lang"))
	if so := n.child("style-options"); so != nil {
		for k, v := range so.Attr {
			l.opts[k] = v
		}
	}
	for _, d := range n.children("date") {
		l.dates[d.a("form")] = d
	}
	for _, ts := range n.children("terms") {
		for _, t := range ts.children("term") {
			l.addTerm(t)
		}
	}
	return l
}

func (l *Locale) addTerm(t *xnode) {
	tm := &term{Name: t.a("name"), Form: t.a("form"), Gender: t.a("gender"),
		GenderForm: t.a("gender-form"), Match: t.a("match")}
	if tm.Form == "" {
		tm.Form = "long"
	}
	if s := t.child("single"); s != nil || t.child("multiple") != nil {
		tm.Single = s.text()
		tm.Multiple = t.child("multiple").text()
	} else {
		tm.Single = strings.TrimSpace(t.Text)
		tm.Multiple = tm.Single
	}
	l.putTerm(tm)
}

func (n *xnode) text() string {
	if n == nil {
		return ""
	}
	return strings.TrimSpace(n.Text)
}

func (l *Locale) putTerm(tm *term) {
	list := l.terms[tm.Name]
	for i, o := range list {
		if o.Form == tm.Form && o.GenderForm == tm.GenderForm && o.Match == tm.Match && o.Gender == tm.Gender {
			list[i] = tm
			return
		}
	}
	l.terms[tm.Name] = append(list, tm)
}

// overlay copies what o defines onto l.
func (l *Locale) overlay(o *Locale) {
	for _, ts := range o.terms {
		for _, t := range ts {
			c := *t
			l.putTerm(&c)
		}
	}
	for k, v := range o.dates {
		l.dates[k] = v
	}
	for k, v := range o.opts {
		l.opts[k] = v
	}
}

func formChain(form string) []string {
	switch form {
	case "verb-short":
		return []string{"verb-short", "verb", "long"}
	case "symbol":
		return []string{"symbol", "short", "long"}
	case "short":
		return []string{"short", "long"}
	case "verb":
		return []string{"verb", "long"}
	case "", "long":
		return []string{"long"}
	}
	return []string{form, "long"}
}

// lookup finds a term in the requested form (falling back as the CSL specification says).
func (l *Locale) lookup(name, form string, plural bool) (string, bool) {
	list := l.terms[name]
	if len(list) == 0 {
		return "", false
	}
	for _, f := range formChain(form) {
		var fallback *term
		for _, t := range list {
			if t.Form != f || t.Match != "" || t.GenderForm != "" {
				continue
			}
			if t.Gender == "" {
				if plural {
					return t.Multiple, true
				}
				return t.Single, true
			}
			if fallback == nil {
				fallback = t
			}
		}
		if fallback != nil {
			if plural {
				return fallback.Multiple, true
			}
			return fallback.Single, true
		}
	}
	return "", false
}

// genderOf is the grammatical gender of a term (empty when neuter or unknown).
func (l *Locale) genderOf(name string) string {
	for _, t := range l.terms[name] {
		if t.Gender != "" {
			return t.Gender
		}
	}
	return ""
}

func (l *Locale) option(name string) string { return l.opts[name] }

// Locales is a set of locale files by language tag.
type Locales struct{ byLang map[string]*Locale }

// NewLocales builds a set from locale files.
func NewLocales(ls ...*Locale) *Locales {
	s := &Locales{byLang: map[string]*Locale{}}
	for _, l := range ls {
		s.Add(l)
	}
	return s
}

// Add puts a locale in the set, replacing one for the same language.
func (s *Locales) Add(l *Locale) { s.byLang[strings.ToLower(l.Lang)] = l }

func (s *Locales) get(lang string) *Locale {
	if s == nil {
		return nil
	}
	return s.byLang[strings.ToLower(lang)]
}

// resolve builds the locale to use for a language: en-US, then the language's locale file
// (primary dialect first), then the style's own <locale> overrides from general to specific.
func (s *Locales) resolve(lang string, inline []*Locale) *Locale {
	if lang == "" {
		lang = "en-US"
	}
	out := newLocale(lang)
	if b := s.get("en-US"); b != nil {
		out.overlay(b)
	}
	short := lang
	if i := strings.IndexByte(lang, '-'); i > 0 {
		short = lang[:i]
	}
	if d, ok := primaryDialects[short]; ok && !strings.EqualFold(d, lang) {
		if l := s.get(d); l != nil && strings.EqualFold(d, "en-US") == false {
			out.overlay(l)
		}
	}
	if l := s.get(lang); l != nil {
		out.overlay(l)
	}
	for _, l := range inline {
		if l.Lang != "" && strings.EqualFold(l.Lang, short) && !strings.EqualFold(l.Lang, lang) {
			out.overlay(l)
		}
	}
	for _, l := range inline {
		if strings.EqualFold(l.Lang, lang) {
			out.overlay(l)
		}
	}
	for _, l := range inline {
		if l.Lang == "" {
			out.overlay(l)
		}
	}
	return out
}
