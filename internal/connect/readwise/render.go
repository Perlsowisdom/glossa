package readwise

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// How a source becomes a note. The note holds a line saying where the highlights came from, then each
// highlight as a quote with the reader's own note under it, its place in the source and a link back to
// Readwise, in the order they stand in the source.

const (
	maxTagLen   = 40 // what note.update accepts
	maxTags     = 30
	noteTag     = "readwise"
	maxTitleLen = 200
)

// Meta is what is kept about a source so its note can be rebuilt without asking Readwise again.
type Meta struct {
	Title       string   `json:"title"`
	Author      string   `json:"author,omitempty"`
	Category    string   `json:"category,omitempty"`
	ReadwiseURL string   `json:"readwise_url,omitempty"`
	SourceURL   string   `json:"source_url,omitempty"`
	DocNote     string   `json:"doc_note,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func webURL(u string) string {
	u = strings.TrimSpace(u)
	if (strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) && !strings.ContainsAny(u, " ()<>\n") {
		return u
	}
	return ""
}

func metaOf(s Source) Meta {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = strings.TrimSpace(s.ReadableName)
	}
	m := Meta{Title: title, Author: strings.TrimSpace(s.Author), Category: s.Category, ReadwiseURL: webURL(s.ReadwiseURL),
		SourceURL: webURL(s.SourceURL), DocNote: strings.TrimSpace(s.DocumentNote)}
	for _, t := range s.Tags {
		m.Tags = append(m.Tags, string(t))
	}
	return m
}

// NoteTitle is "Title — Author", or the title alone.
func (m Meta) NoteTitle() string {
	t := m.Title
	if t == "" {
		t = "Untitled source"
	}
	if m.Author != "" {
		t += " — " + m.Author
	}
	if utf8.RuneCountInString(t) > maxTitleLen {
		t = string([]rune(t)[:maxTitleLen])
	}
	return t
}

// NoteTags is "readwise" and the source's own tags, made to fit what a note takes.
func (m Meta) NoteTags() []string {
	seen := map[string]bool{noteTag: true}
	out := []string{noteTag}
	for _, t := range m.Tags {
		t = strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(t, ",", " ")), " "))
		if r := []rune(t); len(r) > maxTagLen {
			t = strings.TrimSpace(string(r[:maxTagLen]))
		}
		if t == "" || seen[t] || len(out) >= maxTags {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func quote(text string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

func place(h Highlight) string {
	loc := strings.TrimSpace(h.Location.String())
	if loc == "" || loc == "0" || loc == "null" {
		return ""
	}
	switch h.LocationTyp {
	case "page":
		return "Page " + loc
	case "location":
		return "Location " + loc
	}
	return ""
}

// blockOf is one highlight as Markdown.
func blockOf(h Highlight) string {
	parts := []string{quote(h.Text)}
	if n := strings.TrimSpace(h.Note); n != "" {
		parts = append(parts, "Note: "+n)
	}
	var foot []string
	if p := place(h); p != "" {
		foot = append(foot, p)
	}
	if u := webURL(h.ReadwiseURL); u != "" {
		foot = append(foot, "[Open in Readwise]("+u+")")
	}
	if len(foot) > 0 {
		parts = append(parts, strings.Join(foot, " · "))
	}
	return strings.Join(parts, "\n\n")
}

// HL is a kept highlight.
type HL struct {
	ID, Block, At, UpdatedAt string
	Loc                      int64
}

func hlOf(h Highlight) HL {
	loc, _ := strconv.ParseInt(strings.TrimSpace(h.Location.String()), 10, 64)
	return HL{ID: h.ID.String(), Block: blockOf(h), Loc: loc, At: h.HighlightAt, UpdatedAt: h.UpdatedAt}
}

// bodyOf builds the note's text from the source's details and its highlights.
func bodyOf(m Meta, hls []HL) string {
	sort.SliceStable(hls, func(i, j int) bool {
		a, b := hls[i], hls[j]
		if a.Loc != b.Loc {
			return a.Loc < b.Loc
		}
		if a.At != b.At {
			return a.At < b.At
		}
		return a.ID < b.ID
	})
	var head []string
	from := "From Readwise"
	switch {
	case m.ReadwiseURL != "":
		from = "[From Readwise](" + m.ReadwiseURL + ")"
	}
	head = append(head, from)
	if m.SourceURL != "" {
		head = append(head, "[Original]("+m.SourceURL+")")
	}
	out := []string{"*" + strings.Join(head, " · ") + "*"}
	if m.DocNote != "" {
		out = append(out, "Note on the whole source: "+m.DocNote)
	}
	if len(hls) == 0 {
		out = append(out, "No highlights are kept in Readwise for this source now.")
	}
	for _, h := range hls {
		out = append(out, h.Block)
	}
	return strings.Join(out, "\n\n---\n\n")
}
