package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/folder"
	"github.com/Perlsowisdom/glossa/internal/store"
)

func TestCryptoBindsFilesToTheirPlace(t *testing.T) {
	key := deriveKey("pw", []byte("0123456789abcdef"), fastKDF)
	data, err := seal(key, kindChange, "DEVICE", 7, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := open(key, data, kindChange, "DEVICE", 7); err != nil || string(got) != "hello" {
		t.Fatalf("round trip = %q, %v", got, err)
	}
	for name, try := range map[string]func() error{
		"other seq":    func() error { _, err := open(key, data, kindChange, "DEVICE", 8); return err },
		"other device": func() error { _, err := open(key, data, kindChange, "OTHER", 7); return err },
		"other kind":   func() error { _, err := open(key, data, kindSnapshot, "DEVICE", 7); return err },
		"other key": func() error {
			_, err := open(deriveKey("x", []byte("0123456789abcdef"), fastKDF), data, kindChange, "DEVICE", 7)
			return err
		},
		"empty": func() error { _, err := open(key, nil, kindChange, "DEVICE", 7); return err },
	} {
		if err := try(); err == nil {
			t.Errorf("%s: opened", name)
		}
	}
	// Nonces are random: the same text never encrypts the same way twice.
	again, _ := seal(key, kindChange, "DEVICE", 7, []byte("hello"))
	if string(again) == string(data) {
		t.Error("two encryptions were identical")
	}
}

func TestPasswordRules(t *testing.T) {
	if err := CheckPassword("short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("short password accepted: %v", err)
	}
	if err := CheckPassword("long enough pw"); err != nil {
		t.Fatal(err)
	}
	if s, _ := Strength("short"); s != 0 {
		t.Errorf("short = %d", s)
	}
	if s, _ := Strength("aaaaaaaaaaaaaaaaaaaa"); s > 1 {
		t.Errorf("repeated = %d", s)
	}
	if s, h := Strength("tree harbour violet 42 lantern"); s != 4 || h == "" {
		t.Errorf("phrase = %d %q", s, h)
	}
	if err := Initialize(context.Background(), NewFolder(t.TempDir()), "short", fastKDF); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("Initialize accepted a short password: %v", err)
	}
	if err := Initialize(context.Background(), NewFolder(t.TempDir()), password, KDFParams{Time: 1, MemoryKiB: 8, Threads: 1}); err == nil {
		t.Fatal("Initialize accepted a trivial memory cost")
	}
}

func TestKeyFileHasSaltAndNoPassword(t *testing.T) {
	folder := folderOf(t)
	ctx := context.Background()
	if err := Initialize(ctx, NewFolder(folder), password, fastKDF); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, NewFolder(folder), password, fastKDF); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second Initialize = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(folder, RootName, keyName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), password) {
		t.Fatal("password is in the key file")
	}
	// Two folders with the same password get different salts, so different keys.
	f2 := folderOf(t)
	if err := Initialize(ctx, NewFolder(f2), password, fastKDF); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(filepath.Join(f2, RootName, keyName))
	if string(raw) == string(raw2) {
		t.Fatal("two key files are identical")
	}
}

func TestNoteReachesAnotherDeviceAndReapplyIsHarmless(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)

	a.mkNote("N1", "in the beginning")
	a.mkList("L1", "Favourites")
	r := a.sync()
	if r.Pushed != 2 || r.Files != 1 {
		t.Fatalf("A pushed %d changes in %d files; want 2 in 1", r.Pushed, r.Files)
	}
	r = b.sync()
	if r.Pulled.Created != 2 || r.Pulled.Changes != 2 {
		t.Fatalf("B pulled %+v", r.Pulled)
	}
	if body, _, ok := b.note("N1"); !ok || body != "in the beginning" {
		t.Fatalf("B has note %q, %v", body, ok)
	}
	sameLibraries(t, a, b)

	// A second sync finds nothing new and writes nothing.
	before := b.changeCount()
	r = b.sync()
	if r.Pulled != (Summary{}) || r.Pushed != 0 {
		t.Fatalf("second sync = %+v", r)
	}
	if b.changeCount() != before {
		t.Fatal("re-sync added change records")
	}
	// B's copy of A's records came from A: B never writes A's folder, and B's folder has only B's files.
	// B has joined (its marker is there) but has written no change file of its own.
	if f, err := b.eng.listDevice(context.Background(), b.st.Device()); err != nil || len(f.changes)+len(f.snaps) != 0 {
		t.Fatalf("B wrote change files: %+v, %v", f, err)
	}
	if f, err := b.eng.listDevice(context.Background(), a.st.Device()); err != nil || len(f.changes) != 1 {
		t.Fatalf("A's files: %+v, %v", f, err)
	}

	// Forcing the same file to be applied again (a lost cursor) changes nothing either.
	if err := b.st.SetSyncState(context.Background(), stateAppliedP+a.st.Device(), ""); err != nil {
		t.Fatal(err)
	}
	r = b.sync()
	if r.Pulled != (Summary{}) {
		t.Fatalf("re-applying the same file changed things: %+v", r.Pulled)
	}
	sameLibraries(t, a, b)
}

