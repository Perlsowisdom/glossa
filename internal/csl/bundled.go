package csl

import (
	"bytes"
	"embed"
	"fmt"
	"sort"
	"strings"
)

// The bundled styles and locales are data under styles/, licensed CC BY-SA 3.0 (see
// styles/README.md). They are read like any other style a user adds.
//
//go:embed styles/*.csl styles/*.xml
var bundledFS embed.FS

// StyleInfo describes a style without making a Processor.
type StyleInfo struct {
	ID      string // short id: the file name without .csl
	Title   string
	Class   string // note or in-text
	Bundled bool
}

// BundledStyles lists the styles that ship with Glossa.
func BundledStyles() []StyleInfo {
	ents, err := bundledFS.ReadDir("styles")
	if err != nil {
		return nil
	}
	var out []StyleInfo
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".csl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".csl")
		if st, err := BundledStyle(id); err == nil {
			out = append(out, StyleInfo{ID: id, Title: st.Title, Class: st.Class, Bundled: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

// BundledStyle loads a bundled style by its short id.
func BundledStyle(id string) (*Style, error) {
	b, err := bundledFS.ReadFile("styles/" + id + ".csl")
	if err != nil {
		return nil, fmt.Errorf("csl: no bundled style %q", id)
	}
	return LoadStyle(bytes.NewReader(b))
}

// BundledLocales returns the locales that ship with Glossa (en-US and en-GB).
func BundledLocales() *Locales {
	set := NewLocales()
	for _, name := range []string{"locales-en-US.xml", "locales-en-GB.xml"} {
		b, err := bundledFS.ReadFile("styles/" + name)
		if err != nil {
			continue
		}
		if l, err := LoadLocale(bytes.NewReader(b)); err == nil {
			set.Add(l)
		}
	}
	return set
}
