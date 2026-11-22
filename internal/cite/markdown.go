package cite

import (
	"regexp"
	"strings"
)

// Cite is one citation of one source, as it is written in a document.
type Cite struct {
	Key            string `json:"key" required:"true" desc:"The source: lib:<book>, bible:<version>, zotero:<key> or manual:<id>"`
	Label          string `json:"label,omitempty" desc:"What the locator is: page (default), chapter, section, verse, paragraph, figure, note, volume, column, line or article. Not used for a Bible"`
	Locator        string `json:"locator,omitempty" desc:"A page, chapter or section number; for a Bible, the passage, e.g. Rom 8:28-30"`
	Prefix         string `json:"prefix,omitempty" desc:"Words before the citation, e.g. see"`
	Suffix         string `json:"suffix,omitempty" desc:"Words after the citation, e.g. for more"`
	SuppressAuthor bool   `json:"suppress_author,omitempty" desc:"Leave out the author (when the sentence names them)"`
}

// IsBible reports whether the citation is of a Bible version.
func (c Cite) IsBible() bool { return strings.HasPrefix(c.Key, "bible:") }

// In the Markdown a document is saved as, a citation is Pandoc-like, in square brackets:
//
//	[@lib:01HX...]                      the source alone
//	[@lib:01HX..., p. 12]               with a locator (p., pp., ch., sec., v., para., fig., n., vol., col., l., art.)
//	[see @lib:01HX..., p. 12, for more] with words before and after
//	[-@lib:01HX...]                     without the author
//	[@bible:nrsv, Rom 8:28-30]          a Bible version: the passage is the locator
//
// In the words before and after, a backslash, [ and ] are escaped with a backslash, and so is @ in the
// words before. A "[...]" followed by "(" is an ordinary link and is never read as a citation.
// In the expression, x5c is the backslash.
var citeRe = regexp.MustCompile(`^\[(?:((?:[^\[\]\x5c@]|\x5c.)+?) )?(-?)@((?:lib|bible|zotero|manual):[A-Za-z0-9_.:-]*[A-Za-z0-9_])(?:, ((?:[^\[\]\x5c]|\x5c.)*))?\]`)

var labelRe = regexp.MustCompile(`^(p|pp|ch|chap|sec|v|vv|para|fig|n|nn|vol|col|l|ll|art|§|¶)\.? (.+)$`)

var tokenLabel = map[string]string{"p": "page", "pp": "page", "ch": "chapter", "chap": "chapter", "sec": "section", "§": "section",
	"v": "verse", "vv": "verse", "para": "paragraph", "¶": "paragraph", "fig": "figure", "n": "note", "nn": "note",
	"vol": "volume", "col": "column", "l": "line", "ll": "line", "art": "article"}

var labelToken = map[string]string{"page": "p.", "chapter": "ch.", "section": "sec.", "verse": "v.", "paragraph": "para.",
	"figure": "fig.", "note": "n.", "volume": "vol.", "column": "col.", "line": "l.", "article": "art."}

// Labels lists the locator labels, in the order a picker offers them.
var Labels = []string{"page", "chapter", "section", "verse", "paragraph", "figure", "note", "volume", "column", "line", "article"}

const backslash = "\x5c"

func unescape(s string) string {
	if !strings.Contains(s, backslash) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x5c && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var escaper = strings.NewReplacer(backslash, backslash+backslash, "[", backslash+"[", "]", backslash+"]")

func escape(s string, at bool) string {
	s = escaper.Replace(s)
	if at {
		s = strings.ReplaceAll(s, "@", backslash+"@")
	}
	return s
}

// ParseInline reads a citation at the start of s, and says how many bytes it took.
func ParseInline(s string) (Cite, int, bool) {
	if len(s) == 0 || s[0] != '[' {
		return Cite{}, 0, false
	}
	m := citeRe.FindStringSubmatch(s)
	if m == nil || strings.HasPrefix(s[len(m[0]):], "(") {
		return Cite{}, 0, false
	}
	c := Cite{Key: m[3], Prefix: unescape(m[1]), SuppressAuthor: m[2] == "-"}
	tail := unescape(m[4])
	if tail == "" {
		return c, len(m[0]), true
	}
	first, rest, more := strings.Cut(tail, ", ")
	if !more {
		first, rest = tail, ""
	}
	switch lm := labelRe.FindStringSubmatch(first); {
	case c.IsBible():
		c.Locator, c.Suffix = first, rest
	case lm != nil:
		c.Label, c.Locator, c.Suffix = tokenLabel[lm[1]], lm[2], rest
	default:
		c.Suffix = tail
	}
	return c, len(m[0]), true
}

// Format writes a citation as ParseInline reads it.
func Format(c Cite) string {
	var b strings.Builder
	b.WriteByte('[')
	if c.Prefix != "" {
		b.WriteString(escape(c.Prefix, true) + " ")
	}
	if c.SuppressAuthor {
		b.WriteByte('-')
	}
	b.WriteString("@" + c.Key)
	loc := strings.ReplaceAll(normalSpace(c.Locator), ", ", ",")
	switch {
	case c.IsBible() && loc != "":
		b.WriteString(", " + escape(loc, false))
	case !c.IsBible() && loc != "":
		tok := labelToken[c.Label]
		if tok == "" {
			tok = "p."
		}
		if tok == "p." && strings.ContainsAny(loc, "-–,&") {
			tok = "pp."
		}
		b.WriteString(", " + tok + " " + escape(loc, false))
	}
	if c.Suffix != "" {
		b.WriteString(", " + escape(c.Suffix, false))
	}
	b.WriteByte(']')
	return b.String()
}

// Extract lists the citations in a Markdown text, in order.
func Extract(md string) []Cite {
	var out []Cite
	for i := 0; i < len(md); i++ {
		if md[i] != '[' {
			continue
		}
		if c, n, ok := ParseInline(md[i:]); ok {
			out = append(out, c)
			i += n - 1
		}
	}
	return out
}

// BibliographyMarker is the line in a document where its bibliography goes.
const BibliographyMarker = "<!-- bibliography -->"
