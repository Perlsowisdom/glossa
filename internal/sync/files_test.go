package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perlsowisdom/glossa/internal/store"
)

func twoDevices(t *testing.T) (a, b *libDev, folder string) {
	t.Helper()
	folder = folderOf(t)
	a, b = newLibDev(t, "Laptop"), newLibDev(t, "Desktop")
	a.connectFiles(folder)
	b.connectFiles(folder)
	return a, b, folder
}

// leftovers lists temporary download files in a folder.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".glossa-download-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestBookAppearsAndOpensOnOtherDevice(t *testing.T) {
	a, b, folder := twoDevices(t)
	id, sha := a.addBook("Mere Christianity", 50_000)

	res := a.sync()
	if res.Blobs.Uploaded != 1 || len(res.Blobs.Failed) != 0 {
		t.Fatalf("A uploaded %+v", res.Blobs)
	}
	if got := blobFiles(t, folder); manifests(got) != 1 {
		t.Fatalf("blobs = %v", got)
	}
	// Nothing in the folder is plain: neither the title nor the book's bytes.
	filepath.WalkDir(folder, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), "Mere Christianity") || strings.Contains(string(raw), "PK\x03\x04") {
				t.Errorf("%s holds plain text", p)
			}
		}
		return nil
	})

	if b.hasItem("Mere Christianity") {
		t.Fatal("B has the book before syncing")
	}
	res = b.sync()
	if res.Blobs.Downloaded != 1 {
		t.Fatalf("B downloaded %+v", res.Blobs)
	}
	if !b.hasItem("Mere Christianity") {
		t.Fatal("the book did not appear on B")
	}
	if !b.opens(id, sha) {
		t.Fatal("the book on B does not open")
	}
	if p := b.localFile(id); !strings.HasPrefix(p, b.synced) {
		t.Fatalf("B keeps it at %s, not in its synced-books folder", p)
	}
	if left := leftovers(t, b.synced); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
	// B's library folder for it is a real library folder: a scan finds the file where it is.
	var scanned struct{ Added, Unchanged, Removed int }
	var fid string
	if err := b.st.QueryRow(context.Background(), `SELECT folder_id FROM files WHERE item_id = ?`, id).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	b.must("library.scan", map[string]any{"folder_id": fid}, &scanned)
	if scanned.Added != 0 || scanned.Removed != 0 {
		t.Fatalf("a scan of the synced folder changed things: %+v", scanned)
	}
	if !b.opens(id, sha) {
		t.Fatal("not open after a scan")
	}

	// A second sync on both does nothing new, and the licence came with the book.
	if r := a.sync(); r.Blobs.Uploaded+r.Blobs.Downloaded != 0 {
		t.Fatalf("A again: %+v", r.Blobs)
	}
	if r := b.sync(); r.Blobs.Uploaded+r.Blobs.Downloaded != 0 {
		t.Fatalf("B again: %+v", r.Blobs)
	}
	for _, l := range []*libDev{a, b} {
		var typ string
		if err := l.st.QueryRow(context.Background(), `SELECT type FROM licences WHERE item_id = ?`, id).Scan(&typ); err != nil || typ == "" {
			t.Fatalf("%s: licence = %q, %v", l.name, typ, err)
		}
	}
}

