package logosimport

import (
	"database/sql"
	"strings"
)

// resInfo is what the catalogue says about a resource: metadata only.
type resInfo struct {
	Title string
	Type  string
	Found bool
}

// IsBible reports whether the resource is a Bible (not a commentary on one).
func (r resInfo) IsBible() bool {
	return strings.HasPrefix(r.Type, "text.monograph.bible") || r.Type == "lbx.reverse-interlinear"
}

// catalog looks up resources by ID. The catalogue may be missing, then nothing is found.
type catalog struct {
	db    *sql.DB
	cache map[string]resInfo
}

func (s *Snapshot) catalog() *catalog {
	c := &catalog{cache: map[string]resInfo{}}
	if db, err := s.open("catalog"); err == nil {
		c.db = db
	}
	return c
}

func (c *catalog) lookup(id string) resInfo {
	if r, ok := c.cache[id]; ok {
		return r
	}
	var r resInfo
	if c.db != nil && id != "" {
		var title, typ sql.NullString
		err := c.db.QueryRow(`SELECT COALESCE(NULLIF(UserTitle, ''), Title), Type FROM Records
			WHERE ResourceId = ? OR ResourceIdAsAlternate = ? LIMIT 1`, id, id).Scan(&title, &typ)
		if err == nil {
			r = resInfo{Title: title.String, Type: typ.String, Found: true}
		}
	}
	c.cache[id] = r
	return r
}

// title is the resource's title, or its ID when the catalogue does not have it.
func (c *catalog) title(id string) string {
	if r := c.lookup(id); r.Title != "" {
		return r.Title
	}
	return id
}

// resourceLink opens a Logos book. Metadata only: it points at the book, never at its content.
func resourceLink(id string) string { return "https://ref.ly/logosres/" + id }
