package obsidian

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect"
)

// Register adds the obsidian.* commands. svc is the framework service that connect.Register returned.
func (c *Conn) Register(r *command.Registry, svc *connect.Service) {
	s := &commands{c: c, svc: svc, reg: r}
	r.Add(
		command.Define("obsidian.set_vault", "Choose the Obsidian vault folder on this computer, and the subfolder of it that Glossa writes notes and sermons to (default “Glossa”). An empty path forgets the vault. Choosing another vault forgets which notes were linked to files. Works on this computer only; it is never synced. Needs Obsidian turned on in Settings → Connections.", true, s.setVault),
		command.Define("obsidian.status", "Say which vault is chosen, when notes were last exported and imported, how many are linked to files, and which wait for a decision (changed in both Glossa and the file). Reads the linked files; never uses the network.", false, s.status),
		command.Define("obsidian.export", "Write notes and sermons to the vault as Markdown files with front matter (title, glossa_id, glossa_kind, passages, tags, created, updated), one file each in the chosen subfolder. Give items, or all_notes and/or all_sermons. A file that has not changed is not rewritten; a file that changed in Obsidian is not overwritten: when only the file changed it is left for import, and when both changed it is listed in conflicts and obsidian.resolve settles it. Never deletes a file and never writes outside the vault.", true, s.export),
		command.Define("obsidian.import", "Bring Markdown files from the vault in as notes: from the chosen subfolder, another folder of the vault, or the whole vault. Front matter title, tags and passages are used when present, otherwise the file name is the title; [[wikilinks]] become plain text and ![[embeds]] stay as text. A file Glossa exported maps back to the same note (found again by glossa_id if renamed). Importing again changes nothing that did not change. When both Glossa and the file changed, nothing is changed: it is listed in conflicts and obsidian.resolve settles it.", true, s.importFiles),
		command.Define("obsidian.resolve", "Settle a note or sermon that changed in both Glossa and its file: use Obsidian's text, keep Glossa's (the file is rewritten), or keep both (the file's text goes into a new note, then Glossa's goes to the file).", true, s.resolve),
	)
}

type commands struct {
	c   *Conn
	svc *connect.Service
	reg *command.Registry
	mu  sync.Mutex // one export, import or resolution at a time
}

