package logosimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
	"github.com/Perlsowisdom/glossa/internal/verse"
)

const (
	maxTags   = 30
	maxTagLen = 40
)

func nowMS() int64 { return time.Now().UnixMilli() }

// logRow is a line of the import log: what a Logos thing became in Glossa.
type logRow struct {
	changed string
	objType string
	objID   string
	runID   string
	detail  string
}

func logKey(kind, key string) string { return kind + "\x00" + key }

func loadLog(ctx context.Context, env *command.Env) (map[string]logRow, error) {
	rows, err := env.Store.Query(ctx, `SELECT kind, logos_key, changed, object_type, object_id, run_id, COALESCE(detail_json, '')
		FROM logos_import_log`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]logRow{}
	for rows.Next() {
		var kind, key string
		var r logRow
		if err := rows.Scan(&kind, &key, &r.changed, &r.objType, &r.objID, &r.runID, &r.detail); err != nil {
			return nil, err
		}
		out[logKey(kind, key)] = r
	}
	return out, rows.Err()
}

// live reports whether a row of a table with deleted_at is still there and not deleted.
func live(ctx context.Context, env *command.Env, table, id string) bool {
	var n int
	err := env.Store.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id = ? AND deleted_at IS NULL", id).Scan(&n)
	return err == nil && n > 0
}

// ImportParams are logos.import's parameters.
type ImportParams struct {
	Folder string   `json:"folder,omitempty" desc:"A folder holding Logos's Data and Documents folders; leave out to look where Logos keeps them"`
	Kinds  []string `json:"kinds,omitempty" desc:"Only these kinds: note, highlight, clipping, passage_list, word_list, prayer_list, reading_plan; default all"`
}

// KindResult is what an import did with one kind.
type KindResult struct {
	Kind    Kind   `json:"kind"`
	Label   string `json:"label"`
	Created int    `json:"created"`
	Updated int    `json:"updated" desc:"Imported before and changed in Logos since, so changed here"`
	Skipped int    `json:"skipped" desc:"Already imported and unchanged, or deleted here since"`
	Failed  int    `json:"failed"`
}

// ImportResult is the report of one import.
type ImportResult struct {
	RunID    string          `json:"run_id" desc:"Give this to logos.undo to take the import back"`
	Kinds    []KindResult    `json:"kinds"`
	Folders  int             `json:"folders" desc:"Folders made"`
	Unmapped []UnmappedGroup `json:"unmapped"`
	Notes    []string        `json:"notes"`
	Failures []string        `json:"failures,omitempty" desc:"What went wrong, for the first few that failed"`
}

type importRun struct {
	id       string
	kinds    map[Kind]*KindResult
	folders  int
	failures []string
}

func (s *service) importCmd(ctx context.Context, env *command.Env, p ImportParams) (ImportResult, error) {
	want := map[Kind]bool{}
	for _, k := range p.Kinds {
		if _, ok := KindLabels[Kind(k)]; !ok {
			return ImportResult{}, command.Errorf("bad_params", "logos.import: %q is not a kind of thing that can be imported", k)
		}
		want[Kind(k)] = true
	}
	res, root, err := s.read(p.Folder)
	if err != nil {
		return ImportResult{}, err
	}
	log, err := loadLog(ctx, env)
	if err != nil {
		return ImportResult{}, err
	}
	r := &importRun{id: store.NewID(), kinds: map[Kind]*KindResult{}}
	for _, k := range Kinds {
		r.kinds[k] = &KindResult{Kind: k, Label: KindLabels[k]}
	}
	if err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
		return tx.Create("logos_import_runs", store.Row{"id": r.id, "started_at": nowMS(), "source": root, "counts_json": "{}"})
	}); err != nil {
		return ImportResult{}, err
	}
	folderNames := map[string]string{}
	for _, f := range res.Folders {
		folderNames[f.Key] = f.Name
	}
	folderIDs := map[string]string{}
	for _, it := range res.Items {
		if len(want) > 0 && !want[it.Kind] {
			continue
		}
		kr := r.kinds[it.Kind]
		folderID := ""
		if it.Folder != "" {
			if folderID, err = s.ensureFolder(ctx, env, r, log, folderIDs, it.Folder, folderNames[it.Folder]); err != nil {
				r.fail(kr, it, err)
				continue
			}
		}
		out, err := s.apply(ctx, env, r, log, it, folderID)
		if err != nil {
			r.fail(kr, it, err)
			continue
		}
		switch out {
		case "created":
			kr.Created++
		case "updated":
			kr.Updated++
		default:
			kr.Skipped++
		}
	}
	result := ImportResult{RunID: r.id, Kinds: []KindResult{}, Folders: r.folders, Unmapped: groupUnmapped(res.Unmapped), Notes: []string{}, Failures: r.failures}
	for _, k := range Kinds {
		if kr := r.kinds[k]; kr.Created+kr.Updated+kr.Skipped+kr.Failed > 0 || hasUnmapped(res, k) {
			result.Kinds = append(result.Kinds, *kr)
		}
	}
	for text, n := range res.Notes {
		result.Notes = append(result.Notes, noteLine(text, n))
	}
	sort.Strings(result.Notes)
	did := r.folders
	for _, kr := range r.kinds {
		did += kr.Created + kr.Updated
	}
	err = env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
		if did == 0 { // nothing happened, so there is no import to list or take back
			return tx.Delete("logos_import_runs", store.ByID(r.id))
		}
		return tx.Update("logos_import_runs", store.ByID(r.id), store.Row{"counts_json": marshal(result)})
	})
	if did == 0 {
		result.RunID = ""
	}
	return result, err
}

