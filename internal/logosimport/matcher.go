package logosimport

import (
	"fmt"
	"regexp"
	"sort"
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
	// PD is true when the source's own record says the text is public domain (a Gutenberg book, a
	// module with a public-domain licence). CCEL's list does not say; its rights are checked at download.
	PD bool
}

// Book is the part of a Logos book that matching uses.
type Book struct {
	Title        string
	Abbreviation string
	Authors      string
	Type         string
	Languages    string // codes separated by commas
	// Published and Rights are the catalogue's date and copyright note; PublicDomain is true when Logos's
	// own record says public domain. They only decide whether a book is clearly a modern work.
	Published    string
	Rights       string
	PublicDomain bool
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

// volumeRe finds "Vol. 2", "Volume III" and "Part 1": which volume a book is, not what it is called.
var volumeRe = regexp.MustCompile(`\b(?:vol|volume|volumes|part|pt)\b\.?(?:\s*(?:[0-9]+|[ivxlc]+)\b)?`)

// canonicalVolume writes any volume mark the same way, "volume 2", so "Vol. 2", "Volume II" and "Part 2"
// agree and a different volume does not.
func canonicalVolume(m string) string {
	n := strings.Trim(strings.TrimLeft(strings.TrimSpace(m), "abcdefghijklmnopqrstuvwxyz"), ". ")
	if r, ok := romanNumerals[n]; ok {
		n = r
	}
	return " volume " + n + " "
}

var romanNumerals = map[string]string{"i": "1", "ii": "2", "iii": "3", "iv": "4", "v": "5", "vi": "6", "vii": "7", "viii": "8", "ix": "9", "x": "10", "xi": "11", "xii": "12"}

// hasVolume says a title names a volume or part.
func hasVolume(s string) bool { return volumeRe.MatchString(strings.ToLower(s)) }

// mainTitle is the title before a colon: "Institutes: Book 1" gives "Institutes".
func mainTitle(s string) string {
	if i := strings.Index(s, ":"); i > 0 {
		return s[:i]
	}
	return s
}

// words splits a title into lower-case words without accents, possessives, punctuation, text in
// brackets, small words and edition noise.
func words(s string) []string {
	s = volumeRe.ReplaceAllStringFunc(strings.ToLower(norm.NFD.String(s)), canonicalVolume)
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
	words     []string
	mainWords []string // the words before a colon in the title
	set       map[string]bool
	abbr      string
	years     []string
	idAbbr    string
	about     map[string]bool // the words of the name and of the author or description
	lang      string
	// hasAuthor is true when the item's About names an author (CCEL's, Gutenberg's and the library's
	// do; the catalogues' often hold a description instead, so for those it is false and no clash is
	// judged and no author is needed for a certain match).
	hasAuthor bool
}

// authorSources are the sources whose About is the author's name.
var authorSources = map[string]bool{"ccel": true, "gutenberg": true, "library": true}

func newIndex(items []FreeItem) *index {
	ix := &index{byTok: map[string][]int{}}
	for _, it := range items {
		w := words(it.Name)
		e := indexed{FreeItem: it, words: w, mainWords: words(mainTitle(it.Name)), set: set(w), years: yearRe.FindAllString(it.Name, -1), abbr: plain(it.Abbreviation), idAbbr: plain(it.ID),
			about: set(words(it.Name + " " + it.About)), lang: baseLanguage(it.Language),
			hasAuthor: authorSources[it.Source] && it.About != ""}
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

// surnames are the family names of the authors, whichever way round they are written: "Barnes, Albert",
// "C. H. Spurgeon" and "Augustine of Hippo" give "barnes", "spurgeon" and "augustine".
func surnames(authors string) []string {
	var out []string
	for _, a := range strings.FieldsFunc(authors, func(r rune) bool { return r == '\t' || r == ';' }) {
		a = strings.TrimSpace(a)
		last, _, hasComma := strings.Cut(a, ",")
		if !hasComma {
			if before, _, ok := strings.Cut(strings.ToLower(a), " of "); ok {
				last = before
			}
			if f := strings.Fields(last); len(f) > 0 {
				last = f[len(f)-1]
			}
		}
		if p := plain(last); len(p) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

// bookYear is the latest year the Logos record gives: its publication date, and the date in its
// copyright note when that note claims a copyright. 0 when it gives none.
func bookYear(b Book) int {
	year := 0
	scan := func(s string) {
		for _, m := range yearRe.FindAllString(s, -1) {
			var y int
			fmt.Sscanf(m, "%d", &y)
			year = max(year, y)
		}
	}
	scan(b.Published)
	if !b.PublicDomain {
		scan(b.Rights)
	}
	return year
}

// modernYear is the year after which a book that Logos does not call public domain is taken to be a
// modern work.
const modernYear = 1930

// hasSurname says whether one of the surnames is among the words.
func hasSurname(words map[string]bool, surs []string) (string, bool) {
	for _, x := range surs {
		if words[x] {
			return x, true
		}
		for _, part := range strings.Split(x, "-") {
			if len(part) >= 3 && words[part] {
				return x, true
			}
		}
	}
	return "", false
}

// match gives the best free copy from each source for one book, best first.
func (ix *index) match(b Book) []Match {
	kinds := kindsFor(b.Type)
	if len(kinds) == 0 || len(ix.items) == 0 {
		return nil
	}
	tw := words(b.Title)
	ts := set(tw)
	mw := words(mainTitle(b.Title))
	abbr := plain(b.Abbreviation)
	surs := surnames(b.Authors)
	volume := hasVolume(b.Title)
	years := yearRe.FindAllString(b.Title, -1)
	modern := !b.PublicDomain && bookYear(b) > modernYear
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
	best := map[string]Match{}
	for i := range cand {
		it := ix.items[i]
		if it.Kind != "any" && !contains(kinds, it.Kind) {
			continue
		}
		// A text in another language than the book's is another book.
		if len(langs) > 0 && it.lang != "" && !langs[it.lang] {
			continue
		}
		m, ok := score(it, tw, ts, mw, abbr, surs, volume, years)
		if !ok {
			continue
		}
		// A clearly modern work has a free copy only when the source itself says public domain and the
		// title and author agree. A book already in the user's library is never held back.
		if modern && it.Source != "library" && !(it.PD && m.Confidence == Certain) {
			continue
		}
		if cur, have := best[it.Source]; !have || better(m, cur) {
			best[it.Source] = m
		}
	}
	out := make([]Match, 0, len(best))
	for _, m := range best {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return preferred(out[i], out[j]) })
	return out
}

// sourceRank is the order the sources are preferred in when the same work is in more than one: CCEL
// first (its markup is best for theology), then Project Gutenberg, then the Bible modules.
func sourceRank(source string) int {
	switch source {
	case "library":
		return 0
	case "ccel":
		return 1
	case "gutenberg":
		return 2
	}
	return 3
}

// preferred orders matches of different sources: a certain match before a possible one, then the
// source order, then the score.
func preferred(a, b Match) bool {
	if a.Confidence != b.Confidence {
		return a.Confidence == Certain
	}
	if ra, rb := sourceRank(a.Item.Source), sourceRank(b.Item.Source); ra != rb {
		return ra < rb
	}
	return better(a, b)
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

func score(it indexed, tw []string, ts map[string]bool, mw []string, abbr string, surs []string, volume bool, years []string) (Match, bool) {
	shared, union, subset := overlap(ts, it.set)
	jac := 0.0
	if union > 0 {
		jac = float64(shared) / float64(union)
	}
	abbrHit := abbr != "" && (it.abbr == abbr || it.idAbbr == abbr)
	sur, surHit := hasSurname(it.about, surs)
	// Both sides name an author and they differ: the same words in the title are another book.
	authorClash := len(surs) > 0 && it.hasAuthor && !surHit
	if authorClash {
		return Match{}, false
	}
	// With an author on the free side but none agreed, only a long, close title is worth showing, and
	// only as a possible match.
	if it.hasAuthor && !surHit && len(tw) < 3 {
		return Match{}, false
	}
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
	// "Institutes of the Christian Religion: Book 1" and "Institutes of the Christian Religion" are the
	// same title once the subtitle after the colon is set aside.
	if !exact && len(mw) >= 2 && equalWords(mw, it.mainWords) {
		exact = true
	}
	// "Calvin's Institutes of the Christian Religion" and "Institutes of the Christian Religion" by
	// Calvin are the same title once the author's name is set aside.
	if !exact && surHit {
		a, b := without(tw, sur), without(it.words, sur)
		exact = len(a) >= 2 && equalWords(a, b)
	}
	// Where the free side names an author, a certain match needs the author to agree too.
	authorOK := !it.hasAuthor || surHit
	switch {
	case exact && len(tw) >= 2 && !volume && !yearClash && authorOK:
		m.Confidence = Certain
	case exact && (surHit || abbrHit) && !volume && !yearClash && authorOK:
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

// MatchAllSources gives, for each book (keyed by its index in books), the best free copy from each
// source, the preferred first.
func MatchAllSources(books []Book, items []FreeItem) map[int][]Match {
	ix := newIndex(items)
	out := map[int][]Match{}
	for i, b := range books {
		if ms := ix.match(b); len(ms) > 0 {
			out[i] = ms
		}
	}
	return out
}

// MatchAll gives the preferred free copy for each book, keyed by the book's index in books.
func MatchAll(books []Book, items []FreeItem) map[int]Match {
	out := map[int]Match{}
	for i, ms := range MatchAllSources(books, items) {
		out[i] = ms[0]
	}
	return out
}