func TestOnDemandDownloadsWhenOpened(t *testing.T) {
	a, b, folder := twoDevices(t)
	if err := b.eng.SetBooksMode(context.Background(), BooksWhenOpened); err != nil {
		t.Fatal(err)
	}
	if err := b.eng.SetBooksMode(context.Background(), "sometimes"); err == nil {
		t.Fatal("a made-up mode was accepted")
	}
	id, sha := a.addBook("Pilgrim's Progress", 120_000)
	a.sync()

	var events []BlobProgress
	b.eng.OnProgress = func(p BlobProgress) { events = append(events, p) }
	res := b.sync()
	if res.Blobs.Downloaded != 0 || res.Blobs.OnDemand != 1 {
		t.Fatalf("on-demand sync: %+v", res.Blobs)
	}
	if !b.hasItem("Pilgrim's Progress") {
		t.Fatal("the book is not listed on B")
	}
	if b.localFile(id) != "" {
		t.Fatal("B downloaded the book at sync time in on-demand mode")
	}
	st, err := b.eng.Stats(context.Background())
	if err != nil || st.Mode != BooksWhenOpened || st.RemoteOnly != 1 || st.RemoteSize == 0 {
		t.Fatalf("stats = %+v, %v", st, err)
	}
	if len(events) != 0 {
		t.Fatalf("progress reported with nothing to download: %v", events)
	}
	if left := blobFiles(t, folder); manifests(left) != 1 {
		t.Fatal("the blob should stay in the folder")
	}

	// Opening it downloads it, with progress.
	got, err := b.eng.EnsureBook(context.Background(), id)
	if err != nil || got.Downloaded != 1 || got.OnDevice {
		t.Fatalf("EnsureBook = %+v, %v", got, err)
	}
	if !b.opens(id, sha) {
		t.Fatal("the book does not open after downloading")
	}
	if len(events) == 0 {
		t.Fatal("no progress events")
	}
	last := events[len(events)-1]
	first := events[0]
	if first.Direction != "download" || first.BytesTotal == 0 || last.BytesDone != last.BytesTotal ||
		last.FilesDone != 1 || last.FilesTotal != 1 {
		t.Fatalf("progress = first %+v last %+v", first, last)
	}
	for i := 1; i < len(events); i++ {
		if events[i].BytesDone < events[i-1].BytesDone {
			t.Fatalf("bytes went backwards: %+v then %+v", events[i-1], events[i])
		}
	}
	if p := b.eng.Progress(); p.Active {
		t.Fatalf("still active after finishing: %+v", p)
	}
	again, err := b.eng.EnsureBook(context.Background(), id)
	if err != nil || !again.OnDevice || again.Downloaded != 0 {
		t.Fatalf("second EnsureBook = %+v, %v", again, err)
	}
	if _, err := b.eng.EnsureBook(context.Background(), "no-such-item"); !errors.Is(err, ErrNotShared) {
		t.Fatalf("unknown book: %v", err)
	}
	if st, _ := b.eng.Stats(context.Background()); st.RemoteOnly != 0 {
		t.Fatalf("stats after = %+v", st)
	}
}

