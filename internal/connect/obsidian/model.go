package obsidian

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Perlsowisdom/glossa/internal/backup"
	"github.com/Perlsowisdom/glossa/internal/connect/syncutil"
	"github.com/Perlsowisdom/glossa/internal/verse"
)

const (
	kindNote   = "note"
	kindSermon = "sermon"

	maxTitle  = 200 // what a title is cut to on import
	maxTags   = 30  // what note.update accepts
	maxTagLen = 40
)

// item is a note or sermon as it is written to a file.
type item struct {
	Kind, ID, Title, Body string
	DocKind               string   // sermon, lesson or study (sermons only)
	Passages              []string // plain references
	Tags                  []string
	Created, Updated      int64 // Unix milliseconds
	Preached, Theme       string
}

// fileView is a Markdown file read as a note would be: the title, text, passages and tags it holds.
type fileView struct {
	Rel, ID, Kind, DocKind string
	P                      Parsed
	Title, Body            string
	Passages, Tags         []string
}

func normBody(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.TrimLeft(strings.TrimRight(s, " \t\n"), "\n")
}

// canonPassages writes references the way Glossa does ("Jn 3:16" and "John 3:16" are the same passage), keeps what
// it cannot read as it is, and drops empty ones.
func canonPassages(in []string) []string {
	var out []string
	for _, p := range in {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if r, err := verse.Parse(p); err == nil {
			p = r.String()
		}
		out = append(out, p)
	}
	return out
}

// tagKey is a tag as Obsidian holds it: lower case, no spaces (Obsidian tags cannot have any) and no leading #.
func tagKey(t string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimLeft(strings.TrimSpace(t), "#")), "-"))
}

func tagKeys(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		if k := tagKey(t); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// semHash is the fingerprint of what a note or sermon says, whichever side it is read from. Tags count only for notes
// (a sermon's tags cannot be changed from outside).
func semHash(kind, title, body string, passages, tags []string) string {
	if kind == kindSermon {
		tags = nil
	}
	return syncutil.Hash(kind, strings.TrimSpace(title), normBody(body), strings.Join(canonPassages(passages), "\n"), strings.Join(tagKeys(tags), "\n"))
}

func (it item) hash() string { return semHash(it.Kind, it.Title, it.Body, it.Passages, it.Tags) }

// hash leaves out a title that is only the file name: renaming such a file is not an edit, and the file can be found again by its text.
func (f fileView) hash() string {
	title := f.Title
	if strings.TrimSpace(f.P.Fields[keyTitle].Text) == "" {
		title = ""
	}
	return semHash(f.Kind, title, f.Body, f.Passages, f.Tags)
}

// view reads a file's text as a note or sermon.
func view(rel, raw string) fileView {
	p := parseFile(raw)
	f := fileView{Rel: rel, P: p, Kind: kindNote}
	f.ID = strings.TrimSpace(p.Fields[keyID].Text)
	if strings.TrimSpace(p.Fields[keyKind].Text) == kindSermon {
		f.Kind = kindSermon
	}
	f.DocKind = strings.TrimSpace(p.Fields[keyDocKind].Text)
	f.Title = strings.TrimSpace(p.Fields[keyTitle].Text)
	if f.Title == "" {
		f.Title = strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	}
	f.Body = normBody(p.Body)
	f.Passages = canonPassages(p.Fields[keyPassages].Items(";"))
	f.Tags = tagKeys(p.Fields[keyTags].Items(","))
	return f
}

// render is the file for an item. extra is the other front matter a file already had, kept as it was.
func render(it item, extra string) string {
	var b strings.Builder
	b.WriteString("---\n")
	writeKey(&b, keyTitle, Value{Text: strings.TrimSpace(it.Title)})
	writeKey(&b, keyID, Value{Text: it.ID})
	writeKey(&b, keyKind, Value{Text: it.Kind})
	if it.Kind == kindSermon && it.DocKind != "" && it.DocKind != "sermon" {
		writeKey(&b, keyDocKind, Value{Text: it.DocKind})
	}
	writeKey(&b, keyPassages, Value{IsList: true, List: canonPassages(it.Passages)})
	writeKey(&b, keyTags, Value{IsList: true, List: tagKeys(it.Tags)})
	writeKey(&b, keyCreated, Value{Text: stamp(it.Created)})
	writeKey(&b, keyUpdated, Value{Text: stamp(it.Updated)})
	writeKey(&b, keyPreached, Value{Text: it.Preached})
	writeKey(&b, keyTheme, Value{Text: it.Theme})
	b.WriteString(extra)
	b.WriteString("---\n")
	b.WriteString(backup.EncodeBody(it.Body))
	return b.String()
}

func stamp(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05Z")
}

// Wikilinks ---------------------------------------------------------------------------------------------

var wikiRe = regexp.MustCompile(`(!?)\[\[([^\[\]\n]+?)\]\]`)

// targets maps the names a wikilink can use to the title of the note that file becomes.
type targets map[string]string

func (t targets) add(rel, title string) {
	noExt := strings.TrimSuffix(rel, path.Ext(rel))
	t[strings.ToLower(noExt)] = title
	if _, dup := t[strings.ToLower(path.Base(noExt))]; !dup {
		t[strings.ToLower(path.Base(noExt))] = title
	}
}

// convertLinks turns [[wikilinks]] into plain text: the alias if there is one, else the title of the note the
// target file becomes (when it is in this import), else the target's name. Embeds (![[...]]) are left as they are
// and counted. Code is left alone. It returns the text, how many links it changed and how many embeds it left.
func convertLinks(body string, t targets) (out string, links, embeds int) {
	lines := strings.Split(body, "\n")
	fence := ""
	for i, l := range lines {
		trim := strings.TrimLeft(l, " ")
		if len(l)-len(trim) <= 3 && (strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~")) {
			mark := trim[:3]
			switch {
			case fence == "":
				fence = mark
			case fence == mark:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		parts := strings.Split(l, "`")
		for j := 0; j < len(parts); j += 2 { // even parts are outside `code`
			parts[j] = wikiRe.ReplaceAllStringFunc(parts[j], func(m string) string {
				sub := wikiRe.FindStringSubmatch(m)
				if sub[1] == "!" {
					embeds++
					return m
				}
				links++
				return linkText(sub[2], t)
			})
		}
		lines[i] = strings.Join(parts, "`")
	}
	return strings.Join(lines, "\n"), links, embeds
}

func linkText(inner string, t targets) string {
	target, alias, _ := strings.Cut(inner, "|")
	if a := strings.TrimSpace(alias); a != "" {
		return a
	}
	target, _, _ = strings.Cut(target, "#") // a heading or block inside the note
	target = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(target), ".md"))
	if title, ok := t[strings.ToLower(target)]; ok {
		return title
	}
	if title, ok := t[strings.ToLower(path.Base(target))]; ok {
		return title
	}
	if target == "" {
		return strings.TrimSpace(inner)
	}
	return path.Base(target)
}
