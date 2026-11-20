package logosimport

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/verse"
)

type anchorJSON struct {
	TextRange *struct {
		ResourceID string `json:"resourceId"`
		Offset     int    `json:"offset"`
		Length     int    `json:"length"`
	} `json:"textRange"`
	Reference *struct {
		Raw string `json:"raw"`
	} `json:"reference"`
}

type noteRow struct {
	id        int64
	external  string
	kind      int
	created   string
	modified  string
	content   string
	anchors   string
	tags      string
	notebook  string
	style     string
	colour    string
	role      int
	deleted   bool
	trashed   bool
	facetRefs map[int][]string // by anchor index
}

// readNotes reads notebooks, notes and highlights from notestool.db.
func (s *Snapshot) readNotes(res *Result, cat *catalog) error {
	if !s.Has("notes") {
		return nil
	}
	db, err := s.open("notes")
	if err != nil {
		return err
	}
	books, err := readNotebooks(db)
	if err != nil {
		return err
	}
	facets, err := readFacets(db)
	if err != nil {
		return err
	}
	rows, err := db.Query(`SELECT n.NoteId, n.ExternalId, n.Kind, n.CreatedDate, COALESCE(n.ModifiedDate, ''),
			COALESCE(n.ContentRichText, ''), COALESCE(n.AnchorsJson, ''), COALESCE(n.TagsJson, ''),
			n.NotebookExternalId, COALESCE(st.Name, ''), COALESCE(co.Name, ''), n.Role, n.IsDeleted, n.IsTrashed
		FROM Notes n
		LEFT JOIN NoteStyles st ON st.NoteStyleId = n.NoteStyleId
		LEFT JOIN NoteColors co ON co.NoteColorId = n.NoteColorId
		ORDER BY n.NoteId`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var n noteRow
		if err := rows.Scan(&n.id, &n.external, &n.kind, &n.created, &n.modified, &n.content, &n.anchors, &n.tags,
			&n.notebook, &n.style, &n.colour, &n.role, &n.deleted, &n.trashed); err != nil {
			return err
		}
		n.facetRefs = facets[n.id]
		mapNote(res, cat, books, n)
	}
	return rows.Err()
}

type notebook struct {
	key   string
	name  string
	role  int
	gone  bool
	known bool
}

func readNotebooks(db *sql.DB) (map[string]notebook, error) {
	rows, err := db.Query(`SELECT ExternalId, COALESCE(Title, ''), Role, IsDeleted, IsTrashed FROM Notebooks ORDER BY NotebookId`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]notebook{}
	for rows.Next() {
		var id, title string
		var role int
		var del, trash bool
		if err := rows.Scan(&id, &title, &role, &del, &trash); err != nil {
			return nil, err
		}
		name := strings.Join(strings.Fields(title), " ")
		if name == "" {
			name = "Untitled notebook"
		}
		out[id] = notebook{key: "notebook:" + id, name: name, role: role, gone: del || trash, known: true}
	}
	return out, rows.Err()
}

// readFacets reads the references Logos worked out for each note's anchors, by note.
func readFacets(db *sql.DB) (map[int64]map[int][]string, error) {
	rows, err := db.Query(`SELECT NoteId, AnchorIndex, Reference FROM NoteAnchorFacetReferences
		ORDER BY NoteId, AnchorIndex, NoteAnchorFacetReferenceId`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[int][]string{}
	for rows.Next() {
		var id int64
		var idx int
		var ref string
		if err := rows.Scan(&id, &idx, &ref); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[int][]string{}
		}
		out[id][idx] = append(out[id][idx], ref)
	}
	return out, rows.Err()
}

// anchorPlace is where one of a note's anchors points.
type anchorPlace struct {
	verses     *verse.Range // a Bible place
	resourceID string       // a book place
	partial    bool         // the anchor covered part of a verse
	problem    string       // why a Bible place could not be read
}

func placesOf(n noteRow, cat *catalog) []anchorPlace {
	var anchors []anchorJSON
	if n.anchors != "" {
		_ = json.Unmarshal([]byte(n.anchors), &anchors)
	}
	var out []anchorPlace
	for i, a := range anchors {
		switch {
		case a.Reference != nil && isBibleRef(a.Reference.Raw):
			r, err := ParseRef(a.Reference.Raw)
			if err != nil {
				out = append(out, anchorPlace{problem: err.Error()})
				continue
			}
			out = append(out, anchorPlace{verses: &r})
		case a.TextRange != nil:
			info := cat.lookup(a.TextRange.ResourceID)
			if info.Found && !info.IsBible() {
				out = append(out, anchorPlace{resourceID: a.TextRange.ResourceID})
				continue
			}
			// A Bible, or a resource the catalogue does not know: Logos worked out the verses.
			var span *verse.Range
			for _, ref := range n.facetRefs[i] {
				if !isBibleRef(ref) {
					continue
				}
				r, err := ParseRef(ref)
				if err != nil {
					continue
				}
				if span == nil {
					span = &r
				} else {
					u := union(*span, r)
					span = &u
				}
			}
			switch {
			case span != nil:
				out = append(out, anchorPlace{verses: span, partial: true})
			case info.Found:
				out = append(out, anchorPlace{problem: "the verses it is on could not be worked out"})
			default:
				out = append(out, anchorPlace{resourceID: a.TextRange.ResourceID})
			}
		}
	}
	return out
}

