// Package csljson is Glossa's own model of a CSL-JSON item: one source (book, article, chapter...)
// as the citation engine reads it. It is written from the public CSL-JSON schema
// (https://citeproc-js.readthedocs.io/en/latest/csl-json/markup.html). Connections such as Zotero
// map their items to it, and internal/csl consumes it.
package csljson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Item is one source. Field names and types follow CSL-JSON; unknown fields are dropped.
type Item struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	Title               string `json:"title,omitempty"`
	TitleShort          string `json:"title-short,omitempty"`
	ContainerTitle      string `json:"container-title,omitempty"`
	ContainerTitleShort string `json:"container-title-short,omitempty"`
	CollectionTitle     string `json:"collection-title,omitempty"`
	Publisher           string `json:"publisher,omitempty"`
	PublisherPlace      string `json:"publisher-place,omitempty"`
	Edition             Text   `json:"edition,omitempty"`
	Volume              Text   `json:"volume,omitempty"`
	Issue               Text   `json:"issue,omitempty"`
	Number              Text   `json:"number,omitempty"`
	CollectionNumber    Text   `json:"collection-number,omitempty"`
	NumberOfVolumes     Text   `json:"number-of-volumes,omitempty"`
	Page                Text   `json:"page,omitempty"`
	Section             Text   `json:"section,omitempty"`
	DOI                 string `json:"DOI,omitempty"`
	ISBN                string `json:"ISBN,omitempty"`
	ISSN                string `json:"ISSN,omitempty"`
	URL                 string `json:"URL,omitempty"`
	Abstract            string `json:"abstract,omitempty"`
	Note                string `json:"note,omitempty"`
	Language            string `json:"language,omitempty"`
	Genre               string `json:"genre,omitempty"`
	Medium              string `json:"medium,omitempty"`
	Source              string `json:"source,omitempty"`
	Archive             string `json:"archive,omitempty"`
	ArchiveLocation     string `json:"archive_location,omitempty"`
	Event               string `json:"event-title,omitempty"`
	Authority           string `json:"authority,omitempty"`

	Author           []Name `json:"author,omitempty"`
	Editor           []Name `json:"editor,omitempty"`
	Translator       []Name `json:"translator,omitempty"`
	CollectionEditor []Name `json:"collection-editor,omitempty"`
	ContainerAuthor  []Name `json:"container-author,omitempty"`
	Director         []Name `json:"director,omitempty"`
	Interviewer      []Name `json:"interviewer,omitempty"`

	Issued    *Date `json:"issued,omitempty"`
	Accessed  *Date `json:"accessed,omitempty"`
	Submitted *Date `json:"submitted,omitempty"`
	EventDate *Date `json:"event-date,omitempty"`
	Original  *Date `json:"original-date,omitempty"`
}

// Name is a person (Family and Given) or an organisation (Literal).
type Name struct {
	Family              string `json:"family,omitempty"`
	Given               string `json:"given,omitempty"`
	DroppingParticle    string `json:"dropping-particle,omitempty"`
	NonDroppingParticle string `json:"non-dropping-particle,omitempty"`
	Suffix              string `json:"suffix,omitempty"`
	CommaSuffix         bool   `json:"comma-suffix,omitempty"`
	Literal             string `json:"literal,omitempty"`
}

// Display is the name as people write it: "Family, Given", or the organisation's name.
func (n Name) Display() string {
	switch {
	case n.Literal != "":
		return n.Literal
	case n.Family != "" && n.Given != "":
		return n.Family + ", " + n.Given
	}
	return n.Family + n.Given
}

// Date is a date as parts (year, then optional month and day), or free text when it has none.
// Parts are zero when unknown. A range has an End.
type Date struct {
	Year, Month, Day int
	End              *Date
	Literal          string // free text, or the raw text that could not be split
	Circa            bool
}

// IsZero is true when the date says nothing.
func (d *Date) IsZero() bool { return d == nil || (d.Year == 0 && d.Literal == "") }

type dateJSON struct {
	Parts   [][]flexInt `json:"date-parts,omitempty"`
	Literal string      `json:"literal,omitempty"`
	Raw     string      `json:"raw,omitempty"`
	Circa   flexBool    `json:"circa,omitempty"`
}

// flexInt reads 2020, "2020" and null alike.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(b)), `"`)
	n, err := strconv.Atoi(s)
	if err != nil {
		*f = 0 // null, or a word such as "Spring": not a part we can use
		return nil
	}
	*f = flexInt(n)
	return nil
}

type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(b)), `"`)
	*f = flexBool(s == "true" || s == "1")
	return nil
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var raw dateJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*d = Date{Literal: raw.Literal, Circa: bool(raw.Circa)}
	if d.Literal == "" {
		d.Literal = raw.Raw
	}
	if len(raw.Parts) > 0 {
		d.Year, d.Month, d.Day = parts(raw.Parts[0])
		if len(raw.Parts) > 1 {
			y, m, dd := parts(raw.Parts[1])
			d.End = &Date{Year: y, Month: m, Day: dd}
		}
		if d.Year != 0 {
			d.Literal = "" // parts win over text
		}
	}
	return nil
}

func parts(p []flexInt) (y, m, d int) {
	if len(p) > 0 {
		y = int(p[0])
	}
	if len(p) > 1 {
		m = int(p[1])
	}
	if len(p) > 2 {
		d = int(p[2])
	}
	return
}

func (d Date) MarshalJSON() ([]byte, error) {
	out := dateJSON{Literal: d.Literal, Circa: flexBool(d.Circa)}
	if d.Year != 0 {
		out.Parts = [][]flexInt{dateParts(d)}
		if d.End != nil && d.End.Year != 0 {
			out.Parts = append(out.Parts, dateParts(*d.End))
		}
	}
	return json.Marshal(out)
}

func dateParts(d Date) []flexInt {
	p := []flexInt{flexInt(d.Year)}
	if d.Month != 0 {
		p = append(p, flexInt(d.Month))
		if d.Day != 0 {
			p = append(p, flexInt(d.Day))
		}
	}
	return p
}

// Text is a CSL "standard" or "number" variable: JSON may give it as text or as a number.
type Text string

func (t *Text) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" {
		*t = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = Text(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("csljson: not text or a number: %s", b)
	}
	*t = Text(n.String())
	return nil
}

// Year is the issue year, or 0.
func (i Item) Year() int {
	if i.Issued == nil {
		return 0
	}
	return i.Issued.Year
}

// Creators is the authors, or the editors when there are no authors, for lists.
func (i Item) Creators() []Name {
	if len(i.Author) > 0 {
		return i.Author
	}
	return i.Editor
}

// Parse reads the response of a CSL-JSON request: an array of items, or {"items": [...]}.
// Items with no ID or no type are left out.
func Parse(data []byte) ([]Item, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var items []Item
	var err error
	if data[0] == '{' {
		var w struct {
			Items []Item `json:"items"`
		}
		err = json.Unmarshal(data, &w)
		items = w.Items
	} else {
		err = json.Unmarshal(data, &items)
	}
	if err != nil {
		return nil, fmt.Errorf("csljson: %w", err)
	}
	out := items[:0]
	for _, it := range items {
		if it.ID != "" && it.Type != "" {
			out = append(out, it)
		}
	}
	return out, nil
}
