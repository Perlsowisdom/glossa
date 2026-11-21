package logosimport

import (
	"strings"
	"testing"
)

var matchItems = []FreeItem{
	{Source: "crosswire", ID: "KJV", Name: "King James Version (1769) with Strongs Numbers and Morphology", Abbreviation: "KJV", Language: "en", Kind: "bible"},
	{Source: "crosswire", ID: "ASV", Name: "American Standard Version (1901)", Abbreviation: "ASV", Language: "en", Kind: "bible"},
	{Source: "crosswire", ID: "WEB", Name: "World English Bible", Abbreviation: "WEB", Language: "en", Kind: "bible"},
	{Source: "crosswire", ID: "Luther", Name: "Luther Bibel 1912", Abbreviation: "Luther", Language: "de", Kind: "bible"},
	{Source: "ebible", ID: "engkjv", Name: "King James Version (1611)", Abbreviation: "KJV", Language: "en", Kind: "bible"},
	{Source: "crosswire", ID: "Easton", Name: "Easton's Bible Dictionary", Abbreviation: "Easton", Language: "en", Kind: "dictionary"},
	{Source: "crosswire", ID: "ISBE", Name: "International Standard Bible Encyclopedia", Abbreviation: "ISBE", Language: "en", Kind: "dictionary"},
	{Source: "crosswire", ID: "MHC", Name: "Matthew Henry's Concise Commentary", Abbreviation: "MHC", Language: "en", Kind: "commentary"},
	{Source: "crosswire", ID: "Barnes", Name: "Albert Barnes' Notes on the Whole Bible", Abbreviation: "Barnes", Language: "en", Kind: "commentary", About: "Albert Barnes, 1798-1870"},
	{Source: "crosswire", ID: "TDavid", Name: "The Treasury of David", Abbreviation: "TDavid", Language: "en", Kind: "commentary", About: "Charles Haddon Spurgeon"},
	{Source: "crosswire", ID: "Pilgrim", Name: "The Pilgrim's Progress", Abbreviation: "Pilgrim", Language: "en", Kind: "book", About: "John Bunyan"},
	{Source: "crosswire", ID: "Gen1", Name: "Genesis", Abbreviation: "Gen1", Language: "en", Kind: "book"},
	{Source: "crosswire", ID: "TR", Name: "Textus Receptus", Abbreviation: "TR", Language: "grc", Kind: "bible"},
}

