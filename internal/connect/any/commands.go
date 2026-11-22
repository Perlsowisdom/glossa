package anyapp

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect"
)

var codeRE = regexp.MustCompile(`^[0-9]{4}$`)

// Register adds the any.* commands. svc is the framework service that connect.Register returned.
func (c *Conn) Register(r *command.Registry, svc *connect.Service) {
	s := &commands{c: c, svc: svc, reg: r}
	r.OnClose(c.Close)
	r.Add(
		command.Define("any.pair_start", "Start signing Glossa in to the Any app on this computer. Any shows a 4-digit code; give it to any.pair_finish. Needs Any turned on in Settings → Connections, and the Any app running.", false, s.pairStart),
		command.Define("any.pair_finish", "Finish signing in with the 4-digit code the Any app is showing. The key Any returns goes to the system keychain and is never shown.", false, s.pairFinish),
		command.Define("any.unpair", "Sign Glossa out of Any: the saved key is removed from the system keychain. Links to pages are kept.", false, s.unpair),
		command.Define("any.spaces", "List the spaces in Any, and which one Glossa sends to by default. Needs Any turned on and signed in.", false, s.spaces),
		command.Define("any.set_space", "Choose the space Glossa sends to and imports from by default.", true, s.setSpace),
		command.Define("any.pages", "List the pages in an Any space, newest change first, a page of results at a time. Marks the pages already linked to a note or sermon.", false, s.pages),
		command.Define("any.search", "Search the pages in an Any space by words in their title or text.", false, s.search),
		command.Define("any.send", "Send a note or sermon to Any as a page, with its text as Markdown and its passages as a property. Sending again updates the same page. When both sides changed, nothing is changed: the result says both_changed, and you call again with resolve.", true, s.send),
		command.Define("any.import", "Bring a page from Any in as a new note, or update the note it was imported to before. When both sides changed, nothing is changed: the result says both_changed, and you call again with resolve.", true, s.importPage),
		command.Define("any.sync_status", "Say whether a note or sermon is linked to a page in Any, and whether either side has changed since. Without check it never uses the network.", false, s.syncStatus),
	)
}

type commands struct {
	c   *Conn
	svc *connect.Service
	reg *command.Registry
}

