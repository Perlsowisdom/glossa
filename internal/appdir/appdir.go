// Package appdir holds what the app renamed from Xegesis to Glossa and keeps working for people
// who already used the old name: environment variables and per-user folders.
package appdir

import (
	"os"
	"path/filepath"
	"strings"
)

// Name is the app's name and OldName the name it had before; per-user folders and keychain entries
// made under OldName are carried over.
const (
	Name    = "Glossa"
	OldName = "Xegesis"
)

// Getenv returns the environment variable name (such as GLOSSA_LIBRARY). If it is unset or empty,
// the same variable under the old name (XEGESIS_LIBRARY) is used, so existing set-ups keep working.
func Getenv(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if rest, ok := strings.CutPrefix(name, "GLOSSA_"); ok {
		return os.Getenv("XEGESIS_" + rest)
	}
	return ""
}

// Migrate returns the folder under parent that the app should use for name, moving the folder made
// under oldName there first when only the old one exists. The move is a single rename: nothing is
// copied or deleted, and a folder that already has the new name is never touched. When the rename
// fails (the folder is in use, say), the old folder is used and logf says why.
func Migrate(parent, name, oldName string, rename func(oldPath, newPath string) error, logf func(format string, args ...any)) string {
	newPath := filepath.Join(parent, name)
	oldPath := filepath.Join(parent, oldName)
	if _, err := os.Lstat(newPath); err == nil || !os.IsNotExist(err) {
		return newPath
	}
	if _, err := os.Lstat(oldPath); err != nil {
		return newPath
	}
	if rename == nil {
		rename = os.Rename
	}
	if err := rename(oldPath, newPath); err != nil {
		if logf != nil {
			logf("could not rename %s to %s (%v); continuing to use %s", oldPath, newPath, err, oldPath)
		}
		return oldPath
	}
	return newPath
}

// DataDir is the app's per-user data folder inside the user's config folder (%APPDATA%\Glossa),
// carried over from %APPDATA%\Xegesis when needed (see Migrate).
func DataDir(configDir string, logf func(format string, args ...any)) string {
	return Migrate(configDir, Name, OldName, nil, logf)
}
