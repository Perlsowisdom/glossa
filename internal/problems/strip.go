// Package problems keeps Glossa's local problem log and builds reports from it (ADR 0009, T32).
// Nothing here uses the network: a report goes nowhere unless the person opens it as a GitHub issue
// themselves. This file holds the one set of rules that removes private content from text.
package problems

import (
	"regexp"
	"sort"
	"strings"
)

// Roots are the folders whose names must not appear in a report. Empty ones are ignored.
type Roots struct {
	Home    string // the user's home folder
	Data    string // Glossa's data folder
	Library string // the library file (library.db)
}

// Placeholders that replace private text. Strip is idempotent: they are left alone.
const (
	tokHome, tokData, tokLibrary, tokPath = "<home>", "<data>", "<library>", "<path>"
	tokEmail, tokSecret, tokRemoved       = "<email>", "<secret>", "<removed>"
)

const (
	// seg is one folder or file name of a path. A name may hold spaces inside it but does not begin
	// or end with one, so "a.epub and" never runs on into the next word.
	seg = `[^\\/\r\n"'<>|:*?\s](?:[^\\/\r\n"'<>|:*?]*[^\\/\r\n"'<>|:*?\s,;)\]}])?`
	// word is a piece of a name between spaces.
	word = `[^\\/\s"'<>|:*?,;()\[\]{}]`
	// last is the final name of a path. It may hold spaces when it ends in a file extension
	// ("My Study Bible.epub"); otherwise it stops at whitespace and at the punctuation around a path in prose.
	last = `(?:(?:` + word + `+ ){1,5}` + word + `+\.[A-Za-z0-9]{1,5}\b|` + word + `*)`
	// tail is what follows a drive, a share or a known root: more names.
	tail = `(?:[\\/]+(?:` + seg + `[\\/]+)*` + last + `)?`
)

var (
	reFile   = regexp.MustCompile(`(?i)\bfile:///?`)
	reURL    = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://(?:[^\s"'<>]|<removed>)+`)
	reUNC    = regexp.MustCompile(`\\\\(?:[?.]\\(?:UNC\\)?)?[A-Za-z0-9._$\-]+\\` + seg + tail)
	reDrive  = regexp.MustCompile(`(?:\\\\[?.]\\)?\b[A-Za-z]:[\\/]+(?:` + seg + `[\\/]+)*` + last)
	reUnix   = regexp.MustCompile(`(?m)(^|[\s"'(=\[,])(/[^/\s"'<>|:*?,;()\[\]{}]*(?:/` + seg + `)*/?)`)
	reTilde  = regexp.MustCompile(`(?m)(^|[\s"'(=\[,])~[\\/](?:` + seg + `[\\/]+)*` + last)
	reEmail  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+`)
	reAssign = regexp.MustCompile(`(?i)\b((?:api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|id[_-]?token|token|secret|password|passwd|pwd|key)["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&}\])]+)`)
	reScheme = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=\-]{6,}`)
	reLong   = regexp.MustCompile(`[A-Za-z0-9+/=_\-]{32,}`)
	reQuoted = regexp.MustCompile(`"[^"\n]*"|“[^”\n]*”`)
)

// unixRoots are the first folders of an absolute path that is clearly a path even with one name after it.
var unixRoots = map[string]bool{"users": true, "home": true, "var": true, "tmp": true, "private": true, "volumes": true,
	"mnt": true, "opt": true, "etc": true, "usr": true, "root": true, "library": true, "applications": true, "media": true, "run": true}

// Strip removes what must never leave the computer from text: the home, data and library folders
// become <home>, <data> and <library>; any other absolute path becomes <path>; e-mail addresses,
// the query and fragment of a URL, and secrets (key=value, bearer tokens, long hex or base64 runs)
// are replaced too. Text the rules do not touch is returned unchanged.
func Strip(s string, r Roots) string {
	s = reFile.ReplaceAllString(s, "")
	rootRes := rootPatterns(r)
	var b strings.Builder
	prev := 0
	for _, m := range reURL.FindAllStringIndex(s, -1) {
		b.WriteString(stripText(s[prev:m[0]], rootRes))
		b.WriteString(stripURL(s[m[0]:m[1]]))
		prev = m[1]
	}
	b.WriteString(stripText(s[prev:], rootRes))
	return b.String()
}