func (r *importRun) fail(kr *KindResult, it Item, err error) {
	kr.Failed++
	if len(r.failures) < 20 {
		r.failures = append(r.failures, fmt.Sprintf("%s %q: %v", strings.ToLower(KindLabels[it.Kind]), it.Title, err))
	}
}

// ensureFolder finds or makes the Glossa folder for a Logos notebook or clippings folder.
func (s *service) ensureFolder(ctx context.Context, env *command.Env, r *importRun, log map[string]logRow, made map[string]string, key, name string) (string, error) {
	if id, ok := made[key]; ok {
		return id, nil
	}
	if row, ok := log[logKey("folder", key)]; ok && live(ctx, env, "folders", row.objID) {
		made[key] = row.objID
		return row.objID, nil
	}
	var f struct {
		ID string `json:"id"`
	}
	if err := s.call(ctx, env, "folder.create", map[string]any{"name": name}, &f); err != nil {
		return "", err
	}
	if err := s.writeLog(ctx, env, log, "folder", key, "", "folders", f.ID, r.id, ""); err != nil {
		return "", err
	}
	made[key] = f.ID
	r.folders++
	return f.ID, nil
}

// call runs another command and reads its result.
func (s *service) call(ctx context.Context, env *command.Env, id string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	res, err := s.r.Run(ctx, env, id, raw)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// writeLog records, or updates, what a Logos thing became.
func (s *service) writeLog(ctx context.Context, env *command.Env, log map[string]logRow, kind, key, changed, objType, objID, runID, detail string) error {
	k := logKey(kind, key)
	err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
		return putLog(tx, kind, key, changed, objType, objID, runID, detail, log[k].objID != "")
	})
	if err != nil {
		return err
	}
	log[k] = logRow{changed: changed, objType: objType, objID: objID, runID: runID, detail: detail}
	return nil
}

func putLog(tx *store.Tx, kind, key, changed, objType, objID, runID, detail string, exists bool) error {
	row := store.Row{"changed": changed, "object_type": objType, "object_id": objID}
	if detail != "" {
		row["detail_json"] = detail
	}
	if exists {
		return tx.Update("logos_import_log", store.Row{"kind": kind, "logos_key": key}, row)
	}
	row["kind"], row["logos_key"], row["run_id"], row["created_at"] = kind, key, runID, nowMS()
	return tx.Create("logos_import_log", row)
}

