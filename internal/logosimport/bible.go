package logosimport

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/verse"
)

// Logos numbers the books of the Bible 1-39 (Old Testament), 61-87 (New Testament), with the
// deuterocanonical books in between. Glossa numbers them 1-66, then 67 onward.
var logosBooks = map[int]int{
	40: 67, // Tobit
	41: 68, // Judith
	43: 70, // Wisdom of Solomon
	44: 71, // Sirach
	45: 72, // Baruch
	50: 77, // 1 Maccabees
	51: 78, // 2 Maccabees
}

func glossaBook(n int) (int, bool) {
	switch {
	case n >= 1 && n <= 39:
		return n, true
	case n >= 61 && n <= 87:
		return n - 21, true
	}
	g, ok := logosBooks[n]
	return g, ok
}

var refRe = regexp.MustCompile(`^(bible(?:\+[\w-]+)?)\.(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-(\d+)(?:\.(\d+))?(?:\.(\d+))?)?$`)

// isBibleRef reports whether a Logos reference is to a Bible ("bible.64.3.16", "bible+esv.64.3.16").
func isBibleRef(ref string) bool { return strings.HasPrefix(ref, "bible") && refRe.MatchString(ref) }

// ParseRef reads a Logos Bible reference into verses, in English numbering. A chapter alone means
// the whole chapter and a book alone the whole book.
func ParseRef(ref string) (verse.Range, error) {
	m := refRe.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return verse.Range{}, fmt.Errorf("not a Bible reference")
	}
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	book, ok := glossaBook(num(m[2]))
	if !ok {
		return verse.Range{}, fmt.Errorf("book %s is not one Glossa has", m[2])
	}
	b, _ := verse.BookByNumber(book)
	sc, sv := 1, 1
	if m[3] != "" {
		sc = num(m[3])
	}
	if m[4] != "" {
		sv = num(m[4])
	}
	start, err := verse.Make(book, sc, sv)
	if err != nil {
		return verse.Range{}, err
	}
	// The end: chapters and verses given, or the end of what the start names.
	ec, ev := sc, sv
	switch {
	case m[5] != "": // a range: "-64.3.18" repeats the book number
		if num(m[5]) != num(m[2]) {
			return verse.Range{}, fmt.Errorf("a range across books")
		}
		ec = sc
		if m[6] != "" {
			ec = num(m[6])
		}
		ev = verse.EndOfChapter
		if m[7] != "" {
			ev = num(m[7])
		}
	case m[3] == "": // whole book
		ec, ev = b.Chapters, verse.EndOfChapter
	case m[4] == "": // whole chapter
		ev = verse.EndOfChapter
	}
	end, err := verse.Make(book, ec, ev)
	if err != nil {
		return verse.Range{}, err
	}
	if end < start {
		return verse.Range{}, fmt.Errorf("a range that runs backwards")
	}
	return verse.Range{Start: start, End: end}, nil
}

// union is the smallest range that holds both.
func union(a, b verse.Range) verse.Range {
	if b.Start < a.Start {
		a.Start = b.Start
	}
	if b.End > a.End {
		a.End = b.End
	}
	return a
}
