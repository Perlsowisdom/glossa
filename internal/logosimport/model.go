// Package logosimport brings the user's own Logos data into Glossa: notes, highlights, clippings,
// lists and reading plans. It is the only code that reads Logos data (ADR 0007, AGENTS.md).
//
// It reads the user's own data files and the library catalogue's metadata (title, type, rights)
// and nothing else. Every file is opened read-only from a copy made in a temporary folder, so
// Logos is never changed or blocked. It never opens a Logos resource (book) content file and
// never decrypts anything.
package logosimport

import (
	"time"

	"github.com/Perlsowisdom/glossa/internal/verse"
)

// Kind is a kind of thing that can be imported.
type Kind string

const (
	KindNote        Kind = "note"
	KindHighlight   Kind = "highlight"
	KindClipping    Kind = "clipping"
	KindPassageList Kind = "passage_list"
	KindWordList    Kind = "word_list"
	KindPrayerList  Kind = "prayer_list"
	KindReadingPlan Kind = "reading_plan"
	KindSermon      Kind = "sermon"
)

// Kinds lists the kinds in the order they are shown.
var Kinds = []Kind{KindNote, KindHighlight, KindClipping, KindPassageList, KindWordList, KindPrayerList, KindReadingPlan, KindSermon}

// FromLogosTag marks what came from a book in Logos.
const FromLogosTag = "from-logos"

// PrayerFolder is the folder prayer lists go in.
const PrayerFolder = "Prayer lists"

// Folder is a Logos notebook or clippings folder that becomes a Glossa folder.
type Folder struct {
	Key  string // stable Logos ID, prefixed by where it came from
	Name string
}

// Item is one thing to import, already mapped to what Glossa keeps.
type Item struct {
	Kind    Kind
	Key     string // stable Logos ID (with the kind), used to skip it next time
	Changed string // when it last changed in Logos, as Logos wrote it
	Title   string
	Sample  string // a short piece of text for the preview
	Folder  string // key of the Folder it goes in, "" for none

	Note      *NoteData
	Highlight *HighlightData
	List      *ListData
	Plan      *PlanData
	Sermon    *SermonData
}

// NoteData is a note to make.
type NoteData struct {
	Title   string
	Body    string // Markdown
	Tags    []string
	Verses  *verse.Range // the verses it is attached to, if any
	Created int64        // UTC milliseconds; 0 if unknown
	Updated int64
}

// HighlightData is a Bible highlight to make.
type HighlightData struct {
	Range         verse.Range
	Style         string // the Glossa style
	OriginalStyle string // the Logos style it came from
	Created       int64
}

// ListData is a saved list of verses or words.
type ListData struct {
	Kind    string // verses or words
	Name    string
	Entries []ListEntry
	Created int64
	Updated int64
}

// ListEntry is one verse range or word in a list.
type ListEntry struct {
	Verses   *verse.Range
	Word     string
	Lemma    string
	Language string
	Note     string
}

// PlanData is a reading plan with what has been read.
type PlanData struct {
	Name  string
	Start string     // ISO date of the first day
	Days  [][]string // each day's Bible passages
	Done  []int      // 1-based days marked read
	Dates []string   // each day's date in Logos, to find the days of the week it reads on
}

// Unmapped is something that could not be placed in Glossa, with the reason.
type Unmapped struct {
	Kind   Kind
	Key    string
	Title  string // what it is, for the report: a book's title, a note's first words
	Reason string
}

// Result is everything read from one Logos data folder.
type Result struct {
	Folders  []Folder
	Items    []Item
	Unmapped []Unmapped
	// Notes are things worth telling the user that are not failures: how many highlights had to
	// cover whole verses, say. Keyed by sentence.
	Notes map[string]int
}

func (r *Result) note(s string) {
	if r.Notes == nil {
		r.Notes = map[string]int{}
	}
	r.Notes[s]++
}

func (r *Result) skip(k Kind, key, title, reason string) {
	r.Unmapped = append(r.Unmapped, Unmapped{Kind: k, Key: key, Title: title, Reason: reason})
}

func (r *Result) addFolder(f Folder) {
	for _, have := range r.Folders {
		if have.Key == f.Key {
			return
		}
	}
	r.Folders = append(r.Folders, f)
}

// millis reads a time as Logos writes it ("2024-02-16T11:22:49-05:00" or "...Z").
func millis(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// changed is the time something last changed: its changed time, or its created time.
func changed(modified, created string) string {
	if modified != "" {
		return modified
	}
	return created
}
