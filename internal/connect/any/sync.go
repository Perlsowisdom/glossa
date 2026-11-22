package anyapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
)

// Passage property: the passages of a note or sermon, in plain references, kept in a text property
// Glossa makes in the space the first time it sends.
const (
	passagesKey  = "glossa_passages"
	passagesName = "Passages"
)

// item is a note or sermon as it is sent.
type item struct {
	Kind, ID, Title, Body, Passages string
}

// Resolutions the person can choose when both sides changed.
const (
	resolveGlossa = "glossa" // keep Glossa's text; the page in Any is replaced
	resolveAny    = "any"    // keep Any's text; the note or sermon is replaced
	resolveCopy   = "copy"   // keep both: Any's text is saved as a new note, then Glossa's goes to Any
)

// Conflict says what each side holds, so the person can choose.
type Conflict struct {
	GlossaTitle   string `json:"glossa_title"`
	GlossaExcerpt string `json:"glossa_excerpt"`
	AnyTitle      string `json:"any_title"`
	AnyExcerpt    string `json:"any_excerpt"`
}

// SyncResult is what sending or importing did.
type SyncResult struct {
	State    string    `json:"state" enum:"created,updated,unchanged,both_changed,any_changed,gone" desc:"What happened. both_changed and any_changed send or change nothing; ask the person and call again with resolve"`
	Kind     string    `json:"kind" enum:"note,sermon"`
	ItemID   string    `json:"item_id" desc:"The note or sermon"`
	SpaceID  string    `json:"space_id,omitempty"`
	ObjectID string    `json:"object_id,omitempty" desc:"The page in Any"`
	Title    string    `json:"title,omitempty"`
	Passages bool      `json:"passages_sent,omitempty" desc:"True when the passages went to Any as a property"`
	CopyID   string    `json:"copy_id,omitempty" desc:"With resolve copy: the new note holding Any's version"`
	Message  string    `json:"message" desc:"One plain sentence for the person"`
	Conflict *Conflict `json:"conflict,omitempty"`
}

func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 240 {
		return string(r[:240]) + "…"
	}
	return s
}

// reading and writing Glossa's side, through its own commands ----------------------------------------

func (s *commands) call(ctx context.Context, env *command.Env, id string, params, out any) error {
	raw, _ := json.Marshal(params)
	res, err := s.reg.Run(ctx, env, id, raw)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(res)
	return json.Unmarshal(b, out)
}

type refOut struct {
	Display string `json:"display"`
}

func (s *commands) load(ctx context.Context, env *command.Env, kind, id string) (item, error) {
	switch kind {
	case kindNote:
		var n struct {
			Title string  `json:"title"`
			Body  string  `json:"body"`
			Ref   *refOut `json:"ref"`
		}
		if err := s.call(ctx, env, "note.get", map[string]string{"id": id}, &n); err != nil {
			return item{}, err
		}
		it := item{Kind: kind, ID: id, Title: n.Title, Body: n.Body}
		if n.Ref != nil {
			it.Passages = n.Ref.Display
		}
		return it, nil
	case kindSermon:
		var d struct {
			Title    string   `json:"title"`
			Body     string   `json:"body"`
			Passages []refOut `json:"passages"`
		}
		if err := s.call(ctx, env, "document.get", map[string]string{"id": id}, &d); err != nil {
			return item{}, err
		}
		it := item{Kind: kind, ID: id, Title: d.Title, Body: d.Body}
		var refs []string
		for _, p := range d.Passages {
			refs = append(refs, p.Display)
		}
		it.Passages = strings.Join(refs, "; ")
		return it, nil
	}
	return item{}, command.Errorf("bad_params", "kind must be note or sermon")
}

// replace puts Any's text into an existing note or sermon.
func (s *commands) replace(ctx context.Context, env *command.Env, kind, id, title, body string) error {
	params := map[string]any{"id": id, "title": title, "body": body}
	var out json.RawMessage
	if kind == kindNote {
		return s.call(ctx, env, "note.update", params, &out)
	}
	if strings.TrimSpace(title) == "" {
		delete(params, "title")
	}
	return s.call(ctx, env, "document.update", params, &out)
}

// newNote makes a note from a page's text and returns its ID.
func (s *commands) newNote(ctx context.Context, env *command.Env, title, body string) (string, error) {
	var n struct {
		ID string `json:"id"`
	}
	if err := s.call(ctx, env, "note.create", map[string]any{"title": title, "body": body}, &n); err != nil {
		return "", err
	}
	return n.ID, nil
}

// pageText is a page's title and text as a note would hold them. Any may start the text with the
// title as a heading; the note already has the title, so that heading is dropped.
func pageText(p Page) (title, body string) {
	title, body = strings.TrimSpace(p.Name), p.Markdown
	if first, rest, ok := strings.Cut(body, "\n"); ok && title != "" && strings.TrimSpace(strings.TrimLeft(first, "# ")) == title && strings.HasPrefix(first, "#") {
		body = strings.TrimLeft(rest, "\n")
	}
	return title, strings.TrimSpace(body)
}

// the sync itself ----------------------------------------------------------------------------------

type direction int

const (
	sending direction = iota
	importing
)

func (s *commands) props(ctx context.Context, cl *Client, key, space string, it item) ([]Prop, bool) {
	ok, err := cl.EnsureProperty(ctx, key, space, passagesKey, passagesName)
	if !ok || err != nil {
		return nil, false
	}
	return []Prop{{Key: passagesKey, Text: it.Passages}}, true
}

