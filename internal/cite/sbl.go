package cite

import (
	"fmt"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/verse"
)

// Bible books are shortened as the SBL Handbook of Style (2nd ed., section 8.3.1) shortens them:
// Gen, Exod, 1 Sam, Matt, Phlm ... The numbered books get a space after the number.
//
// The Handbook cites a Bible by book, chapter and verse ("Gen 1:1") and names the version once,
// in parentheses, when it is first used or changes ("Gen 1:1 [NRSV]" in running text, "(NRSV)" in a
// note). It does not list a Bible version among the works cited unless the version itself is
// discussed, so Bible cites get no bibliography entry here.
func sblBook(b verse.Book) string {
	o := b.OSIS
	if len(o) > 1 && o[0] >= '1' && o[0] <= '9' {
		return o[:1] + " " + o[1:]
	}
	return o
}

// Passage reads a typed reference (Rom 8:28-30, John 3) and writes it with the SBL book
// abbreviation: "Rom 8:28-30". The second result is false when it is not a reference.
func Passage(ref string) (string, bool) {
	r, err := verse.Parse(ref)
	if err != nil {
		return "", false
	}
	b, ok := verse.BookByNumber(r.Start.Book())
	if !ok {
		return "", false
	}
	if r.End.Book() != r.Start.Book() {
		return "", false
	}
	name := sblBook(b)
	sc, sv, ec, ev := r.Start.Chapter(), r.Start.Verse(), r.End.Chapter(), r.End.Verse()
	wholeEnd := ev == verse.EndOfChapter
	switch {
	case sv <= 1 && wholeEnd && sc == ec:
		return fmt.Sprintf("%s %d", name, sc), true
	case sv <= 1 && wholeEnd:
		return fmt.Sprintf("%s %d-%d", name, sc, ec), true
	case r.Start == r.End:
		return fmt.Sprintf("%s %d:%d", name, sc, sv), true
	case sc == ec:
		return fmt.Sprintf("%s %d:%d-%d", name, sc, sv, ev), true
	}
	return fmt.Sprintf("%s %d:%d-%d:%d", name, sc, sv, ec, ev), true
}

func normalSpace(s string) string { return strings.Join(strings.Fields(s), " ") }
