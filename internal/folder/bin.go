package folder

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// Kinds of thing the bin holds, and the table each lives in.
var binTables = map[string]string{"folder": "folders", "note": "notes", "document": "documents", "list": "lists", "plan": "reading_plans", "collection": "collections"}

// BinEntry is one thing in the bin.
type BinEntry struct {
	Kind      string `json:"kind" enum:"folder,note,document,list,plan,collection"`
	ID        string `json:"id"`
	Title     string `json:"title" desc:"Its name or title; empty for an untitled note"`
	Sort      string `json:"sort,omitempty" desc:"A document's kind (sermon, lesson or study) or a list's kind (verses, words or excerpts)"`
	DeletedAt int64  `json:"deleted_at"`
	Folders   int    `json:"folders,omitempty" desc:"A folder: subfolders deleted with it"`
	Notes     int    `json:"notes,omitempty" desc:"A folder: notes deleted with it"`
	Documents int    `json:"documents,omitempty" desc:"A folder: documents deleted with it"`
	// Collections counts the collections inside a deleted collection that went with it.
	Collections int `json:"collections,omitempty" desc:"A collection: collections inside it deleted with it"`
}

// Things inside a deleted folder are listed with the folder, not on their own.
var binUnion = `
SELECT 'folder', f.id, f.name, '', f.deleted_at,
	(SELECT COUNT(*) FROM folders s WHERE s.parent_id = f.id AND ` + with("s", "f") + `),
	(SELECT COUNT(*) FROM notes n WHERE ` + with("n", "f") + ` AND n.folder_id IN
		(SELECT f.id UNION SELECT s.id FROM folders s WHERE s.parent_id = f.id AND ` + with("s", "f") + `)),
	(SELECT COUNT(*) FROM documents d WHERE ` + with("d", "f") + ` AND d.folder_id IN
		(SELECT f.id UNION SELECT s.id FROM folders s WHERE s.parent_id = f.id AND ` + with("s", "f") + `))
	FROM folders f LEFT JOIN folders p ON p.id = f.parent_id
	WHERE f.deleted_at IS NOT NULL AND (p.id IS NULL OR p.deleted_at IS NULL)
UNION ALL
SELECT 'note', n.id, COALESCE(n.title, ''), '', n.deleted_at, 0, 0, 0
	FROM notes n LEFT JOIN folders f ON f.id = n.folder_id
	WHERE n.deleted_at IS NOT NULL AND (f.id IS NULL OR f.deleted_at IS NULL)
UNION ALL
SELECT 'document', d.id, d.title, d.kind, d.deleted_at, 0, 0, 0
	FROM documents d LEFT JOIN folders f ON f.id = d.folder_id
	WHERE d.deleted_at IS NOT NULL AND (f.id IS NULL OR f.deleted_at IS NULL)
UNION ALL
SELECT 'list', l.id, l.name, l.kind, l.deleted_at, 0, 0, 0 FROM lists l WHERE l.deleted_at IS NOT NULL
UNION ALL
SELECT 'plan', p.id, p.name, '', p.deleted_at, 0, 0, 0 FROM reading_plans p WHERE p.deleted_at IS NOT NULL
UNION ALL
SELECT 'collection', c.id, c.name, '', c.deleted_at, 0, 0, 0
	FROM collections c LEFT JOIN collections p ON p.id = c.parent_id
	WHERE c.deleted_at IS NOT NULL AND (p.id IS NULL OR p.deleted_at IS NULL)
`

var binQuery = binUnion + `ORDER BY 5 DESC, 2 LIMIT ? OFFSET ?`

var binCountQuery = `SELECT COUNT(*) FROM (` + binUnion + `)`

// with is the condition that row c was deleted together with row p: the same deletion batch or, for rows
// deleted before batches existed (no batch on p), a deleted_at no earlier than p's.
func with(c, p string) string {
	return "(" + c + ".deleted_at IS NOT NULL AND (" + p + ".deletion_batch IS NOT NULL AND " + c + ".deletion_batch = " + p + ".deletion_batch OR " +
		p + ".deletion_batch IS NULL AND " + c + ".deleted_at >= " + p + ".deleted_at))"
}