func TestMatcher(t *testing.T) {
	bible, comm, dict, text := "text.monograph.bible", "text.monograph.commentary.bible", "text.monograph.encyclopedia", "text.monograph.church-history"
	for _, c := range []struct {
		name string
		book Book
		item string // "" for no match at all
		conf string // "" to accept any confidence
	}{
		{"edition note in brackets", Book{Title: "American Standard Version", Type: bible}, "ASV", Certain},
		{"leading the and a different case", Book{Title: "the world english BIBLE", Type: bible}, "WEB", Certain},
		{"abbreviation agrees with the title", Book{Title: "American Standard Version (Revised)", Abbreviation: "ASV", Type: bible}, "ASV", Certain},
		{"abbreviation alone", Book{Title: "Darby's Translation", Abbreviation: "ASV", Type: bible}, "ASV", Likely},
		{"possessive and accents", Book{Title: "Easton’s Bible Dictionary", Type: "text.monograph.dictionary"}, "Easton", Certain},
		{"an encyclopedia is a dictionary", Book{Title: "International Standard Bible Encyclopedia", Abbreviation: "ISBE", Type: dict}, "ISBE", Certain},
		{"title and a subtitle are close", Book{Title: "Matthew Henry’s Concise Commentary on the Bible", Authors: "Henry, Matthew\tScott, Thomas", Type: comm}, "MHC", Likely},
		{"author helps a close title", Book{Title: "Notes on the Whole Bible", Authors: "Barnes, Albert", Type: comm}, "Barnes", Likely},
		{"apostrophes in a name", Book{Title: "The Treasury of David", Authors: "Spurgeon, C. H.", Type: comm}, "TDavid", Certain},
		{"a book of another kind", Book{Title: "The Pilgrim’s Progress", Authors: "Bunyan, John", Type: text}, "Pilgrim", Certain},
		{"a single word title is only likely", Book{Title: "Genesis", Type: "text.monograph"}, "Gen1", Likely},
		{"volumes are never certain", Book{Title: "The Treasury of David, Volume 2", Authors: "Spurgeon", Type: comm}, "TDavid", Likely},
		{"another language is another book", Book{Title: "Luther Bibel 1912", Type: bible, Languages: "en"}, "", ""},
		{"same language matches", Book{Title: "Luther Bibel 1912", Type: bible, Languages: "de"}, "Luther", Certain},
		{"greek is greek", Book{Title: "Textus Receptus", Type: bible, Languages: "el"}, "TR", Certain},
		{"a commentary is not matched to a Bible", Book{Title: "World English Bible", Type: comm}, "", ""},
		{"media matches nothing", Book{Title: "World English Bible", Type: "lbx.media"}, "", ""},
		{"journals match nothing", Book{Title: "King James Version", Type: "text.serial.journal"}, "", ""},
		{"nothing close", Book{Title: "Systematic Theology", Type: text}, "", ""},
		{"a few shared small words are not a match", Book{Title: "The Bible and Its Story", Type: text}, "", ""},
	} {
		got, ok := MatchAll([]Book{c.book}, matchItems)[0]
		if c.item == "" {
			if ok {
				t.Errorf("%s: matched %s with %s, want none", c.name, got.Item.ID, got.Confidence)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: no match, want %s (%s)", c.name, c.item, c.conf)
			continue
		}
		if got.Item.ID != c.item {
			t.Errorf("%s: matched %s, want %s", c.name, got.Item.ID, c.item)
		}
		if c.conf != "" && got.Confidence != c.conf {
			t.Errorf("%s: %s, want %s", c.name, got.Confidence, c.conf)
		}
	}
}

// Two years of the same translation: the year in the Logos title picks the one it names, and a
// title with no year never calls either of them certain.
func TestMatcherYears(t *testing.T) {
	m := MatchAll([]Book{
		{Title: "King James Version 1611", Type: "text.monograph.bible"},
		{Title: "King James Version 1769", Type: "text.monograph.bible"},
		{Title: "King James Version 1900", Type: "text.monograph.bible"},
	}, matchItems)
	if m[0].Item.ID != "engkjv" || m[1].Item.ID != "KJV" {
		t.Fatalf("by year: %v %v", m[0].Item.ID, m[1].Item.ID)
	}
	if got, ok := m[2]; ok && got.Confidence == Certain {
		t.Fatalf("a clashing year was called certain: %+v", got)
	}
}

// The choice is the same however the catalogue is ordered.
func TestMatcherPicksStably(t *testing.T) {
	book := Book{Title: "King James Version", Abbreviation: "KJV", Type: "text.monograph.bible"}
	first := MatchAll([]Book{book}, matchItems)[0]
	if first.Item.ID != "KJV" && first.Item.ID != "engkjv" {
		t.Fatalf("matched %v", first.Item)
	}
	rev := append([]FreeItem{}, matchItems...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	if other := MatchAll([]Book{book}, rev)[0]; other.Item != first.Item {
		t.Fatalf("the order of the catalogue changed the choice: %v and %v", first.Item.ID, other.Item.ID)
	}
}

func TestWords(t *testing.T) {
	for in, want := range map[string]string{
		"The King James Version (1769) with Strong's Numbers": "king james version",
		"Easton’s Bible Dictionary":                           "easton bible dictionary",
		"Café Évangile":                                       "cafe evangile",
		"Job, Volume 1":                                       "job volume 1",
		"Notes [Second Edition]":                              "notes",
	} {
		if got := strings.Join(words(in), " "); got != want {
			t.Errorf("words(%q) = %q, want %q", in, got, want)
		}
	}
}
