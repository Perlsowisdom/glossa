package logosimport

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Matching a public-domain Logos book to a free copy Glossa already knows how to get. Only titles,
// authors and abbreviations from the catalogue's metadata are compared; nothing of a book's
// content is read. A match is "certain" when the titles are the same once edition notes, small
// words and accents are set aside, and "likely" when they are close or only the short name agrees.

// Confidence of a match.
const (
	Certain = "certain"
	Likely  = "likely"
)

// FreeItem is one free text a catalogue offers, as much as matching needs.
type FreeItem struct {
	Source       string
	ID           string
	Name         string
	Abbreviation string
	About        string
	Language     string
	Kind         string // bible, commentary, dictionary or book
	Installed    bool
}

// Book is the part of a Logos book that matching uses.
type Book struct {
	Title        string
	Abbreviation string
	Authors      string
	Type         string
	Languages    string // codes separated by commas
}

// Match is a free copy for one book.
type Match struct {
	Item       FreeItem
	Confidence string
	Score      float64
}

var stopWords = map[string]bool{"the": true, "a": true, "an": true, "of": true, "and": true, "with": true, "in": true, "on": true,
	"for": true, "to": true, "by": true, "its": true}

// noiseWords describe an edition or its markup, not the work.
var noiseWords = map[string]bool{"strongs": true, "strong": true, "numbers": true, "number": true, "edition": true, "ed": true,
	"complete": true, "text": true, "morphology": true, "ebook": true, "annotated": true}

var volumeWords = map[string]bool{"vol": true, "volume": true, "volumes": true, "part": true}

