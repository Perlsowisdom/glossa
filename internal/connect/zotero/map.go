package zotero

import (
	"strings"

	"github.com/Perlsowisdom/glossa/internal/connect/csljson"
)

// IDPrefix starts the CSL ID of every Zotero item Glossa holds: "zotero:<key>".
const IDPrefix = "zotero:"

// Map turns an item as Zotero's CSL-JSON export gives it into Glossa's own CSL-JSON item. Zotero's
// IDs depend on where it was asked ("2/ABCD2345", or a URL for the local app); Glossa's are
// "zotero:<key>", the same wherever the item came from, so a citation finds it again.
func Map(it csljson.Item) Entry {
	key := it.ID
	if i := strings.LastIndex(key, "/"); i >= 0 {
		key = key[i+1:]
	}
	key = strings.TrimPrefix(key, IDPrefix)
	it.ID = IDPrefix + key
	it.Title = strings.TrimSpace(it.Title)
	return Entry{Key: key, Item: it}
}

// Summary is an item in a list: enough to recognise it and choose it.
type Summary struct {
	Key     string `json:"key"`
	Type    string `json:"type" desc:"CSL type, such as book, chapter, article-journal"`
	Title   string `json:"title"`
	Authors string `json:"authors,omitempty" desc:"As in a list: \"Moo\", \"Moo and Wright\", \"Moo et al.\""`
	Year    int    `json:"year,omitempty"`
	Where   string `json:"where,omitempty" desc:"The journal, book or series it is in, or the publisher"`
}

// Summarize makes the short form of an item.
func Summarize(e Entry) Summary {
	it := e.Item
	title := it.Title
	if title == "" {
		title = "(untitled)"
	}
	where := it.ContainerTitle
	if where == "" {
		where = it.CollectionTitle
	}
	if where == "" {
		where = it.Publisher
	}
	return Summary{Key: e.Key, Type: it.Type, Title: title, Authors: authors(it.Creators()), Year: it.Year(), Where: where}
}

func authors(ns []csljson.Name) string {
	short := make([]string, 0, len(ns))
	for _, n := range ns {
		switch {
		case n.Literal != "":
			short = append(short, n.Literal)
		case n.Family != "":
			short = append(short, strings.TrimSpace(n.NonDroppingParticle+" "+n.Family))
		case n.Given != "":
			short = append(short, n.Given)
		}
	}
	switch len(short) {
	case 0:
		return ""
	case 1:
		return short[0]
	case 2:
		return short[0] + " and " + short[1]
	}
	return short[0] + " et al."
}