// apply brings one item in: made if new, changed if it changed in Logos since, else left alone.
func (s *service) apply(ctx context.Context, env *command.Env, r *importRun, log map[string]logRow, it Item, folderID string) (string, error) {
	kind := string(it.Kind)
	prev, seen := log[logKey(kind, it.Key)]
	if seen {
		if !live(ctx, env, prev.objType, prev.objID) {
			return "skipped", nil // deleted here since: it is not brought back
		}
		if prev.changed == it.Changed {
			return "skipped", nil
		}
	}
	switch it.Kind {
	case KindNote, KindClipping, KindPrayerList:
		return s.applyNote(ctx, env, r, log, it, folderID, prev, seen)
	case KindHighlight:
		return s.applyHighlight(ctx, env, r, log, it, prev, seen)
	case KindPassageList, KindWordList:
		return s.applyList(ctx, env, r, log, it, prev, seen)
	case KindReadingPlan:
		if it.Plan != nil {
			return s.applyPlan(ctx, env, r, log, it, prev, seen)
		}
		return s.applyNote(ctx, env, r, log, it, folderID, prev, seen)
	}
	return "skipped", nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func stamp(v int64) int64 {
	if v <= 0 {
		return nowMS()
	}
	return v
}

func cleanTags(in []string) []string {
	var out []string
	for _, t := range in {
		if len([]rune(t)) > maxTagLen || len(out) >= maxTags {
			continue
		}
		out = addTag(out, t)
	}
	return out
}

func (s *service) applyNote(ctx context.Context, env *command.Env, r *importRun, log map[string]logRow, it Item, folderID string, prev logRow, seen bool) (string, error) {
	d := it.Note
	kind, tags := string(it.Kind), cleanTags(d.Tags)
	if seen {
		err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
			cur, err := tx.Get("notes", store.ByID(prev.objID))
			if err != nil {
				return err
			}
			updated := stamp(d.Updated)
			if p, ok := cur["updated_at"].(int64); ok && updated <= p {
				updated = p + 1
			}
			if err := tx.Update("notes", store.ByID(prev.objID), store.Row{"title": nullable(d.Title), "body_md": d.Body, "updated_at": updated}); err != nil {
				return err
			}
			if err := addNoteTags(ctx, env, tx, prev.objID, tags); err != nil {
				return err
			}
			return putLog(tx, kind, it.Key, it.Changed, "notes", prev.objID, r.id, "", true)
		})
		if err != nil {
			return "", err
		}
		log[logKey(kind, it.Key)] = logRow{changed: it.Changed, objType: "notes", objID: prev.objID, runID: prev.runID}
		return "updated", nil
	}
	id := store.NewID()
	err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
		row := store.Row{"id": id, "title": nullable(d.Title), "body_md": d.Body, "folder_id": nullable(folderID),
			"created_at": stamp(d.Created), "updated_at": stamp(max(d.Updated, d.Created))}
		if d.Verses != nil {
			aid := store.NewID()
			if err := tx.Create("anchors", store.Row{"id": aid, "verse_start": int64(d.Verses.Start), "verse_end": int64(d.Verses.End),
				"versification": string(verse.English)}); err != nil {
				return err
			}
			row["anchor_id"] = aid
		}
		if err := tx.Create("notes", row); err != nil {
			return err
		}
		for _, t := range tags {
			if err := tx.Create("note_tags", store.Row{"note_id": id, "tag": t}); err != nil {
				return err
			}
		}
		return putLog(tx, kind, it.Key, it.Changed, "notes", id, r.id, "", false)
	})
	if err != nil {
		return "", err
	}
	log[logKey(kind, it.Key)] = logRow{changed: it.Changed, objType: "notes", objID: id, runID: r.id}
	return "created", nil
}

// addNoteTags gives a note the tags it lacks. Tags the user added are kept.
func addNoteTags(ctx context.Context, env *command.Env, tx *store.Tx, id string, tags []string) error {
	have := map[string]bool{}
	rows, err := env.Store.Query(ctx, `SELECT tag FROM note_tags WHERE note_id = ?`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		have[t] = true
	}
	rows.Close()
	for _, t := range tags {
		if !have[t] && len(have) < maxTags {
			if err := tx.Create("note_tags", store.Row{"note_id": id, "tag": t}); err != nil {
				return err
			}
			have[t] = true
		}
	}
	return nil
}

func (s *service) applyHighlight(ctx context.Context, env *command.Env, r *importRun, log map[string]logRow, it Item, prev logRow, seen bool) (string, error) {
	h := it.Highlight
	detail := marshal(map[string]string{"original_style": h.OriginalStyle})
	if seen {
		err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
			if err := tx.Update("highlights", store.ByID(prev.objID), store.Row{"style": h.Style}); err != nil {
				return err
			}
			return putLog(tx, string(it.Kind), it.Key, it.Changed, "highlights", prev.objID, r.id, detail, true)
		})
		if err != nil {
			return "", err
		}
		log[logKey(string(it.Kind), it.Key)] = logRow{changed: it.Changed, objType: "highlights", objID: prev.objID, runID: prev.runID, detail: detail}
		return "updated", nil
	}
	id, aid := store.NewID(), store.NewID()
	err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
		if err := tx.Create("anchors", store.Row{"id": aid, "verse_start": int64(h.Range.Start), "verse_end": int64(h.Range.End),
			"versification": string(verse.English)}); err != nil {
			return err
		}
		if err := tx.Create("highlights", store.Row{"id": id, "anchor_id": aid, "style": h.Style, "created_at": stamp(h.Created)}); err != nil {
			return err
		}
		return putLog(tx, string(it.Kind), it.Key, it.Changed, "highlights", id, r.id, detail, false)
	})
	if err != nil {
		return "", err
	}
	log[logKey(string(it.Kind), it.Key)] = logRow{changed: it.Changed, objType: "highlights", objID: id, runID: r.id, detail: detail}
	return "created", nil
}

