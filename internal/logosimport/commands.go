package logosimport

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"unicode"

	"github.com/Perlsowisdom/glossa/internal/command"
)

type service struct {
	r      *command.Registry
	tmpDir string // where the temporary copy of the Logos data goes; empty for the system's
}

// Register adds the Logos import commands to r.
func Register(r *command.Registry) {
	s := &service{r: r}
	r.Add(
		command.Define("logos.find", "Look for the Logos data on this computer. Nothing is read beyond the folder names.", false, s.find),
		command.Define("logos.preview", "Count what could be imported from your own Logos data, with samples and what cannot be placed and why. Reads a temporary copy; changes nothing.", false, s.preview),
		command.Define("logos.import", "Import your notes, highlights, clippings, lists and plans from Logos. Running it again adds only what is new or changed. Logos is never changed.", true, s.importCmd),
		command.Define("logos.library_refresh", "Read the list of books in your Logos library (title, author, type, ID and rights fields only) from a fresh temporary copy of its catalogue, and keep it. Never opens a book. Logos is never changed.", true, s.refreshLibrary),
		command.Define("logos.library", "List the books in your Logos library as last read, with search, sort and a filter, each with a link that opens it in Logos. Public-domain books show a free copy Glossa can get, when one is known.", false, s.library),
		command.Define("logos.runs", "List the earlier imports from Logos, newest first.", false, s.runs),
		command.Define("logos.undo", "Take back one import from Logos: what it added goes to the bin, and later changes you made to it are kept in its history.", true, s.undo),
	)
}

// FindParams names where Logos keeps its data.
type FindParams struct {
	Folder string `json:"folder,omitempty" desc:"A folder holding Logos's Data and Documents folders; leave out to look where Logos keeps them"`
}

// FindResult says whether Logos data was found.
type FindResult struct {
	Found  bool   `json:"found"`
	Folder string `json:"folder,omitempty" desc:"The Logos data folder"`
}

func (s *service) find(_ context.Context, _ *command.Env, p FindParams) (FindResult, error) {
	src, err := Find(p.Folder)
	if err != nil {
		return FindResult{}, nil
	}
	return FindResult{Found: true, Folder: src.Root}, nil
}

// KindLabels are the plain names the interface shows for each kind.
var KindLabels = map[Kind]string{
	KindNote: "Notes", KindHighlight: "Highlights", KindClipping: "Clippings", KindPassageList: "Passage lists",
	KindWordList: "Word lists", KindPrayerList: "Prayer lists", KindReadingPlan: "Reading plans", KindSermon: "Sermons",
}

// Sample is one thing shown in the preview.
type Sample struct {
	Title  string `json:"title"`
	Text   string `json:"text,omitempty"`
	Status string `json:"status" enum:"new,changed,imported" desc:"New, changed in Logos since it was imported, or already imported"`
}

// KindCount is what the preview found of one kind.
type KindCount struct {
	Kind     Kind     `json:"kind"`
	Label    string   `json:"label"`
	Found    int      `json:"found"`
	New      int      `json:"new" desc:"Not imported before"`
	Changed  int      `json:"changed" desc:"Imported before, changed in Logos since"`
	Imported int      `json:"imported" desc:"Imported before and unchanged; these are skipped"`
	Samples  []Sample `json:"samples"`
}

