package readwise

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/Perlsowisdom/glossa/internal/command"
)

// Matching a Readwise source to a library book is deliberately strict. Both the title and the authors
// must agree after plain tidying (letter case, accents, punctuation, a leading "The", "A" or "An",
// "Last, First" against "First Last", the part of a title after a colon). Nothing fuzzier: no spelling
// guesses, no initials against full names, no matching on the title alone. When two library books
// match, none is chosen.

// fold lower-cases, removes accents and turns every run of anything but letters and digits into one space.
func fold(s string) string {
	s = strings.NewReplacer("&", " and ", "'", "", "’", "").Replace(s)
	var b strings.Builder
	space := true
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

var articles = []string{"the ", "a ", "an "}

// titleParts splits a title into its main part (without a leading article) and its subtitle.
func titleParts(title string) (main, sub string) {
	m, s, _ := strings.Cut(title, ":")
	main = fold(m)
	for _, a := range articles {
		if rest, ok := strings.CutPrefix(main, a); ok && rest != "" {
			main = rest
			break
		}
	}
	return main, fold(s)
}

// authorKeys turns an author line into the sorted, folded names it holds. "Lewis, C. S." and
// "C. S. Lewis" give the same key; "A and B", "A & B" and "A; B" give two.
func authorKeys(line string) []string {
	line = strings.ReplaceAll(line, " & ", ";")
	line = strings.ReplaceAll(line, " and ", ";")
	var names []string
	for _, seg := range strings.Split(line, ";") {
		names = append(names, splitCommas(seg)...)
	}
	set := map[string]bool{}
	for _, n := range names {
		if k := fold(n); k != "" {
			set[k] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// splitCommas reads one segment: "Last, First" is one name written backwards, "A, B" is two names.
func splitCommas(seg string) []string {
	parts := strings.Split(seg, ",")
	if len(parts) == 2 && len(strings.Fields(parts[0])) == 1 && strings.TrimSpace(parts[1]) != "" {
		return []string{strings.TrimSpace(parts[1]) + " " + strings.TrimSpace(parts[0])}
	}
	return parts
}

type libBook struct {
	id, main, sub string
	authors       string // sorted keys joined
}

// Matcher finds the library book a source belongs to.
type Matcher struct {
	byTitle map[string][]libBook
	Books   int
}

// LoadMatcher reads the titles and authors of the library's books. It reads nothing else.
func LoadMatcher(ctx context.Context, env *command.Env) (*Matcher, error) {
	m := &Matcher{byTitle: map[string][]libBook{}}
	if env == nil || env.Store == nil {
		return m, nil
	}
	rows, err := env.Store.Query(ctx, `SELECT i.id, i.title, COALESCE(i.subtitle, ''), COALESCE(p.name, '')
		FROM items i LEFT JOIN item_people ip ON ip.item_id = i.id AND ip.role = 'author'
		LEFT JOIN people p ON p.id = ip.person_id
		WHERE i.deleted_at IS NULL ORDER BY i.id, ip.position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type acc struct {
		title, subtitle string
		names           []string
	}
	items := map[string]*acc{}
	var order []string
	for rows.Next() {
		var id, title, subtitle, name string
		if err := rows.Scan(&id, &title, &subtitle, &name); err != nil {
			return nil, err
		}
		a := items[id]
		if a == nil {
			a = &acc{title: title, subtitle: subtitle}
			items[id] = a
			order = append(order, id)
		}
		if name != "" {
			a.names = append(a.names, splitCommas(name)...)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range order {
		a := items[id]
		main, sub := titleParts(a.title)
		if sub == "" {
			sub = fold(a.subtitle)
		}
		keys := authorKeys(strings.Join(a.names, ";"))
		if main == "" || len(keys) == 0 {
			continue
		}
		m.byTitle[main] = append(m.byTitle[main], libBook{id: id, main: main, sub: sub, authors: strings.Join(keys, "|")})
		m.Books++
	}
	return m, nil
}

// Match returns the one library book the source belongs to. ambiguous is true when several fit,
// and then no book is returned.
func (m *Matcher) Match(title, author string) (id string, ambiguous bool) {
	main, sub := titleParts(title)
	keys := authorKeys(author)
	if main == "" || len(keys) == 0 {
		return "", false
	}
	want := strings.Join(keys, "|")
	var found []string
	for _, b := range m.byTitle[main] {
		if b.authors != want {
			continue
		}
		if b.sub != "" && sub != "" && b.sub != sub {
			continue // two different subtitles: different books
		}
		found = append(found, b.id)
	}
	switch len(found) {
	case 0:
		return "", false
	case 1:
		return found[0], false
	}
	return "", true
}