func TestConcurrentEditsToDifferentFieldsBothSurvive(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "body")
	a.sync()
	b.at(2000).sync()

	a.at(3000).setNoteTitle("N1", "A's title")
	b.at(4000).editNote("N1", "B's body")
	a.sync()
	b.sync()
	a.sync()
	sameLibraries(t, a, b)
	body, _, _ := a.note("N1")
	var title string
	a.st.QueryRow(context.Background(), `SELECT title FROM notes WHERE id='N1'`).Scan(&title)
	if body != "B's body" || title != "A's title" {
		t.Fatalf("merged = %q / %q", body, title)
	}
	if c, _ := a.st.SyncConflicts(context.Background(), true); len(c) != 0 {
		t.Fatalf("edits to different fields were reported as a conflict: %v", c)
	}
}

func TestSameFieldLastWriterWinsByClock(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkList("L1", "Old")
	a.sync()
	b.at(1500).sync()

	// B renames first, A renames later: A's is the later write, so it wins on both devices,
	// whatever order they sync in.
	b.at(2000).renameList("L1", "B name")
	a.at(3000).renameList("L1", "A name")
	b.sync()
	a.sync()
	b.sync()
	for _, d := range []*dev{a, b} {
		if n, _ := d.listName("L1"); n != "A name" {
			t.Fatalf("%s sees %q, want the later write", d.name, n)
		}
	}
}

func TestTextEditedOnBothDevicesKeepsBoth(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "original")
	a.sync()
	b.at(1500).sync()

	a.at(2000).editNote("N1", "text from the laptop")
	b.at(3000).editNote("N1", "text from the desktop")
	a.sync()
	b.sync()
	a.sync()

	sameLibraries(t, a, b)
	for _, d := range []*dev{a, b} {
		if body, _, _ := d.note("N1"); body != "text from the desktop" {
			t.Fatalf("%s: newer text should win, got %q", d.name, body)
		}
		cs, err := d.st.SyncConflicts(context.Background(), false)
		if err != nil || len(cs) != 1 {
			t.Fatalf("%s: conflicts = %v, %v", d.name, cs, err)
		}
		c := cs[0]
		if c.Text != "text from the laptop" || c.Note != "Changed on Laptop too" || c.Field != "body_md" {
			t.Fatalf("%s: conflict = %+v", d.name, c)
		}
		// The replaced text is in the version history and can be restored from there.
		h, _ := d.st.History(context.Background(), "notes", "N1")
		found := false
		for _, ch := range h {
			found = found || ch.ID == c.ChangeID
		}
		if !found {
			t.Fatalf("%s: the replaced edit is not in the history", d.name)
		}
		if err := d.st.Restore(context.Background(), "note.restore", "notes", store.ByID("N1"), c.ChangeID); err != nil {
			t.Fatal(err)
		}
		if body, _, _ := d.note("N1"); body != "text from the laptop" {
			t.Fatalf("%s: restore gave %q", d.name, body)
		}
		// Put it back for the next device's check.
		d.write("note.edit", func(tx *store.Tx) error {
			return tx.Update("notes", store.ByID("N1"), store.Row{"body_md": "text from the desktop"})
		})
	}
	// Dismissing hides it from the list but keeps the text.
	cs, _ := a.st.SyncConflicts(context.Background(), false)
	if err := a.st.DismissSyncConflict(context.Background(), cs[0].ID); err != nil {
		t.Fatal(err)
	}
	if cs, _ := a.st.SyncConflicts(context.Background(), false); len(cs) != 0 {
		t.Fatal("dismissed conflict still listed")
	}
	if cs, _ := a.st.SyncConflicts(context.Background(), true); len(cs) != 1 {
		t.Fatal("dismissed conflict lost")
	}
}

