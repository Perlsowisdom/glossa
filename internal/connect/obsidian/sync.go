package obsidian

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect/syncutil"
)

// Conflict is a note or sermon that changed both in Glossa and in its file since they last matched.
type Conflict struct {
	Kind            string `json:"kind" enum:"note,sermon"`
	ID              string `json:"id"`
	Path            string `json:"path" desc:"The file, inside the vault"`
	Title           string `json:"title" desc:"Its title in Glossa"`
	ObsidianTitle   string `json:"obsidian_title"`
	YourExcerpt     string `json:"your_excerpt" desc:"Glossa's text from where the two differ, as Markdown"`
	ObsidianExcerpt string `json:"obsidian_excerpt" desc:"The file's text from where the two differ, as Markdown"`
}

func conflictOf(it item, fv fileView, l link) Conflict {
	mine, theirs := syncutil.FromDifference(normBody(it.Body), fv.Body)
	if strings.TrimSpace(strings.TrimPrefix(mine, "… ")) == "" && strings.TrimSpace(strings.TrimPrefix(theirs, "… ")) == "" {
		// Only the title, passages or tags differ: say so in words.
		mine, theirs = detail(it.Title, it.Passages, it.Tags), detail(fv.Title, fv.Passages, fv.Tags)
	}
	return Conflict{Kind: it.Kind, ID: it.ID, Path: l.Path, Title: it.Title, ObsidianTitle: fv.Title,
		YourExcerpt: syncutil.Excerpt(mine, 800), ObsidianExcerpt: syncutil.Excerpt(theirs, 800)}
}

func detail(title string, passages, tags []string) string {
	parts := []string{"Title: " + title}
	if len(passages) > 0 {
		parts = append(parts, "Passages: "+strings.Join(passages, "; "))
	}
	if len(tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(tags, ", "))
	}
	return strings.Join(parts, "\n")
}

// run is one export, import or resolution: the vault, the folder in it, and what has been taken so far.
type run struct {
	s       *commands
	ctx     context.Context
	env     *command.Env
	v       *vault
	cfg     config
	claimed claims
	index   map[string]string // glossa_id of a file in the subfolder -> its path; built when first needed
}

func (s *commands) newRun(ctx context.Context, env *command.Env) (*run, error) {
	if _, err := s.svc.Require(ctx, env, "obsidian"); err != nil {
		return nil, err
	}
	if env == nil || env.Store == nil {
		return nil, command.Errorf("unavailable", "no library is open")
	}
	cfg, err := loadConfig(ctx, env)
	if err != nil {
		return nil, err
	}
	if cfg.Vault == "" {
		return nil, command.Errorf("no_vault", "Choose your Obsidian vault folder first, in Settings → Connections.")
	}
	v, err := openVault(cfg.Vault)
	if err != nil {
		return nil, command.Errorf("no_vault", "%s", Explain(err))
	}
	return &run{s: s, ctx: ctx, env: env, v: v, cfg: cfg, claimed: claims{}}, nil
}

func idKey(kind, id string) string { return kind + "\x00" + id }

// ids lists the files in the subfolder that Glossa wrote, by the id in their front matter.
func (r *run) ids() map[string]string {
	if r.index != nil {
		return r.index
	}
	r.index = map[string]string{}
	files, _, _ := r.v.scan(r.cfg.sub())
	for _, rel := range files {
		raw, err := r.v.read(rel)
		if err != nil {
			continue
		}
		fv := view(rel, raw)
		if fv.ID == "" {
			continue
		}
		if _, dup := r.index[idKey(fv.Kind, fv.ID)]; !dup {
			r.index[idKey(fv.Kind, fv.ID)] = rel
		}
	}
	return r.index
}

