package readwise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect"
)

// Register adds the readwise.* commands. svc is the framework service that connect.Register returned.
func (c *Conn) Register(r *command.Registry, svc *connect.Service) {
	s := &commands{c: c, svc: svc, reg: r}
	r.OnClose(c.Close)
	r.Add(
		command.Define("readwise.test", "Check the Readwise token. Needs Readwise turned on in Settings → Connections.", false, s.test),
		command.Define("readwise.preview", "Say what an import from Readwise would do, without changing anything: how many sources and highlights, how many match library books, how many notes would be made or updated. Needs Readwise turned on and a token.", false, s.preview),
		command.Define("readwise.import", "Bring in your Readwise highlights as notes, one per source (attached to the library book when the title and author match). The first import is complete; later ones ask only for what changed since the last. A note you edited is not overwritten: it is listed in conflicts, and readwise.resolve settles it.", true, s.importAll),
		command.Define("readwise.status", "Say when Readwise was last imported, how many sources are linked to notes, and which notes wait for a decision. Never uses the network.", false, s.status),
		command.Define("readwise.resolve", "Settle a note you edited and Readwise also changed: use Readwise's text (replacing yours), keep yours (and stop updating it), or keep both (Readwise's text goes in a new note). Never uses the network.", true, s.resolve),
	)
}

type commands struct {
	c   *Conn
	svc *connect.Service
	reg *command.Registry
	mu  sync.Mutex // one import at a time
}

func fail(err error) error {
	var ce *command.Error
	switch {
	case errors.As(err, &ce):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, ErrBadToken):
		return command.Errorf("bad_token", "%s", Explain(err))
	}
	return command.Errorf("unavailable", "%s", Explain(err))
}

func (s *commands) session(ctx context.Context, env *command.Env) (*Client, error) {
	cfg, err := s.svc.Require(ctx, env, "readwise")
	if err != nil {
		return nil, err
	}
	if env == nil || env.Store == nil {
		return nil, command.Errorf("unavailable", "no library is open")
	}
	if cfg.Key == "" {
		return nil, command.Errorf("no_token", "Paste your Readwise token first, in Settings → Connections.")
	}
	return s.c.client(cfg.Key)
}

func (s *commands) test(ctx context.Context, env *command.Env, _ struct{}) (connect.TestResult, error) {
	cfg, err := s.svc.Require(ctx, env, "readwise")
	if err != nil {
		return connect.TestResult{}, err
	}
	res, err := s.c.Test(ctx, cfg)
	if err != nil {
		return res, fail(err)
	}
	return res, nil
}

// Results ----------------------------------------------------------------------------------------

// ImportParams chooses what to bring in.
type ImportParams struct {
	Full bool `json:"full,omitempty" desc:"Ask for everything again instead of only what changed since the last import. Also removes highlights you deleted in Readwise from their notes"`
}

// Conflict is a note you edited that Readwise has since changed.
type Conflict struct {
	SourceID        string `json:"source_id"`
	NoteID          string `json:"note_id"`
	Title           string `json:"title"`
	YourExcerpt     string `json:"your_excerpt"`
	ReadwiseExcerpt string `json:"readwise_excerpt"`
}

// Result is what a preview or an import found or did.
type Result struct {
	Preview           bool       `json:"preview" desc:"True when nothing was changed"`
	Full              bool       `json:"full" desc:"True when everything was asked for, not only what changed since the last import"`
	Since             string     `json:"since,omitempty" desc:"The time asked from, when only changes were asked for"`
	Sources           int        `json:"sources" desc:"Sources with highlights in this answer"`
	Highlights        int        `json:"highlights" desc:"Highlights in this answer (not counting discarded or deleted ones)"`
	Matched           int        `json:"matched" desc:"Sources that match a library book by title and author"`
	Ambiguous         int        `json:"ambiguous" desc:"Sources that fit more than one library book; they are not attached to any"`
	NotesNew          int        `json:"notes_new" desc:"Notes made (or, in a preview, that would be)"`
	NotesNewOnBook    int        `json:"notes_new_on_book" desc:"Of those, notes attached to a library book"`
	NotesUpdated      int        `json:"notes_updated"`
	NotesUnchanged    int        `json:"notes_unchanged"`
	HighlightsNew     int        `json:"highlights_new"`
	HighlightsChanged int        `json:"highlights_changed"`
	HighlightsGone    int        `json:"highlights_gone" desc:"Discarded or deleted in Readwise, so taken out of their notes"`
	Skipped           int        `json:"skipped" desc:"Sources whose note you deleted or chose to keep as your own"`
	Conflicts         []Conflict `json:"conflicts" desc:"Notes you edited that Readwise also changed. Nothing was changed in them; settle each with readwise.resolve"`
	Message           string     `json:"message"`
}