func TestSuccessiveEditsAreNotConflicts(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "one")
	a.sync()
	b.at(1500).sync()
	b.at(2000).editNote("N1", "two") // made after reading A's note
	b.sync()
	a.at(3000).sync()
	a.at(4000).editNote("N1", "three") // made after reading B's edit
	a.sync()
	b.at(5000).sync()
	sameLibraries(t, a, b)
	for _, d := range []*dev{a, b} {
		if cs, _ := d.st.SyncConflicts(context.Background(), true); len(cs) != 0 {
			t.Fatalf("%s: %v", d.name, cs)
		}
		if body, _, _ := d.note("N1"); body != "three" {
			t.Fatalf("%s: %q", d.name, body)
		}
	}
}

func TestDeleteIsATombstoneAndRestoreWins(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "keep me")
	a.sync()
	b.at(1500).sync()

	a.at(2000).deleteNote("N1")
	b.at(2500).editNote("N1", "edited while deleted elsewhere")
	a.sync()
	b.sync()
	a.sync()
	sameLibraries(t, a, b)
	body, deleted, exists := b.note("N1")
	if !exists || !deleted || body != "edited while deleted elsewhere" {
		t.Fatalf("B: %q deleted=%v exists=%v; the delete and the edit are different fields and both apply", body, deleted, exists)
	}

	// Restoring from the bin (deleted_at back to NULL) is a later write and wins.
	b.at(4000).write("note.restore", func(tx *store.Tx) error {
		return tx.Update("notes", store.ByID("N1"), store.Row{"deleted_at": nil})
	})
	b.sync()
	a.at(5000).sync()
	if _, deleted, _ := a.note("N1"); deleted {
		t.Fatal("restore did not reach A")
	}
	sameLibraries(t, a, b)
}

func TestHardDeleteRemovesTheRowEverywhere(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "x")
	a.write("tag.add", func(tx *store.Tx) error {
		return tx.Create("note_tags", store.Row{"note_id": "N1", "tag": "grace"})
	})
	a.sync()
	b.at(1500).sync()
	count := func(d *dev) (n int) {
		d.st.QueryRow(context.Background(), `SELECT COUNT(*) FROM note_tags`).Scan(&n)
		return
	}
	if count(b) != 1 {
		t.Fatal("tag did not arrive (composite key)")
	}
	a.at(2000).write("tag.remove", func(tx *store.Tx) error {
		return tx.Delete("note_tags", store.Row{"note_id": "N1", "tag": "grace"})
	})
	a.sync()
	b.at(3000).sync()
	if count(b) != 0 {
		t.Fatal("tag delete did not reach B")
	}
}

func TestPurgePropagatesAndErasesTheFolder(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	ea := a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "a secret sermon illustration")
	a.sync()
	b.at(1500).sync()
	if !strings.Contains(readAll(t, ea, folder), "secret sermon") {
		t.Fatal("test setup: the text should be in the folder before the purge")
	}

	a.at(2000).deleteNote("N1")
	a.sync()
	a.at(2500).purgeNote("N1")
	r := a.sync()
	if !r.Compacted {
		t.Fatal("a purge should compact the folder so the old values are not left in it")
	}
	if strings.Contains(readAll(t, ea, folder), "secret sermon") {
		t.Fatal("purged text is still in the folder")
	}

	b.at(3000).sync()
	if _, _, exists := b.note("N1"); exists {
		t.Fatal("B still has the purged note")
	}
	h, _ := b.st.History(context.Background(), "notes", "N1")
	if len(h) == 0 {
		t.Fatal("the purge should leave the history entries, without values")
	}
	for _, c := range h {
		if c.Before != nil || c.After != nil {
			t.Fatalf("B's history still holds values: %+v", c)
		}
	}
	// A late edit to a purged note from a device that had not heard of the purge cannot bring it back.
	c := newDev(t, "Phone")
	c.connect(folder)
	c.at(5000).sync()
	if _, _, exists := c.note("N1"); exists {
		t.Fatal("a device that joins later got the purged note")
	}
}

