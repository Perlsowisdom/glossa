package logosimport

import (
	"encoding/xml"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Logos keeps the text of notes and clippings as XAML-like rich text:
//
//	<Paragraph><Run FontBold="True" Text="Hello"/><UriLink Uri="https://..."><Run Text="link"/></UriLink></Paragraph>
//
// ToMarkdown turns it into Glossa Markdown (docs/editor-behaviour.md): paragraphs separated by a
// blank line, "- " and "1. " list lines, **bold**, *italic*, <u>underline</u>, [text](url) for web
// links and pipe tables. Colours, fonts and sizes are dropped. Text is written as it is, without
// escaping, because Glossa reads only the forms it writes.

type segment struct {
	text                    string
	bold, italic, underline bool
	link                    string
	br                      bool
}

type listState struct {
	ordered bool
	n       int
}

type mdWriter struct {
	blocks  []string
	cur     []segment
	lists   []listState
	itemNew bool // the next paragraph in a list item starts it
	link    string
	table   [][]string
	row     []string
	cellBuf []string
	inCell  bool
	tabHdr  bool
}

// ToMarkdown converts Logos rich text to Markdown. Plain text comes back as it is.
func ToMarkdown(src string) string {
	src = strings.TrimPrefix(src, "\ufeff")
	if strings.TrimSpace(src) == "" {
		return ""
	}
	if !strings.HasPrefix(strings.TrimSpace(src), "<") {
		return strings.TrimSpace(src)
	}
	w := &mdWriter{}
	dec := xml.NewDecoder(strings.NewReader("<Root>" + src + "</Root>"))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	for {
		tok, err := dec.Token()
		if err != nil {
			if err != io.EOF {
				// Broken markup: keep what was read so far, then fall back to the text alone.
				if len(w.blocks) == 0 && len(w.cur) == 0 {
					return strings.TrimSpace(stripTags(src))
				}
			}
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			w.start(t)
		case xml.EndElement:
			w.end(t.Name.Local)
		}
	}
	w.flushParagraph()
	return strings.TrimSpace(strings.Join(w.blocks, "\n\n"))
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return tagRe.ReplaceAllString(s, "") }

func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (w *mdWriter) start(t xml.StartElement) {
	switch t.Name.Local {
	case "Run":
		text := strings.ReplaceAll(attr(t, "Text"), "\ufeff", "")
		if text == "" {
			return
		}
		w.cur = append(w.cur, segment{
			text:      text,
			bold:      attr(t, "FontBold") == "True",
			italic:    attr(t, "FontItalic") == "True",
			underline: attr(t, "HasUnderline") == "True",
			link:      w.link,
		})
	case "UriLink":
		if u := attr(t, "Uri"); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			w.link = u
		}
	case "LineBreak":
		w.cur = append(w.cur, segment{br: true})
	case "List":
		w.flushParagraph()
		w.lists = append(w.lists, listState{ordered: attr(t, "Kind") == "Decimal"})
	case "ListItem":
		w.flushParagraph()
		if n := len(w.lists); n > 0 {
			w.lists[n-1].n++
		}
		w.itemNew = true
	case "Table":
		w.flushParagraph()
		w.table = nil
	case "TableRow":
		w.row = nil
	case "TableCell":
		w.flushParagraph()
		w.inCell, w.cellBuf = true, nil
	}
}

func (w *mdWriter) end(name string) {
	switch name {
	case "Paragraph":
		w.flushParagraph()
	case "UriLink":
		w.link = ""
	case "List":
		w.flushParagraph()
		if n := len(w.lists); n > 0 {
			w.lists = w.lists[:n-1]
		}
	case "TableCell":
		w.flushParagraph()
		w.row = append(w.row, strings.Join(w.cellBuf, "<br>"))
		w.inCell = false
	case "TableRow":
		if len(w.row) > 0 {
			w.table = append(w.table, w.row)
		}
	case "Table":
		w.flushTable()
	}
}