// push writes Glossa's text to Any (making the page when there is no link) and records the link.
func (s *commands) push(ctx context.Context, env *command.Env, cl *Client, key string, it item, space string, l *link) (SyncResult, error) {
	props, sent := s.props(ctx, cl, key, space, it)
	if it.Passages == "" && l == nil {
		props = nil
	}
	res := SyncResult{Kind: it.Kind, ItemID: it.ID, SpaceID: space, Title: it.Title, Passages: sent && len(props) > 0}
	var page Page
	var err error
	if l == nil {
		page, err = cl.Create(ctx, key, space, it.Title, it.Body, props)
		res.State = "created"
	} else {
		err = cl.Update(ctx, key, space, l.ObjectID, it.Title, it.Body, props)
		page, res.State = Page{ID: l.ObjectID}, "updated"
	}
	if err != nil {
		return res, err
	}
	// Read the page back: its modified time and text, as Any holds them, are the new baseline.
	back, err := cl.Get(ctx, key, space, page.ID)
	if err != nil {
		return res, err
	}
	res.ObjectID = back.ID
	return res, saveLink(ctx, env, link{Kind: it.Kind, ItemID: it.ID, SpaceID: space, ObjectID: back.ID,
		GlossaHash: glossaHash(it), AnyHash: anyHash(back), AnyModified: back.Modified})
}

// pull puts Any's text into Glossa and records the link.
func (s *commands) pull(ctx context.Context, env *command.Env, it item, page Page, space string) (SyncResult, error) {
	title, body := pageText(page)
	if err := s.replace(ctx, env, it.Kind, it.ID, title, body); err != nil {
		return SyncResult{}, err
	}
	now, err := s.load(ctx, env, it.Kind, it.ID)
	if err != nil {
		return SyncResult{}, err
	}
	err = saveLink(ctx, env, link{Kind: it.Kind, ItemID: it.ID, SpaceID: space, ObjectID: page.ID,
		GlossaHash: glossaHash(now), AnyHash: anyHash(page), AnyModified: page.Modified})
	return SyncResult{State: "updated", Kind: it.Kind, ItemID: it.ID, SpaceID: space, ObjectID: page.ID, Title: now.Title}, err
}

func conflictOf(it item, page Page) *Conflict {
	title, body := pageText(page)
	return &Conflict{GlossaTitle: it.Title, GlossaExcerpt: excerpt(it.Body), AnyTitle: title, AnyExcerpt: excerpt(body)}
}

// reconcile handles an item that is already linked to a page: it works out which side changed since
// the last sync, and either does the obvious thing or reports a conflict.
func (s *commands) reconcile(ctx context.Context, env *command.Env, cl *Client, key string, dir direction, it item, l *link, resolve string) (SyncResult, error) {
	base := SyncResult{Kind: it.Kind, ItemID: it.ID, SpaceID: l.SpaceID, ObjectID: l.ObjectID, Title: it.Title}
	page, err := cl.Get(ctx, key, l.SpaceID, l.ObjectID)
	if errors.Is(err, ErrNotFound) {
		return s.pageGone(ctx, env, cl, key, dir, it, l, base)
	}
	if err != nil {
		return base, err
	}
	glossaChanged := glossaHash(it) != l.GlossaHash
	anyChanged := page.Modified != l.AnyModified && anyHash(page) != l.AnyHash
	switch {
	case !glossaChanged && !anyChanged:
		base.State, base.Message = "unchanged", "Already up to date."
		return base, nil
	case glossaChanged && anyChanged && resolve == "":
		base.State, base.Conflict = "both_changed", conflictOf(it, page)
		base.Message = "This changed in both Glossa and Any since they were last in step. Nothing was changed."
		return base, nil
	case anyChanged && !glossaChanged && dir == sending && resolve == "":
		base.State, base.Conflict = "any_changed", conflictOf(it, page)
		base.Message = "The page in Any has newer changes than Glossa. Nothing was changed."
		return base, nil
	case glossaChanged && !anyChanged && dir == importing && resolve == "":
		base.State = "unchanged"
		base.Message = "The page in Any has not changed. Your changes in Glossa have not been sent."
		return base, nil
	}
	switch resolve {
	case resolveAny:
		return s.pull(ctx, env, it, page, l.SpaceID)
	case resolveCopy:
		title, body := pageText(page)
		copyID, err := s.newNote(ctx, env, title+" (from Any)", body)
		if err != nil {
			return base, err
		}
		res, err := s.push(ctx, env, cl, key, it, l.SpaceID, l)
		res.CopyID = copyID
		return res, err
	case resolveGlossa:
		return s.push(ctx, env, cl, key, it, l.SpaceID, l)
	}
	// No conflict and no choice needed: send Glossa's changes, or bring Any's in.
	if dir == importing {
		return s.pull(ctx, env, it, page, l.SpaceID)
	}
	return s.push(ctx, env, cl, key, it, l.SpaceID, l)
}

// pageGone: the page was deleted in Any. Sending makes a new page; importing has nothing to bring in.
func (s *commands) pageGone(ctx context.Context, env *command.Env, cl *Client, key string, dir direction, it item, l *link, base SyncResult) (SyncResult, error) {
	if dir == sending {
		if err := dropLink(ctx, env, it.Kind, it.ID); err != nil {
			return base, err
		}
		res, err := s.push(ctx, env, cl, key, it, l.SpaceID, nil)
		res.Message = "The page had been deleted in Any, so a new page was made."
		return res, err
	}
	base.State = "gone"
	base.Message = "That page was deleted in Any."
	return base, nil
}
