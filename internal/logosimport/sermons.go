package logosimport

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/verse"
)

// SermonFolder is the folder sermons go in when they belong to no series.
const SermonFolder = "Sermons from Logos"

const (
	maxSermonPassages = 20 // what a Glossa sermon can hold
	maxPointHeading   = 300
	maxTheme          = 200
)

// SermonData is a sermon to make: what a Sermon Builder document becomes in Glossa.
type SermonData struct {
	Title      string
	Passages   []string // each as Glossa writes a reference
	PreachedOn string   // YYYY-MM-DD, or empty
	Theme      string
	Points     []SermonPoint
	Blocks     []SermonBlock
	Updated    int64 // UTC milliseconds
}

// SermonPoint is one heading of the outline.
type SermonPoint struct {
	Heading string
	Ref     string
	Level   int
}

// SermonBlock is a piece of the sermon's text: Markdown, or a Bible quotation whose text comes from
// a Bible installed in Glossa when the sermon is made.
type SermonBlock struct {
	Text  string       // Markdown
	Tight bool         // joins the block before it with a line break, not a blank line (list items)
	Quote *SermonQuote // when set, this block is a verse quotation
}

// SermonQuote is a verse quote in a sermon: where it is, and which Bible Logos showed it in.
type SermonQuote struct {
	Display    string // "Romans 8:28"
	OSIS       string
	Abbr       string // the Bible's short name in Logos
	Title      string
	ResourceID string
	Tag        string // the short name in the reference, such as "esv"
}

type sermonDoc struct {
	id                                                                   int64
	external, modified, title, author, church, series, date, tags, notes string
	description, slides                                                  string
	template                                                             bool
}

type sermonBlockRow struct {
	kind, content, passage, clipping string
	indent                           int
}

