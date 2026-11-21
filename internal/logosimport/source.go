package logosimport

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// readOnlyURI is the SQLite URI that opens path for reading only: file:///C:/a/b.db on Windows,
// file:///Users/a/b.db elsewhere, with spaces and the like escaped ("Application Support").
// "file:/var/…" is read as having an authority "var" on macOS and Linux, so the three slashes
// matter.
func readOnlyURI(path string) string {
	return fileURI(path) + "?_pragma=query_only(1)&_pragma=busy_timeout(3000)"
}

// fileURI is the plain file:/// URI for path, for SQLite (tests open their fixtures with it).
func fileURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// The user's own data files, relative to the user's folder under Documents or Data. These and the
// catalogue's metadata are all the importer reads.
const (
	fileNotes     = "NotesToolManager/notestool.db"
	fileClippings = "Documents/Clippings/Clippings.db"
	filePassages  = "Documents/PassageList/PassageList.db"
	fileWords     = "Documents/WordList/wordlist.db"
	filePrayers   = "Documents/PrayerList/PrayerList.db"
	filePlans     = "Documents/ReadingPlan/ReadingPlan.db"
	fileSermons   = "Documents/Sermon/Sermon.db"
	fileCatalog   = "LibraryCatalog/catalog.db"
)

type fileSpec struct {
	name string
	rel  string
	data bool // under Data, not Documents
}

var allFiles = []fileSpec{
	{"notes", fileNotes, false},
	{"clippings", fileClippings, false},
	{"passages", filePassages, false},
	{"words", fileWords, false},
	{"prayers", filePrayers, false},
	{"plans", filePlans, false},
	{"sermons", fileSermons, false},
	{"catalog", fileCatalog, true},
}

// Source is a folder with Logos data: its Data and Documents folders for one user.
type Source struct {
	Root      string `json:"root" desc:"The Logos data folder"`
	Data      string `json:"-"`
	Documents string `json:"-"`
}

// ErrNotFound means there is no Logos data to read.
var ErrNotFound = errors.New("no Logos data found")

// Find looks for Logos data. root is a folder holding Data and Documents (the Logos folder under
// the user's local application data); empty means the usual places. With several Logos users the
// most recently used is taken.
func Find(root string) (Source, error) {
	var roots []string
	if root != "" {
		roots = []string{root}
	} else {
		roots = defaultRoots()
	}
	for _, r := range roots {
		docs, ok1 := userDir(filepath.Join(r, "Documents"))
		data, ok2 := userDir(filepath.Join(r, "Data"))
		if ok1 || ok2 {
			if !ok1 {
				docs = ""
			}
			if !ok2 {
				data = ""
			}
			return Source{Root: r, Data: data, Documents: docs}, nil
		}
	}
	return Source{}, ErrNotFound
}

func defaultRoots() []string {
	var out []string
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		out = append(out, filepath.Join(local, "Logos"), filepath.Join(local, "Verbum"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		if runtime.GOOS == "darwin" {
			out = append(out, filepath.Join(home, "Library", "Application Support", "Logos4"))
		}
		out = append(out, filepath.Join(home, "AppData", "Local", "Logos"))
	}
	return out
}

// userDir picks the user's folder inside parent: the most recently changed one.
func userDir(parent string) (string, bool) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", false
	}
	type cand struct {
		path string
		mod  int64
	}
	var cs []cand
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil {
			cs = append(cs, cand{filepath.Join(parent, e.Name()), info.ModTime().UnixNano()})
		}
	}
	if len(cs) == 0 {
		return "", false
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].mod > cs[j].mod })
	return cs[0].path, true
}

// Snapshot is a temporary copy of the data files. Everything is read from it; Close deletes it.
type Snapshot struct {
	dir   string
	have  map[string]string // file name -> path of the copy
	dbs   []*sql.DB
	Files []string // names of the files that were found, for the report
}

// Snapshot copies the user's data files (and their write-ahead logs, so recent changes are
// included) into a temporary folder. The originals are only read.
func (s Source) Snapshot(tmpParent string) (*Snapshot, error) {
	dir, err := os.MkdirTemp(tmpParent, "glossa-logos-")
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{dir: dir, have: map[string]string{}}
	for _, f := range allFiles {
		base := s.Documents
		if f.data {
			base = s.Data
		}
		if base == "" {
			continue
		}
		src := filepath.Join(base, filepath.FromSlash(f.rel))
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(dir, f.name+".db")
		if err := copyFile(src, dst); err != nil {
			snap.Close()
			return nil, fmt.Errorf("copy %s: %w", f.name, err)
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(src + suffix); err == nil {
				if err := copyFile(src+suffix, dst+suffix); err != nil {
					snap.Close()
					return nil, fmt.Errorf("copy %s: %w", f.name, err)
				}
			}
		}
		snap.have[f.name] = dst
		snap.Files = append(snap.Files, f.name)
	}
	if len(snap.have) == 0 {
		snap.Close()
		return nil, ErrNotFound
	}
	return snap, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Has reports whether a data file was found.
func (s *Snapshot) Has(name string) bool { _, ok := s.have[name]; return ok }

// open opens a copied file for reading only. The copy is never written to.
func (s *Snapshot) open(name string) (*sql.DB, error) {
	path, ok := s.have[name]
	if !ok {
		return nil, ErrNotFound
	}
	db, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s.dbs = append(s.dbs, db)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

// Close closes the databases and deletes the copy.
func (s *Snapshot) Close() error {
	for _, db := range s.dbs {
		db.Close()
	}
	s.dbs = nil
	return os.RemoveAll(s.dir)
}