func fail(err error) error {
	var ce *command.Error
	switch {
	case errors.As(err, &ce):
		return err
	case errors.Is(err, ErrNotFound):
		return command.Errorf("not_found", "%s", Explain(err))
	case errors.Is(err, ErrNotPaired):
		return command.Errorf("not_paired", "%s", Explain(err))
	case errors.Is(err, ErrBadCode):
		return command.Errorf("bad_code", "%s", Explain(err))
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return command.Errorf("unavailable", "%s", Explain(err))
}

func needStore(env *command.Env) error {
	if env == nil || env.Store == nil {
		return command.Errorf("unavailable", "no library is open")
	}
	return nil
}

// session is the checks every command that talks to Any makes: the switch is on, the library is
// open and Glossa is signed in.
func (s *commands) session(ctx context.Context, env *command.Env) (*Client, string, error) {
	cfg, err := s.svc.Require(ctx, env, "any")
	if err != nil {
		return nil, "", err
	}
	if err := needStore(env); err != nil {
		return nil, "", err
	}
	if cfg.Key == "" {
		return nil, "", command.Errorf("not_paired", "%s", Explain(ErrNotPaired))
	}
	cl, err := s.c.client()
	if err != nil {
		return nil, "", fail(err)
	}
	return cl, cfg.Key, nil
}

// Pairing ----------------------------------------------------------------------------------------

// PairStartResult carries the challenge to finish with.
type PairStartResult struct {
	ChallengeID string `json:"challenge_id" desc:"Pass to any.pair_finish with the code"`
	Message     string `json:"message"`
}

func (s *commands) pairStart(ctx context.Context, env *command.Env, _ struct{}) (PairStartResult, error) {
	if _, err := s.svc.Require(ctx, env, "any"); err != nil {
		return PairStartResult{}, err
	}
	cl, err := s.c.client()
	if err != nil {
		return PairStartResult{}, fail(err)
	}
	id, err := cl.StartPairing(ctx)
	if err != nil {
		return PairStartResult{}, fail(err)
	}
	return PairStartResult{ChallengeID: id, Message: "Any is showing a 4-digit code. Type it here to finish."}, nil
}

// PairFinishParams is the code the person read from Any.
type PairFinishParams struct {
	ChallengeID string `json:"challenge_id" required:"true" desc:"From any.pair_start"`
	Code        string `json:"code" required:"true" desc:"The 4 digits Any is showing"`
}

// PairResult says Glossa is signed in. The key itself is never returned.
type PairResult struct {
	Paired  bool   `json:"paired"`
	Message string `json:"message"`
}

func (s *commands) pairFinish(ctx context.Context, env *command.Env, p PairFinishParams) (PairResult, error) {
	if _, err := s.svc.Require(ctx, env, "any"); err != nil {
		return PairResult{}, err
	}
	code := strings.TrimSpace(p.Code)
	if !codeRE.MatchString(code) {
		return PairResult{}, command.Errorf("bad_code", "The code is 4 digits.")
	}
	cl, err := s.c.client()
	if err != nil {
		return PairResult{}, fail(err)
	}
	key, err := cl.FinishPairing(ctx, p.ChallengeID, code)
	if err != nil {
		return PairResult{}, fail(err)
	}
	if err := s.svc.SaveSecret("any", key); err != nil {
		return PairResult{}, command.Errorf("failed", "could not save to the system keychain: %v", err)
	}
	return PairResult{Paired: true, Message: "Signed in to Any."}, nil
}

func (s *commands) unpair(ctx context.Context, env *command.Env, _ struct{}) (PairResult, error) {
	if err := s.svc.DropSecret("any"); err != nil {
		return PairResult{}, command.Errorf("failed", "could not update the system keychain: %v", err)
	}
	return PairResult{Message: "Signed out of Any."}, nil
}

// Spaces -----------------------------------------------------------------------------------------

// SpacesResult lists spaces.
type SpacesResult struct {
	Spaces  []Space `json:"spaces"`
	Default string  `json:"default_space_id,omitempty" desc:"The space chosen with any.set_space; when there is only one space it is used without choosing"`
}

func (s *commands) spaces(ctx context.Context, env *command.Env, _ struct{}) (SpacesResult, error) {
	cl, key, err := s.session(ctx, env)
	if err != nil {
		return SpacesResult{}, err
	}
	list, err := cl.Spaces(ctx, key)
	if err != nil {
		return SpacesResult{}, fail(err)
	}
	d, err := loadDefaultSpace(ctx, env)
	if err != nil {
		return SpacesResult{}, err
	}
	res := SpacesResult{Spaces: list}
	for _, sp := range list {
		if sp.ID == d.SpaceID {
			res.Default = d.SpaceID
		}
	}
	if res.Default == "" && len(list) == 1 {
		res.Default = list[0].ID
	}
	return res, nil
}

// SetSpaceParams chooses the default space.
type SetSpaceParams struct {
	SpaceID string `json:"space_id" required:"true" desc:"From any.spaces"`
}

func (s *commands) setSpace(ctx context.Context, env *command.Env, p SetSpaceParams) (SpacesResult, error) {
	cl, key, err := s.session(ctx, env)
	if err != nil {
		return SpacesResult{}, err
	}
	list, err := cl.Spaces(ctx, key)
	if err != nil {
		return SpacesResult{}, fail(err)
	}
	for _, sp := range list {
		if sp.ID == p.SpaceID {
			if err := saveDefaultSpace(ctx, env, defaultSpace{SpaceID: sp.ID, Name: sp.Name}); err != nil {
				return SpacesResult{}, err
			}
			return SpacesResult{Spaces: list, Default: sp.ID}, nil
		}
	}
	return SpacesResult{}, command.Errorf("not_found", "Any has no such space.")
}

// space picks the space to use: the one given, else the default, else the only one there is.
func (s *commands) space(ctx context.Context, env *command.Env, cl *Client, key, given string) (string, error) {
	if given != "" {
		return given, nil
	}
	d, err := loadDefaultSpace(ctx, env)
	if err != nil {
		return "", err
	}
	if d.SpaceID != "" {
		return d.SpaceID, nil
	}
	list, err := cl.Spaces(ctx, key)
	if err != nil {
		return "", fail(err)
	}
	if len(list) == 1 {
		return list[0].ID, nil
	}
	return "", command.Errorf("choose_space", "Choose which Any space to use first, in Settings → Connections.")
}

// Pages ------------------------------------------------------------------------------------------

// PagesParams chooses what to list.
type PagesParams struct {
	SpaceID string `json:"space_id,omitempty" desc:"The space; default is the one chosen in Settings"`
	Query   string `json:"query,omitempty" desc:"Words to find in titles and text"`
	Offset  int    `json:"offset,omitempty" desc:"Where to start; use next_offset from the last page"`
	Limit   int    `json:"limit,omitempty" desc:"Most pages to return, up to 100 (default 50)"`
}

// PageInfo is a page in a listing.
type PageInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Snippet  string `json:"snippet,omitempty"`
	Modified string `json:"modified,omitempty"`
	LinkedTo string `json:"linked_to,omitempty" enum:"note,sermon" desc:"Set when the page is already linked to a note or sermon"`
	ItemID   string `json:"item_id,omitempty" desc:"That note or sermon"`
}