func (s *service) applyList(ctx context.Context, env *command.Env, r *importRun, log map[string]logRow, it Item, prev logRow, seen bool) (string, error) {
	d := it.List
	id := store.NewID()
	err := env.Store.Write(ctx, "logos.import", func(tx *store.Tx) error {
		if seen { // a changed list is replaced; the old one waits in the bin
			if err := tx.Delete("lists", store.ByID(prev.objID)); err != nil {
				return err
			}
		}
		name := d.Name
		if len(name) > 200 {
			name = string([]rune(name)[:200])
		}
		if err := tx.Create("lists", store.Row{"id": id, "kind": d.Kind, "name": name,
			"created_at": stamp(d.Created), "updated_at": stamp(max(d.Updated, d.Created))}); err != nil {
			return err
		}
		for i, e := range d.Entries {
			row := store.Row{"id": store.NewID(), "list_id": id, "position": int64(i + 1), "note": nullable(e.Note)}
			if e.Verses != nil {
				aid := store.NewID()
				if err := tx.Create("anchors", store.Row{"id": aid, "verse_start": int64(e.Verses.Start), "verse_end": int64(e.Verses.End),
					"versification": string(verse.English)}); err != nil {
					return err
				}
				row["anchor_id"] = aid
			} else {
				row["word"], row["lemma"], row["language"] = e.Word, nullable(e.Lemma), nullable(e.Language)
			}
			if err := tx.Create("list_entries", row); err != nil {
				return err
			}
		}
		return putLog(tx, string(it.Kind), it.Key, it.Changed, "lists", id, r.id, "", seen)
	})
	if err != nil {
		return "", err
	}
	runID := r.id
	if seen {
		runID = prev.runID
	}
	log[logKey(string(it.Kind), it.Key)] = logRow{changed: it.Changed, objType: "lists", objID: id, runID: runID}
	if seen {
		return "updated", nil
	}
	return "created", nil
}

var weekdayNames = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// planWeekdays are the days of the week a plan's dates fall on, or none when it reads every day.
func planWeekdays(days []string) []string {
	var set [7]bool
	for _, d := range days {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			set[(int(t.Weekday())+6)%7] = true
		}
	}
	var out []string
	for i, on := range set {
		if on {
			out = append(out, weekdayNames[i])
		}
	}
	if len(out) == 7 {
		return nil
	}
	return out
}

func (s *service) applyPlan(ctx context.Context, env *command.Env, r *importRun, log map[string]logRow, it Item, prev logRow, seen bool) (string, error) {
	d := it.Plan
	manual := make([]map[string]any, len(d.Days))
	for i, refs := range d.Days {
		manual[i] = map[string]any{"refs": refs}
	}
	params := map[string]any{"name": d.Name, "manual": manual}
	if d.Start != "" {
		params["start"] = d.Start
	}
	if wd := planWeekdays(d.Dates); len(wd) > 0 {
		params["weekdays"] = wd
	}
	var plan struct {
		ID string `json:"id"`
	}
	if err := s.call(ctx, env, "plan.create", params, &plan); err != nil {
		return "", err
	}
	if len(d.Done) > 0 {
		if err := s.call(ctx, env, "plan.mark", map[string]any{"plan_id": plan.ID, "days": d.Done}, nil); err != nil {
			return "", err
		}
	}
	if seen {
		if err := s.call(ctx, env, "plan.delete", map[string]any{"id": prev.objID}, nil); err != nil {
			return "", err
		}
	}
	runID := r.id
	if seen {
		runID = prev.runID
	}
	if err := s.writeLog(ctx, env, log, string(it.Kind), it.Key, it.Changed, "reading_plans", plan.ID, runID, ""); err != nil {
		return "", err
	}
	if seen {
		return "updated", nil
	}
	return "created", nil
}

// Runs and undo ---------------------------------------------------------------------------------

// RunInfo describes one earlier import.
type RunInfo struct {
	ID        string        `json:"id"`
	StartedAt int64         `json:"started_at"`
	Source    string        `json:"source,omitempty"`
	Report    *ImportResult `json:"report,omitempty"`
	Added     int           `json:"added" desc:"Things it added that are still here"`
	Undone    bool          `json:"undone"`
}