// readSermons reads the Sermon Builder documents in Sermon.db.
func (s *Snapshot) readSermons(res *Result) error {
	if !s.Has("sermons") {
		return nil
	}
	db, err := s.open("sermons")
	if err != nil {
		return err
	}
	rows, err := db.Query(`SELECT Id, COALESCE(ExternalId, ''), COALESCE(ModifiedDate, ''), COALESCE(Title, ''), COALESCE(AuthorName, ''),
			COALESCE(ChurchTitle, ''), COALESCE(Series, ''), COALESCE(Date, ''), COALESCE(TagsJson, ''), COALESCE(Notes, ''),
			COALESCE(Description, ''), COALESCE(SlideGroupJson, ''), IsTemplate
		FROM Documents WHERE IsDeleted = 0 ORDER BY Id`)
	if err != nil {
		return err
	}
	var docs []sermonDoc
	for rows.Next() {
		var d sermonDoc
		if err := rows.Scan(&d.id, &d.external, &d.modified, &d.title, &d.author, &d.church, &d.series, &d.date, &d.tags, &d.notes,
			&d.description, &d.slides, &d.template); err != nil {
			rows.Close()
			return err
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, d := range docs {
		blocks, err := readSermonBlocks(db, d.id)
		if err != nil {
			return err
		}
		mapSermon(res, d, blocks)
	}
	return nil
}

func readSermonBlocks(db *sql.DB, id int64) ([]sermonBlockRow, error) {
	rows, err := db.Query(`SELECT Kind, COALESCE(Indent, 0), COALESCE(Content, ''), COALESCE(PassageJson, ''), COALESCE(ClippingJson, '')
		FROM Blocks WHERE DocumentId = ? AND IsDeleted = 0 ORDER BY Rank, Id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sermonBlockRow
	for rows.Next() {
		var b sermonBlockRow
		if err := rows.Scan(&b.kind, &b.indent, &b.content, &b.passage, &b.clipping); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type passageBlockJSON struct {
	Reference *struct {
		Raw  string `json:"raw"`
		Text string `json:"text"`
	} `json:"reference"`
	Resource *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Abbr  string `json:"abbreviatedTitle"`
	} `json:"resource"`
}

type clippingBlockJSON struct {
	Title     string `json:"title"`
	SlideText string `json:"slideText"`
	Resource  *struct {
		Title string `json:"title"`
	} `json:"resource"`
}

type tagsJSON struct {
	ReferenceTags []struct {
		Raw  string `json:"raw"`
		Text string `json:"text"`
	} `json:"referenceTags"`
	TopicTags []struct {
		Raw  string `json:"raw"`
		Text string `json:"text"`
	} `json:"topicTags"`
	Misc []string `json:"miscellaneousTags"`
}

var tagShortRe = regexp.MustCompile(`^bible\+([\w-]+)\.`)
var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// headingMark is the Markdown for a Logos heading level: Glossa has three sizes.
func headingMark(level int) string { return strings.Repeat("#", min(max(level, 1), 3)) }

func mapSermon(res *Result, d sermonDoc, blocks []sermonBlockRow) {
	key := "sermon:" + d.external
	if d.external == "" {
		key = "sermon:id" + strconv.FormatInt(d.id, 10)
	}
	title := strings.Join(strings.Fields(PlainText(d.title)), " ")
	if d.template {
		res.skip(KindSermon, key, title, "A Logos sermon template, not a sermon")
		return
	}
	data := &SermonData{Title: title, Updated: millis(d.modified)}
	if len(data.Title) > 300 {
		data.Title = string([]rune(data.Title)[:300])
	}

	var md []SermonBlock
	var points []SermonPoint
	var blockRefs []string
	numbers := map[int]int{} // the count of numbered lines at each indent
	prevList := ""
	unknown := 0
	add := func(text, list string) {
		if strings.TrimSpace(text) == "" {
			prevList = ""
			return
		}
		md = append(md, SermonBlock{Text: text, Tight: list != "" && list == prevList})
		prevList = list
	}
	for _, b := range blocks {
		kind := b.kind
		if kind != "number" {
			numbers = map[int]int{}
		}
		switch {
		case strings.HasPrefix(kind, "heading"):
			level, _ := strconv.Atoi(strings.TrimPrefix(kind, "heading"))
			text := PlainText(b.content)
			if text == "" {
				continue
			}
			add(headingMark(level)+" "+text, "")
			h := text
			if len([]rune(h)) > maxPointHeading {
				h = string([]rune(h)[:maxPointHeading])
			}
			lv := 1
			if level > 1 {
				lv = 2
			}
			points = append(points, SermonPoint{Heading: h, Level: lv})
		case kind == "bullet":
			add(listLine(strings.Repeat("  ", b.indent)+"- ", ToMarkdown(b.content)), "bullet")
		case kind == "number":
			for i := range numbers {
				if i > b.indent {
					delete(numbers, i)
				}
			}
			numbers[b.indent]++
			add(listLine(strings.Repeat("   ", b.indent)+strconv.Itoa(numbers[b.indent])+". ", ToMarkdown(b.content)), "number")
		case kind == "blockquote":
			add(quoteLines(ToMarkdown(b.content)), "")
		case kind == "illustration":
			add(labelled("Illustration", ToMarkdown(b.content)), "")
		case kind == "prompt":
			add(labelled("Question", ToMarkdown(b.content)), "")
		case kind == "passage":
			q, text := passageBlock(b.passage, res)
			if q != nil {
				blockRefs = append(blockRefs, q.Display)
				if len(points) > 0 && points[len(points)-1].Ref == "" {
					points[len(points)-1].Ref = q.Display
				}
				md = append(md, SermonBlock{Quote: q})
				prevList = ""
			} else {
				add(text, "")
			}
		case kind == "clipping":
			add(clippingBlock(b, ToMarkdown(b.content)), "")
		case kind == "normal" || kind == "":
			add(ToMarkdown(b.content), "")
		default:
			unknown++
			add(ToMarkdown(b.content), "")
		}
	}
	if unknown > 0 {
		res.note("sermons with blocks of a kind Glossa does not know, kept as plain text")
	}

	// The description comes first, then the text, then the notes the user wrote in the sermon.
	if desc := ToMarkdown(d.description); desc != "" {
		md = append([]SermonBlock{{Text: quoteLines(desc)}}, md...)
	}
	if notes := ToMarkdown(d.notes); notes != "" {
		md = append(md, SermonBlock{Text: "## Notes"}, SermonBlock{Text: notes})
	}
	data.Blocks = md
	data.Points = points

	// Passages: the sermon's own reference tags, or else the verses it quotes.
	var tags tagsJSON
	if d.tags != "" {
		json.Unmarshal([]byte(d.tags), &tags)
	}
	var passages []string
	seen := map[string]bool{}
	addPassage := func(display string) {
		if display != "" && !seen[display] {
			seen[display] = true
			passages = append(passages, display)
		}
	}
	lost := 0
	for _, t := range tags.ReferenceTags {
		if r, err := ParseRef(t.Raw); err == nil {
			addPassage(r.String())
		} else {
			lost++
		}
	}
	if len(passages) == 0 {
		for _, r := range blockRefs {
			addPassage(r)
		}
	}
	if lost > 0 {
		res.note("sermons with passage tags that are not Bible passages Glossa has, left out")
	}
	if len(passages) > maxSermonPassages {
		res.note(fmt.Sprintf("sermons with more than %d passages, which is all a sermon in Glossa can hold; the rest are left out", maxSermonPassages))
		passages = passages[:maxSermonPassages]
	}
	data.Passages = passages

	// The date preached, and the theme: the series, or else the topic tags.
	if m := dateRe.FindString(strings.TrimSpace(d.date)); m != "" {
		data.PreachedOn = m
	} else if strings.TrimSpace(d.date) != "" {
		res.note("sermons whose date Glossa could not read, left out")
	}
	series := strings.Join(strings.Fields(d.series), " ")
	var topics []string
	for _, t := range tags.TopicTags {
		name := t.Text
		if name == "" {
			name = t.Raw
		}
		if name = strings.Join(strings.Fields(name), " "); name != "" {
			topics = append(topics, name)
		}
	}
	switch {
	case series != "":
		data.Theme = series
	case len(topics) > 0:
		data.Theme = strings.Join(topics, ", ")
	}
	if r := []rune(data.Theme); len(r) > maxTheme {
		data.Theme = string(r[:maxTheme])
	}
	if series != "" && len(topics) > 0 || len(tags.Misc) > 0 {
		res.note("sermons with topic or other tags that are not the theme, which are not kept")
	}
	if d.author != "" || d.church != "" {
		res.note("sermons with an author or church name, which Glossa sermons do not keep")
	}
	if d.slides != "" && d.slides != "[]" && d.slides != "{}" {
		res.note("sermons with slide settings, which Glossa sermons do not keep")
	}

	if len(data.Blocks) == 0 && data.Title == "" {
		res.skip(KindSermon, key, "", "An empty sermon with no title or text")
		return
	}
	folder := "sermons"
	if series != "" {
		folder = "sermon-series:" + series
		res.addFolder(Folder{Key: folder, Name: series})
	} else {
		res.addFolder(Folder{Key: folder, Name: SermonFolder})
	}
	sample := ""
	if len(passages) > 0 {
		sample = strings.Join(passages, "; ")
	}
	t := data.Title
	if t == "" {
		t = "Untitled sermon"
	}
	res.Items = append(res.Items, Item{Kind: KindSermon, Key: key, Changed: d.modified, Title: t, Sample: sample, Folder: folder, Sermon: data})
}

// passageBlock reads a verse quotation block. With a reference Glossa has, it is a quotation to
// fill in later; otherwise the text to keep in its place.
func passageBlock(raw string, res *Result) (*SermonQuote, string) {
	var p passageBlockJSON
	if raw == "" || json.Unmarshal([]byte(raw), &p) != nil || p.Reference == nil {
		return nil, ""
	}
	r, err := ParseRef(p.Reference.Raw)
	if err != nil {
		res.note("verse quotes whose passage Glossa could not read, kept as their reference text")
		return nil, quoteLines(p.Reference.Text)
	}
	q := &SermonQuote{Display: r.String(), OSIS: osisOf(r)}
	if m := tagShortRe.FindStringSubmatch(p.Reference.Raw); m != nil {
		q.Tag = m[1]
	}
	if p.Resource != nil {
		q.Abbr, q.Title, q.ResourceID = p.Resource.Abbr, p.Resource.Title, p.Resource.ID
	}
	return q, ""
}

func osisOf(r verse.Range) string {
	o := r.Start.OSIS()
	if r.End != r.Start {
		o += "-" + r.End.OSIS()
	}
	return o
}

// PlainQuote is a verse quotation kept without its text: the reference, linked, and the Bible's name.
func PlainQuote(q SermonQuote) string {
	line := fmt.Sprintf("> [%s](#passage/%s)", q.Display, q.OSIS)
	if n := q.bibleName(); n != "" {
		line += " (" + strings.NewReplacer("(", "", ")", "").Replace(n) + ")"
	}
	return line
}

func (q SermonQuote) bibleName() string {
	switch {
	case q.Abbr != "":
		return q.Abbr
	case q.Title != "":
		return q.Title
	}
	return strings.ToUpper(q.Tag)
}

func clippingBlock(b sermonBlockRow, text string) string {
	var c clippingBlockJSON
	json.Unmarshal([]byte(b.clipping), &c)
	if text == "" {
		text = strings.TrimSpace(c.SlideText)
	}
	if text == "" {
		return ""
	}
	out := quoteLines(text)
	var parts []string
	if t := strings.Join(strings.Fields(c.Title), " "); t != "" {
		parts = append(parts, t)
	}
	if c.Resource != nil && c.Resource.Title != "" {
		parts = append(parts, c.Resource.Title)
	}
	if len(parts) > 0 {
		out += "\n> — " + strings.Join(parts, ", ")
	}
	return out
}

func quoteLines(md string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

func labelled(label, md string) string {
	if md == "" {
		return ""
	}
	return "**" + label + ":** " + md
}

// listLine puts a list marker before text; later lines of the same item are indented under it.
func listLine(marker, md string) string {
	md = strings.ReplaceAll(strings.TrimSpace(md), "\n\n", "\n")
	lines := strings.Split(md, "\n")
	pad := strings.Repeat(" ", len(marker))
	for i := range lines {
		if i == 0 {
			lines[i] = marker + lines[i]
		} else {
			lines[i] = pad + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// Body writes the sermon's text. quote turns a verse quotation into Markdown, given the text so
// far (a Bible's licence may limit how much of it one document can quote).
func (d *SermonData) Body(quote func(q SermonQuote, sofar string) string) string {
	var b strings.Builder
	for i, bl := range d.Blocks {
		text := bl.Text
		if bl.Quote != nil {
			text = strings.TrimRight(quote(*bl.Quote, b.String()), "\n")
		}
		if text == "" {
			continue
		}
		if i > 0 && b.Len() > 0 {
			if bl.Tight {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(text)
	}
	return b.String()
}