type rootPattern struct {
	re  *regexp.Regexp
	tok string
}

// rootPatterns makes one pattern per known folder, longest first so the library file wins over its folder.
func rootPatterns(r Roots) []rootPattern {
	type root struct{ path, tok string }
	var roots []root
	for _, x := range []root{{r.Library, tokLibrary}, {r.Data, tokData}, {r.Home, tokHome}} {
		p := strings.TrimRight(strings.TrimPrefix(x.path, `\\?\`), `\/`)
		if len(p) >= 3 {
			roots = append(roots, root{p, x.tok})
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return len(roots[i].path) > len(roots[j].path) })
	var out []rootPattern
	for _, x := range roots {
		var pat strings.Builder
		pat.WriteString(`(?:\\\\\?\\)?`)
		if strings.ContainsAny(x.path, `:\`) { // a Windows path: names are not case-sensitive
			pat.WriteString(`(?i)`)
		}
		lead := x.path[0] == '/' || x.path[0] == '\\'
		for i, part := range strings.FieldsFunc(x.path, func(c rune) bool { return c == '\\' || c == '/' }) {
			if i > 0 || lead {
				pat.WriteString(`[\\/]+`)
			}
			pat.WriteString(regexp.QuoteMeta(part))
		}
		out = append(out, rootPattern{regexp.MustCompile(pat.String() + tail), x.tok})
	}
	return out
}

// isNameChar reports whether c would continue a folder name (so /home/jane does not match /home/janet).
func isNameChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-' || c == '.' || c >= 0x80
}

func stripText(s string, roots []rootPattern) string {
	for _, rp := range roots {
		s = replaceMatches(rp.re, s, rp.tok)
	}
	s = reUNC.ReplaceAllString(s, tokPath)
	s = reDrive.ReplaceAllString(s, tokPath)
	s = reTilde.ReplaceAllString(s, "${1}"+tokHome)
	s = reUnix.ReplaceAllStringFunc(s, func(m string) string {
		sub := reUnix.FindStringSubmatch(m)
		names := strings.Split(strings.Trim(sub[2], "/"), "/")
		if len(names) < 2 && !unixRoots[strings.ToLower(names[0])] {
			return m
		}
		return sub[1] + tokPath
	})
	s = reEmail.ReplaceAllString(s, tokEmail)
	s = reScheme.ReplaceAllString(s, "${1} "+tokSecret)
	s = reAssign.ReplaceAllString(s, "${1}"+tokSecret)
	return reLong.ReplaceAllStringFunc(s, func(m string) string {
		if strings.ContainsAny(m, "0123456789") {
			return tokSecret
		}
		return m
	})
}

// replaceMatches replaces each match of re with tok, unless the next character would continue a name.
func replaceMatches(re *regexp.Regexp, s, tok string) string {
	var b strings.Builder
	prev := 0
	for _, m := range re.FindAllStringIndex(s, -1) {
		if m[1] < len(s) && isNameChar(s[m[1]]) && !strings.ContainsAny(s[m[1]-1:m[1]], `\/`) {
			continue
		}
		b.WriteString(s[prev:m[0]])
		b.WriteString(tok)
		prev = m[1]
	}
	b.WriteString(s[prev:])
	return b.String()
}

// stripURL keeps the scheme, host and path of a web address and drops what could hold private
// content: user and password, the query string and the fragment.
func stripURL(u string) string {
	scheme, rest, _ := strings.Cut(u, "://")
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		hadQuery := rest[i] == '?'
		rest = rest[:i]
		if hadQuery {
			rest += "?" + tokRemoved
		}
	}
	if at := strings.LastIndex(strings.SplitN(rest, "/", 2)[0], "@"); at >= 0 {
		rest = rest[at+1:]
	}
	return scheme + "://" + rest
}

// redactQuoted hides quoted text in a message that may name something of the person's, such as a
// book or note title in an error. Used for messages Glossa writes to its own log.
func redactQuoted(s string) string {
	return reQuoted.ReplaceAllStringFunc(s, func(m string) string {
		r := []rune(m)
		return string(r[0]) + "…" + string(r[len(r)-1])
	})
}