// words splits a title into lower-case words without accents, possessives, punctuation, text in
// brackets, small words and edition noise.
func words(s string) []string {
	s = strings.ToLower(norm.NFD.String(s))
	var b strings.Builder
	depth := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r == '(' || r == '[':
			depth++
			b.WriteRune(' ')
		case r == ')' || r == ']':
			if depth > 0 {
				depth--
			}
			b.WriteRune(' ')
		case depth > 0:
			continue
		case r == '\'' || r == '’' || r == '‘' || r == '`':
			// "Easton's" and "Eastons" are the same word; "'s" is dropped, an apostrophe inside a word goes.
			if i+1 < len(runes) && runes[i+1] == 's' && (i+2 >= len(runes) || !unicode.IsLetter(runes[i+2])) {
				i++
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	var out []string
	for _, w := range strings.Fields(b.String()) {
		if stopWords[w] || noiseWords[w] || isYear(w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// plain keeps only letters and digits, for comparing short names.
func plain(s string) string { return fold(norm.NFD.String(s)) }

func isYear(w string) bool {
	if len(w) != 4 {
		return false
	}
	for _, r := range w {
		if r < '0' || r > '9' {
			return false
		}
	}
	return w[0] == '1' || w[0] == '2'
}

// without drops the words that are the author's surname.
func without(ws []string, sur string) []string {
	var out []string
	for _, w := range ws {
		if plain(w) != sur {
			out = append(out, w)
		}
	}
	return out
}

func equalWords(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func set(ws []string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

// overlap gives the shared words, the size of the union and whether one set holds the other.
func overlap(a, b map[string]bool) (shared, union int, subset bool) {
	for w := range a {
		if b[w] {
			shared++
		}
	}
	union = len(a) + len(b) - shared
	return shared, union, shared == len(a) || shared == len(b)
}

// kindsFor says which kinds of free text a Logos resource type could be matched with. Types that
// are not texts (media, courseware, datasets, journals) match nothing.
func kindsFor(typ string) []string {
	t := strings.ToLower(typ)
	switch {
	case strings.HasPrefix(t, "text.monograph.bible"), t == "lbx.reverse-interlinear":
		return []string{"bible"}
	case strings.Contains(t, ".commentary"):
		return []string{"commentary"}
	case strings.Contains(t, ".lexicon"), strings.Contains(t, ".dictionary"), strings.Contains(t, ".encyclopedia"):
		return []string{"dictionary"}
	case strings.HasPrefix(t, "text.monograph"):
		if strings.Contains(t, "courseware") || strings.Contains(t, "study-guide") {
			return nil
		}
		return []string{"book"}
	}
	return nil
}

// freeKinds are the kinds of free text matching looks at, for asking the catalogues.
var freeKinds = []string{"bible", "commentary", "dictionary", "book"}

// index holds the free items ready to compare.
type index struct {
	items []indexed
	byTok map[string][]int
}

type indexed struct {
	FreeItem
	words  []string
	set    map[string]bool
	abbr   string
	years  []string
	idAbbr string
	about  string
	lang   string
}

func newIndex(items []FreeItem) *index {
	ix := &index{byTok: map[string][]int{}}
	for _, it := range items {
		w := words(it.Name)
		e := indexed{FreeItem: it, words: w, set: set(w), years: yearRe.FindAllString(it.Name, -1), abbr: plain(it.Abbreviation), idAbbr: plain(it.ID),
			about: plain(it.Name + " " + it.About), lang: baseLanguage(it.Language)}
		i := len(ix.items)
		ix.items = append(ix.items, e)
		for t := range e.set {
			ix.byTok[t] = append(ix.byTok[t], i)
		}
	}
	return ix
}

func baseLanguage(tag string) string {
	switch strings.ToLower(tag) {
	case "grc", "gre":
		return "el"
	case "hbo":
		return "he"
	}
	b, _, _ := strings.Cut(strings.ToLower(tag), "-")
	return b
}

// surname is the family name of the first author ("Barnes, Albert" gives "barnes").
func surname(authors string) string {
	first := authors
	for _, sep := range []string{"\t", ";"} {
		first, _, _ = strings.Cut(first, sep)
	}
	last, _, _ := strings.Cut(first, ",")
	return plain(last)
}

func (ix *index) match(b Book) (Match, bool) {
	kinds := kindsFor(b.Type)
	if len(kinds) == 0 || len(ix.items) == 0 {
		return Match{}, false
	}
	tw := words(b.Title)
	ts := set(tw)
	abbr := plain(b.Abbreviation)
	sur := surname(b.Authors)
	volume := false
	years := yearRe.FindAllString(b.Title, -1)
	for _, w := range tw {
		volume = volume || volumeWords[w]
	}
	langs := map[string]bool{}
	for _, l := range strings.Split(b.Languages, ",") {
		if l = baseLanguage(strings.TrimSpace(l)); l != "" {
			langs[l] = true
		}
	}

	cand := map[int]bool{}
	for t := range ts {
		for _, i := range ix.byTok[t] {
			cand[i] = true
		}
	}
	if abbr != "" {
		for i, it := range ix.items {
			if it.abbr == abbr || it.idAbbr == abbr {
				cand[i] = true
			}
		}
	}
	var best Match
	found := false
	for i := range cand {
		it := ix.items[i]
		if !contains(kinds, it.Kind) {
			continue
		}
		// A text in another language than the book's is another book.
		if len(langs) > 0 && it.lang != "" && !langs[it.lang] {
			continue
		}
		m, ok := score(it, tw, ts, abbr, sur, volume, years)
		if !ok {
			continue
		}
		if !found || better(m, best) {
			best, found = m, true
		}
	}
	return best, found
}

func better(a, b Match) bool {
	if a.Confidence != b.Confidence {
		return a.Confidence == Certain
	}
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return a.Item.Source+"/"+a.Item.ID < b.Item.Source+"/"+b.Item.ID
}

func score(it indexed, tw []string, ts map[string]bool, abbr, sur string, volume bool, years []string) (Match, bool) {
	shared, union, subset := overlap(ts, it.set)
	jac := 0.0
	if union > 0 {
		jac = float64(shared) / float64(union)
	}
	abbrHit := abbr != "" && (it.abbr == abbr || it.idAbbr == abbr)
	surHit := sur != "" && len(sur) >= 3 && strings.Contains(it.about, sur)
	// A year in both titles that is not the same year means another edition.
	yearHit := false
	for _, y := range years {
		for _, w := range it.years {
			yearHit = yearHit || w == y
		}
	}
	yearClash := len(years) > 0 && len(it.years) > 0 && !yearHit
	m := Match{Item: it.FreeItem, Score: jac}
	exact := len(tw) > 0 && (equalWords(tw, it.words) || (len(tw) == len(it.words) && shared == len(tw)))
	// "Calvin's Institutes of the Christian Religion" and "Institutes of the Christian Religion" by
	// Calvin are the same title once the author's name is set aside.
	if !exact && surHit && len(sur) >= 3 {
		a, b := without(tw, sur), without(it.words, sur)
		exact = len(a) >= 2 && equalWords(a, b)
	}
	switch {
	case exact && len(tw) >= 2 && !volume && !yearClash:
		m.Confidence = Certain
	case exact && (surHit || abbrHit) && !volume && !yearClash:
		m.Confidence = Certain
	case exact:
		m.Confidence = Likely
	case abbrHit && jac >= 0.5 && !volume && !yearClash:
		m.Confidence = Certain
		m.Score = jac + 0.5
	case jac >= 0.75:
		m.Confidence = Likely
	case subset && min(len(ts), len(it.set)) >= 2 && surHit:
		m.Confidence = Likely
	case abbrHit && it.Kind == "bible":
		m.Confidence = Likely
		m.Score = 0.1 + jac
	default:
		return Match{}, false
	}
	if abbrHit {
		m.Score += 0.25
	}
	if yearHit {
		m.Score += 0.2
	}
	if surHit {
		m.Score += 0.1
	}
	return m, true
}

// MatchAll gives the best free copy for each book, keyed by the book's index in books.
func MatchAll(books []Book, items []FreeItem) map[int]Match {
	ix := newIndex(items)
	out := map[int]Match{}
	for i, b := range books {
		if m, ok := ix.match(b); ok {
			out[i] = m
		}
	}
	return out
}