// readLinked reads the file a link points to. When it is not there any more it looks for the file that
// carries the same glossa_id (it was renamed or moved in Obsidian). It returns nil when there is none.
func (r *run) readLinked(l *link) (*fileView, error) {
	raw, err := r.v.read(l.Path)
	if err == nil {
		fv := view(l.Path, raw)
		return &fv, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if rel, ok := r.ids()[idKey(l.Kind, l.ItemID)]; ok && rel != l.Path {
		raw, err := r.v.read(rel)
		if err != nil {
			return nil, err
		}
		l.Path = rel
		fv := view(rel, raw)
		return &fv, nil
	}
	return nil, nil
}

// writeItem writes the file for it and returns what the file now says.
func (r *run) writeItem(it item, rel, extra string) (fileView, error) {
	content := render(it, extra)
	if err := r.v.write(rel, []byte(content)); err != nil {
		return fileView{}, err
	}
	return view(rel, content), nil
}

// States of an exported item.
const (
	stateWritten  = "written"        // a new file
	stateUpdated  = "updated"        // the file was rewritten from Glossa
	stateSame     = "unchanged"      // nothing to do
	stateNewer    = "obsidian_newer" // the file changed and Glossa did not: import brings it in
	stateConflict = "conflict"
)

// ExportItem is what happened to one note or sermon.
type ExportItem struct {
	Kind  string `json:"kind" enum:"note,sermon"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Path  string `json:"path,omitempty" desc:"The file, inside the vault"`
	State string `json:"state" enum:"written,updated,unchanged,obsidian_newer,conflict"`
}

func (r *run) export(it item) (ExportItem, *Conflict, error) {
	out := ExportItem{Kind: it.Kind, ID: it.ID, Title: it.Title}
	l, err := linkOfItem(r.ctx, r.env, it.Kind, it.ID)
	if err != nil {
		return out, nil, err
	}
	var fv *fileView
	if l != nil {
		if fv, err = r.readLinked(l); err != nil {
			return out, nil, fmt.Errorf("%s: %w", l.Path, err)
		}
	} else if rel, ok := r.ids()[idKey(it.Kind, it.ID)]; ok {
		// Glossa wrote this file before, and the link is gone (another vault setting, or a copy of the library).
		l = &link{Kind: it.Kind, ItemID: it.ID, Path: rel}
		raw, err := r.v.read(rel)
		if err != nil {
			return out, nil, fmt.Errorf("%s: %w", rel, err)
		}
		v := view(rel, raw)
		fv = &v
	}
	gh := it.hash()
	if fv == nil {
		rel := ""
		if l != nil {
			rel = l.Path // it was there once: write it back
		} else {
			rel = r.claimed.newPath(r.v, r.cfg.sub(), safeName(titleOrFirstLine(it)))
		}
		w, err := r.writeItem(it, rel, "")
		if err != nil {
			return out, nil, fmt.Errorf("%s: %w", rel, err)
		}
		out.Path, out.State = rel, stateWritten
		return out, nil, saveLink(r.ctx, r.env, link{Kind: it.Kind, ItemID: it.ID, Path: rel, GlossaHash: gh, FileHash: w.hash()})
	}
	out.Path = fv.Rel
	fh := fv.hash()
	switch syncutil.Decide(gh, l.GlossaHash, fh, l.FileHash, gh == fh) {
	case syncutil.Nothing:
		out.State = stateSame
		if l.GlossaHash != gh || l.FileHash != fh || l.Path != fv.Rel {
			err = saveLink(r.ctx, r.env, link{Kind: it.Kind, ItemID: it.ID, Path: fv.Rel, GlossaHash: gh, FileHash: fh})
		}
	case syncutil.KeepOwn:
		w, err := r.writeItem(it, fv.Rel, fv.P.Extra)
		if err != nil {
			return out, nil, fmt.Errorf("%s: %w", fv.Rel, err)
		}
		out.State = stateUpdated
		return out, nil, saveLink(r.ctx, r.env, link{Kind: it.Kind, ItemID: it.ID, Path: fv.Rel, GlossaHash: gh, FileHash: w.hash()})
	case syncutil.KeepOther:
		out.State = stateNewer
		if l.Path != fv.Rel {
			err = saveLink(r.ctx, r.env, link{Kind: it.Kind, ItemID: it.ID, Path: fv.Rel, GlossaHash: l.GlossaHash, FileHash: l.FileHash})
		}
	default:
		out.State = stateConflict
		c := conflictOf(it, *fv, link{Path: fv.Rel})
		return out, &c, nil
	}
	return out, nil, err
}

// titleOrFirstLine is what an untitled note is called in a file name: its first line of words.
func titleOrFirstLine(it item) string {
	if t := strings.TrimSpace(it.Title); t != "" {
		return t
	}
	for _, l := range strings.Split(it.Body, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#>-*0123456789. "))
		if l != "" {
			return cut(l, 60)
		}
	}
	return "Untitled " + it.Kind
}

// Import --------------------------------------------------------------------------------------------------

// ImportItem is what happened to one file.
type ImportItem struct {
	Path  string `json:"path"`
	ID    string `json:"id,omitempty" desc:"The note or sermon"`
	Title string `json:"title,omitempty"`
	State string `json:"state" enum:"created,updated,unchanged,glossa_newer,conflict,skipped"`
}

const (
	stateCreated    = "created"
	stateGlossaNewr = "glossa_newer" // Glossa changed and the file did not: export sends it
	stateSkipped    = "skipped"
)

type importCounts struct {
	Created, Updated, Unchanged, GlossaNewer, Renamed int
	Links, Embeds                                     int
	Notes                                             []string
}

// matchFile finds the link a file belongs to, if it has one: by its glossa_id, else by its path, else (a file
// renamed in Obsidian that has no id) by the text it had when last synced.
func (r *run) matchFile(fv fileView, all func() []link) (*link, string, error) {
	if fv.ID != "" {
		state, err := r.s.existence(r.ctx, r.env, fv.Kind, fv.ID)
		if err != nil {
			return nil, "", err
		}
		switch state {
		case binned:
			return nil, "it was deleted in Glossa, so it is left alone", nil
		case present:
			l, err := linkOfItem(r.ctx, r.env, fv.Kind, fv.ID)
			if err != nil {
				return nil, "", err
			}
			if l == nil {
				return &link{Kind: fv.Kind, ItemID: fv.ID, Path: fv.Rel}, "", nil // found by its id: no baseline yet
			}
			if !strings.EqualFold(l.Path, fv.Rel) {
				if _, err := r.v.stat(l.Path); err == nil {
					return nil, fmt.Sprintf("it has the same Glossa id as %s (a copy?), so it is left alone", l.Path), nil
				}
				l.Path = fv.Rel // renamed or moved in Obsidian
			}
			return l, "", nil
		}
	}
	l, err := linkOfPath(r.ctx, r.env, fv.Rel)
	if err != nil || l != nil {
		return l, "", err
	}
	if fv.ID == "" {
		for _, c := range all() {
			if c.FileHash != "" && c.FileHash == fv.hash() && !r.v.exists(c.Path) {
				c := c
				c.Path = fv.Rel
				return &c, "", nil
			}
		}
	}
	return nil, "", nil
}

func (r *run) importFile(fv fileView, tg targets, all func() []link, n *importCounts) (ImportItem, *Conflict, string, error) {
	out := ImportItem{Path: fv.Rel, Title: fv.Title}
	l, why, err := r.matchFile(fv, all)
	if err != nil {
		return out, nil, "", err
	}
	if why != "" {
		out.State = stateSkipped
		return out, nil, why, nil
	}
	body, nl, ne := convertLinks(fv.Body, tg)
	if l == nil {
		id, bad, err := r.s.newFrom(r.ctx, r.env, fv, body, "")
		if err != nil {
			return out, nil, "", err
		}
		it, err := r.s.load(r.ctx, r.env, fv.Kind, id)
		if err != nil {
			return out, nil, "", err
		}
		n.Created++
		n.Links += nl
		n.Embeds += ne
		n.noteUnattached(fv.Rel, bad)
		out.State, out.ID = stateCreated, id
		return out, nil, "", saveLink(r.ctx, r.env, link{Kind: it.Kind, ItemID: id, Path: fv.Rel, GlossaHash: it.hash(), FileHash: fv.hash()})
	}
	out.ID = l.ItemID
	it, err := r.s.load(r.ctx, r.env, l.Kind, l.ItemID)
	if err != nil {
		var ce *command.Error
		if errors.As(err, &ce) && ce.Code == "not_found" {
			out.State = stateSkipped
			return out, nil, "it was deleted in Glossa, so it is left alone", nil
		}
		return out, nil, "", err
	}
	gh, fh := it.hash(), fv.hash()
	switch syncutil.Decide(gh, l.GlossaHash, fh, l.FileHash, gh == fh) {
	case syncutil.Nothing:
		out.State = "unchanged"
		n.Unchanged++
		if l.GlossaHash != gh || l.FileHash != fh || l.Path != fv.Rel {
			err = saveLink(r.ctx, r.env, link{Kind: it.Kind, ItemID: it.ID, Path: fv.Rel, GlossaHash: gh, FileHash: fh})
		}
	case syncutil.KeepOther:
		bad, err := r.s.pull(r.ctx, r.env, it, fv, body)
		if err != nil {
			return out, nil, "", err
		}
		now, err := r.s.load(r.ctx, r.env, it.Kind, it.ID)
		if err != nil {
			return out, nil, "", err
		}
		n.Updated++
		n.Links += nl
		n.Embeds += ne
		n.noteUnattached(fv.Rel, bad)
		out.State = "updated"
		return out, nil, "", saveLink(r.ctx, r.env, link{Kind: it.Kind, ItemID: it.ID, Path: fv.Rel, GlossaHash: now.hash(), FileHash: fh})
	case syncutil.KeepOwn:
		out.State = stateGlossaNewr
		n.GlossaNewer++
		if l.Path != fv.Rel {
			err = saveLink(r.ctx, r.env, link{Kind: it.Kind, ItemID: it.ID, Path: fv.Rel, GlossaHash: l.GlossaHash, FileHash: l.FileHash})
		}
	default:
		out.State = stateConflict
		c := conflictOf(it, fv, link{Path: fv.Rel})
		return out, &c, "", nil
	}
	return out, nil, "", err
}

func (n *importCounts) noteUnattached(rel string, bad []string) {
	if len(bad) > 0 && len(n.Notes) < 20 {
		n.Notes = append(n.Notes, fmt.Sprintf("%s: %s not attached (a note holds one passage that Glossa can read)", rel, plural(len(bad), "passage was", "passages were")))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// resolveOne settles a conflict.
func (r *run) resolveOne(kind, id, choice string) (string, string, error) {
	l, err := linkOfItem(r.ctx, r.env, kind, id)
	if err != nil {
		return "", "", err
	}
	if l == nil {
		return "", "", command.Errorf("not_found", "That note is not linked to a file.")
	}
	it, err := r.s.load(r.ctx, r.env, kind, id)
	if err != nil {
		return "", "", err
	}
	fv, err := r.readLinked(l)
	if err != nil {
		return "", "", command.Errorf("failed", "%s: %s", l.Path, explainFile(err))
	}
	if fv == nil {
		return "", "", command.Errorf("not_found", "The file %s is not in the vault any more.", l.Path)
	}
	body, _, _ := convertLinks(fv.Body, targets{})
	switch choice {
	case "obsidian":
		if _, err := r.s.pull(r.ctx, r.env, it, *fv, body); err != nil {
			return "", "", err
		}
		now, err := r.s.load(r.ctx, r.env, kind, id)
		if err != nil {
			return "", "", err
		}
		return "", "The note now has the text from Obsidian.", saveLink(r.ctx, r.env, link{Kind: kind, ItemID: id, Path: fv.Rel, GlossaHash: now.hash(), FileHash: fv.hash()})
	case "glossa", "both":
		copyID := ""
		if choice == "both" {
			cv := *fv
			cv.Kind = kindNote // the copy is a note whatever the original was
			if copyID, _, err = r.s.newFrom(r.ctx, r.env, cv, body, " (from Obsidian)"); err != nil {
				return "", "", err
			}
		}
		w, err := r.writeItem(it, fv.Rel, fv.P.Extra)
		if err != nil {
			return "", "", command.Errorf("failed", "%s: %s", fv.Rel, explainFile(err))
		}
		msg := "The file now has Glossa’s text."
		if copyID != "" {
			msg = "Glossa’s text is in the file, and the text that was in the file is in a new note."
		}
		return copyID, msg, saveLink(r.ctx, r.env, link{Kind: kind, ItemID: id, Path: fv.Rel, GlossaHash: it.hash(), FileHash: w.hash()})
	}
	return "", "", command.Errorf("bad_params", "resolve must be obsidian, glossa or both")
}

// explainFile says in words why a file could not be used.
func explainFile(err error) string {
	switch {
	case errors.Is(err, errTooBig), errors.Is(err, errNotText), errors.Is(err, errNotFile):
		return "it was left alone because " + err.Error() + "."
	case errors.Is(err, errOutside):
		return "that path is outside the vault, so it was not used."
	case errors.Is(err, fs.ErrPermission):
		return "Glossa is not allowed to change it."
	case errors.Is(err, fs.ErrNotExist):
		return "it is not there."
	}
	return err.Error()
}
