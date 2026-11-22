package csl

import (
	"fmt"
	"io"
	"strings"
)

// Style is a parsed CSL style.
type Style struct {
	ID, Title string
	Class     string // "in-text" or "note"
	Version   string

	// Dependent styles hold only metadata and a link to the independent parent style.
	Dependent  bool
	ParentLink string

	root         *xnode
	opts         map[string]string
	macros       map[string]*xnode
	citation     *section
	bibliography *section
	locales      []*Locale
	usesSuffix   bool // the style renders year-suffix itself
	usesDisamb   bool // the style has an if disambiguate="true" branch
	condCount    int  // how many disambiguate tests it has
	usesInitWith bool // some name uses initialize-with
}

// section is the citation or the bibliography part of a style.
type section struct {
	node   *xnode
	layout *xnode
	sort   *xnode
}

// LoadStyle reads a CSL style (independent or dependent).
func LoadStyle(r io.Reader) (*Style, error) {
	root, err := parseXML(r)
	if err != nil {
		return nil, err
	}
	if root.Name != "style" {
		return nil, fmt.Errorf("csl: root element is %q, not style", root.Name)
	}
	s := &Style{root: root, opts: root.Attr, macros: map[string]*xnode{}, Class: root.a("class"),
		Version: root.a("version")}
	if info := root.child("info"); info != nil {
		s.ID = info.child("id").text()
		s.Title = info.child("title").text()
		for _, l := range info.children("link") {
			if l.a("rel") == "independent-parent" {
				s.Dependent, s.ParentLink = true, l.a("href")
			}
		}
	}
	for _, m := range root.children("macro") {
		s.macros[m.a("name")] = m
	}
	for _, l := range root.children("locale") {
		s.locales = append(s.locales, localeFromNode(l))
	}
	s.citation = sectionOf(root.child("citation"))
	s.bibliography = sectionOf(root.child("bibliography"))
	if !s.Dependent {
		if s.citation == nil || s.citation.layout == nil {
			return nil, fmt.Errorf("csl: style has no citation layout")
		}
	}
	s.scan(root)
	return s, nil
}

func sectionOf(n *xnode) *section {
	if n == nil {
		return nil
	}
	return &section{node: n, layout: n.child("layout"), sort: n.child("sort")}
}

func (s *Style) scan(n *xnode) {
	if n.Name == "text" && n.a("variable") == "year-suffix" {
		s.usesSuffix = true
	}
	if n.has("disambiguate") {
		s.usesDisamb = true
		s.condCount++
	}
	if n.has("initialize-with") {
		s.usesInitWith = true
	}
	for _, k := range n.Kids {
		s.scan(k)
	}
}

// WithParent makes a dependent style render with its parent's rules, keeping the dependent's
// own title and default locale.
func (s *Style) WithParent(parent *Style) *Style {
	if !s.Dependent || parent == nil {
		return s
	}
	c := *parent
	c.ID, c.Title = s.ID, s.Title
	c.Dependent, c.ParentLink = false, ""
	if dl := s.opts["default-locale"]; dl != "" {
		opts := map[string]string{}
		for k, v := range parent.opts {
			opts[k] = v
		}
		opts["default-locale"] = dl
		c.opts = opts
	}
	return &c
}

// DefaultLocale is the style's default-locale (en-US when unset).
func (s *Style) DefaultLocale() string {
	if dl := s.opts["default-locale"]; dl != "" {
		return dl
	}
	return "en-US"
}

// IsNote reports whether this is a note style (citations become footnotes or endnotes).
func (s *Style) IsNote() bool { return s.Class == "note" }

// HasBibliography reports whether the style defines a bibliography.
func (s *Style) HasBibliography() bool { return s.bibliography != nil && s.bibliography.layout != nil }

// optBool reads a boolean-ish style option.
func optBool(v string, def bool) bool {
	switch strings.ToLower(v) {
	case "true":
		return true
	case "false":
		return false
	}
	return def
}