// UnmappedGroup is a number of things left out for one reason.
type UnmappedGroup struct {
	Kind     Kind     `json:"kind"`
	Label    string   `json:"label"`
	Reason   string   `json:"reason"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

// PreviewResult is what logos.preview shows.
type PreviewResult struct {
	Source   string          `json:"source" desc:"The Logos data folder read"`
	Kinds    []KindCount     `json:"kinds"`
	Folders  int             `json:"folders" desc:"Folders that would be made"`
	Unmapped []UnmappedGroup `json:"unmapped" desc:"What cannot be placed in Glossa, grouped by reason"`
	Notes    []string        `json:"notes" desc:"Things worth knowing that are not failures"`
}

// PreviewParams are logos.preview's parameters.
type PreviewParams struct {
	Folder string `json:"folder,omitempty" desc:"A folder holding Logos's Data and Documents folders; leave out to look where Logos keeps them"`
}

// read finds, copies and reads the Logos data, then deletes the copy.
func (s *service) read(folder string) (*Result, string, error) {
	src, err := Find(folder)
	if err != nil {
		return nil, "", command.Errorf("not_found", "no Logos data was found on this computer; choose the folder that holds Logos's Data and Documents folders")
	}
	snap, err := src.Snapshot(s.tmpDir)
	if err != nil {
		return nil, "", fmt.Errorf("copy the Logos data: %w", err)
	}
	defer snap.Close()
	res, err := snap.Read()
	if err != nil {
		return nil, "", fmt.Errorf("read the Logos data: %w", err)
	}
	return res, src.Root, nil
}

func (s *service) preview(ctx context.Context, env *command.Env, p PreviewParams) (PreviewResult, error) {
	res, root, err := s.read(p.Folder)
	if err != nil {
		return PreviewResult{}, err
	}
	var log map[string]logRow
	if env.Store != nil {
		if log, err = loadLog(ctx, env); err != nil {
			return PreviewResult{}, err
		}
	}
	out := buildPreview(res, root, log)
	// Verse quotes need the same Bible installed in Glossa; say now which are missing.
	var mods []bibleMod
	loaded := false
	missing := map[string]int{}
	for _, it := range res.Items {
		if it.Sermon == nil {
			continue
		}
		for _, b := range it.Sermon.Blocks {
			if b.Quote == nil {
				continue
			}
			if !loaded {
				mods, loaded = s.installedBibles(ctx, env), true
			}
			if _, ok := matchBible(mods, *b.Quote); !ok {
				name := b.Quote.bibleName()
				if name == "" {
					name = "a Bible"
				}
				missing[fmt.Sprintf("verse quotes that will be plain quotes with their reference, because %s is not installed in Glossa", name)]++
			}
		}
	}
	for text, n := range missing {
		out.Notes = append(out.Notes, noteLine(text, n))
	}
	sort.Strings(out.Notes)
	return out, nil
}

const sampleCount = 5

func buildPreview(res *Result, root string, log map[string]logRow) PreviewResult {
	out := PreviewResult{Source: root, Kinds: []KindCount{}, Unmapped: []UnmappedGroup{}, Notes: []string{}}
	counts := map[Kind]*KindCount{}
	for _, k := range Kinds {
		counts[k] = &KindCount{Kind: k, Label: KindLabels[k], Samples: []Sample{}}
	}
	folders := map[string]bool{}
	for _, it := range res.Items {
		c := counts[it.Kind]
		c.Found++
		status := "new"
		if row, ok := log[logKey(string(it.Kind), it.Key)]; ok {
			status = "imported"
			if row.changed != it.Changed {
				status = "changed"
			}
		}
		switch status {
		case "new":
			c.New++
			if it.Folder != "" && log[logKey("folder", it.Folder)].objID == "" {
				folders[it.Folder] = true
			}
		case "changed":
			c.Changed++
		default:
			c.Imported++
		}
		if len(c.Samples) < sampleCount {
			c.Samples = append(c.Samples, Sample{Title: it.Title, Text: it.Sample, Status: status})
		}
	}
	for _, k := range Kinds {
		if c := counts[k]; c.Found > 0 || hasUnmapped(res, k) {
			out.Kinds = append(out.Kinds, *c)
		}
	}
	out.Folders = len(folders)
	out.Unmapped = groupUnmapped(res.Unmapped)
	for text, n := range res.Notes {
		out.Notes = append(out.Notes, noteLine(text, n))
	}
	sort.Strings(out.Notes)
	return out
}

func hasUnmapped(res *Result, k Kind) bool {
	for _, u := range res.Unmapped {
		if u.Kind == k {
			return true
		}
	}
	return false
}

// groupUnmapped counts what was left out by kind and reason, biggest first, with a few examples.
func groupUnmapped(list []Unmapped) []UnmappedGroup {
	idx := map[string]int{}
	out := []UnmappedGroup{}
	for _, u := range list {
		k := string(u.Kind) + "\x00" + u.Reason
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, UnmappedGroup{Kind: u.Kind, Label: KindLabels[u.Kind], Reason: u.Reason})
		}
		out[i].Count++
		if u.Title != "" && len(out[i].Examples) < 5 && !contains(out[i].Examples, u.Title) {
			out[i].Examples = append(out[i].Examples, u.Title)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func marshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// noteLine writes a note with how many it applies to: "Highlights that ... (3)".
func noteLine(text string, n int) string {
	r := []rune(text)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return fmt.Sprintf("%s (%d)", string(r), n)
}