func TestPurgeBeatsALateEditFromAnotherDevice(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "text")
	a.sync()
	b.at(1500).sync()
	a.at(2000).deleteNote("N1")
	a.at(2100).purgeNote("N1")
	b.at(3000).editNote("N1", "edit made without knowing")
	a.sync()
	b.sync()
	a.sync()
	for _, d := range []*dev{a, b} {
		if _, _, exists := d.note("N1"); exists {
			t.Fatalf("%s: purged note came back", d.name)
		}
	}
	h, _ := b.st.History(context.Background(), "notes", "N1")
	for _, c := range h {
		if c.After != nil || c.Before != nil {
			t.Fatalf("values survive on B: %+v", c)
		}
	}
}

func TestThreeDevicesConverge(t *testing.T) {
	folder := folderOf(t)
	a, b, c := newDev(t, "Laptop"), newDev(t, "Desktop"), newDev(t, "Phone")
	for _, d := range []*dev{a, b, c} {
		d.connect(folder)
	}
	a.at(1000).mkNote("N1", "one")
	a.mkNote("N2", "two")
	a.mkList("L1", "list")
	a.sync()
	b.at(1100).sync()
	c.at(1100).sync()

	a.at(2000).editNote("N1", "one from A")
	b.at(2100).editNote("N1", "one from B")
	c.at(2200).editNote("N1", "one from C")
	b.deleteNote("N2")
	c.renameList("L1", "renamed on C")
	a.renameList("L1", "renamed on A")
	b.mkNote("N3", "three")
	c.at(2300).mkNote("N4", "four")

	// Different sync orders.
	c.sync()
	a.sync()
	b.sync()
	c.sync()
	a.sync()
	b.sync()
	sameLibraries(t, a, b, c)

	if body, _, _ := a.note("N1"); body != "one from C" {
		t.Fatalf("N1 = %q; the latest edit should win", body)
	}
	if n, _ := a.listName("L1"); n != "renamed on C" {
		t.Fatalf("L1 = %q", n)
	}
	if _, del, _ := a.note("N2"); !del {
		t.Fatal("N2 delete lost")
	}
	// Both older N1 edits are kept: the two that lost.
	cs, _ := a.st.SyncConflicts(context.Background(), true)
	var texts []string
	for _, x := range cs {
		texts = append(texts, x.Text)
	}
	if len(cs) != 2 {
		t.Fatalf("conflicts = %v", texts)
	}
}

func TestClockBehindDoesNotLoseALaterEdit(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	hour := int64(3600 * 1000)
	a.at(10*hour).mkList("L1", "from A")
	a.sync()
	// B's wall clock is five hours behind A's. After B has read A's change, its own edit is
	// still later in the hybrid clock, so it wins, as the user would expect.
	b.at(5 * hour).sync()
	b.renameList("L1", "from B, made later")
	b.sync()
	a.at(10*hour + 1).sync()
	if n, _ := a.listName("L1"); n != "from B, made later" {
		t.Fatalf("A has %q", n)
	}
}

func TestClockFarAheadIsRefused(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	day := int64(24 * 3600 * 1000)
	a.at(3*day).mkList("L1", "from the future")
	a.sync()
	_, err := b.at(1000).eng.Sync(context.Background())
	if !errors.Is(err, ErrClockSkew) {
		t.Fatalf("sync = %v; want ErrClockSkew", err)
	}
	if _, ok := b.listName("L1"); ok {
		t.Fatal("a refused file was applied")
	}
}

func TestPreviewChangesNothing(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	a.at(1000).mkNote("N1", "from the laptop")
	a.mkNote("N2", "another")
	a.mkList("L1", "list")
	a.sync()

	// B already has its own data; it joins.
	b.at(2000).mkNote("N9", "already on the desktop")
	b.mkList("L9", "mine")
	b.connect(folder)
	beforeDump, beforeChanges := b.dump(), b.changeCount()
	filesBefore := listFiles(t, folder)

	p, err := b.eng.Preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !p.FolderHasData || len(p.Devices) != 1 || p.Devices[0] != "Laptop" {
		t.Fatalf("preview devices: %+v", p)
	}
	if p.ThisDevice["notes"] != 1 || p.AfterMerge["notes"] != 3 || p.InFolder["notes"] != 2 {
		t.Fatalf("notes this/folder/after = %d/%d/%d; want 1/2/3", p.ThisDevice["notes"], p.InFolder["notes"], p.AfterMerge["notes"])
	}
	if p.ToThisDevice.Created != 3 || p.ToThisDevice.Changes != 3 || p.ToFolder != 2 {
		t.Fatalf("preview = %+v", p)
	}
	if b.dump() != beforeDump || b.changeCount() != beforeChanges {
		t.Fatal("the preview changed the library")
	}
	if got := listFiles(t, folder); got != filesBefore {
		t.Fatalf("the preview changed the folder:\n%s\n%s", filesBefore, got)
	}
	// The real sync then does what the preview said.
	r := b.sync()
	if r.Pulled.Created != 3 || r.Pushed != 2 {
		t.Fatalf("sync = %+v", r)
	}
}