func TestSameBookOnBothDevicesIsOneBlob(t *testing.T) {
	a, b, folder := twoDevices(t)
	spyB := &spy{Transport: b.eng.t}
	b.eng.t = spyB
	_, sha := a.addBook("Confessions", 80_000)
	_, shaB := b.addBook("Confessions", 80_000)
	if sha != shaB {
		t.Fatal("the test books differ")
	}
	a.sync()
	res := b.sync()
	if got := blobFiles(t, folder); manifests(got) != 1 {
		t.Fatalf("expected one blob, have %v", got)
	}
	if res.Blobs.Uploaded != 0 {
		t.Fatalf("B uploaded a blob the folder had: %+v", res.Blobs)
	}
	// A's copy of the item is on B too, as B's own file was linked rather than downloaded.
	if res.Blobs.Linked != 1 || res.Blobs.Downloaded != 0 || spyB.reads() != 0 {
		t.Fatalf("B: %+v, chunk reads %d", res.Blobs, spyB.reads())
	}
	a.sync()
	var n int
	if err := a.st.QueryRow(context.Background(), `SELECT COUNT(*) FROM items WHERE title = 'Confessions' AND deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("A has %d items for the book, want 2 (one per device; merging them is not done)", n)
	}
	if blobs := manifests(blobFiles(t, folder)); blobs != 1 {
		t.Fatalf("blobs = %d", blobs)
	}
}

func TestDeletePropagatesAndBlobIsCollected(t *testing.T) {
	a, b, folder := twoDevices(t)
	id, sha := a.addBook("Letters and Papers", 60_000)
	other, otherSHA := a.addBook("Keep Me", 30_000)
	a.sync()
	b.sync()
	if !b.opens(id, sha) {
		t.Fatal("B should have the book")
	}
	path := b.localFile(id)

	// A deletes the book the way the library does when a file goes: file and item.
	a.eng.BlobGrace, b.eng.BlobGrace = 24*time.Hour, 24*time.Hour // the bin could still restore it
	a.write("library.delete", func(tx *store.Tx) error { return tx.Delete("items", store.ByID(id)) })
	a.sync()
	if got := blobFiles(t, folder); manifests(got) != 2 {
		t.Fatalf("the blob must stay during the grace period: %v", got)
	}
	res := b.sync()
	if b.hasItem("Letters and Papers") {
		t.Fatal("the deletion did not reach B")
	}
	if res.Blobs.Removed != 1 {
		t.Fatalf("B removed %+v", res.Blobs)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("B still has the file: %v", err)
	}
	if b.localFile(id) != "" {
		t.Fatal("B still has a file record")
	}
	if !b.hasItem("Keep Me") || !b.opens(other, otherSHA) {
		t.Fatal("the other book was affected")
	}

	// With no grace, the next sync on any device collects the blob nobody uses.
	a.eng.BlobGrace = 0
	res = a.sync()
	if res.Blobs.Collected != 1 {
		t.Fatalf("collected %+v", res.Blobs)
	}
	got := blobFiles(t, folder)
	if manifests(got) != 1 {
		t.Fatalf("blobs after collection: %v", got)
	}
	for name := range got {
		if strings.HasPrefix(name, sha) {
			t.Fatalf("the deleted book's blob is still there: %s", name)
		}
	}
	// B's sync is still clean and does not bring it back.
	if r := b.sync(); r.Blobs.Downloaded != 0 || len(r.Blobs.Failed) != 0 {
		t.Fatalf("B after collection: %+v", r.Blobs)
	}
}

func TestModuleInstallSyncsAndIsUsable(t *testing.T) {
	a, b, folder := twoDevices(t)
	a.installModule("test-bible", usfmMal)
	if a.readModule("test-bible") == "" {
		t.Fatal("A cannot read its own module")
	}
	if res := a.sync(); res.Blobs.Uploaded != 1 {
		t.Fatalf("A: %+v", res.Blobs)
	}
	// Modules come at sync time even when books are on demand.
	if err := b.eng.SetBooksMode(context.Background(), BooksWhenOpened); err != nil {
		t.Fatal(err)
	}
	if res := b.sync(); res.Blobs.Downloaded != 1 {
		t.Fatalf("B: %+v", res.Blobs)
	}
	if got := b.readModule("test-bible"); !strings.Contains(got, "Elijah") {
		t.Fatalf("B reads %q", got)
	}
	am, bm := a.moduleMeta("test-bible"), b.moduleMeta("test-bible")
	if am != bm || !strings.Contains(am, "personal") && !strings.Contains(am, "Personal") {
		t.Fatalf("the licence did not travel with the module: %q vs %q", am, bm)
	}
	if manifests(blobFiles(t, folder)) != 1 {
		t.Fatal("one module, one blob")
	}

	// Removing it on A removes it on B, and then the blob (B holds off collecting so A does it).
	a.eng.BlobGrace, b.eng.BlobGrace = 24*time.Hour, 24*time.Hour
	a.must("bible.remove", map[string]any{"module": "test-bible"}, nil)
	a.sync()
	if res := b.sync(); res.Blobs.Removed != 1 {
		t.Fatalf("B removed %+v", res.Blobs)
	}
	if ids := b.moduleIDs(); len(ids) != 0 {
		t.Fatalf("B still has %v", ids)
	}
	a.eng.BlobGrace = 0
	if res := a.sync(); res.Blobs.Collected != 1 || manifests(blobFiles(t, folder)) != 0 {
		t.Fatalf("collected %+v, blobs %v", res.Blobs, blobFiles(t, folder))
	}
}

func TestModuleChangedOnOneDeviceReachesTheOther(t *testing.T) {
	a, b, _ := twoDevices(t)
	a.installModule("kjv-test", usfmMal)
	a.sync()
	b.sync()
	if b.readModule("kjv-test") == "" {
		t.Fatal("B has no module")
	}
	// B opens it (the Bible library keeps it open), then A changes its licence: the new file replaces B's.
	a.must("bible.set_licence", map[string]any{"module": "kjv-test", "type": "public_domain"}, nil)
	if res := a.sync(); res.Blobs.Uploaded != 1 {
		t.Fatalf("A: %+v", res.Blobs)
	}
	if res := b.sync(); res.Blobs.Downloaded != 1 {
		t.Fatalf("B: %+v", res.Blobs)
	}
	if !strings.Contains(b.moduleMeta("kjv-test"), "public") {
		t.Fatalf("B's licence = %s", b.moduleMeta("kjv-test"))
	}
	if r := b.sync(); r.Blobs.Uploaded+r.Blobs.Downloaded != 0 {
		t.Fatalf("B settles: %+v", r.Blobs)
	}
	if r := a.sync(); r.Blobs.Uploaded+r.Blobs.Downloaded != 0 {
		t.Fatalf("A settles: %+v", r.Blobs)
	}
}

func TestFilesAndFoldersStayOnTheirDevice(t *testing.T) {
	a, b, folder := twoDevices(t)
	a.addBook("Local Paths", 10_000)
	a.write("settings", func(tx *store.Tx) error {
		return tx.Create("settings", store.Row{"key": "library.search_roots", "value_json": `["C:\\\\Users\\\\a\\\\Books"]`})
	})
	a.sync()
	b.sync()
	plain := readAll(t, a.eng, folder)
	for _, bad := range []string{`"object_type":"files"`, `"object_type":"library_folders"`, "search_roots", a.mine, a.src} {
		if strings.Contains(plain, bad) {
			t.Fatalf("a per-device value was sent: %s", bad)
		}
	}
	if !strings.Contains(plain, `"object_type":"shared_files"`) || !strings.Contains(plain, `"object_type":"items"`) {
		t.Fatal("the book's identity was not sent")
	}
	var n int
	b.st.QueryRow(context.Background(), `SELECT COUNT(*) FROM settings WHERE key = 'library.search_roots'`).Scan(&n)
	if n != 0 {
		t.Fatal("A's search folders reached B")
	}
	// B's library folders are its own: the one for synced books, and none of A's.
	rows, err := b.st.Query(context.Background(), `SELECT path_on_volume FROM library_folders`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		rows.Scan(&p)
		if p != b.synced {
			t.Fatalf("B has a library folder at %s", p)
		}
	}
	// The preview counts books, not paths.
	pv, err := b.eng.Preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pv.InFolder["files"] != 0 || pv.InFolder["library_folders"] != 0 {
		t.Fatalf("preview counts paths: %v", pv.InFolder)
	}
}

func TestLicenceThatForbidsStoringIsNotUploaded(t *testing.T) {
	a, b, folder := twoDevices(t)
	id, _ := a.addBook("Restricted", 10_000)
	a.write("licence", func(tx *store.Tx) error {
		return tx.Update("licences", store.Row{"id": a.licenceID(id)}, store.Row{"store_allowed": int64(0)})
	})
	res := a.sync()
	if res.Blobs.Uploaded != 0 || res.Blobs.Skipped == 0 || manifests(blobFiles(t, folder)) != 0 {
		t.Fatalf("A: %+v, blobs %v", res.Blobs, blobFiles(t, folder))
	}
	b.sync()
	if !b.hasItem("Restricted") || b.localFile(id) != "" {
		t.Fatal("B should know of the book but not have a copy")
	}
}

func TestTamperedTruncatedAndMissingBlobsAreRefused(t *testing.T) {
	cases := []struct {
		name   string
		damage func(t *testing.T, dir string, sha string)
		waits  bool
	}{
		{"altered", func(t *testing.T, dir, sha string) {
			p := filepath.Join(dir, sha+".000000.c")
			raw, _ := os.ReadFile(p)
			raw[len(raw)/2] ^= 0xFF
			os.WriteFile(p, raw, 0o644)
		}, false},
		{"truncated", func(t *testing.T, dir, sha string) {
			p := filepath.Join(dir, sha+".000000.c")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, raw[:len(raw)/2], 0o644)
		}, false},
		{"swapped chunks", func(t *testing.T, dir, sha string) {
			p0, p1 := filepath.Join(dir, sha+".000000.c"), filepath.Join(dir, sha+".000001.c")
			r0, _ := os.ReadFile(p0)
			r1, _ := os.ReadFile(p1)
			os.WriteFile(p0, r1, 0o644)
			os.WriteFile(p1, r0, 0o644)
		}, false},
		{"missing chunk", func(t *testing.T, dir, sha string) {
			os.Remove(filepath.Join(dir, sha+".000001.c"))
		}, true},
		{"no manifest", func(t *testing.T, dir, sha string) {
			os.Remove(filepath.Join(dir, sha+".m"))
		}, true},
		{"manifest altered", func(t *testing.T, dir, sha string) {
			p := filepath.Join(dir, sha+".m")
			raw, _ := os.ReadFile(p)
			raw[len(raw)-3] ^= 1
			os.WriteFile(p, raw, 0o644)
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b, folder := twoDevices(t)
			a.eng.BlobChunk = 64 * 1024
			id, sha := a.addBook("Damaged", 150_000) // several chunks
			a.sync()
			c.damage(t, filepath.Join(folder, RootName, "blobs"), sha)

			res, err := b.eng.Sync(context.Background())
			if err != nil {
				t.Fatalf("the sync as a whole should go on: %v", err)
			}
			switch {
			case c.waits && (res.Blobs.Waiting != 1 || len(res.Blobs.Failed) != 0):
				t.Fatalf("want one file waiting: %+v", res.Blobs)
			case !c.waits && (len(res.Blobs.Failed) != 1 || res.Blobs.Failed[0].Error == ""):
				t.Fatalf("want one failure: %+v", res.Blobs)
			}
			if res.Blobs.Downloaded != 0 {
				t.Fatalf("something was installed: %+v", res.Blobs)
			}
			// Nothing half-installed: no file, no record, no temporary file, no library folder files.
			if b.localFile(id) != "" {
				t.Fatal("a file record was made")
			}
			if es, _ := os.ReadDir(b.synced); len(es) != 0 {
				var names []string
				for _, e := range es {
					names = append(names, e.Name())
				}
				t.Fatalf("books folder holds %v", names)
			}
			if _, err := b.eng.EnsureBook(context.Background(), id); err == nil {
				t.Fatal("opening the damaged book succeeded")
			} else if !c.waits && !errors.Is(err, ErrCorrupt) || c.waits && !errors.Is(err, ErrMissingFile) {
				t.Fatalf("EnsureBook error = %v", err)
			}
			if b.localFile(id) != "" || len(leftovers(t, b.synced)) != 0 {
				t.Fatal("opening left something behind")
			}
		})
	}
}

func TestLargeFileIsChunkedAndRoundTrips(t *testing.T) {
	a, b, folder := twoDevices(t)
	a.eng.BlobChunk = 1 << 20
	id, sha := a.addBook("Big Commentary", 3_500_000)
	a.sync()
	files := blobFiles(t, folder)
	chunks := 0
	for name, size := range files {
		if strings.HasSuffix(name, ".c") {
			chunks++
			if size > (1<<20)+1024 {
				t.Fatalf("chunk %s is %d bytes, more than one piece", name, size)
			}
		}
	}
	if chunks != 4 {
		t.Fatalf("%d chunks for a 3.5 MB file in 1 MiB pieces: %v", chunks, files)
	}
	var events []BlobProgress
	b.eng.OnProgress = func(p BlobProgress) { events = append(events, p) }
	b.sync()
	if !b.opens(id, sha) {
		t.Fatal("the big book does not open on B")
	}
	// Progress moved piece by piece up to the full size.
	if len(events) < 4 {
		t.Fatalf("only %d progress events for 4 pieces", len(events))
	}
	if last := events[len(events)-1]; last.BytesDone != last.BytesTotal || last.BytesTotal < 3_500_000 {
		t.Fatalf("last progress %+v", last)
	}
}

func TestUnfinishedUploadIsNotACompleteBlobAndResumes(t *testing.T) {
	a, b, folder := twoDevices(t)
	a.eng.BlobChunk = 1 << 20
	sp := &spy{Transport: a.eng.t, failAfter: 2}
	a.eng.t = sp
	id, sha := a.addBook("Interrupted", 3_200_000)

	res, err := a.eng.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Blobs.Uploaded != 0 || len(res.Blobs.Failed) != 1 {
		t.Fatalf("A: %+v", res.Blobs)
	}
	files := blobFiles(t, folder)
	if manifests(files) != 0 || len(files) != 2 {
		t.Fatalf("a part upload must leave chunks and no manifest: %v", files)
	}
	for name := range files {
		if strings.Contains(name, ".tmp") {
			t.Fatalf("temporary name left behind: %s", name)
		}
	}
	// Another device does not take it for a book it can fetch.
	res = b.sync()
	if res.Blobs.Downloaded != 0 || res.Blobs.Waiting != 1 || b.localFile(id) != "" {
		t.Fatalf("B: %+v", res.Blobs)
	}
	// The disk has room again: the next sync keeps the two good chunks and finishes.
	sp.mu.Lock()
	sp.failAfter, sp.creates = 0, 0
	sp.mu.Unlock()
	res = a.sync()
	if res.Blobs.Uploaded != 1 {
		t.Fatalf("A resumed: %+v", res.Blobs)
	}
	if sp.creates != 2 {
		t.Fatalf("A rewrote %d chunks, want only the 2 missing ones", sp.creates)
	}
	res = b.sync()
	if res.Blobs.Downloaded != 1 || !b.opens(id, sha) {
		t.Fatalf("B: %+v", res.Blobs)
	}
}

func TestUnfinishedUploadWithWrongChunkIsRewritten(t *testing.T) {
	a, _, folder := twoDevices(t)
	a.eng.BlobChunk = 1 << 20
	_, sha := a.addBook("Stale", 1_500_000)
	dir := filepath.Join(folder, RootName, "blobs")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, sha+".000000.c"), []byte("left from something else"), 0o644)
	if res := a.sync(); res.Blobs.Uploaded != 1 {
		t.Fatalf("%+v", res.Blobs)
	}
	b := newLibDev(t, "Third")
	b.connectFiles(folder)
	if res := b.sync(); res.Blobs.Downloaded != 1 {
		t.Fatalf("%+v", res.Blobs)
	}
}

func TestThreeDevicesShareBooksAndModules(t *testing.T) {
	folder := folderOf(t)
	a, b, c := newLibDev(t, "A"), newLibDev(t, "B"), newLibDev(t, "C")
	for _, l := range []*libDev{a, b, c} {
		l.connectFiles(folder)
	}
	ida, shaA := a.addBook("From A", 20_000)
	idb, shaB := b.addBook("From B", 25_000)
	a.installModule("a-bible", usfmMal)
	c.eng.SetBooksMode(context.Background(), BooksWhenOpened)
	for i := 0; i < 2; i++ {
		a.sync()
		b.sync()
		c.sync()
	}
	for _, l := range []*libDev{a, b} {
		if !l.opens(ida, shaA) || !l.opens(idb, shaB) {
			t.Fatalf("%s is missing a book", l.name)
		}
	}
	for _, l := range []*libDev{a, b, c} {
		if l.readModule("a-bible") == "" {
			t.Fatalf("%s cannot read the module", l.name)
		}
	}
	if c.localFile(ida) != "" || c.localFile(idb) != "" {
		t.Fatal("C downloaded books it was told to keep in the folder")
	}
	if _, err := c.eng.EnsureBook(context.Background(), idb); err != nil || !c.opens(idb, shaB) {
		t.Fatalf("C opening B's book: %v", err)
	}
	if got := manifests(blobFiles(t, folder)); got != 3 {
		t.Fatalf("%d blobs, want 3", got)
	}
}

func TestStatusAndDevicesReportBlobSpace(t *testing.T) {
	a, _, _ := twoDevices(t)
	a.addBook("Sizes", 200_000)
	a.sync()
	u, err := a.eng.Devices(context.Background())
	if err != nil || u.BlobBytes < 150_000 || u.Bytes < u.BlobBytes {
		t.Fatalf("usage = %+v, %v", u, err)
	}
}
