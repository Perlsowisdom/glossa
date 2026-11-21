package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// This file is the store's side of folder sync (ADR 0013): it lets the sync package export this
// device's changes and merge other devices' changes into the library. Nothing here talks to a
// folder or encrypts anything.

// Tables that hold this device's own sync bookkeeping or identity. They are never logged or synced.
func localOnlyTable(name string) bool {
	return name == "changes" || name == "devices" || strings.HasPrefix(name, "sync_")
}

// perDeviceTable is a table whose rows describe one device: paths on its disks. Writes to it are
// logged like any other, but never sent to another device, and changes for it that arrive are
// ignored. What travels for books is shared_files instead (ADR 0013, T20b).
func perDeviceTable(name string) bool { return name == "files" || name == "library_folders" }

// perDeviceSettings are settings that only mean something on the device that made them. A name
// ending in a dot is a prefix.
var perDeviceSettings = []string{"device_id", "library.search_roots", "library.scanned_at.", "backup.schedule"}

func perDeviceObject(typ, id string) bool {
	if perDeviceTable(typ) {
		return true
	}
	if typ == "settings" {
		var key []string
		if json.Unmarshal([]byte(id), &key) != nil || len(key) != 1 {
			return false
		}
		for _, p := range perDeviceSettings {
			if key[0] == p || strings.HasSuffix(p, ".") && strings.HasPrefix(key[0], p) {
				return true
			}
		}
	}
	return false
}

// syncableSQL is a WHERE condition on the changes table that leaves out per-device objects.
const syncableSQL = `object_type NOT IN ('files', 'library_folders') AND NOT (object_type = 'settings' AND
	(object_id IN ('["device_id"]', '["library.search_roots"]', '["backup.schedule"]') OR object_id LIKE '["library.scanned\_at.%' ESCAPE '\'))`

// SetNow replaces the wall clock this store's hybrid clock reads, so tests can simulate a device
// whose clock is wrong. The function returns UTC milliseconds.
func (s *Store) SetNow(now func() int64) {
	s.clock.mu.Lock()
	s.clock.now = now
	s.clock.mu.Unlock()
}

// ObserveClock moves the store's clock past a timestamp received from another device.
func (s *Store) ObserveClock(hlc string) error { return s.clock.Observe(hlc) }

// SetDeviceName changes the name shown for this device.
func (s *Store) SetDeviceName(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET name = ? WHERE id = ?`, name, s.device)
	return err
}

// DeviceName returns the name of a device this library knows of.
func (s *Store) DeviceName(ctx context.Context, id string) (string, error) {
	var n string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM devices WHERE id = ?`, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return n, err
}