func listFiles(t *testing.T, folder string) string {
	t.Helper()
	var out []string
	filepath.WalkDir(folder, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			info, _ := d.Info()
			rel, _ := filepath.Rel(folder, p)
			out = append(out, rel+" "+jsonOf(info.Size()))
		}
		return nil
	})
	return strings.Join(out, "\n")
}

func TestFolderMissing(t *testing.T) {
	folder := folderOf(t)
	a := newDev(t, "Laptop")
	a.connect(folder)
	a.mkNote("N1", "x")
	if err := os.RemoveAll(folder); err != nil {
		t.Fatal(err)
	}
	if _, err := a.eng.Sync(context.Background()); !errors.Is(err, ErrFolderMissing) {
		t.Fatalf("sync = %v", err)
	}
	if _, err := Open(context.Background(), a.st, NewFolder(folder), password); !errors.Is(err, ErrFolderMissing) {
		t.Fatalf("open = %v", err)
	}
}

func TestForgetDevice(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "x")
	a.sync()
	b.at(1500).sync()
	b.mkNote("N2", "from B")
	b.sync()
	ctx := context.Background()
	if err := a.eng.Forget(ctx, a.st.Device()); err == nil {
		t.Fatal("a device forgot itself")
	}
	if err := a.eng.Forget(ctx, b.st.Device()); err != nil {
		t.Fatal(err)
	}
	u, err := a.eng.Devices(ctx)
	if err != nil || len(u.Devices) != 1 || !u.Devices[0].Self {
		t.Fatalf("devices = %+v, %v", u, err)
	}
	if _, err := b.eng.Sync(ctx); !errors.Is(err, ErrForgotten) {
		t.Fatalf("forgotten device synced: %v", err)
	}
	a.sync() // still works
}