func (w *mdWriter) flushParagraph() {
	text := renderSegments(w.cur)
	w.cur = nil
	if text == "" {
		return
	}
	if w.inCell {
		w.cellBuf = append(w.cellBuf, strings.ReplaceAll(strings.ReplaceAll(text, "|", `\|`), "\n", "<br>"))
		return
	}
	if n := len(w.lists); n > 0 {
		lines := strings.Split(text, "\n")
		marker := "- "
		if w.lists[n-1].ordered {
			marker = strconv.Itoa(w.lists[n-1].n) + ". "
		}
		if !w.itemNew {
			marker = strings.Repeat(" ", len(marker))
		}
		w.itemNew = false
		for i := range lines {
			if i == 0 {
				lines[i] = marker + lines[i]
			} else {
				lines[i] = strings.Repeat(" ", len(marker)) + lines[i]
			}
		}
		// Items of one list stay on consecutive lines.
		if last := len(w.blocks) - 1; last >= 0 && isListBlock(w.blocks[last]) {
			w.blocks[last] += "\n" + strings.Join(lines, "\n")
		} else {
			w.blocks = append(w.blocks, strings.Join(lines, "\n"))
		}
		return
	}
	w.blocks = append(w.blocks, text)
}

func isListBlock(b string) bool {
	return strings.HasPrefix(b, "- ") || (len(b) > 2 && b[0] >= '0' && b[0] <= '9' && strings.Contains(b[:min(len(b), 5)], ". "))
}

func (w *mdWriter) flushTable() {
	if len(w.table) == 0 {
		return
	}
	cols := 0
	for _, r := range w.table {
		cols = max(cols, len(r))
	}
	row := func(r []string) string {
		cells := make([]string, cols)
		for i := range cells {
			if i < len(r) {
				cells[i] = r[i]
			}
		}
		return "| " + strings.Join(cells, " | ") + " |"
	}
	sep := make([]string, cols)
	for i := range sep {
		sep[i] = "---"
	}
	lines := []string{"<!-- table header=no -->", row(w.table[0]), "| " + strings.Join(sep, " | ") + " |"}
	for _, r := range w.table[1:] {
		lines = append(lines, row(r))
	}
	w.blocks = append(w.blocks, strings.Join(lines, "\n"))
	w.table = nil
}

type marks struct {
	bold, italic, underline bool
	link                    string
}

func (s segment) marks() marks { return marks{s.bold, s.italic, s.underline, s.link} }

// renderSegments joins runs of the same formatting and writes the marks around each, keeping the
// spaces outside them so the Markdown stays valid.
func renderSegments(segs []segment) string {
	var out strings.Builder
	for i := 0; i < len(segs); {
		if segs[i].br {
			out.WriteString("\n")
			i++
			continue
		}
		j, text := i, ""
		for j < len(segs) && !segs[j].br && segs[j].marks() == segs[i].marks() {
			text += segs[j].text
			j++
		}
		out.WriteString(wrap(text, segs[i].marks()))
		i = j
	}
	return strings.TrimSpace(out.String())
}

func wrap(text string, m marks) string {
	body := strings.TrimSpace(text)
	if body == "" {
		return text
	}
	lead := text[:len(text)-len(strings.TrimLeft(text, " \t\r\n"))]
	trail := text[len(strings.TrimRight(text, " \t\r\n")):]
	if m.underline {
		body = "<u>" + body + "</u>"
	}
	if m.italic {
		body = "*" + body + "*"
	}
	if m.bold {
		body = "**" + body + "**"
	}
	if m.link != "" {
		body = "[" + body + "](" + m.link + ")"
	}
	return lead + body + trail
}

// PlainText is the text of rich text with no marks, as a title or a sample.
func PlainText(src string) string {
	md := ToMarkdown(src)
	r := strings.NewReplacer("**", "", "<u>", "", "</u>", "", "<br>", " ", "\n", " ")
	return strings.Join(strings.Fields(r.Replace(md)), " ")
}
