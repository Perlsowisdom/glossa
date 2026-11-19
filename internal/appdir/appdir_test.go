package appdir

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func mk(t *testing.T, path, file, content string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOnlyOldExistsIsMoved(t *testing.T) {
	root := t.TempDir()
	mk(t, filepath.Join(root, "Xegesis"), "library.db", "mine")
	got := DataDir(root, t.Logf)
	if got != filepath.Join(root, "Glossa") {
		t.Fatalf("got %s", got)
	}
	if read(t, filepath.Join(got, "library.db")) != "mine" {
		t.Fatal("library did not move with the folder")
	}
	if _, err := os.Stat(filepath.Join(root, "Xegesis")); !os.IsNotExist(err) {
		t.Fatal("old folder should be gone after the rename")
	}
}

func TestNeitherExists(t *testing.T) {
	root := t.TempDir()
	if got := DataDir(root, t.Logf); got != filepath.Join(root, "Glossa") {
		t.Fatalf("got %s", got)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("nothing should be created")
	}
}

func TestBothExistNewUsedOldUntouched(t *testing.T) {
	root := t.TempDir()
	mk(t, filepath.Join(root, "Xegesis"), "library.db", "old")
	mk(t, filepath.Join(root, "Glossa"), "library.db", "new")
	got := DataDir(root, t.Logf)
	if got != filepath.Join(root, "Glossa") {
		t.Fatalf("got %s", got)
	}
	if read(t, filepath.Join(root, "Glossa", "library.db")) != "new" || read(t, filepath.Join(root, "Xegesis", "library.db")) != "old" {
		t.Fatal("neither folder may change when both exist")
	}
}

func TestRenameFailsOldUsed(t *testing.T) {
	root := t.TempDir()
	mk(t, filepath.Join(root, "Xegesis"), "library.db", "old")
	var logged string
	fail := func(string, string) error { return errors.New("in use") }
	got := Migrate(root, "Glossa", "Xegesis", fail, func(f string, a ...any) { logged = f })
	if got != filepath.Join(root, "Xegesis") {
		t.Fatalf("got %s", got)
	}
	if logged == "" {
		t.Fatal("the failure should be logged")
	}
	if read(t, filepath.Join(root, "Xegesis", "library.db")) != "old" {
		t.Fatal("old folder must be intact")
	}
	if _, err := os.Stat(filepath.Join(root, "Glossa")); !os.IsNotExist(err) {
		t.Fatal("no new folder should appear")
	}
}

func TestWebViewFolderNames(t *testing.T) {
	root := t.TempDir()
	mk(t, filepath.Join(root, "Xegesis.exe"), "EBWebView", "x")
	got := Migrate(root, "Glossa.exe", "Xegesis.exe", nil, t.Logf)
	if got != filepath.Join(root, "Glossa.exe") || read(t, filepath.Join(got, "EBWebView")) != "x" {
		t.Fatalf("got %s", got)
	}
}

func TestGetenvFallsBackToOldName(t *testing.T) {
	t.Setenv("GLOSSA_LIBRARY", "")
	t.Setenv("XEGESIS_LIBRARY", "old.db")
	if Getenv("GLOSSA_LIBRARY") != "old.db" {
		t.Fatal("old name should be used when the new one is unset")
	}
	t.Setenv("GLOSSA_LIBRARY", "new.db")
	if Getenv("GLOSSA_LIBRARY") != "new.db" {
		t.Fatal("new name wins")
	}
	if Getenv("GOMEMLIMIT") != os.Getenv("GOMEMLIMIT") {
		t.Fatal("other names pass through")
	}
}