type BinListParams struct {
	Limit  int `json:"limit,omitempty" desc:"Most entries to return (default 500)"`
	Offset int `json:"offset,omitempty" desc:"Entries to skip first, to read the next page (bin.count says how many there are in all)"`
}

func binList(ctx context.Context, env *command.Env, p BinListParams) ([]BinEntry, error) {
	if err := needStore(env); err != nil {
		return nil, err
	}
	if p.Limit <= 0 {
		p.Limit = 500
	}
	rows, err := env.Store.Query(ctx, binQuery, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BinEntry{}
	for rows.Next() {
		var e BinEntry
		if err := rows.Scan(&e.Kind, &e.ID, &e.Title, &e.Sort, &e.DeletedAt, &e.Folders, &e.Notes, &e.Documents); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Kind != "collection" {
			continue
		}
		d, err := deletedRow(ctx, env, BinParams{Kind: "collection", ID: out[i].ID})
		if err != nil {
			return nil, err
		}
		inside, err := collectionContents(ctx, env, out[i].ID, d)
		if err != nil {
			return nil, err
		}
		out[i].Collections = len(inside)
	}
	return out, nil
}

// BinCount is how many things the bin holds in all, however many bin.list returns at once.
type BinCount struct {
	Count int `json:"count"`
}

func binCount(ctx context.Context, env *command.Env, _ struct{}) (BinCount, error) {
	if err := needStore(env); err != nil {
		return BinCount{}, err
	}
	var n int
	err := env.Store.QueryRow(ctx, binCountQuery).Scan(&n)
	return BinCount{n}, err
}

type BinParams struct {
	Kind string `json:"kind" required:"true" enum:"folder,note,document,list,plan,collection"`
	ID   string `json:"id" required:"true"`
}

// Restored counts what a bin.restore brought back.
type Restored struct {
	Folders   int `json:"folders"`
	Notes     int `json:"notes"`
	Documents int `json:"documents"`
	Lists     int `json:"lists"`
	Plans     int `json:"plans"`
	// Collections counts every collection brought back or removed, the one asked for included.
	Collections int `json:"collections"`
}

// deletion says when a row in the bin was deleted and, if it has one, which deletion batch it went
// with. Rows deleted before batches existed have no batch and are matched by time (ADR 0015).
type deletion struct {
	at    int64
	batch string
}

// together is the SQL condition that a row went with this deletion, and its arguments.
func (d deletion) together() (string, []any) {
	if d.batch != "" {
		return "deletion_batch = ?", []any{d.batch}
	}
	return "deleted_at >= ?", []any{d.at}
}

// apart is the opposite: not deleted with this deletion (a live row counts as apart).
func (d deletion) apart() (string, []any) {
	if d.batch != "" {
		return "(deleted_at IS NULL OR COALESCE(deletion_batch, '') <> ?)", []any{d.batch}
	}
	return "(deleted_at IS NULL OR deleted_at < ?)", []any{d.at}
}

// hasBatch tells whether this kind of row carries a deletion batch.
func hasBatch(kind string) bool {
	return kind == "folder" || kind == "note" || kind == "document" || kind == "collection"
}

// deletedRow reads a row in the bin and its deletion.
func deletedRow(ctx context.Context, env *command.Env, p BinParams) (deletion, error) {
	table, ok := binTables[p.Kind]
	if !ok {
		return deletion{}, command.Errorf("bad_params", "kind must be folder, note, document, list, plan or collection")
	}
	q := `SELECT deleted_at, NULL FROM ` + table + ` WHERE id = ?`
	if hasBatch(p.Kind) {
		q = `SELECT deleted_at, deletion_batch FROM ` + table + ` WHERE id = ?`
	}
	var at sql.NullInt64
	var batch sql.NullString
	err := env.Store.QueryRow(ctx, q, p.ID).Scan(&at, &batch)
	if errors.Is(err, sql.ErrNoRows) {
		return deletion{}, command.Errorf("not_found", "no %s %s", p.Kind, p.ID)
	} else if err != nil {
		return deletion{}, err
	}
	if !at.Valid {
		return deletion{}, command.Errorf("bad_params", "%s %s is not in the bin", p.Kind, p.ID)
	}
	return deletion{at.Int64, batch.String}, nil
}

// folderContents finds what was deleted with a folder: its subfolders, and the notes and documents
// in it or them.
func folderContents(ctx context.Context, env *command.Env, id string, d deletion) (subs, notes, docs []string, err error) {
	cond, carg := d.together()
	if subs, err = ids(ctx, env, `SELECT id FROM folders WHERE parent_id = ? AND `+cond, append([]any{id}, carg...)...); err != nil {
		return
	}
	all := append([]string{id}, subs...)
	args := append(anys(all), carg...)
	if notes, err = ids(ctx, env, `SELECT id FROM notes WHERE folder_id IN (`+marks(all)+`) AND `+cond, args...); err != nil {
		return
	}
	docs, err = ids(ctx, env, `SELECT id FROM documents WHERE folder_id IN (`+marks(all)+`) AND `+cond, args...)
	return
}

// collectionContents finds the collections inside one that were deleted with it, at any depth,
// parents before the collections inside them.
func collectionContents(ctx context.Context, env *command.Env, id string, d deletion) ([]string, error) {
	var out []string
	cond, carg := d.together()
	for next := []string{id}; len(next) > 0; {
		subs, err := ids(ctx, env, `SELECT id FROM collections WHERE parent_id IN (`+marks(next)+`) AND `+cond,
			append(anys(next), carg...)...)
		if err != nil {
			return nil, err
		}
		out = append(out, subs...)
		next = subs
	}
	return out, nil
}

func binRestore(ctx context.Context, env *command.Env, p BinParams) (Restored, error) {
	if err := needStore(env); err != nil {
		return Restored{}, err
	}
	d, err := deletedRow(ctx, env, p)
	if err != nil {
		return Restored{}, err
	}
	var subs, notes, docs, colls []string
	if p.Kind == "folder" {
		if subs, notes, docs, err = folderContents(ctx, env, p.ID, d); err != nil {
			return Restored{}, err
		}
	}
	if p.Kind == "collection" {
		if colls, err = collectionContents(ctx, env, p.ID, d); err != nil {
			return Restored{}, err
		}
	}
	var got Restored
	err = env.Store.Write(ctx, "bin.restore", func(tx *store.Tx) error {
		table := binTables[p.Kind]
		cur, err := tx.Get(table, store.ByID(p.ID))
		if err != nil {
			return err
		}
		fields := store.Row{"deleted_at": nil}
		if hasBatch(p.Kind) {
			fields["deletion_batch"] = nil
		}
		// Something whose folder is still in the bin comes back at the top.
		switch p.Kind {
		case "folder":
			if parent, ok := cur["parent_id"].(string); ok && !folderLive(tx, parent) {
				fields["parent_id"] = nil
			}
		case "note", "document":
			if f, ok := cur["folder_id"].(string); ok && !folderLive(tx, f) {
				fields["folder_id"] = nil
			}
		case "collection":
			if parent, ok := cur["parent_id"].(string); ok && !collectionLive(tx, parent) {
				fields["parent_id"] = nil
			}
		}
		if err := tx.Update(table, store.ByID(p.ID), fields); err != nil {
			return err
		}
		for _, x := range []struct {
			table string
			ids   []string
		}{{"folders", subs}, {"notes", notes}, {"documents", docs}, {"collections", colls}} {
			for _, id := range x.ids {
				if err := tx.Update(x.table, store.ByID(id), store.Row{"deleted_at": nil, "deletion_batch": nil}); err != nil {
					return err
				}
			}
		}
		got = Restored{Folders: len(subs), Notes: len(notes), Documents: len(docs), Collections: len(colls)}
		switch p.Kind {
		case "folder":
			got.Folders++
		case "note":
			got.Notes++
		case "document":
			got.Documents++
		case "list":
			got.Lists++
		case "plan":
			got.Plans++
		case "collection":
			got.Collections++
		}
		return nil
	})
	return got, err
}

func collectionLive(tx *store.Tx, id string) bool {
	row, err := tx.Get("collections", store.ByID(id))
	return err == nil && row["deleted_at"] == nil
}

func folderLive(tx *store.Tx, id string) bool {
	row, err := tx.Get("folders", store.ByID(id))
	return err == nil && row["deleted_at"] == nil
}

// Purged counts what a bin.purge removed for good.
type Purged = Restored

func binPurge(ctx context.Context, env *command.Env, p BinParams) (Purged, error) {
	if err := needStore(env); err != nil {
		return Purged{}, err
	}
	d, err := deletedRow(ctx, env, p)
	if err != nil {
		return Purged{}, err
	}
	var out Purged
	var folders, notes, docs, entries, lists, plans, colls []string
	var planRows []planRow
	switch p.Kind {
	case "folder":
		var subs []string
		if subs, notes, docs, err = folderContents(ctx, env, p.ID, d); err != nil {
			return Purged{}, err
		}
		folders = append(subs, p.ID) // subfolders before the folder that holds them
	case "note":
		notes = []string{p.ID}
	case "document":
		docs = []string{p.ID}
	case "plan":
		plans = []string{p.ID}
		if planRows, err = planChildren(ctx, env, p.ID); err != nil {
			return Purged{}, err
		}
	case "collection":
		inside, err := collectionContents(ctx, env, p.ID, d)
		if err != nil {
			return Purged{}, err
		}
		colls = []string{p.ID}
		colls = append(colls, inside...)
		for _, c := range colls {
			books, err := ids(ctx, env, `SELECT item_id FROM collection_items WHERE collection_id = ?`, c)
			if err != nil {
				return Purged{}, err
			}
			for _, b := range books {
				planRows = append(planRows, planRow{"collection_items", store.Row{"collection_id": c, "item_id": b}})
			}
		}
		// The collections inside go before the one that holds them.
		for i, j := 0, len(colls)-1; i < j; i, j = i+1, j-1 {
			colls[i], colls[j] = colls[j], colls[i]
		}
	case "list":
		if entries, err = ids(ctx, env, `SELECT id FROM list_entries WHERE list_id = ?`, p.ID); err != nil {
			return Purged{}, err
		}
		lists = []string{p.ID}
	}
	out = Purged{Folders: len(folders), Notes: len(notes), Documents: len(docs), Lists: len(lists), Plans: len(plans), Collections: len(colls)}

	// A note's tags and every place it was ever attached go with it, as do a list entry's places.
	tags := map[string][]string{}
	places := map[string]bool{}
	for _, n := range notes {
		if tags[n], err = ids(ctx, env, `SELECT tag FROM note_tags WHERE note_id = ?`, n); err != nil {
			return Purged{}, err
		}
		if err := pastAnchors(ctx, env, "notes", n, places); err != nil {
			return Purged{}, err
		}
	}
	for _, e := range entries {
		if err := pastAnchors(ctx, env, "list_entries", e, places); err != nil {
			return Purged{}, err
		}
	}
	anchors, err := unusedAnchors(ctx, env, places, notes, entries)
	if err != nil {
		return Purged{}, err
	}
	// Anything else still filed in a purged folder (deleted on its own before the folder was)
	// stays, at the top.
	var strayNotes, strayDocs, strayColls []string
	if len(colls) > 0 {
		// A collection deleted on its own earlier stays, at the top.
		if strayColls, err = ids(ctx, env, `SELECT id FROM collections WHERE parent_id IN (`+marks(colls)+`) AND id NOT IN (`+marks(colls)+`)`,
			append(anys(colls), anys(colls)...)...); err != nil {
			return Purged{}, err
		}
	}
	if len(folders) > 0 {
		cond, carg := d.apart()
		args := append(anys(folders), carg...)
		q := ` WHERE folder_id IN (` + marks(folders) + `) AND ` + cond
		if strayNotes, err = ids(ctx, env, `SELECT id FROM notes`+q, args...); err != nil {
			return Purged{}, err
		}
		if strayDocs, err = ids(ctx, env, `SELECT id FROM documents`+q, args...); err != nil {
			return Purged{}, err
		}
	}

	err = env.Store.Write(ctx, "bin.purge", func(tx *store.Tx) error {
		for _, x := range []struct {
			table string
			ids   []string
		}{{"notes", strayNotes}, {"documents", strayDocs}} {
			for _, id := range x.ids {
				if err := tx.Update(x.table, store.ByID(id), store.Row{"folder_id": nil}); err != nil {
					return err
				}
			}
		}
		for _, id := range strayColls {
			if err := tx.Update("collections", store.ByID(id), store.Row{"parent_id": nil}); err != nil {
				return err
			}
		}
		for _, n := range notes {
			for _, t := range tags[n] {
				if err := tx.Purge("note_tags", store.Row{"note_id": n, "tag": t}); err != nil {
					return err
				}
			}
		}
		for _, r := range planRows {
			if err := tx.Purge(r.table, r.key); err != nil {
				return err
			}
		}
		// Children first, so no foreign key ever points at a removed row.
		for _, x := range []struct {
			table string
			ids   []string
		}{{"notes", notes}, {"documents", docs}, {"list_entries", entries}, {"lists", lists}, {"reading_plans", plans}, {"collections", colls}, {"folders", folders}, {"anchors", anchors}} {
			for _, id := range x.ids {
				if err := tx.Purge(x.table, store.ByID(id)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return Purged{}, err
	}
	return out, nil
}

// pastAnchors adds to found every anchor a row has pointed to, from its history and its current row.
func pastAnchors(ctx context.Context, env *command.Env, table, id string, found map[string]bool) error {
	rows, err := env.Store.Query(ctx, `SELECT op, before_json, after_json FROM changes WHERE object_type = ? AND object_id = ?
		AND (field = 'anchor_id' OR op = 'create')`, table, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var op string
		var before, after sql.NullString
		if err := rows.Scan(&op, &before, &after); err != nil {
			return err
		}
		for _, raw := range []sql.NullString{before, after} {
			if !raw.Valid {
				continue
			}
			if op == "create" {
				var row struct {
					Anchor *string `json:"anchor_id"`
				}
				if json.Unmarshal([]byte(raw.String), &row) == nil && row.Anchor != nil {
					found[*row.Anchor] = true
				}
			} else {
				var a string
				if json.Unmarshal([]byte(raw.String), &a) == nil && a != "" {
					found[a] = true
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var cur sql.NullString
	err = env.Store.QueryRow(ctx, `SELECT anchor_id FROM `+table+` WHERE id = ?`, id).Scan(&cur)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if cur.Valid {
		found[cur.String] = true
	}
	return nil
}

// unusedAnchors keeps the anchors that still exist and that nothing outside the purge points to.
func unusedAnchors(ctx context.Context, env *command.Env, places map[string]bool, notes, entries []string) ([]string, error) {
	skipNotes, skipEntries := append([]string{""}, notes...), append([]string{""}, entries...)
	out := []string{}
	for a := range places {
		var exists, used int
		args := append([]any{a, a}, anys(skipNotes)...)
		args = append(append(args, a, a), anys(skipEntries)...)
		err := env.Store.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM anchors WHERE id = ?),
			(SELECT COUNT(*) FROM notes WHERE anchor_id = ? AND id NOT IN (`+marks(skipNotes)+`)) +
			(SELECT COUNT(*) FROM highlights WHERE anchor_id = ?) +
			(SELECT COUNT(*) FROM list_entries WHERE anchor_id = ? AND id NOT IN (`+marks(skipEntries)+`))`,
			args...).Scan(&exists, &used)
		if err != nil {
			return nil, err
		}
		if exists == 1 && used == 0 {
			out = append(out, a)
		}
	}
	return out, nil
}

// planRow is one row that belongs to a reading plan: a scheduled day, a reading, or a day marked read.
type planRow struct {
	table string
	key   store.Row
}

// planChildren lists the rows of a plan's days, readings and progress, which go when the plan does.
func planChildren(ctx context.Context, env *command.Env, id string) ([]planRow, error) {
	var out []planRow
	for _, q := range []struct {
		table, cols string
	}{{"plan_progress", "day_no"}, {"plan_schedule", "day_no"}, {"plan_days", "day_no, verse_start"}} {
		rows, err := env.Store.Query(ctx, `SELECT `+q.cols+` FROM `+q.table+` WHERE plan_id = ?`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			key := store.Row{"plan_id": id}
			var day int64
			if q.table == "plan_days" {
				var start sql.NullInt64
				if err = rows.Scan(&day, &start); err == nil {
					key["verse_start"] = start.Int64
				}
			} else {
				err = rows.Scan(&day)
			}
			if err != nil {
				rows.Close()
				return nil, err
			}
			key["day_no"] = day
			out = append(out, planRow{q.table, key})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