func fail(err error) error {
	var ce *command.Error
	switch {
	case errors.As(err, &ce):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return command.Errorf("failed", "%s", explainFile(err))
}

// Vault ----------------------------------------------------------------------------------------------------

// VaultParams chooses the vault.
type VaultParams struct {
	Path      string `json:"path" desc:"The vault folder on this computer; empty forgets it"`
	Subfolder string `json:"subfolder,omitempty" desc:"Where in the vault notes and sermons go, such as Glossa or Notes/Glossa (default Glossa); a dot means the vault itself"`
}

// StatusResult is what Glossa knows about the vault and the links.
type StatusResult struct {
	Vault      string     `json:"vault,omitempty" desc:"The vault folder on this computer"`
	Subfolder  string     `json:"subfolder" desc:"Where in the vault notes and sermons go"`
	Problem    string     `json:"problem,omitempty" desc:"Set when the folder cannot be used, in a sentence"`
	LastExport *Last      `json:"last_export,omitempty"`
	LastImport *Last      `json:"last_import,omitempty"`
	Linked     int        `json:"linked" desc:"Notes and sermons that have a file"`
	Conflicts  []Conflict `json:"conflicts" desc:"Changed in both Glossa and the file; settle each with obsidian.resolve"`
}

func (s *commands) setVault(ctx context.Context, env *command.Env, p VaultParams) (StatusResult, error) {
	if _, err := s.svc.Require(ctx, env, "obsidian"); err != nil {
		return StatusResult{}, err
	}
	if env == nil || env.Store == nil {
		return StatusResult{}, command.Errorf("unavailable", "no library is open")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, err := cleanSub(p.Subfolder)
	if err != nil {
		return StatusResult{}, err
	}
	old, err := loadConfig(ctx, env)
	if err != nil {
		return StatusResult{}, err
	}
	next := config{Subfolder: sub}
	if strings.TrimSpace(p.Path) != "" {
		v, err := openVault(p.Path)
		if err != nil {
			return StatusResult{}, command.Errorf("bad_params", "%s", Explain(err))
		}
		if err := v.canWrite(); err != nil {
			return StatusResult{}, command.Errorf("bad_params", "%s", Explain(err))
		}
		next.Vault = v.root
	}
	if p.Subfolder == "" && next.Vault == old.Vault {
		next.Subfolder = old.Subfolder // only the vault was given: keep the subfolder
	}
	if next.Vault != old.Vault {
		if err := forgetLinks(ctx, env); err != nil { // the links point into the old vault
			return StatusResult{}, err
		}
	}
	if err := saveSetting(ctx, env, "obsidian.set_vault", configSetting, next); err != nil {
		return StatusResult{}, err
	}
	return s.statusOf(ctx, env, false)
}

func (s *commands) status(ctx context.Context, env *command.Env, _ struct{}) (StatusResult, error) {
	if env == nil || env.Store == nil {
		return StatusResult{}, command.Errorf("unavailable", "no library is open")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusOf(ctx, env, true)
}

func (s *commands) statusOf(ctx context.Context, env *command.Env, withConflicts bool) (StatusResult, error) {
	cfg, err := loadConfig(ctx, env)
	if err != nil {
		return StatusResult{}, err
	}
	sy, err := loadSync(ctx, env)
	if err != nil {
		return StatusResult{}, err
	}
	out := StatusResult{Vault: cfg.Vault, Subfolder: displaySub(cfg.Subfolder), LastExport: sy.Export, LastImport: sy.Import, Conflicts: []Conflict{}}
	links, err := allLinks(ctx, env)
	if err != nil {
		return out, err
	}
	out.Linked = len(links)
	if cfg.Vault == "" {
		return out, nil
	}
	v, err := openVault(cfg.Vault)
	if err != nil {
		out.Problem = Explain(err)
		return out, nil
	}
	if !withConflicts {
		return out, nil
	}
	r := &run{s: s, ctx: ctx, env: env, v: v, cfg: cfg, claimed: claims{}}
	for _, l := range links {
		l := l
		fv, err := r.readLinked(&l)
		if err != nil || fv == nil {
			continue
		}
		it, err := s.load(ctx, env, l.Kind, l.ItemID)
		if err != nil {
			continue
		}
		if isConflict(it, *fv, l) {
			out.Conflicts = append(out.Conflicts, conflictOf(it, *fv, link{Path: fv.Rel}))
		}
	}
	return out, nil
}

// Export ---------------------------------------------------------------------------------------------------

// Ref names a note or sermon.
type Ref struct {
	Kind string `json:"kind" required:"true" enum:"note,sermon" desc:"A note, or a sermon (also a lesson or study)"`
	ID   string `json:"id" required:"true"`
}

// ExportParams chooses what to write.
type ExportParams struct {
	Items      []Ref `json:"items,omitempty" desc:"Notes and sermons to write"`
	AllNotes   bool  `json:"all_notes,omitempty" desc:"Write every note"`
	AllSermons bool  `json:"all_sermons,omitempty" desc:"Write every sermon, lesson and study"`
}

// ExportResult is what an export did.
type ExportResult struct {
	Folder        string       `json:"folder" desc:"Where in the vault the files went"`
	Written       int          `json:"written" desc:"Files made"`
	Updated       int          `json:"updated" desc:"Files rewritten because Glossa's text changed"`
	Unchanged     int          `json:"unchanged"`
	ObsidianNewer int          `json:"obsidian_newer" desc:"Files that changed in Obsidian and not in Glossa; they were left alone, and an import brings them in"`
	Items         []ExportItem `json:"items"`
	Conflicts     []Conflict   `json:"conflicts" desc:"Changed in both Glossa and the file. Nothing was changed in them; settle each with obsidian.resolve"`
	Skipped       []Skip       `json:"skipped" desc:"Left out, and why"`
	Message       string       `json:"message"`
}

func (s *commands) export(ctx context.Context, env *command.Env, p ExportParams) (ExportResult, error) {
	r, err := s.newRun(ctx, env)
	if err != nil {
		return ExportResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []item
	for _, ref := range p.Items {
		it, err := s.load(ctx, env, ref.Kind, ref.ID)
		if err != nil {
			var ce *command.Error
			if errors.As(err, &ce) && ce.Code == "not_found" {
				return ExportResult{}, command.Errorf("not_found", "That %s is not there (it may have been deleted).", ref.Kind)
			}
			return ExportResult{}, err
		}
		items = append(items, it)
	}
	if p.AllNotes || p.AllSermons {
		more, err := s.loadAll(ctx, env, p.AllNotes, p.AllSermons)
		if err != nil {
			return ExportResult{}, err
		}
		seen := map[string]bool{}
		for _, it := range items {
			seen[idKey(it.Kind, it.ID)] = true
		}
		for _, it := range more {
			if !seen[idKey(it.Kind, it.ID)] {
				items = append(items, it)
			}
		}
	}
	if len(items) == 0 && !p.AllNotes && !p.AllSermons {
		return ExportResult{}, command.Errorf("bad_params", "Choose which notes or sermons to export.")
	}
	res := ExportResult{Folder: displaySub(r.cfg.Subfolder), Items: []ExportItem{}, Conflicts: []Conflict{}, Skipped: []Skip{}}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		out, c, err := r.export(it)
		if err != nil {
			res.Skipped = append(res.Skipped, Skip{Path: pathOf(err, it), Reason: explainFile(unpathed(err))})
			continue
		}
		res.Items = append(res.Items, out)
		switch out.State {
		case stateWritten:
			res.Written++
		case stateUpdated:
			res.Updated++
		case stateSame:
			res.Unchanged++
		case stateNewer:
			res.ObsidianNewer++
		case stateConflict:
			res.Conflicts = append(res.Conflicts, *c)
		}
	}
	if err := saveSync(ctx, env, &Last{At: nowStamp(), Written: res.Written + res.Updated, Unchanged: res.Unchanged}, nil); err != nil {
		return res, err
	}
	res.Message = exportMessage(res)
	return res, nil
}

// pathOf and unpathed split an error that begins with the file it is about ("Glossa/x.md: ...").
func pathOf(err error, it item) string {
	if p, _, ok := strings.Cut(err.Error(), ": "); ok && strings.HasSuffix(strings.ToLower(p), ".md") {
		return p
	}
	return it.Title
}

func unpathed(err error) error {
	if u := errors.Unwrap(err); u != nil {
		return u
	}
	return err
}

func exportMessage(r ExportResult) string {
	var parts []string
	if r.Written > 0 {
		parts = append(parts, plural(r.Written, "file made", "files made"))
	}
	if r.Updated > 0 {
		parts = append(parts, plural(r.Updated, "file updated", "files updated"))
	}
	if r.Unchanged > 0 {
		parts = append(parts, fmt.Sprintf("%d already up to date", r.Unchanged))
	}
	msg := "Nothing was written."
	if len(parts) > 0 {
		msg = strings.Join(parts, ", ") + " in “" + r.Folder + "”."
	}
	if r.ObsidianNewer > 0 {
		msg += " " + plural(r.ObsidianNewer, "file has newer changes in Obsidian and was left alone; import brings it in.", "files have newer changes in Obsidian and were left alone; import brings them in.")
	}
	if n := len(r.Conflicts); n > 0 {
		msg += " " + plural(n, "note changed in both Glossa and Obsidian and was left alone.", "notes changed in both Glossa and Obsidian and were left alone.")
	}
	if n := len(r.Skipped); n > 0 {
		msg += " " + plural(n, "item was skipped.", "items were skipped.")
	}
	return msg
}

// Import ---------------------------------------------------------------------------------------------------

// ImportParams chooses what to read.
type ImportParams struct {
	Folder string `json:"folder,omitempty" desc:"A folder of the vault to read, such as Inbox; a dot reads the whole vault. Default: the subfolder Glossa writes to"`
}

// ImportResult is what an import did.
type ImportResult struct {
	Folder      string       `json:"folder" desc:"What was read"`
	Files       int          `json:"files" desc:"Markdown files looked at"`
	Created     int          `json:"created" desc:"Notes made"`
	Updated     int          `json:"updated" desc:"Notes updated from a changed file"`
	Unchanged   int          `json:"unchanged"`
	GlossaNewer int          `json:"glossa_newer" desc:"Notes that changed in Glossa and whose file did not; they were left alone, and an export sends them"`
	Links       int          `json:"links" desc:"[[wikilinks]] turned into plain text"`
	Embeds      int          `json:"embeds" desc:"![[embeds]] left as text; Glossa cannot show another note or picture inside a note"`
	Items       []ImportItem `json:"items"`
	Conflicts   []Conflict   `json:"conflicts" desc:"Changed in both Glossa and the file. Nothing was changed in them; settle each with obsidian.resolve"`
	Skipped     []Skip       `json:"skipped" desc:"Files left out, and why"`
	Notes       []string     `json:"notes" desc:"Things to know about files that were imported, such as passages that could not be attached"`
	Message     string       `json:"message"`
}

func (s *commands) importFiles(ctx context.Context, env *command.Env, p ImportParams) (ImportResult, error) {
	r, err := s.newRun(ctx, env)
	if err != nil {
		return ImportResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	folder := strings.TrimSpace(p.Folder)
	switch folder {
	case "":
		folder = r.cfg.sub()
	case ".":
		folder = ""
	default:
		if folder, err = cleanRel(folder); err != nil {
			return ImportResult{}, command.Errorf("bad_params", "The folder must be inside the vault.")
		}
	}
	files, skips, err := r.v.scan(folder)
	if err != nil {
		return ImportResult{}, fail(err)
	}
	shown := folder
	if shown == "" {
		shown = "(the whole vault)"
	}
	res := ImportResult{Folder: shown, Items: []ImportItem{}, Conflicts: []Conflict{}, Skipped: skips, Notes: []string{}}
	if res.Skipped == nil {
		res.Skipped = []Skip{}
	}
	var views []fileView
	tg := targets{}
	for _, rel := range files {
		raw, err := r.v.read(rel)
		if err != nil {
			res.Skipped = append(res.Skipped, Skip{Path: rel, Reason: explainFile(err)})
			continue
		}
		fv := view(rel, raw)
		views = append(views, fv)
		tg.add(rel, fv.Title)
	}
	res.Files = len(views)
	var cached []link
	all := func() []link {
		if cached == nil {
			cached, _ = allLinks(ctx, env)
		}
		return cached
	}
	var n importCounts
	for _, fv := range views {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		out, c, why, err := r.importFile(fv, tg, all, &n)
		if err != nil {
			res.Skipped = append(res.Skipped, Skip{Path: fv.Rel, Reason: explainFile(err)})
			continue
		}
		if why != "" {
			res.Skipped = append(res.Skipped, Skip{Path: fv.Rel, Reason: why})
			continue
		}
		res.Items = append(res.Items, out)
		if c != nil {
			res.Conflicts = append(res.Conflicts, *c)
		}
		cached = nil // links changed
	}
	res.Created, res.Updated, res.Unchanged, res.GlossaNewer = n.Created, n.Updated, n.Unchanged, n.GlossaNewer
	res.Links, res.Embeds, res.Notes = n.Links, n.Embeds, append(res.Notes, n.Notes...)
	if err := saveSync(ctx, env, nil, &Last{At: nowStamp(), Created: n.Created, Updated: n.Updated, Unchanged: n.Unchanged}); err != nil {
		return res, err
	}
	res.Message = importMessage(res)
	return res, nil
}

func importMessage(r ImportResult) string {
	if r.Files == 0 && len(r.Skipped) == 0 {
		return "There are no Markdown files in “" + r.Folder + "”."
	}
	msg := fmt.Sprintf("%s made, %d updated, %d already up to date.", plural(r.Created, "note", "notes"), r.Updated, r.Unchanged)
	if r.Embeds > 0 {
		msg += " " + plural(r.Embeds, "embed (![[…]]) was left as text.", "embeds (![[…]]) were left as text.")
	}
	if r.GlossaNewer > 0 {
		msg += " " + plural(r.GlossaNewer, "note has newer changes in Glossa and was left alone; export sends it.", "notes have newer changes in Glossa and were left alone; export sends them.")
	}
	if n := len(r.Conflicts); n > 0 {
		msg += " " + plural(n, "note changed in both Glossa and Obsidian and was left alone.", "notes changed in both Glossa and Obsidian and were left alone.")
	}
	if n := len(r.Skipped); n > 0 {
		msg += " " + plural(n, "file was skipped.", "files were skipped.")
	}
	return msg
}

// Resolve --------------------------------------------------------------------------------------------------

// ResolveParams settles one note or sermon.
type ResolveParams struct {
	Kind    string `json:"kind" required:"true" enum:"note,sermon"`
	ID      string `json:"id" required:"true" desc:"From a conflict in obsidian.export, obsidian.import or obsidian.status"`
	Resolve string `json:"resolve" required:"true" enum:"obsidian,glossa,both" desc:"obsidian: replace the note with the file's text; glossa: keep the note and rewrite the file from it; both: put the file's text in a new note, then rewrite the file from Glossa's"`
}

// ResolveResult says what was done.
type ResolveResult struct {
	CopyID  string `json:"copy_id,omitempty" desc:"With both: the new note that holds the file's text"`
	Message string `json:"message"`
}

func (s *commands) resolve(ctx context.Context, env *command.Env, p ResolveParams) (ResolveResult, error) {
	r, err := s.newRun(ctx, env)
	if err != nil {
		return ResolveResult{}, err
	}
	if p.Kind != kindNote && p.Kind != kindSermon {
		return ResolveResult{}, command.Errorf("bad_params", "kind must be note or sermon")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copyID, msg, err := r.resolveOne(p.Kind, p.ID, p.Resolve)
	if err != nil {
		return ResolveResult{}, fail(err)
	}
	return ResolveResult{CopyID: copyID, Message: msg}, nil
}

// isConflict: both sides changed since they last matched.
func isConflict(it item, fv fileView, l link) bool {
	gh, fh := it.hash(), fv.hash()
	return gh != fh && (l.GlossaHash == "" || gh != l.GlossaHash) && (l.FileHash == "" || fh != l.FileHash)
}
