package appdir

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

// On Linux the library lives in the XDG config folder: $XDG_CONFIG_HOME/Glossa, or ~/.config/Glossa
// when that is unset (os.UserConfigDir; the app and the CLI both use it with DataDir).
func TestLinuxLibraryFolderFollowsXDGConfigHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG folders are Linux only")
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir, err := os.UserConfigDir()
	if err != nil || dir != cfg {
		t.Fatalf("UserConfigDir = %q, %v; want %q", dir, err, cfg)
	}
	if got := DataDir(dir, t.Logf); got != filepath.Join(cfg, "Glossa") {
		t.Fatalf("DataDir = %s", got)
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	dir, err = os.UserConfigDir()
	if err != nil || dir != filepath.Join(home, ".config") {
		t.Fatalf("UserConfigDir = %q, %v; want ~/.config", dir, err)
	}
	// A folder made under the old name is carried over here too.
	mk(t, filepath.Join(dir, "Xegesis"), "library.db", "x")
	got := DataDir(dir, t.Logf)
	if got != filepath.Join(home, ".config", "Glossa") || read(t, filepath.Join(got, "library.db")) != "x" {
		t.Fatalf("DataDir = %s", got)
	}
}