func TestEverythingTheFolderHoldsIsEncrypted(t *testing.T) {
	folder := folderOf(t)
	a := newDev(t, "Laptop")
	a.connect(folder)
	a.mkNote("N1", "a very recognisable sentence about Melchizedek")
	a.sync()
	err := filepath.WalkDir(folder, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == keyName {
			return err
		}
		raw, _ := os.ReadFile(p)
		for _, s := range []string{"Melchizedek", "notes", "body_md", "Laptop"} {
			if strings.Contains(string(raw), s) {
				t.Errorf("%s contains %q in the clear", p, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A reading plan goes to the bin and comes back, or is deleted for good, on every device, with the
// rows of its days.
func TestPlanBinRestoreAndPurgePropagate(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).write("plan.create", func(tx *store.Tx) error {
		if err := tx.Create("reading_plans", store.Row{"id": "P1", "name": "Gospels", "weekdays": int64(127), "created_at": int64(1)}); err != nil {
			return err
		}
		if err := tx.Create("plan_schedule", store.Row{"plan_id": "P1", "day_no": int64(1), "day_date": "2026-01-01"}); err != nil {
			return err
		}
		if err := tx.Create("plan_days", store.Row{"plan_id": "P1", "day_no": int64(1), "verse_start": int64(43001001), "verse_end": int64(43001051)}); err != nil {
			return err
		}
		return tx.Create("plan_progress", store.Row{"plan_id": "P1", "day_no": int64(1), "completed_at": int64(5)})
	})
	a.sync()
	b.at(1500).sync()
	count := func(d *dev, q string) int {
		var n int
		if err := d.st.QueryRow(context.Background(), q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	a.at(2000).write("plan.delete", func(tx *store.Tx) error { return tx.Delete("reading_plans", store.ByID("P1")) })
	a.sync()
	b.at(2500).sync()
	if count(b, `SELECT COUNT(*) FROM reading_plans WHERE deleted_at IS NOT NULL`) != 1 {
		t.Fatal("B should have the plan in its bin")
	}
	a.at(3000).write("bin.restore", func(tx *store.Tx) error {
		return tx.Update("reading_plans", store.ByID("P1"), store.Row{"deleted_at": nil})
	})
	a.sync()
	b.at(3500).sync()
	if count(b, `SELECT COUNT(*) FROM reading_plans WHERE deleted_at IS NULL`) != 1 {
		t.Fatal("B should have the plan back")
	}
	a.at(4000).write("plan.delete", func(tx *store.Tx) error { return tx.Delete("reading_plans", store.ByID("P1")) })
	a.at(4500).write("bin.purge", func(tx *store.Tx) error {
		for _, p := range []struct {
			table string
			key   store.Row
		}{
			{"plan_progress", store.Row{"plan_id": "P1", "day_no": int64(1)}},
			{"plan_days", store.Row{"plan_id": "P1", "day_no": int64(1), "verse_start": int64(43001001)}},
			{"plan_schedule", store.Row{"plan_id": "P1", "day_no": int64(1)}},
			{"reading_plans", store.ByID("P1")},
		} {
			if err := tx.Purge(p.table, p.key); err != nil {
				return err
			}
		}
		return nil
	})
	a.sync()
	b.at(5000).sync()
	for _, q := range []string{`reading_plans`, `plan_days`, `plan_schedule`, `plan_progress`} {
		if n := count(b, `SELECT COUNT(*) FROM `+q); n != 0 {
			t.Fatalf("B still has %d rows in %s after the purge", n, q)
		}
	}
}

func TestCollectionBinRestoreAndPurgePropagate(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).write("collection.create", func(tx *store.Tx) error {
		if err := tx.Create("items", store.Row{"id": "I1", "kind": "book", "title": "A book", "source": "import", "created_at": int64(1), "updated_at": int64(1)}); err != nil {
			return err
		}
		if err := tx.Create("collections", store.Row{"id": "C1", "name": "Reformed", "kind": "manual", "position": int64(1), "created_at": int64(1), "updated_at": int64(1)}); err != nil {
			return err
		}
		return tx.Create("collection_items", store.Row{"collection_id": "C1", "item_id": "I1", "position": int64(1)})
	})
	a.sync()
	b.at(1500).sync()
	count := func(d *dev, q string) int {
		var n int
		if err := d.st.QueryRow(context.Background(), q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	a.at(2000).write("collection.delete", func(tx *store.Tx) error { return tx.Delete("collections", store.ByID("C1")) })
	a.sync()
	b.at(2500).sync()
	if count(b, `SELECT COUNT(*) FROM collections WHERE deleted_at IS NOT NULL`) != 1 {
		t.Fatal("B should have the collection in its bin")
	}
	a.at(3000).write("bin.restore", func(tx *store.Tx) error {
		return tx.Update("collections", store.ByID("C1"), store.Row{"deleted_at": nil})
	})
	a.sync()
	b.at(3500).sync()
	if count(b, `SELECT COUNT(*) FROM collections WHERE deleted_at IS NULL`) != 1 || count(b, `SELECT COUNT(*) FROM collection_items`) != 1 {
		t.Fatal("B should have the collection back with its book")
	}
	a.at(4000).write("collection.delete", func(tx *store.Tx) error { return tx.Delete("collections", store.ByID("C1")) })
	a.at(4500).write("bin.purge", func(tx *store.Tx) error {
		if err := tx.Purge("collection_items", store.Row{"collection_id": "C1", "item_id": "I1"}); err != nil {
			return err
		}
		return tx.Purge("collections", store.ByID("C1"))
	})
	a.sync()
	b.at(5000).sync()
	for _, q := range []string{`collections`, `collection_items`} {
		if n := count(b, `SELECT COUNT(*) FROM `+q); n != 0 {
			t.Fatalf("B still has %d rows in %s after the purge", n, q)
		}
	}
}

// A deletion batch is an ordinary synced field: the other device sees the same batch on the folder
// and what went with it, and restoring there brings back exactly that batch.
func TestDeletionBatchRoundTrips(t *testing.T) {
	ctx := context.Background()
	folderDir := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folderDir)
	b.connect(folderDir)
	reg := command.NewRegistry()
	core.Register(reg)
	folder.Register(reg)
	run := func(d *dev, id string, params any, out any) {
		t.Helper()
		raw, _ := json.Marshal(params)
		res, err := reg.Run(ctx, &command.Env{Store: d.st}, id, raw)
		if err != nil {
			t.Fatalf("%s %s: %v", d.name, id, err)
		}
		if out != nil {
			bs, _ := json.Marshal(res)
			_ = json.Unmarshal(bs, out)
		}
	}
	var f struct{ ID string }
	run(a.at(1000), "folder.create", map[string]any{"name": "Series"}, &f)
	var with, apart struct{ ID string }
	run(a.at(1100), "note.create", map[string]any{"body": "with", "folder_id": f.ID}, &with)
	run(a.at(1200), "note.create", map[string]any{"body": "apart", "folder_id": f.ID}, &apart)
	run(a.at(1300), "note.delete", map[string]any{"id": apart.ID}, nil)
	run(a.at(2000), "folder.delete", map[string]any{"id": f.ID}, nil)
	a.sync()
	b.at(2500).sync()

	batch := func(d *dev, table, id string) (got string) {
		t.Helper()
		var s sql.NullString
		if err := d.st.QueryRow(ctx, `SELECT deletion_batch FROM `+table+` WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s.String
	}
	fb := batch(b, "folders", f.ID)
	if fb == "" || fb != batch(a, "folders", f.ID) || fb != batch(b, "notes", with.ID) {
		t.Fatalf("B's batches: folder %q, note %q; A's folder %q", fb, batch(b, "notes", with.ID), batch(a, "folders", f.ID))
	}
	if batch(b, "notes", apart.ID) != "" {
		t.Fatal("the note deleted on its own must have no batch on B")
	}
	var back struct{ Notes int }
	run(b.at(3000), "bin.restore", map[string]any{"kind": "folder", "id": f.ID}, &back)
	if back.Notes != 1 {
		t.Fatalf("B restored %d notes, want just the one deleted with the folder", back.Notes)
	}
	a.at(3500).sync()
	b.at(3600).sync()
	a.at(3700).sync()
	var liveA, liveB int
	for d, n := range map[*dev]*int{a: &liveA, b: &liveB} {
		if err := d.st.QueryRow(ctx, `SELECT COUNT(*) FROM notes WHERE deleted_at IS NULL`).Scan(n); err != nil {
			t.Fatal(err)
		}
	}
	if liveA != 1 || liveB != 1 || batch(a, "notes", with.ID) != "" {
		t.Fatalf("after the restore reached A: live notes A=%d B=%d, batch %q", liveA, liveB, batch(a, "notes", with.ID))
	}
}

// A device is listed as soon as it has joined, before it has written a change file, so it can be
// forgotten. Its name travels in an encrypted marker.
func TestDeviceIsListedAsSoonAsItHasJoined(t *testing.T) {
	ctx := context.Background()
	folderDir := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folderDir)
	eb := b.connect(folderDir)
	if err := eb.Join(ctx); err != nil { // what sync.setup does
		t.Fatal(err)
	}
	if err := eb.Join(ctx); err != nil { // twice is harmless
		t.Fatal(err)
	}
	u, err := a.eng.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got *DeviceInfo
	for i := range u.Devices {
		if u.Devices[i].ID == b.st.Device() {
			got = &u.Devices[i]
		}
	}
	if got == nil || got.Name != "Desktop" || got.Files != 0 || got.Self {
		t.Fatalf("devices = %+v", u.Devices)
	}
	if p, err := a.eng.Preview(ctx); err != nil || p.FolderHasData {
		t.Fatalf("a device that has only joined holds no data: %+v, %v", p, err)
	}
	if err := a.eng.Join(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.eng.Forget(ctx, b.st.Device()); err != nil {
		t.Fatal(err)
	}
	if u, _ := a.eng.Devices(ctx); len(u.Devices) != 1 {
		t.Fatalf("after forgetting: %+v", u.Devices)
	}
	if _, err := eb.Sync(ctx); !errors.Is(err, ErrForgotten) {
		t.Fatalf("forgotten device synced: %v", err)
	}
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	if u, _ := a.eng.Devices(ctx); len(u.Devices) != 1 {
		t.Fatalf("joining again must not bring a forgotten device back: %+v", u.Devices)
	}
}

// writeCounter counts the writes a sync makes to the folder.
type writeCounter struct {
	Transport
	creates, removes int
}

func (w *writeCounter) Create(ctx context.Context, name string, data []byte) error {
	w.creates++
	return w.Transport.Create(ctx, name, data)
}

func (w *writeCounter) Remove(ctx context.Context, name string) error {
	w.removes++
	return w.Transport.Remove(ctx, name)
}

func TestRenamedDeviceIsListedUnderTheNewNameBeforeAnyChangeFile(t *testing.T) {
	ctx := context.Background()
	folderDir := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folderDir)
	eb := b.connect(folderDir)
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.st.SetDeviceName(ctx, "Study PC"); err != nil { // what sync.setup does with a new name
		t.Fatal(err)
	}
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := a.eng.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, d := range u.Devices {
		names[d.ID] = d.Name
	}
	if names[b.st.Device()] != "Study PC" {
		t.Fatalf("the other device lists %q, want the new name; devices = %+v", names[b.st.Device()], u.Devices)
	}
}

func TestSyncWritesNoJoinMarkerWhenNothingChanged(t *testing.T) {
	ctx := context.Background()
	folderDir := folderOf(t)
	b := newDev(t, "Desktop")
	eb := b.connect(folderDir)
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	w := &writeCounter{Transport: eb.t}
	eb.t = w
	for i := 0; i < 2; i++ {
		if _, err := eb.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		if err := eb.Join(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if w.creates != 0 || w.removes != 0 {
		t.Fatalf("nothing changed but the folder saw %d creates and %d removes", w.creates, w.removes)
	}
}

// observer lists the devices as another device would, after every write this engine makes to the
// folder, and records what the renamed device was listed as.
type observer struct {
	Transport
	look func() string
	seen []string
}

func (o *observer) note() { o.seen = append(o.seen, o.look()) }

func (o *observer) Create(ctx context.Context, name string, data []byte) error {
	err := o.Transport.Create(ctx, name, data)
	o.note()
	return err
}

func (o *observer) Remove(ctx context.Context, name string) error {
	err := o.Transport.Remove(ctx, name)
	o.note()
	return err
}

func TestRenameNeverLeavesTheDeviceWithoutAName(t *testing.T) {
	ctx := context.Background()
	folderDir := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	ea := a.connect(folderDir)
	eb := b.connect(folderDir)
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.st.SetDeviceName(ctx, "Study PC"); err != nil {
		t.Fatal(err)
	}
	o := &observer{Transport: eb.t}
	o.look = func() string {
		u, err := ea.Devices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range u.Devices {
			if d.ID == b.st.Device() {
				return d.Name
			}
		}
		return "(not listed)"
	}
	eb.t = o
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	if len(o.seen) == 0 {
		t.Fatal("the rename wrote nothing")
	}
	for i, n := range o.seen {
		if n != "Desktop" && n != "Study PC" {
			t.Fatalf("after step %d another device lists %q, want the old or the new name (seen %q)", i+1, n, o.seen)
		}
	}
}

func TestLeftoverMarkerTempFilesAreIgnoredAndCleanedUp(t *testing.T) {
	ctx := context.Background()
	folderDir := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	ea := a.connect(folderDir)
	eb := b.connect(folderDir)
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(folderDir, RootName, b.st.Device())
	for _, n := range []string{joinedTmpName, ".tmp-123"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("junk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	u, err := ea.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Devices) != 1 || u.Devices[0].Name != "Desktop" {
		t.Fatalf("devices = %+v", u.Devices)
	}
	if err := b.st.SetDeviceName(ctx, "Study PC"); err != nil {
		t.Fatal(err)
	}
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		if e.Name() != joinedName {
			t.Fatalf("leftover %q was not cleaned up", e.Name())
		}
	}
	if n := ea.joinedAs(ctx, b.st.Device()); n != "Study PC" {
		t.Fatalf("marker names %q, want Study PC", n)
	}
}

// A device that crashed between writing joined.new and renaming it clears the file the next time it syncs.
func TestSyncRemovesAStaleJoinedNew(t *testing.T) {
	ctx := context.Background()
	folderDir := folderOf(t)
	b := newDev(t, "Desktop")
	eb := b.connect(folderDir)
	if err := eb.Join(ctx); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(folderDir, RootName, b.st.Device())
	if err := os.WriteFile(filepath.Join(dir, joinedTmpName), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := eb.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, joinedTmpName)); !os.IsNotExist(err) {
		t.Fatalf("joined.new still there after a sync: %v", err)
	}
}
