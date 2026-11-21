package sync

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/Perlsowisdom/glossa/internal/bible"
	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/licence"
)

// libDev is a simulated device with a real library (the library and Bible commands) and its own
// folders for books and modules, all in temp folders.
type libDev struct {
	*dev
	reg     *command.Registry
	env     *command.Env
	bib     *bible.Library
	src     string // where this device's own books are added from
	mine    string // the library folder this device copies its books into
	synced  string // the library folder books from other devices go to
	modules string
}

func newLibDev(t *testing.T, name string) *libDev {
	t.Helper()
	d := newDev(t, name)
	root := t.TempDir()
	l := &libDev{dev: d, src: filepath.Join(root, "src"), mine: filepath.Join(root, "mine"),
		synced: filepath.Join(root, "synced"), modules: filepath.Join(root, "modules")}
	for _, dir := range []string{l.src, l.mine} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	l.bib = bible.NewLibrary(l.modules)
	t.Cleanup(l.bib.Close)
	l.reg = command.NewRegistry()
	library.Register(l.reg)
	bible.RegisterLibrary(l.reg, l.bib)
	t.Cleanup(l.reg.Close)
	l.env = &command.Env{Store: d.st}
	return l
}

// connectFiles opens the folder with this device's real folders for books and modules.
func (l *libDev) connectFiles(folder string) *Engine {
	l.t.Helper()
	e := l.connect(folder)
	e.Now = time.Now
	e.BlobGrace = 0
	e.Local = Locations{BooksDir: l.synced, ModulesDir: l.modules, ReleaseModule: l.bib.Release}
	return e
}

func (l *libDev) run(id string, params, out any) error {
	l.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := l.reg.Run(context.Background(), l.env, id, raw)
	if err != nil || out == nil {
		return err
	}
	b, _ := json.Marshal(res)
	return json.Unmarshal(b, out)
}

func (l *libDev) must(id string, params, out any) {
	l.t.Helper()
	if err := l.run(id, params, out); err != nil {
		l.t.Fatalf("%s: %s: %v", l.name, id, err)
	}
}

const testOPF = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="uid">urn:uuid:TITLE</dc:identifier>
    <dc:title>TITLE</dc:title>
    <dc:creator>An Author</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