func mapNote(res *Result, cat *catalog, books map[string]notebook, n noteRow) {
	kind := KindNote
	if n.kind == 1 {
		kind = KindHighlight
	}
	key := string(kind) + ":" + n.external
	nb := books[n.notebook]
	text := ToMarkdown(n.content)
	label := firstWords(PlainText(n.content), 60)
	switch {
	case n.kind != 0 && n.kind != 1:
		res.skip(KindNote, key, label, "A kind of Logos entry Glossa has no place for")
		return
	case n.deleted:
		return // deleted in Logos: nothing to say
	case n.trashed || nb.gone:
		res.skip(kind, key, label, "In the Logos trash")
		return
	case n.role != 1 || (nb.known && nb.role != 1):
		res.skip(kind, key, label, "Shared with you by someone else, not your own")
		return
	}
	if n.kind == 1 {
		mapHighlight(res, cat, n, key)
		return
	}
	if strings.TrimSpace(text) == "" {
		res.skip(kind, key, "", "An empty note")
		return
	}
	places := placesOf(n, cat)
	data := &NoteData{
		Body:    text,
		Tags:    noteTags(n.tags),
		Created: millis(n.created),
		Updated: millis(changed(n.modified, n.created)),
	}
	var bibles []verse.Range
	var bookID string
	for _, p := range places {
		switch {
		case p.verses != nil:
			bibles = append(bibles, *p.verses)
		case p.resourceID != "" && bookID == "":
			bookID = p.resourceID
		}
	}
	switch {
	case len(bibles) > 0:
		data.Verses = &bibles[0]
		if len(bibles) > 1 {
			var also []string
			for _, r := range bibles[1:] {
				also = append(also, r.String())
			}
			data.Body += "\n\nAlso on: " + strings.Join(also, "; ")
		}
	case bookID != "":
		head := fmt.Sprintf("**Logos:** *%s* — [Open in Logos](%s)", cat.title(bookID), resourceLink(bookID))
		data.Body = head + "\n\n" + data.Body
		data.Tags = addTag(data.Tags, FromLogosTag)
	}
	item := Item{Kind: KindNote, Key: key, Changed: changed(n.modified, n.created), Title: label,
		Sample: firstWords(PlainText(n.content), 120), Note: data}
	if nb.known {
		item.Folder = nb.key
		res.addFolder(Folder{Key: nb.key, Name: nb.name})
	}
	res.Items = append(res.Items, item)
}

func mapHighlight(res *Result, cat *catalog, n noteRow, key string) {
	places := placesOf(n, cat)
	var span *verse.Range
	var partial bool
	bookTitle := ""
	for _, p := range places {
		switch {
		case p.verses != nil:
			if span == nil {
				span = p.verses
			} else {
				u := union(*span, *p.verses)
				span = &u
			}
			partial = partial || p.partial
		case p.resourceID != "" && bookTitle == "":
			bookTitle = cat.title(p.resourceID)
		}
	}
	switch {
	case span == nil && bookTitle != "":
		res.skip(KindHighlight, key, bookTitle, "A highlight in a book, not in a Bible")
		return
	case span == nil && len(places) > 0 && places[0].problem != "":
		res.skip(KindHighlight, key, "", "A highlight whose place could not be read: "+places[0].problem)
		return
	case span == nil:
		res.skip(KindHighlight, key, "", "A highlight with no place to put it")
		return
	}
	if partial {
		res.note("highlights that covered part of a verse now cover the whole verse")
	}
	style, exact := mapStyle(n.style, n.colour)
	if !exact {
		res.note("highlights in a style Glossa has no match for use the nearest colour")
	}
	res.Items = append(res.Items, Item{
		Kind: KindHighlight, Key: key, Changed: changed(n.modified, n.created),
		Title:  span.String(),
		Sample: style + ", from " + strings.TrimPrefix(n.style, "custom:"),
		Highlight: &HighlightData{
			Range: *span, Style: style, OriginalStyle: strings.TrimPrefix(n.style, "custom:"),
			Created: millis(n.created),
		},
	})
}

// mapStyle finds Glossa's nearest highlight style for a Logos style name (or its colour).
// exact is false when the nearest is a guess.
func mapStyle(style, colour string) (string, bool) {
	name := strings.ToLower(strings.TrimPrefix(style, "custom:"))
	if name == "" || name == "none" {
		name = strings.ToLower(colour)
	}
	underline := strings.Contains(name, "underline")
	for _, c := range []struct{ word, style string }{
		{"yellow", "yellow"}, {"green", "green"}, {"blue", "blue"}, {"pink", "pink"}, {"red", "pink"},
		{"purple", "purple"}, {"violet", "purple"}, {"magenta", "purple"}, {"orange", "yellow"},
	} {
		if strings.Contains(name, c.word) {
			exact := c.word == c.style
			if underline {
				return c.style + "-underline", exact
			}
			return c.style, exact
		}
	}
	if underline {
		return "underline", true
	}
	return "yellow", false
}

func noteTags(raw string) []string {
	if raw == "" {
		return nil
	}
	var in []struct {
		Plain struct {
			Text string `json:"text"`
		} `json:"plain"`
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(raw), &in) != nil {
		return nil
	}
	var out []string
	for _, t := range in {
		tag := t.Plain.Text
		if tag == "" {
			tag = t.Text
		}
		out = addTag(out, tag)
	}
	return out
}

// addTag adds a tag as Glossa keeps tags: lower case, single spaces, no commas, once.
func addTag(tags []string, tag string) []string {
	tag = strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(tag, ",", " ")), " "))
	if tag == "" {
		return tags
	}
	for _, t := range tags {
		if t == tag {
			return tags
		}
	}
	tags = append(tags, tag)
	sort.Strings(tags)
	return tags
}

func firstWords(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