// PagesOut is one page of a listing.
type PagesOut struct {
	SpaceID    string     `json:"space_id"`
	Pages      []PageInfo `json:"pages"`
	Total      int        `json:"total" desc:"Pages in the whole result"`
	NextOffset int        `json:"next_offset,omitempty" desc:"Pass as offset for the next page; absent on the last page"`
}

func (s *commands) pages(ctx context.Context, env *command.Env, p PagesParams) (PagesOut, error) {
	cl, key, err := s.session(ctx, env)
	if err != nil {
		return PagesOut{}, err
	}
	space, err := s.space(ctx, env, cl, key, p.SpaceID)
	if err != nil {
		return PagesOut{}, err
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	r, err := cl.Pages(ctx, key, space, p.Query, p.Offset, p.Limit)
	if err != nil {
		return PagesOut{}, fail(err)
	}
	links, err := linksInSpace(ctx, env, space)
	if err != nil {
		return PagesOut{}, err
	}
	out := PagesOut{SpaceID: space, Pages: make([]PageInfo, 0, len(r.Pages)), Total: r.Total, NextOffset: r.NextOffset}
	for _, pg := range r.Pages {
		pi := PageInfo{ID: pg.ID, Title: pg.Name, Snippet: pg.Snippet, Modified: pg.Modified}
		if l, ok := links[pg.ID]; ok {
			pi.LinkedTo, pi.ItemID = l.Kind, l.ItemID
		}
		out.Pages = append(out.Pages, pi)
	}
	return out, nil
}

// SearchParams is a search.
type SearchParams struct {
	Query   string `json:"query" required:"true" desc:"Words to find in titles and text"`
	SpaceID string `json:"space_id,omitempty" desc:"The space; default is the one chosen in Settings"`
	Offset  int    `json:"offset,omitempty"`
	Limit   int    `json:"limit,omitempty" desc:"Most pages to return, up to 100 (default 50)"`
}

func (s *commands) search(ctx context.Context, env *command.Env, p SearchParams) (PagesOut, error) {
	return s.pages(ctx, env, PagesParams{SpaceID: p.SpaceID, Query: p.Query, Offset: p.Offset, Limit: p.Limit})
}

// Send and import --------------------------------------------------------------------------------

func cleanResolve(r string) (string, error) {
	switch r {
	case "", resolveGlossa, resolveAny, resolveCopy:
		return r, nil
	}
	return "", command.Errorf("bad_params", "resolve must be glossa, any or copy")
}

// SendParams names what to send.
type SendParams struct {
	Kind    string `json:"kind" required:"true" enum:"note,sermon" desc:"A note, or a sermon (also a lesson or study)"`
	ID      string `json:"id" required:"true" desc:"The note's or document's id"`
	SpaceID string `json:"space_id,omitempty" desc:"The space for a first send; default is the one chosen in Settings. Once sent, the same page is updated"`
	Resolve string `json:"resolve,omitempty" enum:"glossa,any,copy" desc:"When both sides changed: keep Glossa's text, keep Any's, or keep both (Any's is saved as a new note)"`
}

func (s *commands) send(ctx context.Context, env *command.Env, p SendParams) (SyncResult, error) {
	resolve, err := cleanResolve(p.Resolve)
	if err != nil {
		return SyncResult{}, err
	}
	cl, key, err := s.session(ctx, env)
	if err != nil {
		return SyncResult{}, err
	}
	it, err := s.load(ctx, env, p.Kind, p.ID)
	if err != nil {
		return SyncResult{}, err
	}
	l, err := linkOfItem(ctx, env, p.Kind, p.ID)
	if err != nil {
		return SyncResult{}, err
	}
	var res SyncResult
	if l == nil {
		space, err := s.space(ctx, env, cl, key, p.SpaceID)
		if err != nil {
			return SyncResult{}, err
		}
		res, err = s.push(ctx, env, cl, key, it, space, nil)
		if err != nil {
			return res, fail(err)
		}
		res.Message = "Sent to Any."
		return res, nil
	}
	res, err = s.reconcile(ctx, env, cl, key, sending, it, l, resolve)
	if err != nil {
		return res, fail(err)
	}
	if res.Message == "" {
		res.Message = map[string]string{"updated": "Updated in Any.", "created": "Sent to Any."}[res.State]
	}
	return res, nil
}

// ImportParams names the page to bring in.
type ImportParams struct {
	SpaceID  string `json:"space_id,omitempty" desc:"The page's space; default is the one chosen in Settings"`
	ObjectID string `json:"object_id" required:"true" desc:"The page, from any.pages"`
	Resolve  string `json:"resolve,omitempty" enum:"glossa,any,copy" desc:"When both sides changed: keep Glossa's text, keep Any's, or keep both"`
}

func (s *commands) importPage(ctx context.Context, env *command.Env, p ImportParams) (SyncResult, error) {
	resolve, err := cleanResolve(p.Resolve)
	if err != nil {
		return SyncResult{}, err
	}
	cl, key, err := s.session(ctx, env)
	if err != nil {
		return SyncResult{}, err
	}
	space, err := s.space(ctx, env, cl, key, p.SpaceID)
	if err != nil {
		return SyncResult{}, err
	}
	l, err := linkOfPage(ctx, env, space, p.ObjectID)
	if err != nil {
		return SyncResult{}, err
	}
	if l != nil {
		it, err := s.load(ctx, env, l.Kind, l.ItemID)
		if err == nil {
			res, err := s.reconcile(ctx, env, cl, key, importing, it, l, resolve)
			if err != nil {
				return res, fail(err)
			}
			return res, nil
		}
		var ce *command.Error
		if !errors.As(err, &ce) || ce.Code != "not_found" {
			return SyncResult{}, err
		}
		// The note was deleted in Glossa: forget the link and bring the page in afresh.
		if err := dropLink(ctx, env, l.Kind, l.ItemID); err != nil {
			return SyncResult{}, err
		}
	}
	page, err := cl.Get(ctx, key, space, p.ObjectID)
	if err != nil {
		return SyncResult{}, fail(err)
	}
	title, body := pageText(page)
	id, err := s.newNote(ctx, env, title, body)
	if err != nil {
		return SyncResult{}, err
	}
	it := item{Kind: kindNote, ID: id, Title: title, Body: body}
	if err := saveLink(ctx, env, link{Kind: kindNote, ItemID: id, SpaceID: space, ObjectID: page.ID,
		GlossaHash: glossaHash(it), AnyHash: anyHash(page), AnyModified: page.Modified}); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{State: "created", Kind: kindNote, ItemID: id, SpaceID: space, ObjectID: page.ID, Title: title,
		Message: "Brought in from Any as a new note."}, nil
}

// Status -----------------------------------------------------------------------------------------

// StatusParams names a note or sermon.
type StatusParams struct {
	Kind  string `json:"kind" required:"true" enum:"note,sermon"`
	ID    string `json:"id" required:"true"`
	Check bool   `json:"check,omitempty" desc:"Also ask Any whether the page changed; needs Any running. Without it only Glossa's side is looked at"`
}

// StatusResult is a link's state.
type StatusResult struct {
	Linked   bool   `json:"linked"`
	SpaceID  string `json:"space_id,omitempty"`
	ObjectID string `json:"object_id,omitempty"`
	State    string `json:"state" enum:"not_linked,in_sync,glossa_changed,any_changed,both_changed,gone,unknown" desc:"unknown: Any could not be asked"`
	Message  string `json:"message,omitempty"`
}

func (s *commands) syncStatus(ctx context.Context, env *command.Env, p StatusParams) (StatusResult, error) {
	if err := needStore(env); err != nil {
		return StatusResult{}, err
	}
	l, err := linkOfItem(ctx, env, p.Kind, p.ID)
	if err != nil {
		return StatusResult{}, err
	}
	if l == nil {
		return StatusResult{State: "not_linked"}, nil
	}
	res := StatusResult{Linked: true, SpaceID: l.SpaceID, ObjectID: l.ObjectID, State: "in_sync"}
	it, err := s.load(ctx, env, p.Kind, p.ID)
	if err != nil {
		return StatusResult{}, err
	}
	glossaChanged := glossaHash(it) != l.GlossaHash
	if glossaChanged {
		res.State = "glossa_changed"
	}
	if !p.Check {
		return res, nil
	}
	cl, key, err := s.session(ctx, env)
	if err != nil {
		return StatusResult{}, err
	}
	page, err := cl.Get(ctx, key, l.SpaceID, l.ObjectID)
	switch {
	case errors.Is(err, ErrNotFound):
		res.State = "gone"
	case err != nil:
		res.State, res.Message = "unknown", Explain(err)
	case page.Modified != l.AnyModified && anyHash(page) != l.AnyHash:
		res.State = "any_changed"
		if glossaChanged {
			res.State = "both_changed"
		}
	}
	return res, nil
}
