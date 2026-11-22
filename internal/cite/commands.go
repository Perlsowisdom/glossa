package cite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/csl"
	"github.com/Perlsowisdom/glossa/internal/store"
)

const (
	defaultStyleKey = "cite.style"      // the style for documents that do not choose one
	docStylePrefix  = "cite.doc_style." // + the note's or document's id
)

// Register adds the cite.* commands. userStyles is the folder of downloaded styles.
func Register(r *command.Registry, userStyles string) *Resolver {
	rs := NewResolver(r, userStyles)
	d := NewDownloader(userStyles)
	r.OnClose(d.Close)
	s := &service{rs: rs}
	r.Add(
		command.Define("cite.sources", "Search what can be cited: library books, installed Bibles, Zotero items (when Zotero is on, and the ones kept from earlier citations) and manual entries.", false, s.sources),
		command.Define("cite.keep", "Make sure a source can be cited later without the program it came from (a Zotero item is kept in the library). Call it when a citation is added.", true, s.keep),
		command.Define("cite.manual_save", "Add a source by hand, or change one you added. Returns the source to cite.", true, s.manualSave),
		command.Define("cite.manual_list", "List the sources added by hand.", false, s.manualList),
		command.Define("cite.passage", "Check a Bible passage typed as a locator and write it with SBL abbreviations (Rom 8:28-30).", false, s.passage),
		command.Define("cite.render", "Render citations, given in document order, and their bibliography in a citation style. A note style numbers the footnotes and works out ibid. and short forms; the others give in-text citations.", false, s.render),
		command.Define("cite.short", "Give each citation as a short label (for a chip in the editor).", false, s.short),
		command.Define("cite.document", "Find the citations in a document's Markdown and render them and the bibliography in its style.", false, s.document),
		command.Define("cite.style", "Say which style a note or document uses (its own choice, else the default).", false, s.style),
		command.Define("cite.set_style", "Choose the citation style of a note or document, or, with no id, the default for all of them.", true, s.setStyle),
		command.Define("cite.styles_online", "Say whether downloading more citation styles is turned on, and exactly what it sends and where. Never uses the network.", false, d.online),
		command.Define("cite.styles_enable", "Turn on downloading citation styles from the Citation Style Language project's repository.", true, d.setOnline(true)),
		command.Define("cite.styles_disable", "Turn off downloading citation styles. Downloaded styles stay.", true, d.setOnline(false)),
		command.Define("cite.styles_search", "Search the repository's styles by name. Needs downloading turned on.", false, d.search),
		command.Define("cite.styles_download", "Download a style (and the style it follows) into the styles folder, keeping its licence. Needs downloading turned on.", true, d.download),
		command.Define("cite.styles_remove", "Remove a style you downloaded.", true, d.remove),
	)
	return rs
}

type service struct{ rs *Resolver }

// SourcesParams is a search for sources.
type SourcesParams struct {
	Query string `json:"query,omitempty" desc:"Words in the title or author; leave out to list some of each kind"`
	Limit int    `json:"limit,omitempty" desc:"Most of each kind to return (default 30)"`
}

func (s *service) sources(ctx context.Context, env *command.Env, p SourcesParams) (SearchResult, error) {
	return s.rs.Search(ctx, env, p.Query, p.Limit), nil
}

// KeyParams names a source.
type KeyParams struct {
	Key string `json:"key" required:"true" desc:"A source key: lib:..., bible:..., zotero:... or manual:..."`
}

func (s *service) keep(ctx context.Context, env *command.Env, p KeyParams) (struct{}, error) {
	if KindOf(p.Key) == "" {
		return struct{}{}, command.Errorf("bad_params", "%q is not a source key", p.Key)
	}
	return struct{}{}, s.rs.Keep(ctx, env, p.Key)
}

func (s *service) manualSave(ctx context.Context, env *command.Env, p Manual) (Source, error) {
	return SaveManual(ctx, env, p)
}

func (s *service) manualList(ctx context.Context, env *command.Env, _ struct{}) ([]Source, error) {
	return ManualEntries(ctx, env), nil
}

// PassageParams is a typed passage.
type PassageParams struct {
	Text string `json:"text" required:"true" desc:"e.g. Romans 8:28-30"`
}

// PassageResult is the passage as cited.
type PassageResult struct {
	OK      bool   `json:"ok"`
	Passage string `json:"passage,omitempty" desc:"With the SBL abbreviation, e.g. Rom 8:28-30"`
}

func (s *service) passage(_ context.Context, _ *command.Env, p PassageParams) (PassageResult, error) {
	got, ok := Passage(p.Text)
	return PassageResult{OK: ok, Passage: got}, nil
}

// RenderParams are the citations to render.
type RenderParams struct {
	Style string `json:"style,omitempty" desc:"A style id from csl.styles; default: the one chosen in Settings"`
	Cites []Cite `json:"cites" required:"true" desc:"The citations, in document order"`
}

func (s *service) styleOrDefault(ctx context.Context, env *command.Env, id string) string {
	if id != "" {
		return id
	}
	got, _ := readStyle(ctx, env, defaultStyleKey)
	if got == "" {
		return DefaultStyle
	}
	return got
}

