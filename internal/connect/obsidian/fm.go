package obsidian

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Front matter is the block of YAML between two "---" lines at the top of a file. Glossa writes a small,
// plain subset (text, and lists as "- item" lines) and reads that subset plus the common ways Obsidian
// writes it: quoted text, "[a, b]" lists, and any other keys, which it keeps untouched when it rewrites a file.

const (
	keyTitle    = "title"
	keyID       = "glossa_id"
	keyKind     = "glossa_kind"
	keyDocKind  = "glossa_doc_kind"
	keyPassages = "passages"
	keyTags     = "tags"
	keyCreated  = "created"
	keyUpdated  = "updated"
	keyPreached = "preached"
	keyTheme    = "theme"
)

// managed keys are the ones Glossa writes itself; any other key in a file is kept as it is.
var managed = map[string]bool{keyTitle: true, keyID: true, keyKind: true, keyDocKind: true, keyPassages: true, keyTags: true,
	keyCreated: true, keyUpdated: true, keyPreached: true, keyTheme: true}

// Value is a front matter value: text, or a list.
type Value struct {
	Text   string
	List   []string
	IsList bool
}

// Items is the value as a list; a text value is split at sep.
func (v Value) Items(sep string) []string {
	if v.IsList {
		return v.List
	}
	if strings.TrimSpace(v.Text) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v.Text, sep) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Parsed is a file read apart: its front matter and its text.
type Parsed struct {
	HasFM  bool
	Fields map[string]Value
	Extra  string // the lines of keys Glossa does not write, as they were (each ends with a newline)
	Body   string // the text after the front matter, with line ends as \n
}

var keyRe = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_ .-]*):(?:[ \t]+(.*))?$`)

// parseFile reads a Markdown file. A file without front matter, or whose top block is not front matter, is all text.
func parseFile(raw string) Parsed {
	text := strings.TrimPrefix(raw, string(rune(0xFEFF)))
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	none := Parsed{Fields: map[string]Value{}, Body: text}
	if !strings.HasPrefix(text, "---\n") {
		return none
	}
	rest := text[4:]
	var block []string
	for {
		nl := strings.IndexByte(rest, '\n')
		line, after := rest, ""
		if nl >= 0 {
			line, after = rest[:nl], rest[nl+1:]
		}
		if t := strings.TrimRight(line, " \t"); t == "---" || t == "..." {
			p, ok := parseBlock(block)
			if !ok {
				return none
			}
			p.HasFM, p.Body = true, after
			return p
		}
		if nl < 0 {
			return none
		}
		block = append(block, line)
		rest = after
	}
}

type fmBlock struct {
	key, inline string
	lines       []string // the continuation lines
}

func parseBlock(lines []string) (Parsed, bool) {
	p := Parsed{Fields: map[string]Value{}}
	var blocks []*fmBlock
	for _, l := range lines {
		switch {
		case strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#"):
			if n := len(blocks); n > 0 {
				blocks[n-1].lines = append(blocks[n-1].lines, l)
			}
		case l[0] == ' ' || l[0] == '\t' || strings.HasPrefix(l, "- ") || l == "-":
			if len(blocks) == 0 {
				return p, false
			}
			b := blocks[len(blocks)-1]
			b.lines = append(b.lines, l)
		default:
			m := keyRe.FindStringSubmatch(l)
			if m == nil {
				return p, false
			}
			blocks = append(blocks, &fmBlock{key: strings.TrimSpace(m[1]), inline: strings.TrimSpace(m[2])})
		}
	}
	var extra strings.Builder
	for _, b := range blocks {
		if v, ok := b.value(); ok && managed[b.key] {
			p.Fields[b.key] = v
			continue
		}
		extra.WriteString(b.key + ":")
		if b.inline != "" {
			extra.WriteString(" " + b.inline)
		}
		extra.WriteString("\n")
		for _, l := range trimBlank(b.lines) {
			extra.WriteString(l + "\n")
		}
	}
	p.Extra = extra.String()
	return p, true
}

func trimBlank(ls []string) []string {
	for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}

// value reads a block's value. ok is false for shapes Glossa does not read (block text, nested keys).
func (b *fmBlock) value() (Value, bool) {
	in := b.inline
	switch {
	case in == "":
		var list []string
		for _, l := range b.lines {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if t == "-" || !strings.HasPrefix(t, "- ") {
				return Value{}, false
			}
			list = append(list, unquote(strings.TrimSpace(t[2:])))
		}
		if len(list) == 0 {
			return Value{}, true
		}
		return Value{List: list, IsList: true}, true
	case in[0] == '|' || in[0] == '>' || in[0] == '{' || in[0] == '&' || in[0] == '*':
		return Value{}, false
	case in[0] == '[':
		inner := strings.TrimSpace(stripComment(in))
		if len(trimBlank(b.lines)) > 0 || !strings.HasSuffix(inner, "]") {
			return Value{}, false
		}
		return Value{List: splitFlow(inner[1 : len(inner)-1]), IsList: true}, true
	}
	if len(trimBlank(b.lines)) > 0 {
		return Value{}, false // a text value that goes over several lines
	}
	return Value{Text: unquote(in)}, true
}

// stripComment removes a trailing " # comment" that is not inside quotes.
func stripComment(s string) string {
	var q rune
	for i, r := range s {
		switch {
		case q != 0:
			if r == q {
				q = 0
			}
		case r == '"' || r == '\'':
			q = r
		case r == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return strings.TrimRight(s[:i], " \t")
		}
	}
	return s
}

func splitFlow(s string) []string {
	var out []string
	var cur strings.Builder
	var q rune
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, unquote(t))
		}
		cur.Reset()
	}
	esc := false
	for _, r := range s {
		switch {
		case q != 0:
			cur.WriteRune(r)
			switch {
			case esc:
				esc = false
			case r == '\\' && q == '"':
				esc = true
			case r == q:
				q = 0
			}
		case r == '"' || r == '\'':
			q = r
			cur.WriteRune(r)
		case r == ',':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return stripComment(s)
}

// Writing ------------------------------------------------------------------------------------------------

// scalar writes text so a reader gets back exactly the same text: plain when that is safe, else in quotes.
func scalar(s string) string {
	if !needsQuote(s) {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}

var (
	numberRe = regexp.MustCompile(`^[-+]?(\d[\d_]*)?\.?\d+([eE][-+]?\d+)?$|^0[xo][0-9a-fA-F]+$`)
	dateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
)

func needsQuote(s string) bool {
	if s == "" || s != strings.TrimSpace(s) || !utf8.ValidString(s) {
		return true
	}
	if strings.ContainsAny(s[:1], "-?:,[]{}#&*!|>'\"%@`") {
		return true
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~", "y", "n":
		return true
	}
	if numberRe.MatchString(s) || dateRe.MatchString(s) || strings.HasSuffix(s, ":") || strings.Contains(s, ": ") || strings.Contains(s, " #") {
		return true
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// writeKey writes one front matter key: text on the same line, a list as "- item" lines. Empty values are left out.
func writeKey(b *strings.Builder, key string, v Value) {
	if v.IsList {
		if len(v.List) == 0 {
			return
		}
		b.WriteString(key + ":\n")
		for _, it := range v.List {
			b.WriteString("  - " + scalar(it) + "\n")
		}
		return
	}
	if v.Text == "" {
		return
	}
	text := scalar(v.Text)
	if key == keyCreated || key == keyUpdated || key == keyPreached {
		text = v.Text // a date: Obsidian shows it as one
	}
	b.WriteString(key + ": " + text + "\n")
}