</package>`

const testContainer = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

// makeEPUB writes a small valid EPUB with random padding, so its size is about padBytes.
func makeEPUB(t *testing.T, path, title string, padBytes int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	put := func(name, body string, method uint16) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	put("mimetype", "application/epub+zip", zip.Store)
	put("META-INF/container.xml", testContainer, zip.Deflate)
	put("OEBPS/content.opf", strings.ReplaceAll(testOPF, "TITLE", title), zip.Deflate)
	pad := make([]byte, padBytes)
	rand.New(rand.NewSource(int64(len(title)) + int64(padBytes))).Read(pad)
	put("OEBPS/pad.bin", string(pad), zip.Store)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// addBook makes an EPUB and adds it to the library the real way (library.add copies it into this
// device's library folder and scans it). It returns the new item's ID and the file's hash.
func (l *libDev) addBook(title string, padBytes int) (itemID, sha string) {
	l.t.Helper()
	src := filepath.Join(l.src, title+".epub")
	makeEPUB(l.t, src, title, padBytes)
	l.must("library.add", map[string]any{"paths": []string{src}, "into": l.mine}, nil)
	return l.itemByTitle(title)
}

func (l *libDev) itemByTitle(title string) (itemID, sha string) {
	l.t.Helper()
	err := l.st.QueryRow(context.Background(), `SELECT i.id, COALESCE(f.sha256, '') FROM items i
		LEFT JOIN files f ON f.item_id = i.id WHERE i.title = ? AND i.deleted_at IS NULL`, title).Scan(&itemID, &sha)
	if err != nil {
		l.t.Fatalf("%s: no book %q: %v", l.name, title, err)
	}
	return
}

func (l *libDev) hasItem(title string) bool {
	var n int
	if err := l.st.QueryRow(context.Background(), `SELECT COUNT(*) FROM items WHERE title = ? AND deleted_at IS NULL`, title).Scan(&n); err != nil {
		l.t.Fatal(err)
	}
	return n > 0
}

// localFile is the path of the item's file on this device, or "" when it has none here.
func (l *libDev) localFile(itemID string) string {
	l.t.Helper()
	var dir, rel string
	err := l.st.QueryRow(context.Background(), `SELECT lf.path_on_volume, f.rel_path FROM files f
		JOIN library_folders lf ON lf.id = f.folder_id WHERE f.item_id = ? AND f.deleted_at IS NULL`, itemID).Scan(&dir, &rel)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		l.t.Fatal(err)
	}
	return filepath.Join(dir, filepath.FromSlash(rel))
}

// opens checks that the item has a file here that is a readable EPUB with the given hash.
func (l *libDev) opens(itemID, sha string) bool {
	l.t.Helper()
	p := l.localFile(itemID)
	if p == "" {
		return false
	}
	got, _, err := hashFile(p)
	if err != nil || got != sha {
		return false
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "OEBPS/content.opf" {
			return true
		}
	}
	return false
}

// installModule makes a small Bible module from USFM.
func (l *libDev) installModule(id, text string) {
	l.t.Helper()
	src := filepath.Join(l.src, id+".usfm")
	if err := os.WriteFile(src, []byte(text), 0o644); err != nil {
		l.t.Fatal(err)
	}
	personal, _ := licence.Defaults(licence.PersonalPurchase, "")
	_, _, err := l.bib.Install(context.Background(), bible.InstallOptions{Path: src, ID: id, Name: "Test " + id,
		Abbreviation: id, Language: "en", Format: "usfm", Licence: personal})
	if err != nil {
		l.t.Fatalf("%s: install %s: %v", l.name, id, err)
	}
}

const usfmMal = `\id MAL Test
\h Malachi
\mt1 Malachi
\c 3
\p
\v 23 Behold, I will send you Elijah the prophet before the great and terrible day of Yahweh comes.
\v 24 He will turn the hearts of the fathers to the children.
`

func (l *libDev) readModule(id string) string {
	l.t.Helper()
	var p bible.Passage
	if err := l.run("bible.read", map[string]any{"module": id, "ref": "Malachi 3:23"}, &p); err != nil {
		return ""
	}
	if len(p.Verses) == 0 {
		return ""
	}
	return p.Verses[0].Text
}

func (l *libDev) moduleIDs() []string {
	var out []string
	for _, id := range moduleIDs(l.modules) {
		out = append(out, id)
	}
	return out
}

// blobFiles lists the files in the folder's blobs/ with their sizes.
func blobFiles(t *testing.T, folder string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	es, err := os.ReadDir(filepath.Join(folder, RootName, "blobs"))
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range es {
		info, _ := de.Info()
		out[de.Name()] = info.Size()
	}
	return out
}

func manifests(files map[string]int64) int {
	n := 0
	for name := range files {
		if len(name) > 2 && name[len(name)-2:] == ".m" {
			n++
		}
	}
	return n
}

// spy wraps a transport to record reads and to fail on demand.
type spy struct {
	Transport
	mu         gosync.Mutex
	chunkReads int
	creates    int
	failAfter  int // fail Create of a chunk once this many chunks were created; 0 = never
}

func (s *spy) Read(ctx context.Context, name string) ([]byte, error) {
	if len(name) > 2 && name[len(name)-2:] == ".c" {
		s.mu.Lock()
		s.chunkReads++
		s.mu.Unlock()
	}
	return s.Transport.Read(ctx, name)
}

func (s *spy) Create(ctx context.Context, name string, data []byte) error {
	if len(name) > 2 && name[len(name)-2:] == ".c" {
		s.mu.Lock()
		s.creates++
		n, limit := s.creates, s.failAfter
		s.mu.Unlock()
		if limit > 0 && n > limit {
			return ErrDiskFull
		}
	}
	return s.Transport.Create(ctx, name, data)
}

func (s *spy) reads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chunkReads
}

// moduleMeta is the module's licence as bible.modules reports it, as text.
func (l *libDev) moduleMeta(id string) string {
	l.t.Helper()
	var all []bible.Meta
	l.must("bible.modules", map[string]any{"kind": "all"}, &all)
	for _, m := range all {
		if m.ID == id {
			b, _ := json.Marshal(m.Licence)
			return string(b)
		}
	}
	return ""
}

func (l *libDev) licenceID(itemID string) string {
	l.t.Helper()
	var id string
	if err := l.st.QueryRow(context.Background(), `SELECT id FROM licences WHERE item_id = ?`, itemID).Scan(&id); err != nil {
		l.t.Fatal(err)
	}
	return id
}
