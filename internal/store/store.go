// Package store owns library.db. Every write goes through Store.Write, which records each change in
// the append-only changes table (ADR 0004) in the same transaction as the write itself.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Row is a set of column values. Values are nil, int64, float64, string or []byte.
type Row map[string]any

// ByID is the key of a row whose primary key is its id column.
func ByID(id string) Row { return Row{"id": id} }

type table struct {
	columns map[string]bool
	pk      []string // primary key columns in key order
	soft    bool     // has deleted_at, so deletes are soft
}

type Store struct {
	db     *sql.DB
	device string
	clock  *Clock
	tables map[string]table

	delMu   sync.Mutex
	lastDel int64
	wall    func() int64 // the clock deleted_at reads; nil means the real one
}

// Open opens or creates library.db at path and brings its schema up to date.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	if err := s.loadTables(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.loadDevice(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Device is this installation's device ID.
func (s *Store) Device() string { return s.device }

// SchemaVersion is the applied migration number.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	err := s.db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

// Query runs a read-only query. Writes must use Write.
func (s *Store) Query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, q, args...)
}

func (s *Store) QueryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, q, args...)
}

func (s *Store) migrate() error { return applyMigrations(s.db, migrations, "migrations") }

// applyMigrations runs each migration in dir that is not yet recorded in schema_migrations, in number order,
// and records it with the time it ran. PRAGMA user_version is kept at the highest number applied, for older
// builds that read only that. A library made before the table existed counts every migration up to its
// user_version as applied. A migration missing from the middle (an out-of-order merge) is still applied.
func applyMigrations(db *sql.DB, fsys fs.FS, dir string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	var version, recorded int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&recorded); err != nil {
		return err
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	type file struct {
		n    int
		name string
	}
	var files []file
	for _, e := range entries {
		n, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: name must start with a number", e.Name())
		}
		files = append(files, file{n, e.Name()})
	}
	if recorded == 0 && version > 0 {
		for _, f := range files {
			if f.n <= version {
				if _, err := db.Exec(`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, ?)`, f.n, nowMS()); err != nil {
					return err
				}
			}
		}
	}
	for _, f := range files {
		var one int
		err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, f.n).Scan(&one)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		body, err := fs.ReadFile(fsys, dir+"/"+f.name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", f.name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, f.n, nowMS()); err != nil {
			tx.Rollback()
			return err
		}
		if f.n > version {
			version = f.n
			if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", f.n)); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadTables() error {
	names, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var list []string
	for names.Next() {
		var n string
		if err := names.Scan(&n); err != nil {
			names.Close()
			return err
		}
		list = append(list, n)
	}
	names.Close()
	s.tables = map[string]table{}
	for _, name := range list {
		rows, err := s.db.Query(`SELECT name, pk FROM pragma_table_info(?)`, name)
		if err != nil {
			return err
		}
		t := table{columns: map[string]bool{}}
		pks := map[int]string{}
		for rows.Next() {
			var col string
			var pk int
			if err := rows.Scan(&col, &pk); err != nil {
				rows.Close()
				return err
			}
			t.columns[col] = true
			if pk > 0 {
				pks[pk] = col
			}
		}
		rows.Close()
		for i := 1; i <= len(pks); i++ {
			t.pk = append(t.pk, pks[i])
		}
		t.soft = t.columns["deleted_at"]
		s.tables[name] = t
	}
	return nil
}

func (s *Store) loadDevice() error {
	var raw string
	err := s.db.QueryRow(`SELECT value_json FROM settings WHERE key = 'device_id'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		id := NewID()
		host, _ := os.Hostname()
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO devices (id, name, created_at) VALUES (?, ?, ?)`, id, host, nowMS()); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO settings (key, value_json) VALUES ('device_id', ?)`, strconv.Quote(id)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		s.device = id
	} else if err != nil {
		return err
	} else if s.device, err = strconv.Unquote(raw); err != nil {
		return fmt.Errorf("settings.device_id: %w", err)
	}
	s.clock = NewClock(s.device)
	return nil
}

func nowMS() int64 { return time.Now().UnixMilli() }

// deletedMS stamps a soft delete. It is strictly later than the one before it, so a thing deleted
// on its own just before its folder can never tie with the folder (a tie reads as "deleted with it").
// Things deleted together are stamped in order, so each is no earlier than the first.
func (s *Store) deletedMS() int64 {
	s.delMu.Lock()
	defer s.delMu.Unlock()
	t := nowMS()
	if s.wall != nil {
		t = s.wall()
	}
	if t <= s.lastDel {
		t = s.lastDel + 1
	}
	s.lastDel = t
	return t
}

// SetDeleteClock replaces the clock soft deletes read, so tests can freeze it.
func (s *Store) SetDeleteClock(now func() int64) {
	s.delMu.Lock()
	s.wall = now
	s.delMu.Unlock()
}
