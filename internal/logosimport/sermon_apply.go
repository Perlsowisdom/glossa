package logosimport

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// bibleMod is an installed Bible, as much as is needed to match a Logos Bible to it.
type bibleMod struct {
	ID           string `json:"id"`
	Abbreviation string `json:"abbreviation"`
	Name         string `json:"name"`
}

func (b bibleMod) label() string {
	for _, s := range []string{b.Abbreviation, b.Name, b.ID} {
		if s != "" {
			return s
		}
	}
	return ""
}

func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// installedBibles lists the Bibles in Glossa. If the list cannot be had, none are installed.
func (s *service) installedBibles(ctx context.Context, env *command.Env) []bibleMod {
	var mods []bibleMod
	if err := s.call(ctx, env, "bible.modules", map[string]any{}, &mods); err != nil {
		return nil
	}
	return mods
}

// matchBible finds the Glossa Bible that is the one a Logos quotation was shown in, by its short
// name or title. A Bible that merely looks similar is not used: another translation would be
// quoted as if it were the sermon's.
func matchBible(mods []bibleMod, q SermonQuote) (bibleMod, bool) {
	want := map[string]bool{}
	for _, w := range []string{q.Abbr, q.Title, q.Tag} {
		if f := fold(w); f != "" {
			want[f] = true
		}
	}
	for _, m := range mods {
		for _, have := range []string{m.Abbreviation, m.Name, m.ID} {
			if f := fold(have); f != "" && want[f] {
				return m, true
			}
		}
	}
	return bibleMod{}, false
}

type quoteDoc struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	Quote   string `json:"quote"`
}

// sermonBody writes a sermon's text. Each verse quote is made from the same Bible if it is
// installed in Glossa and its licence allows the quote (document.quote decides); otherwise it stays
// a plain quote with its reference, and the report says why.
func (s *service) sermonBody(ctx context.Context, env *command.Env, mods []bibleMod, d *SermonData, notes map[string]int) string {
	return d.Body(func(q SermonQuote, sofar string) string {
		name := q.bibleName()
		if name == "" {
			name = "a Bible"
		}
		m, ok := matchBible(mods, q)
		if !ok {
			notes[fmt.Sprintf("verse quotes kept as plain quotes with their reference, because %s is not installed in Glossa", name)]++
			return PlainQuote(q)
		}
		var out quoteDoc
		err := s.call(ctx, env, "document.quote", map[string]any{"ref": q.Display, "module": m.ID, "preview": true, "text": sofar}, &out)
		if err != nil || !out.Allowed || out.Quote == "" {
			notes[fmt.Sprintf("verse quotes kept as plain quotes with their reference, because the licence of %s does not allow quoting that text here", m.label())]++
			return PlainQuote(q)
		}
		return out.Quote
	})
}

func sermonPoints(d *SermonData) []map[string]any {
	out := make([]map[string]any, 0, len(d.Points))
	for _, p := range d.Points {
		pt := map[string]any{"h": p.Heading, "level": p.Level}
		if p.Ref != "" {
			pt["ref"] = p.Ref
		}
		out = append(out, pt)
	}
	return out
}

type docID struct {
	ID string `json:"id"`
}

func (s *service) applySermon(ctx context.Context, env *command.Env, r *importRun, log map[string]logRow, it Item, folderID string, prev logRow, seen bool) (string, error) {
	d := it.Sermon
	if !r.biblesLoaded {
		r.bibles, r.biblesLoaded = s.installedBibles(ctx, env), true
	}
	body := s.sermonBody(ctx, env, r.bibles, d, r.notes)
	passages := d.Passages
	if passages == nil {
		passages = []string{}
	}
	kind := string(it.Kind)
	if seen {
		params := map[string]any{"id": prev.objID, "title": d.Title, "passages": passages, "points": sermonPoints(d),
			"body": body, "preached_on": d.PreachedOn, "theme": d.Theme}
		if err := s.call(ctx, env, "document.update", params, nil); err != nil {
			return "", err
		}
		if folderID != "" {
			if err := s.call(ctx, env, "document.move", map[string]any{"id": prev.objID, "folder_id": folderID}, nil); err != nil {
				return "", err
			}
		}
		if d.Updated > 0 {
			err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
				return tx.Update("documents", store.ByID(prev.objID), store.Row{"updated_at": d.Updated})
			})
			if err != nil {
				return "", err
			}
		}
		if err := s.writeLog(ctx, env, log, kind, it.Key, it.Changed, "documents", prev.objID, prev.runID, ""); err != nil {
			return "", err
		}
		return "updated", nil
	}
	params := map[string]any{"kind": "sermon", "title": d.Title, "passages": passages, "points": sermonPoints(d), "body": body,
		"preached_on": d.PreachedOn, "theme": d.Theme}
	if folderID != "" {
		params["folder_id"] = folderID
	}
	var doc docID
	if err := s.call(ctx, env, "document.create", params, &doc); err != nil {
		return "", err
	}
	if d.Updated > 0 { // keep the date Logos has
		err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
			return tx.Update("documents", store.ByID(doc.ID), store.Row{"created_at": d.Updated, "updated_at": d.Updated})
		})
		if err != nil {
			return "", err
		}
	}
	if err := s.writeLog(ctx, env, log, kind, it.Key, it.Changed, "documents", doc.ID, r.id, ""); err != nil {
		return "", err
	}
	return "created", nil
}