func (s *service) runs(ctx context.Context, env *command.Env, _ struct{}) ([]RunInfo, error) {
	if env.Store == nil {
		return nil, command.Errorf("unavailable", "no library is open")
	}
	rows, err := env.Store.Query(ctx, `SELECT r.id, r.started_at, COALESCE(r.source, ''), r.counts_json, r.undone_at IS NOT NULL,
		(SELECT COUNT(*) FROM logos_import_log l WHERE l.run_id = r.id AND l.kind <> 'folder')
		FROM logos_import_runs r ORDER BY r.started_at DESC, r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RunInfo{}
	for rows.Next() {
		var ri RunInfo
		var counts string
		if err := rows.Scan(&ri.ID, &ri.StartedAt, &ri.Source, &counts, &ri.Undone, &ri.Added); err != nil {
			return nil, err
		}
		var rep ImportResult
		if json.Unmarshal([]byte(counts), &rep) == nil && rep.RunID != "" {
			ri.Report = &rep
		}
		out = append(out, ri)
	}
	return out, rows.Err()
}

// UndoParams are logos.undo's parameters.
type UndoParams struct {
	RunID string `json:"run_id" required:"true" desc:"The import to take back, from logos.runs"`
}

// UndoResult says what was taken back.
type UndoResult struct {
	Removed int `json:"removed" desc:"Things that went to the bin"`
	Folders int `json:"folders" desc:"Folders that went to the bin, because nothing of yours was in them"`
	Kept    int `json:"kept" desc:"Things already deleted here, left as they are"`
}

func (s *service) undo(ctx context.Context, env *command.Env, p UndoParams) (UndoResult, error) {
	var undone sql.NullInt64
	if err := env.Store.QueryRow(ctx, `SELECT undone_at FROM logos_import_runs WHERE id = ?`, p.RunID).Scan(&undone); err != nil {
		if err == sql.ErrNoRows {
			return UndoResult{}, command.Errorf("not_found", "no import %s", p.RunID)
		}
		return UndoResult{}, err
	}
	if undone.Valid {
		return UndoResult{}, command.Errorf("bad_params", "logos.undo: that import was already taken back")
	}
	type entry struct{ kind, key, objType, objID string }
	var entries []entry
	rows, err := env.Store.Query(ctx, `SELECT kind, logos_key, object_type, object_id FROM logos_import_log WHERE run_id = ? ORDER BY created_at DESC, rowid DESC`, p.RunID)
	if err != nil {
		return UndoResult{}, err
	}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.kind, &e.key, &e.objType, &e.objID); err != nil {
			rows.Close()
			return UndoResult{}, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return UndoResult{}, err
	}
	rows.Close()

	var out UndoResult
	var folders []entry
	for _, e := range entries {
		switch {
		case e.objType == "folders":
			folders = append(folders, e)
			continue
		case !live(ctx, env, e.objType, e.objID):
			out.Kept++
		case e.objType == "reading_plans":
			if err := s.call(ctx, env, "plan.delete", map[string]any{"id": e.objID}, nil); err != nil {
				return out, err
			}
			out.Removed++
		default:
			err := env.Store.Write(ctx, "logos.undo", func(tx *store.Tx) error { return tx.Delete(e.objType, store.ByID(e.objID)) })
			if err != nil {
				return out, err
			}
			out.Removed++
		}
		if err := dropLog(ctx, env, e.kind, e.key); err != nil {
			return out, err
		}
	}
	for _, f := range folders {
		if live(ctx, env, "folders", f.objID) && folderEmpty(ctx, env, f.objID) {
			err := env.Store.Write(ctx, "logos.undo", func(tx *store.Tx) error { return tx.Delete("folders", store.ByID(f.objID)) })
			if err != nil {
				return out, err
			}
			out.Folders++
			if err := dropLog(ctx, env, f.kind, f.key); err != nil {
				return out, err
			}
		}
	}
	err = env.Store.Write(ctx, "logos.undo", func(tx *store.Tx) error {
		return tx.Update("logos_import_runs", store.ByID(p.RunID), store.Row{"undone_at": nowMS()})
	})
	return out, err
}

func dropLog(ctx context.Context, env *command.Env, kind, key string) error {
	return env.Store.Write(ctx, "logos.undo", func(tx *store.Tx) error {
		return tx.Delete("logos_import_log", store.Row{"kind": kind, "logos_key": key})
	})
}

// folderEmpty reports whether no note or document of the user's is left in a folder.
func folderEmpty(ctx context.Context, env *command.Env, id string) bool {
	var n int
	err := env.Store.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM notes WHERE folder_id = ? AND deleted_at IS NULL)
		+ (SELECT COUNT(*) FROM documents WHERE folder_id = ? AND deleted_at IS NULL)
		+ (SELECT COUNT(*) FROM folders WHERE parent_id = ? AND deleted_at IS NULL)`, id, id, id).Scan(&n)
	return err == nil && n == 0
}
