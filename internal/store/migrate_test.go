package store

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func rawDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func fakeMigrations(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, body := range files {
		m["migrations/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func TestMigrationsAreRecordedWithTheirTime(t *testing.T) {
	s := open(t)
	n := count(t, s.db, `SELECT COUNT(*) FROM schema_migrations WHERE applied_at > 0`)
	files, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if n != len(files) || n == 0 {
		t.Fatalf("recorded %d migrations, %d files", n, len(files))
	}
}

func TestAnOldLibraryUpgradesWithoutRerunningMigrations(t *testing.T) {
	db := rawDB(t)
	// A library made before schema_migrations existed: two tables and user_version 2.
	if _, err := db.Exec(`CREATE TABLE a (x); CREATE TABLE b (x); PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	fsys := fakeMigrations(map[string]string{
		"0001_a.sql": `CREATE TABLE a (x)`, // would fail if it ran again
		"0002_b.sql": `CREATE TABLE b (x)`,
		"0003_c.sql": `CREATE TABLE c (x)`,
	})
	if err := applyMigrations(db, fsys, "migrations"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM schema_migrations`); n != 3 {
		t.Fatalf("recorded %d, want 3", n)
	}
	if v := count(t, db, `PRAGMA user_version`); v != 3 {
		t.Fatalf("user_version %d, want 3", v)
	}
}

func TestAMissingMiddleMigrationIsApplied(t *testing.T) {
	db := rawDB(t)
	first := fakeMigrations(map[string]string{"0001_a.sql": `CREATE TABLE a (x)`, "0003_c.sql": `CREATE TABLE c (x)`})
	if err := applyMigrations(db, first, "migrations"); err != nil {
		t.Fatal(err)
	}
	// A merge brings in 0002 after 0003 is already applied.
	all := fakeMigrations(map[string]string{"0001_a.sql": `CREATE TABLE a (x)`, "0002_b.sql": `CREATE TABLE b (x)`, "0003_c.sql": `CREATE TABLE c (x)`})
	if err := applyMigrations(db, all, "migrations"); err != nil {
		t.Fatal(err)
	}
	if count(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'b'`) != 1 {
		t.Fatal("the missing migration was not applied")
	}
	if v := count(t, db, `PRAGMA user_version`); v != 3 {
		t.Fatalf("user_version %d, want 3", v)
	}
}

func TestNothingRunsTwice(t *testing.T) {
	db := rawDB(t)
	fsys := fakeMigrations(map[string]string{"0001_a.sql": `CREATE TABLE a (x)`, "0002_b.sql": `CREATE TABLE b (x)`})
	for i := 0; i < 3; i++ {
		if err := applyMigrations(db, fsys, "migrations"); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM schema_migrations`); n != 2 {
		t.Fatalf("recorded %d, want 2", n)
	}
}
