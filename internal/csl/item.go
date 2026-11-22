package csl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/connect/csljson"
)

// Name is one person or organisation.
type Name struct {
	Family, Given, Suffix         string
	DroppingParticle, NonDropping string
	CommaSuffix, StaticOrdering   bool
	Literal                       string
}

// Date is a CSL date: parts, an optional end of a range, a season, "circa", or free text.
type Date struct {
	Y, M, D    int
	EndY       int
	EndM, EndD int
	HasEnd     bool // a range; with EndY == 0 it is open-ended
	Season     int  // 1..4
	Circa      bool
	Literal    string
}

// IsEmpty is true when the date says nothing.
func (d *Date) IsEmpty() bool {
	return d == nil || (d.Y == 0 && d.M == 0 && d.D == 0 && d.Season == 0 && d.Literal == "" && !d.HasEnd)
}

// Item is one source as the engine reads it: text and number variables, names and dates by their
// CSL variable names.
type Item struct {
	ID    string
	Type  string
	Vars  map[string]string
	Names map[string][]Name
	Dates map[string]*Date
}

// NewItem makes an empty item.
func NewItem(id, typ string) *Item {
	return &Item{ID: id, Type: typ, Vars: map[string]string{}, Names: map[string][]Name{}, Dates: map[string]*Date{}}
}

// ItemFromCSLJSON converts the connections' item type to the engine's.
func ItemFromCSLJSON(it csljson.Item) (*Item, error) {
	b, err := json.Marshal(it)
	if err != nil {
		return nil, err
	}
	return ParseItem(b)
}

// ParseItems reads a CSL-JSON array of items.
func ParseItems(b []byte) ([]*Item, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(b, &raws); err != nil {
		return nil, fmt.Errorf("csl: items: %w", err)
	}
	var out []*Item
	for _, r := range raws {
		it, err := ParseItem(r)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

var knownDates = map[string]bool{"issued": true, "accessed": true, "event-date": true, "original-date": true,
	"submitted": true, "available-date": true, "container": true}

// ParseItem reads one CSL-JSON item.
func ParseItem(b []byte) (*Item, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("csl: item: %w", err)
	}
	it := NewItem("", "")
	for k, raw := range m {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		switch {
		case k == "id":
			it.ID = jsonScalar(raw)
		case k == "type":
			it.Type = jsonScalar(raw)
		case raw[0] == '[':
			it.Names[k] = parseNames(raw)
		case raw[0] == '{':
			if d := parseDate(raw); d != nil {
				it.Dates[k] = d
			}
		case raw[0] == '"' || (raw[0] >= '0' && raw[0] <= '9') || raw[0] == '-':
			if knownDates[k] {
				if d := parseDate(raw); d != nil {
					it.Dates[k] = d
				}
				continue
			}
			if s := jsonScalar(raw); s != "" || raw[0] == '"' {
				it.Vars[k] = s
			}
		}
	}
	return it, nil
}

func jsonScalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func parseNames(raw json.RawMessage) []Name {
	var objs []map[string]json.RawMessage
	if json.Unmarshal(raw, &objs) != nil {
		return nil
	}
	var out []Name
	for _, o := range objs {
		str := func(k string) string { return jsonScalar(o[k]) }
		boolean := func(k string) bool {
			s := strings.ToLower(str(k))
			return s == "true" || s == "1"
		}
		n := Name{Family: str("family"), Given: str("given"), Suffix: str("suffix"),
			DroppingParticle: str("dropping-particle"), NonDropping: str("non-dropping-particle"),
			CommaSuffix: boolean("comma-suffix"), StaticOrdering: boolean("static-ordering"),
			Literal: str("literal")}
		if n.Literal == "" && n.Family == "" && n.Given == "" {
			if s := str("name"); s != "" {
				n.Literal = s
			}
		}
		out = append(out, n)
	}
	return out
}

type flexNum int

func (f *flexNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(b)), `"`)
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexNum(n)
	return nil
}

func parseDate(raw json.RawMessage) *Date {
	raw = bytes.TrimSpace(raw)
	if raw[0] == '"' || (raw[0] >= '0' && raw[0] <= '9') {
		return parseRawDate(jsonScalar(raw))
	}
	var w struct {
		Parts   [][]flexNum     `json:"date-parts"`
		Season  json.RawMessage `json:"season"`
		Circa   json.RawMessage `json:"circa"`
		Literal string          `json:"literal"`
		Raw     string          `json:"raw"`
	}
	if json.Unmarshal(raw, &w) != nil {
		return nil
	}
	d := &Date{Literal: w.Literal}
	if w.Literal == "" && w.Raw != "" && len(w.Parts) == 0 {
		if rd := parseRawDate(w.Raw); rd != nil {
			return rd
		}
		d.Literal = w.Raw
	}
	if c := strings.ToLower(jsonScalar(w.Circa)); c == "true" || c == "1" {
		d.Circa = true
	}
	if len(w.Season) > 0 {
		d.Season = seasonNumber(jsonScalar(w.Season))
	}
	if len(w.Parts) > 0 {
		p := w.Parts[0]
		d.Y, d.M, d.D = part(p, 0), part(p, 1), part(p, 2)
		if len(w.Parts) > 1 {
			q := w.Parts[1]
			d.HasEnd = true
			d.EndY, d.EndM, d.EndD = part(q, 0), part(q, 1), part(q, 2)
		}
	}
	if d.M >= 13 && d.M <= 16 {
		d.Season, d.M = d.M-12, 0
	} else if d.M >= 21 && d.M <= 24 {
		d.Season, d.M = d.M-20, 0
	}
	if d.Y != 0 || d.M != 0 || d.D != 0 || d.Season != 0 {
		d.Literal = ""
	}
	return d
}

func part(p []flexNum, i int) int {
	if i < len(p) {
		return int(p[i])
	}
	return 0
}

func seasonNumber(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "1", "spring":
		return 1
	case "2", "summer":
		return 2
	case "3", "autumn", "fall":
		return 3
	case "4", "winter":
		return 4
	}
	return 0
}

var monthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}

// parseRawDate reads the simple textual forms people write: 2000, 2000-05, 2000-5-3, 2000/5/3,
// "May 3, 2000" and "3 May 2000".
func parseRawDate(s string) *Date {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if d := parseNumericDate(s); d != nil {
		return d
	}
	toks := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == ' ' || r == ',' || r == '.' })
	d := &Date{}
	for _, t := range toks {
		n, err := strconv.Atoi(t)
		switch {
		case err == nil && len(t) == 4:
			d.Y = n
		case err == nil && n >= 1 && n <= 31:
			d.D = n
		case err != nil && len(t) >= 3:
			for i, m := range monthNames {
				if strings.HasPrefix(t, m) {
					d.M = i + 1
				}
			}
			if d.M == 0 {
				if sn := seasonNumber(t); sn > 0 {
					d.Season = sn
				}
			}
		}
	}
	if d.Y == 0 {
		return &Date{Literal: s}
	}
	return d
}

func parseNumericDate(s string) *Date {
	for _, sep := range []string{"-", "/"} {
		parts := strings.Split(s, sep)
		if len(parts) < 1 || len(parts) > 3 {
			continue
		}
		var nums []int
		ok := true
		for _, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				ok = false
				break
			}
			nums = append(nums, n)
		}
		if !ok {
			continue
		}
		d := &Date{Y: nums[0]}
		if len(nums) > 1 {
			d.M = nums[1]
		}
		if len(nums) > 2 {
			d.D = nums[2]
		}
		if d.Y > 31 || len(parts) == 1 {
			return d
		}
	}
	return nil
}