func (s *service) render(ctx context.Context, env *command.Env, p RenderParams) (Result, error) {
	return s.rs.Render(ctx, env, s.styleOrDefault(ctx, env, p.Style), p.Cites)
}

// ShortResult is the labels.
type ShortResult struct {
	Labels []string `json:"labels" desc:"One per citation, in order"`
}

func (s *service) short(ctx context.Context, env *command.Env, p RenderParams) (ShortResult, error) {
	labels, err := s.rs.Short(ctx, env, s.styleOrDefault(ctx, env, p.Style), p.Cites)
	return ShortResult{Labels: labels}, err
}

// DocumentParams is a document's text.
type DocumentParams struct {
	Markdown string `json:"markdown" required:"true" desc:"The note's or document's Markdown"`
	ID       string `json:"id,omitempty" desc:"The note's or document's id, to use the style chosen for it"`
	Style    string `json:"style,omitempty" desc:"A style id; default: the document's, else the one chosen in Settings"`
}

func (s *service) document(ctx context.Context, env *command.Env, p DocumentParams) (Result, error) {
	style := p.Style
	if style == "" && p.ID != "" {
		style, _ = readStyle(ctx, env, docStylePrefix+p.ID)
	}
	return s.rs.Render(ctx, env, s.styleOrDefault(ctx, env, style), Extract(p.Markdown))
}

// StyleResult says which style applies.
type StyleResult struct {
	Style   string `json:"style" desc:"The style that applies"`
	Title   string `json:"title,omitempty"`
	Own     bool   `json:"own" desc:"True when this document chose it; false when it follows the default"`
	Default string `json:"default" desc:"The default style"`
}

// StyleParams names a document.
type StyleParams struct {
	ID string `json:"id,omitempty" desc:"A note's or document's id; leave out for the default"`
}

func readStyle(ctx context.Context, env *command.Env, key string) (string, error) {
	if env == nil || env.Store == nil {
		return "", nil
	}
	var raw string
	err := env.Store.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var id string
	_ = json.Unmarshal([]byte(raw), &id)
	return id, nil
}

// StyleFor is the style a note or document uses: its own choice, else the default, else SBL. A
// choice naming a style that has since been removed falls back too.
func (r *Resolver) StyleFor(ctx context.Context, env *command.Env, docID string) string {
	var tries []string
	if docID != "" {
		own, _ := readStyle(ctx, env, docStylePrefix+docID)
		tries = append(tries, own)
	}
	def, _ := readStyle(ctx, env, defaultStyleKey)
	tries = append(tries, def, DefaultStyle)
	for _, id := range tries {
		if id == "" {
			continue
		}
		if _, err := csl.StyleByID(r.userStyles, id); err == nil {
			return id
		}
	}
	return DefaultStyle
}

func (s *service) style(ctx context.Context, env *command.Env, p StyleParams) (StyleResult, error) {
	def := s.rs.StyleFor(ctx, env, "")
	res := StyleResult{Default: def, Style: def}
	if p.ID != "" {
		if own, _ := readStyle(ctx, env, docStylePrefix+p.ID); own != "" {
			if _, err := csl.StyleByID(s.rs.userStyles, own); err == nil {
				res.Style, res.Own = own, true
			}
		}
	}
	if st, err := csl.StyleByID(s.rs.userStyles, res.Style); err == nil {
		res.Title = st.Title
	}
	return res, nil
}

// SetStyleParams chooses a style.
type SetStyleParams struct {
	ID    string `json:"id,omitempty" desc:"A note's or document's id; leave out to set the default for all"`
	Style string `json:"style,omitempty" desc:"A style id from csl.styles; leave out with an id to go back to the default"`
}

func (s *service) setStyle(ctx context.Context, env *command.Env, p SetStyleParams) (StyleResult, error) {
	if p.Style != "" {
		if _, err := csl.StyleByID(s.rs.userStyles, p.Style); err != nil {
			return StyleResult{}, command.Errorf("not_found", "no citation style %q (see csl.styles)", p.Style)
		}
	}
	if p.ID == "" && p.Style == "" {
		return StyleResult{}, command.Errorf("bad_params", "say which style to make the default")
	}
	key := defaultStyleKey
	if p.ID != "" {
		if strings.ContainsAny(p.ID, " /\\") {
			return StyleResult{}, command.Errorf("bad_params", "bad id")
		}
		key = docStylePrefix + p.ID
	}
	raw, _ := json.Marshal(p.Style)
	err := env.Store.Write(ctx, "cite.set_style", func(tx *store.Tx) error {
		if p.Style == "" {
			if _, err := tx.Get("settings", store.Row{"key": key}); errors.Is(err, store.ErrNotFound) {
				return nil
			}
			return tx.Delete("settings", store.Row{"key": key})
		}
		return putSetting(tx, key, string(raw))
	})
	if err != nil {
		return StyleResult{}, err
	}
	return s.style(ctx, env, StyleParams{ID: p.ID})
}