// Devices maps the ID of every device this library knows of to its name.
func (s *Store) Devices(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM devices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// SyncState reads one of this device's sync bookkeeping values.
func (s *Store) SyncState(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM sync_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetSyncState writes one of this device's sync bookkeeping values; an empty value deletes it.
func (s *Store) SetSyncState(ctx context.Context, key, value string) error {
	return setState(ctx, s.db, key, value)
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

func setState(ctx context.Context, db execer, key, value string) error {
	if value == "" {
		_, err := db.ExecContext(ctx, `DELETE FROM sync_state WHERE key = ?`, key)
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO sync_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// LocalChanges lists this device's own changes made after the clock value after, oldest first.
// Changes received from other devices are never returned, so each change travels from the device
// that made it.
func (s *Store) LocalChanges(ctx context.Context, after string, limit int) ([]Change, error) {
	rows, err := s.db.QueryContext(ctx, changeSelect+` WHERE device_id = ? AND hlc > ? AND `+syncableSQL+` ORDER BY hlc LIMIT ?`,
		s.device, after, limit)
	if err != nil {
		return nil, err
	}
	return scanChanges(rows)
}

// CountLocalChanges counts this device's own changes made after the clock value after.
func (s *Store) CountLocalChanges(ctx context.Context, after string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM changes WHERE device_id = ? AND hlc > ? AND `+syncableSQL, s.device, after).Scan(&n)
	return n, err
}

const changeSelect = `SELECT id, device_id, hlc, command, object_type, object_id, op, field, before_json, after_json, created_at FROM changes`

func scanChanges(rows *sql.Rows) ([]Change, error) {
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		var field, before, after sql.NullString
		if err := rows.Scan(&c.ID, &c.Device, &c.HLC, &c.Command, &c.ObjectType, &c.ObjectID, &c.Op,
			&field, &before, &after, &c.CreatedAt); err != nil {
			return nil, err
		}
		var err error
		c.Field = field.String
		if c.Before, err = decode(before); err != nil {
			return nil, err
		}
		if c.After, err = decode(after); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SnapshotChanges returns a compact set of changes that rebuilds the library's current state on a
// device that has none: for each object its create, the latest change to each field, and its
// latest delete or purge, from every device. Earlier changes are left out, and the kept updates
// drop their "before" value, so a device that starts from the snapshot does not mistake the gap
// for a text conflict. Values erased by a purge stay erased.
func (s *Store) SnapshotChanges(ctx context.Context) ([]Change, error) {
	rows, err := s.db.QueryContext(ctx, changeSelect+` WHERE `+syncableSQL+` ORDER BY object_type, object_id, hlc`)
	if err != nil {
		return nil, err
	}
	all, err := scanChanges(rows)
	if err != nil {
		return nil, err
	}
	var out []Change
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].ObjectType == all[i].ObjectType && all[j].ObjectID == all[i].ObjectID {
			j++
		}
		out = append(out, compactObject(all[i:j])...)
		i = j
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].HLC < out[b].HLC })
	return out, nil
}

func compactObject(cs []Change) []Change {
	var create, del *Change
	purged := false
	latest := map[string]*Change{}
	for i := range cs {
		c := &cs[i]
		switch c.Op {
		case "create":
			create = c
		case "update":
			latest[c.Field] = c
		case "delete":
			del = c
			if c.Field == "purge" {
				purged = true
			}
		}
	}
	var out []Change
	if purged {
		// Keep the create (values already erased) and the purge: other devices must still learn of it.
		if create != nil {
			out = append(out, *create)
		}
		return append(out, *del)
	}
	if create != nil {
		out = append(out, *create)
	}
	fields := make([]string, 0, len(latest))
	for f := range latest {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	for _, f := range fields {
		u := *latest[f]
		u.Before = nil
		out = append(out, u)
	}
	if del != nil {
		out = append(out, *del)
	}
	return out
}

// CountObjects counts the live objects (rows not deleted) in each table that syncs.
func (s *Store) CountObjects(ctx context.Context) (map[string]int, error) {
	return countObjects(ctx, s, func(q string) (*sql.Row, error) { return s.db.QueryRowContext(ctx, q), nil })
}

func countObjects(_ context.Context, s *Store, run func(string) (*sql.Row, error)) (map[string]int, error) {
	out := map[string]int{}
	for name, tb := range s.tables {
		if localOnlyTable(name) || perDeviceTable(name) || name == "settings" {
			continue
		}
		q := "SELECT COUNT(*) FROM " + name
		if tb.soft {
			q += " WHERE deleted_at IS NULL"
		}
		row, err := run(q)
		if err != nil {
			return nil, err
		}
		var n int
		if err := row.Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			out[name] = n
		}
	}
	return out, nil
}

// Outcome is what merging did to one object.
type Outcome int

const (
	Unchanged Outcome = iota
	Created
	Updated
	Deleted
	Purged
)

// Conflict is a text field edited on two devices: the older edit lost.
type Conflict struct {
	ObjectType, ObjectID, Field string
	Loser, Winner               Change
	New                         bool // not seen before (false when a change was applied twice)
}

// Merged is the result of merging one object.
type Merged struct {
	ObjectType, ObjectID string
	Outcome              Outcome
	Conflicts            []Conflict
}

type objRef struct{ typ, id string }

// Remote is one merge of other devices' changes into the library, inside one transaction.
type Remote struct {
	s       *Store
	tx      *sql.Tx
	ctx     context.Context
	touched map[objRef]bool
}

// ApplyRemote runs fn in a transaction that is committed only if fn succeeds. With dryRun the
// transaction is always rolled back, so fn can report what a merge would do without doing it.
// Foreign keys are checked when the transaction commits, so changes may arrive in any order.
func (s *Store) ApplyRemote(ctx context.Context, dryRun bool, fn func(*Remote) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		tx.Rollback()
		return err
	}
	if err := fn(&Remote{s: s, tx: tx, ctx: ctx, touched: map[objRef]bool{}}); err != nil {
		tx.Rollback()
		return err
	}
	if dryRun {
		return tx.Rollback()
	}
	return tx.Commit()
}

// EnsureDevice records another device (its changes refer to it) and its current name. Pass the ID
// as the name when the name is not known: a known name is kept.
func (r *Remote) EnsureDevice(id, name string) error {
	if id == "" {
		return errors.New("store: device without an ID")
	}
	_, err := r.tx.ExecContext(r.ctx, `INSERT INTO devices (id, name, created_at) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = CASE WHEN excluded.name = excluded.id THEN devices.name ELSE excluded.name END`,
		id, name, nowMS())
	return err
}

// SetState writes a sync bookkeeping value in the merge's transaction.
func (r *Remote) SetState(key, value string) error { return setState(r.ctx, r.tx, key, value) }

// State reads a sync bookkeeping value inside the merge's transaction.
func (r *Remote) State(key string) (string, bool, error) {
	var v string
	err := r.tx.QueryRowContext(r.ctx, `SELECT value FROM sync_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// Counts counts live objects per table, inside the merge's transaction.
func (r *Remote) Counts() (map[string]int, error) {
	return countObjects(r.ctx, r.s, func(q string) (*sql.Row, error) { return r.tx.QueryRowContext(r.ctx, q), nil })
}

// Insert adds a received change to the history if it is not there yet, and marks its object for
// Materialize. It reports whether the change was new. A change that names an unknown table or
// field is refused: it comes from a newer Glossa, and applying part of it would lose data.
func (r *Remote) Insert(c Change) (bool, error) {
	if c.ID == "" || c.Device == "" || c.HLC == "" || c.ObjectType == "" || c.ObjectID == "" {
		return false, errors.New("incomplete change record")
	}
	if perDeviceObject(c.ObjectType, c.ObjectID) {
		return false, nil // paths and settings of another device mean nothing here
	}
	tb, ok := r.s.tables[c.ObjectType]
	if !ok || localOnlyTable(c.ObjectType) {
		return false, fmt.Errorf("change %s is for %q, which this version of Glossa does not have", c.ID, c.ObjectType)
	}
	switch c.Op {
	case "create":
		row, _ := c.After.(map[string]any)
		if r, ok := c.After.(Row); ok {
			row = r
		}
		for col := range row {
			if !tb.columns[col] {
				return false, fmt.Errorf("change %s sets %s.%s, which this version of Glossa does not have", c.ID, c.ObjectType, col)
			}
		}
	case "update":
		if !tb.columns[c.Field] || isKey(tb, c.Field) {
			return false, fmt.Errorf("change %s sets %s.%s, which this version of Glossa does not have", c.ID, c.ObjectType, c.Field)
		}
	case "delete":
	default:
		return false, fmt.Errorf("change %s has an unknown kind %q", c.ID, c.Op)
	}
	if _, err := parseObjectID(tb, c.ObjectID); err != nil {
		return false, fmt.Errorf("change %s: %w", c.ID, err)
	}
	purged, err := r.purged(c.ObjectType, c.ObjectID)
	if err != nil {
		return false, err
	}
	isPurge := c.Op == "delete" && c.Field == "purge"
	if purged || isPurge {
		c.Before, c.After = nil, nil // what was deleted for good stays erased
	}
	b, err := encode(c.Before)
	if err != nil {
		return false, err
	}
	a, err := encode(c.After)
	if err != nil {
		return false, err
	}
	var f any
	if c.Field != "" {
		f = c.Field
	}
	res, err := r.tx.ExecContext(r.ctx, `INSERT OR IGNORE INTO changes
		(id, device_id, hlc, command, object_type, object_id, op, field, before_json, after_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Device, c.HLC, c.Command, c.ObjectType, c.ObjectID, c.Op, f, b, a, c.CreatedAt)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if isPurge {
		if _, err := r.tx.ExecContext(r.ctx, `UPDATE changes SET before_json = NULL, after_json = NULL
			WHERE object_type = ? AND object_id = ?`, c.ObjectType, c.ObjectID); err != nil {
			return false, err
		}
	}
	r.touched[objRef{c.ObjectType, c.ObjectID}] = true
	return true, nil
}

func (r *Remote) purged(typ, id string) (bool, error) {
	var n int
	err := r.tx.QueryRowContext(r.ctx, `SELECT COUNT(*) FROM changes WHERE object_type = ? AND object_id = ?
		AND op = 'delete' AND field = 'purge'`, typ, id).Scan(&n)
	return n > 0, err
}

// Materialize brings every object touched by Insert to the state its full history gives: the latest
// change to each field wins, a delete is a tombstone, a purge removes the row. textFields lists, per
// table, the text fields whose concurrent edits are kept as conflicts. Merging is a pure function
// of the history, so it gives the same result whatever order changes arrived in, and applying
// the same changes again changes nothing.
func (r *Remote) Materialize(textFields map[string][]string) ([]Merged, error) {
	refs := make([]objRef, 0, len(r.touched))
	for ref := range r.touched {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].typ != refs[j].typ {
			return refs[i].typ < refs[j].typ
		}
		return refs[i].id < refs[j].id
	})
	var out []Merged
	for _, ref := range refs {
		m, err := r.materialize(ref, textFields[ref.typ])
		if err != nil {
			return nil, fmt.Errorf("merging %s %s: %w", ref.typ, ref.id, err)
		}
		out = append(out, m)
	}
	r.touched = map[objRef]bool{}
	return out, nil
}

func (r *Remote) objectChanges(ref objRef) ([]Change, error) {
	rows, err := r.tx.QueryContext(r.ctx, changeSelect+` WHERE object_type = ? AND object_id = ? ORDER BY hlc, id`, ref.typ, ref.id)
	if err != nil {
		return nil, err
	}
	return scanChanges(rows)
}

func (r *Remote) currentRow(name string, tb table, key Row) (Row, error) {
	where, args := keyClause(tb, key)
	rows, err := r.tx.QueryContext(r.ctx, fmt.Sprintf("SELECT * FROM %s WHERE %s", name, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return scanRow(rows)
}

func (r *Remote) materialize(ref objRef, text []string) (Merged, error) {
	m := Merged{ObjectType: ref.typ, ObjectID: ref.id}
	tb := r.s.tables[ref.typ]
	key, err := parseObjectID(tb, ref.id)
	if err != nil {
		return m, err
	}
	cs, err := r.objectChanges(ref)
	if err != nil {
		return m, err
	}
	current, err := r.currentRow(ref.typ, tb, key)
	if err != nil {
		return m, err
	}
	hasCreate, purged := false, false
	for _, c := range cs {
		hasCreate = hasCreate || c.Op == "create"
		purged = purged || (c.Op == "delete" && c.Field == "purge")
	}
	where, args := keyClause(tb, key)
	if purged {
		if current != nil {
			if _, err := r.tx.ExecContext(r.ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", ref.typ, where), args...); err != nil {
				return m, err
			}
			m.Outcome = Purged
		}
		return m, nil
	}

	var row Row
	exists := false
	if !hasCreate {
		row, exists = copyRow(current), current != nil
	}
	setBy := map[string]Change{} // the change that gave each field its present value
	isText := map[string]bool{}
	for _, f := range text {
		isText[f] = true
	}
	for _, c := range cs {
		switch c.Op {
		case "create":
			after, _ := c.After.(Row)
			row, exists = copyRow(after), true
			if row == nil {
				row = Row{}
			}
			for f := range row {
				setBy[f] = c
			}
		case "update":
			if !exists {
				continue
			}
			if isText[c.Field] && c.Before != nil {
				prev, had := setBy[c.Field]
				if had && prev.Device != c.Device && !sameValue(c.Before, row[c.Field]) {
					cf, err := r.recordConflict(ref, c.Field, prev, c, row[c.Field])
					if err != nil {
						return m, err
					}
					m.Conflicts = append(m.Conflicts, cf)
				}
			}
			row[c.Field] = c.After
			setBy[c.Field] = c
		case "delete":
			if tb.soft {
				if exists {
					row["deleted_at"] = c.CreatedAt
					setBy["deleted_at"] = c
				}
			} else {
				exists, row = false, nil
			}
		}
	}

	if len(isText) > 0 {
		// Conflicts are a function of the whole history, not of the order changes arrived in: drop
		// any recorded from an earlier, partial view of it.
		keep := map[string]bool{}
		for _, c := range m.Conflicts {
			keep[c.Loser.ID+":"+c.Winner.ID] = true
		}
		rows, err := r.tx.QueryContext(r.ctx, `SELECT id FROM sync_conflicts WHERE object_type = ? AND object_id = ?`, ref.typ, ref.id)
		if err != nil {
			return m, err
		}
		var stale []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return m, err
			}
			if !keep[id] {
				stale = append(stale, id)
			}
		}
		rows.Close()
		for _, id := range stale {
			if _, err := r.tx.ExecContext(r.ctx, `DELETE FROM sync_conflicts WHERE id = ?`, id); err != nil {
				return m, err
			}
		}
	}

	switch {
	case !exists && current == nil:
		return m, nil
	case !exists:
		if _, err := r.tx.ExecContext(r.ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", ref.typ, where), args...); err != nil {
			return m, err
		}
		m.Outcome = Deleted
	case current == nil:
		for _, c := range tb.pk {
			if _, ok := row[c]; !ok {
				row[c] = key[c]
			}
		}
		cols := sortedKeys(row)
		vals := make([]any, len(cols))
		for i, c := range cols {
			vals[i] = row[c]
		}
		q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", ref.typ, strings.Join(cols, ", "),
			strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", "))
		if _, err := r.tx.ExecContext(r.ctx, q, vals...); err != nil {
			return m, err
		}
		m.Outcome = Created
		if tb.soft && row["deleted_at"] != nil {
			m.Outcome = Deleted
		}
	default:
		changed := false
		for _, c := range sortedKeys(row) {
			if isKey(tb, c) || !tb.columns[c] || sameStored(c, current[c], row[c]) {
				continue
			}
			if _, err := r.tx.ExecContext(r.ctx, fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s", ref.typ, c, where),
				append([]any{row[c]}, args...)...); err != nil {
				return m, err
			}
			changed = true
			if c == "deleted_at" && current[c] == nil {
				m.Outcome = Deleted
			}
		}
		if changed && m.Outcome != Deleted {
			m.Outcome = Updated
		}
	}
	return m, nil
}

// sameStored compares a stored value with a merged one. Two deletion times both set count as the
// same: the moment of deletion is not worth rewriting.
func sameStored(col string, a, b any) bool {
	if col == "deleted_at" && a != nil && b != nil {
		return true
	}
	return sameValue(a, b)
}

func (r *Remote) recordConflict(ref objRef, field string, loser, winner Change, loserValue any) (Conflict, error) {
	name := loser.Device
	if n, err := r.deviceName(loser.Device); err == nil && n != "" {
		name = n
	}
	val, err := encode(loserValue)
	if err != nil {
		return Conflict{}, err
	}
	id := loser.ID + ":" + winner.ID
	res, err := r.tx.ExecContext(r.ctx, `INSERT OR IGNORE INTO sync_conflicts
		(id, object_type, object_id, field, loser_change_id, loser_device, loser_value, winner_change_id, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, ref.typ, ref.id, field, loser.ID, loser.Device, val, winner.ID, "Changed on "+name+" too", nowMS())
	if err != nil {
		return Conflict{}, err
	}
	n, _ := res.RowsAffected()
	return Conflict{ObjectType: ref.typ, ObjectID: ref.id, Field: field, Loser: loser, Winner: winner, New: n > 0}, nil
}

func (r *Remote) deviceName(id string) (string, error) {
	var n string
	err := r.tx.QueryRowContext(r.ctx, `SELECT name FROM devices WHERE id = ?`, id).Scan(&n)
	return n, err
}

// SyncConflict is a text edit that lost to a newer edit on another device.
type SyncConflict struct {
	ID          string `json:"id"`
	ObjectType  string `json:"object_type"`
	ObjectID    string `json:"object_id"`
	Field       string `json:"field"`
	ChangeID    string `json:"change_id" desc:"The change in the history that holds the replaced text; restore it to get the text back"`
	Device      string `json:"device"`
	Text        string `json:"text"`
	Note        string `json:"note"`
	CreatedAt   int64  `json:"created_at"`
	DismissedAt int64  `json:"dismissed_at,omitempty"`
}

// SyncConflicts lists the text edits replaced by newer edits, newest first. Dismissed ones are
// left out unless all is true.
func (s *Store) SyncConflicts(ctx context.Context, all bool) ([]SyncConflict, error) {
	q := `SELECT id, object_type, object_id, field, loser_change_id, loser_device, loser_value, note, created_at, dismissed_at
		FROM sync_conflicts`
	if !all {
		q += ` WHERE dismissed_at IS NULL`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncConflict
	for rows.Next() {
		var c SyncConflict
		var val sql.NullString
		var dis sql.NullInt64
		if err := rows.Scan(&c.ID, &c.ObjectType, &c.ObjectID, &c.Field, &c.ChangeID, &c.Device, &val, &c.Note, &c.CreatedAt, &dis); err != nil {
			return nil, err
		}
		if v, err := decode(val); err == nil {
			c.Text, _ = v.(string)
		}
		c.DismissedAt = dis.Int64
		out = append(out, c)
	}
	return out, rows.Err()
}

// DismissSyncConflict marks a replaced edit as dealt with. The text stays in the change history.
func (s *Store) DismissSyncConflict(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sync_conflicts SET dismissed_at = ? WHERE id = ?`, nowMS(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("conflict %s: %w", id, ErrNotFound)
	}
	return nil
}

// parseObjectID is the inverse of objectID.
func parseObjectID(tb table, id string) (Row, error) {
	if len(tb.pk) == 1 && tb.pk[0] == "id" {
		return Row{"id": id}, nil
	}
	d := json.NewDecoder(strings.NewReader(id))
	d.UseNumber()
	var vals []any
	if err := d.Decode(&vals); err != nil || len(vals) != len(tb.pk) {
		return nil, fmt.Errorf("bad object ID %q", id)
	}
	key := Row{}
	for i, c := range tb.pk {
		key[c] = normalize(vals[i])
	}
	return key, nil
}

// PerDevice tells whether a change to this object stays on the device that made it.
func PerDevice(objectType, objectID string) bool { return perDeviceObject(objectType, objectID) }
