package obsidian

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/verse"
)

// Reading and writing Glossa's side goes through its own commands, as every other connection does.

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

type refOut struct {
	Display string `json:"display"`
}

type noteOut struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Ref     *refOut  `json:"ref"`
	Tags    []string `json:"tags"`
	Created int64    `json:"created_at"`
	Updated int64    `json:"updated_at"`
}

func (n noteOut) item() item {
	it := item{Kind: kindNote, ID: n.ID, Title: n.Title, Body: n.Body, Tags: n.Tags, Created: n.Created, Updated: n.Updated}
	if n.Ref != nil && n.Ref.Display != "" {
		it.Passages = []string{n.Ref.Display}
	}
	return it
}

type docOut struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Passages   []refOut `json:"passages"`
	Tags       []string `json:"tags"`
	PreachedOn string   `json:"preached_on"`
	Theme      string   `json:"theme"`
	Created    int64    `json:"created_at"`
	Updated    int64    `json:"updated_at"`
}

func (d docOut) item() item {
	it := item{Kind: kindSermon, ID: d.ID, DocKind: d.Kind, Title: d.Title, Body: d.Body, Tags: d.Tags, Created: d.Created, Updated: d.Updated,
		Preached: d.PreachedOn, Theme: d.Theme}
	for _, p := range d.Passages {
		it.Passages = append(it.Passages, p.Display)
	}
	return it
}

func (s *commands) load(ctx context.Context, env *command.Env, kind, id string) (item, error) {
	switch kind {
	case kindNote:
		var n noteOut
		if err := s.call(ctx, env, "note.get", map[string]string{"id": id}, &n); err != nil {
			return item{}, err
		}
		return n.item(), nil
	case kindSermon:
		var d docOut
		if err := s.call(ctx, env, "document.get", map[string]string{"id": id}, &d); err != nil {
			return item{}, err
		}
		return d.item(), nil
	}
	return item{}, command.Errorf("bad_params", "kind must be note or sermon")
}

// loadAll reads every note, and with sermons every sermon, lesson and study.
func (s *commands) loadAll(ctx context.Context, env *command.Env, notes, sermons bool) ([]item, error) {
	var out []item
	if notes {
		var ns []noteOut
		if err := s.call(ctx, env, "note.list", map[string]any{"limit": 1000000}, &ns); err != nil {
			return nil, err
		}
		for _, n := range ns {
			out = append(out, n.item())
		}
	}
	if sermons {
		var ds []docOut
		if err := s.call(ctx, env, "document.list", map[string]any{"limit": 1000000}, &ds); err != nil {
			return nil, err
		}
		for _, d := range ds {
			out = append(out, d.item())
		}
	}
	return out, nil
}

// Existence of a note or sermon in Glossa.
const (
	absent  = "none"
	present = "live"
	binned  = "deleted"
)

func (s *commands) existence(ctx context.Context, env *command.Env, kind, id string) (string, error) {
	table := "notes"
	if kind == kindSermon {
		table = "documents"
	}
	var deleted sql.NullInt64
	err := env.Store.QueryRow(ctx, `SELECT deleted_at FROM `+table+` WHERE id = ?`, id).Scan(&deleted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return absent, nil
	case err != nil:
		return "", err
	case deleted.Valid:
		return binned, nil
	}
	return present, nil
}

// parseable is the passages Glossa can read, as references.
func parseable(in []string) (ok, bad []string) {
	for _, p := range in {
		if r, err := verse.Parse(p); err == nil {
			ok = append(ok, r.String())
		} else {
			bad = append(bad, p)
		}
	}
	return ok, bad
}

// glossaTags maps a file's tags to Glossa's: a tag that is an existing tag with its spaces turned into hyphens
// stays the existing one, and the rest are cut to what a note can hold.
func glossaTags(file, current []string) []string {
	have := map[string]string{}
	for _, t := range current {
		have[tagKey(t)] = t
	}
	var out []string
	for _, k := range file {
		if t, ok := have[k]; ok {
			k = t
		}
		if utf8.RuneCountInString(k) > maxTagLen || len(out) >= maxTags {
			continue
		}
		out = append(out, k)
	}
	return out
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// newFrom makes a note or sermon from a file's text and returns its ID, plus passages it could not attach.
func (s *commands) newFrom(ctx context.Context, env *command.Env, fv fileView, body, titleSuffix string) (id string, notAttached []string, err error) {
	title := cut(strings.TrimSpace(fv.Title), maxTitle) + titleSuffix
	ok, bad := parseable(fv.Passages)
	var out struct {
		ID string `json:"id"`
	}
	if fv.Kind == kindSermon {
		p := map[string]any{"title": title, "body": body, "passages": ok}
		if k := fv.DocKind; k == "sermon" || k == "lesson" || k == "study" {
			p["kind"] = k
		}
		if d := strings.TrimSpace(fv.P.Fields[keyPreached].Text); len(d) == 10 {
			p["preached_on"] = d
		}
		if t := strings.TrimSpace(fv.P.Fields[keyTheme].Text); t != "" {
			p["theme"] = cut(t, 200)
		}
		if err := s.call(ctx, env, "document.create", p, &out); err != nil {
			// A date or theme the document would not take: try again without the details.
			delete(p, "preached_on")
			delete(p, "theme")
			if err2 := s.call(ctx, env, "document.create", p, &out); err2 != nil {
				return "", nil, err2
			}
		}
		return out.ID, bad, nil
	}
	p := map[string]any{"title": title, "body": body, "tags": glossaTags(fv.Tags, nil)}
	if len(ok) > 0 {
		p["ref"] = ok[0]
		bad = append(bad, ok[1:]...) // a note holds one passage
	}
	if err := s.call(ctx, env, "note.create", p, &out); err != nil {
		return "", nil, err
	}
	return out.ID, bad, nil
}

// pull puts a file's text into an existing note or sermon.
func (s *commands) pull(ctx context.Context, env *command.Env, it item, fv fileView, body string) (notAttached []string, err error) {
	title := cut(strings.TrimSpace(fv.Title), maxTitle)
	ok, bad := parseable(fv.Passages)
	_, hasPassages := fv.P.Fields[keyPassages]
	if it.Kind == kindSermon {
		p := map[string]any{"id": it.ID, "body": body}
		if title != "" {
			p["title"] = title
		}
		if hasPassages {
			p["passages"] = append([]string{}, ok...)
		}
		return bad, s.call(ctx, env, "document.update", p, nil)
	}
	p := map[string]any{"id": it.ID, "title": title, "body": body, "tags": append([]string{}, glossaTags(fv.Tags, it.Tags)...)}
	if err := s.call(ctx, env, "note.update", p, nil); err != nil {
		return nil, err
	}
	if hasPassages {
		var want string
		if len(ok) > 0 {
			want = ok[0]
			bad = append(bad, ok[1:]...)
		}
		have := ""
		if len(it.Passages) > 0 {
			have = it.Passages[0]
		}
		if want != have {
			mv := map[string]any{"id": it.ID}
			if want == "" {
				mv["free"] = true
			} else {
				mv["ref"] = want
			}
			if err := s.call(ctx, env, "note.move", mv, nil); err != nil {
				return bad, err
			}
		}
	}
	return bad, nil
}