// excerpt keeps the text as Markdown with its line breaks (up to 800 characters); the interface shows it as plain words.
func excerpt(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 800 {
		return string(r[:800])
	}
	return s
}

func (s *commands) preview(ctx context.Context, env *command.Env, p ImportParams) (Result, error) {
	return s.run(ctx, env, p, true)
}

func (s *commands) importAll(ctx context.Context, env *command.Env, p ImportParams) (Result, error) {
	return s.run(ctx, env, p, false)
}

func (s *commands) run(ctx context.Context, env *command.Env, p ImportParams, dry bool) (Result, error) {
	cl, err := s.session(ctx, env)
	if err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	started := s.c.now().UTC()
	last, err := loadLast(ctx, env)
	if err != nil {
		return Result{}, err
	}
	res := Result{Preview: dry, Full: p.Full || last == nil, Conflicts: []Conflict{}}
	since := ""
	if !res.Full {
		since = last.At
		res.Since = since
	}
	sources, err := cl.Export(ctx, since)
	if err != nil {
		return res, fail(err)
	}
	m, err := LoadMatcher(ctx, env)
	if err != nil {
		return res, err
	}
	for _, src := range sources {
		if err := s.one(ctx, env, m, src, &res, dry); err != nil {
			return res, fail(err)
		}
	}
	if !dry {
		if err := saveLast(ctx, env, LastImport{At: started.Format(time.RFC3339), Sources: res.Sources, Highlights: res.Highlights,
			NotesNew: res.NotesNew, NotesUpdated: res.NotesUpdated}); err != nil {
			return res, err
		}
	}
	res.Message = message(res)
	return res, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func message(r Result) string {
	var b strings.Builder
	if r.Preview {
		fmt.Fprintf(&b, "%s with %s", plural(r.Sources, "source", "sources"), plural(r.Highlights, "highlight", "highlights"))
		fmt.Fprintf(&b, "; %d match a book in your library. An import would make %s and update %d.", r.Matched, plural(r.NotesNew, "note", "notes"), r.NotesUpdated)
	} else {
		fmt.Fprintf(&b, "Imported %s from %s. %s made, %d updated, %d already up to date.", plural(r.Highlights, "highlight", "highlights"),
			plural(r.Sources, "source", "sources"), plural(r.NotesNew, "note", "notes"), r.NotesUpdated, r.NotesUnchanged)
	}
	if r.HighlightsGone > 0 {
		fmt.Fprintf(&b, " %s removed from notes.", plural(r.HighlightsGone, "highlight was", "highlights were"))
	}
	if n := len(r.Conflicts); n > 0 {
		fmt.Fprintf(&b, " %s you edited and Readwise also changed was left alone.", plural(n, "note", "notes"))
	}
	return b.String()
}

// one brings in one source.
func (s *commands) one(ctx context.Context, env *command.Env, m *Matcher, src Source, res *Result, dry bool) error {
	if src.IsDeleted {
		return nil
	}
	sid := src.ID.String()
	stored, err := loadHighlights(ctx, env, sid)
	if err != nil {
		return err
	}
	have := map[string]HL{}
	for k, v := range stored {
		have[k] = v
	}
	var d delta
	seen := map[string]bool{}
	live := 0
	for _, h := range src.Highlights {
		id := h.ID.String()
		seen[id] = true
		if h.IsDeleted || h.IsDiscard {
			if _, ok := have[id]; ok {
				delete(have, id)
				d.Remove = append(d.Remove, id)
				res.HighlightsGone++
			}
			continue
		}
		live++
		hl := hlOf(h)
		old, ok := have[id]
		switch {
		case !ok:
			res.HighlightsNew++
		case old.Block != hl.Block || old.Loc != hl.Loc || old.At != hl.At:
			res.HighlightsChanged++
		default:
			continue
		}
		have[id] = hl
		d.Put = append(d.Put, hl)
	}
	if res.Full {
		for id := range stored {
			if !seen[id] {
				delete(have, id)
				d.Remove = append(d.Remove, id)
				res.HighlightsGone++
			}
		}
	}
	if live > 0 {
		res.Sources++
		res.Highlights += live
	}
	meta := metaOf(src)
	link, err := loadSource(ctx, env, sid)
	if err != nil {
		return err
	}
	item, ambiguous := "", false
	if link == nil && strings.EqualFold(src.Category, "books") {
		item, ambiguous = m.Match(src.Title, src.Author)
	}
	if link != nil {
		item = link.ItemID
	}
	if item != "" {
		res.Matched++
	}
	if ambiguous {
		res.Ambiguous++
	}
	title, body := meta.NoteTitle(), bodyOf(meta, list(have))
	switch {
	case link == nil:
		if len(have) == 0 {
			return nil
		}
		res.NotesNew++
		if item != "" {
			res.NotesNewOnBook++
		}
		if dry {
			return nil
		}
		id, err := s.makeNote(ctx, env, title, body, meta.NoteTags(), item)
		if err != nil {
			return err
		}
		d.Source = &source{ID: sid, NoteID: id, ItemID: item, State: stateLinked, BodyHash: hashOf(title, body), Meta: meta}
	case link.State != stateLinked:
		res.Skipped++
		d.Source = &source{ID: sid, NoteID: link.NoteID, ItemID: link.ItemID, State: link.State, BodyHash: link.BodyHash, Meta: meta}
	default:
		cur, err := readNote(ctx, env, link.NoteID)
		if err != nil {
			return err
		}
		next := *link
		next.Meta = meta
		switch {
		case !cur.Exists:
			res.Skipped++
			next.State = stateRemoved
		case hashOf(title, body) == link.BodyHash:
			res.NotesUnchanged++
		case hashOf(cur.Title, cur.Body) != link.BodyHash:
			res.Conflicts = append(res.Conflicts, conflictOf(link.ID, link.NoteID, cur, title, body))
		default:
			res.NotesUpdated++
			if !dry {
				if err := s.rewrite(ctx, env, link.NoteID, title, body, meta.NoteTags(), cur.Tags); err != nil {
					return err
				}
				next.BodyHash = hashOf(title, body)
			}
		}
		if !dry {
			d.Source = &next
		}
	}
	if dry {
		return nil
	}
	return apply(ctx, env, sid, d)
}

func conflictOf(sid, noteID string, cur noteState, title, body string) Conflict {
	mine, theirs := fromDifference(cur.Body, body)
	return Conflict{SourceID: sid, NoteID: noteID, Title: cur.Title, YourExcerpt: excerpt(mine), ReadwiseExcerpt: excerpt(theirs)}
}

// fromDifference cuts both texts at the line where they first differ, so the excerpts show what is different.
func fromDifference(a, b string) (string, string) {
	ra, rb := []rune(a), []rune(b)
	i := 0
	for i < len(ra) && i < len(rb) && ra[i] == rb[i] {
		i++
	}
	for i > 0 && ra[i-1] != '\n' {
		i--
	}
	pre := ""
	if i > 0 {
		pre = "… "
	}
	return pre + string(ra[i:]), pre + string(rb[i:])
}

// Writing notes, through the note commands ------------------------------------------------------------

func (s *commands) call(ctx context.Context, env *command.Env, id string, params, out any) error {
	raw, _ := json.Marshal(params)
	res, err := s.reg.Run(ctx, env, id, raw)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	b, _ := json.Marshal(res)
	return json.Unmarshal(b, out)
}

// makeNote creates a note, attached to the library book when there is one.
func (s *commands) makeNote(ctx context.Context, env *command.Env, title, body string, tags []string, item string) (string, error) {
	params := map[string]any{"title": title, "body": body, "tags": tags}
	if item != "" {
		// Readwise knows a place as a Kindle location, not a place in this book, so the note goes to the
		// book as a whole: the first section, where the book's notes panel lists it.
		params["item_id"], params["locator"] = item, map[string]any{"section": 1}
	}
	var n struct {
		ID string `json:"id"`
	}
	if err := s.call(ctx, env, "note.create", params, &n); err != nil {
		return "", err
	}
	return n.ID, nil
}

// rewrite replaces a note's title and text, adding any tags it lacks and keeping the ones the person added.
func (s *commands) rewrite(ctx context.Context, env *command.Env, id, title, body string, want, have []string) error {
	tags := append([]string(nil), have...)
	for _, t := range want {
		if !contains(have, t) && len(tags) < maxTags {
			tags = append(tags, t)
		}
	}
	return s.call(ctx, env, "note.update", map[string]any{"id": id, "title": title, "body": body, "tags": tags}, nil)
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Status and resolving -----------------------------------------------------------------------------

// StatusResult is what Glossa knows without asking Readwise.
type StatusResult struct {
	LastImport *LastImport `json:"last_import,omitempty" desc:"Absent until the first import on this computer"`
	Linked     int         `json:"linked" desc:"Sources that have a note"`
	Conflicts  []Conflict  `json:"conflicts" desc:"Notes you edited that Readwise also changed; settle each with readwise.resolve"`
}

func (s *commands) status(ctx context.Context, env *command.Env, _ struct{}) (StatusResult, error) {
	if env == nil || env.Store == nil {
		return StatusResult{}, command.Errorf("unavailable", "no library is open")
	}
	last, err := loadLast(ctx, env)
	if err != nil {
		return StatusResult{}, err
	}
	srcs, err := loadSources(ctx, env)
	if err != nil {
		return StatusResult{}, err
	}
	out := StatusResult{LastImport: last, Conflicts: []Conflict{}}
	for _, src := range srcs {
		if src.State != stateLinked {
			continue
		}
		out.Linked++
		cur, err := readNote(ctx, env, src.NoteID)
		if err != nil {
			return out, err
		}
		if !cur.Exists || hashOf(cur.Title, cur.Body) == src.BodyHash {
			continue
		}
		hls, err := loadHighlights(ctx, env, src.ID)
		if err != nil {
			return out, err
		}
		title, body := src.Meta.NoteTitle(), bodyOf(src.Meta, list(hls))
		if hashOf(title, body) != src.BodyHash {
			out.Conflicts = append(out.Conflicts, conflictOf(src.ID, src.NoteID, cur, title, body))
		}
	}
	return out, nil
}

// ResolveParams settles one note.
type ResolveParams struct {
	SourceID string `json:"source_id" required:"true" desc:"From a conflict in readwise.import or readwise.status"`
	Resolve  string `json:"resolve" required:"true" enum:"readwise,mine,both" desc:"readwise: replace the note with Readwise's text; mine: keep your note and stop updating it; both: keep your note as it is and put Readwise's text in a new note that is kept up to date"`
}

// ResolveResult says what was done.
type ResolveResult struct {
	NoteID  string `json:"note_id" desc:"The note that is now kept up to date from Readwise (empty with mine)"`
	Message string `json:"message"`
}

func (s *commands) resolve(ctx context.Context, env *command.Env, p ResolveParams) (ResolveResult, error) {
	if env == nil || env.Store == nil {
		return ResolveResult{}, command.Errorf("unavailable", "no library is open")
	}
	if p.Resolve != "readwise" && p.Resolve != "mine" && p.Resolve != "both" {
		return ResolveResult{}, command.Errorf("bad_params", "resolve must be readwise, mine or both")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := loadSource(ctx, env, p.SourceID)
	if err != nil {
		return ResolveResult{}, err
	}
	if src == nil || src.State != stateLinked {
		return ResolveResult{}, command.Errorf("not_found", "That source has no note waiting for a decision.")
	}
	cur, err := readNote(ctx, env, src.NoteID)
	if err != nil {
		return ResolveResult{}, err
	}
	hls, err := loadHighlights(ctx, env, src.ID)
	if err != nil {
		return ResolveResult{}, err
	}
	title, body := src.Meta.NoteTitle(), bodyOf(src.Meta, list(hls))
	next := *src
	var out ResolveResult
	switch p.Resolve {
	case "mine":
		next.State = stateKept
		out.Message = "Your note is kept as it is, and Readwise will not change it again."
	case "readwise":
		if !cur.Exists {
			return ResolveResult{}, command.Errorf("not_found", "The note is gone.")
		}
		if err := s.rewrite(ctx, env, src.NoteID, title, body, src.Meta.NoteTags(), cur.Tags); err != nil {
			return ResolveResult{}, err
		}
		next.BodyHash, out.NoteID = hashOf(title, body), src.NoteID
		out.Message = "The note now has Readwise's text."
	case "both":
		id, err := s.makeNote(ctx, env, title, body, src.Meta.NoteTags(), src.ItemID)
		if err != nil {
			return ResolveResult{}, err
		}
		next.NoteID, next.BodyHash, out.NoteID = id, hashOf(title, body), id
		out.Message = "Your note is untouched. Readwise's text is in a new note, which is kept up to date."
	}
	return out, apply(ctx, env, src.ID, delta{Source: &next})
}
