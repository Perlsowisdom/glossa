package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/store"
)

// threeFiles has A write three change files (one note each), and returns B, a device that has not
// read any of them, with its library as it is before any attempt to sync.
func threeFiles(t *testing.T) (folder string, a, b *dev) {
	t.Helper()
	folder = folderOf(t)
	a, b = newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	for i, id := range []string{"N1", "N2", "N3"} {
		a.at(int64(1000*(i+1))).mkNote(id, "note "+id)
		a.sync()
	}
	b.at(5000).mkNote("OWN", "B's own note")
	return folder, a, b
}

func fileOf(folder string, d *dev, seq uint64) string {
	return filepath.Join(folder, RootName, d.st.Device(), changeName(seq))
}

// mustFailSafely checks that B's sync fails with want, and that nothing of the folder was applied
// and nothing was written into it.
func mustFailSafely(t *testing.T, b *dev, folder string, want error) {
	t.Helper()
	beforeDump, beforeChanges, files := b.dump(), b.changeCount(), listFiles(t, folder)
	_, err := b.eng.Sync(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("sync error = %v; want %v", err, want)
	}
	if b.dump() != beforeDump || b.changeCount() != beforeChanges {
		t.Fatalf("a failed sync changed the library:\n%s", b.dump())
	}
	if got := listFiles(t, folder); got != files {
		t.Fatalf("a failed sync changed the folder:\n%s\n%s", files, got)
	}
	if _, _, ok := b.note("N1"); ok {
		t.Fatal("part of the folder was applied: file 1 is good but file 2 is not")
	}
}

func TestMissingMiddleFile(t *testing.T) {
	folder, a, b := threeFiles(t)
	saved, _ := os.ReadFile(fileOf(folder, a, 2))
	if err := os.Remove(fileOf(folder, a, 2)); err != nil {
		t.Fatal(err)
	}
	mustFailSafely(t, b, folder, ErrMissingFile)
	// The file turns up (the sync service finished): everything applies, nothing was lost.
	if err := os.WriteFile(fileOf(folder, a, 2), saved, 0o644); err != nil {
		t.Fatal(err)
	}
	r := b.sync()
	if r.Pulled.Created != 3 {
		t.Fatalf("pulled %+v", r.Pulled)
	}
}

func TestTruncatedFiles(t *testing.T) {
	folder, a, b := threeFiles(t)
	p := fileOf(folder, a, 2)
	saved, _ := os.ReadFile(p)
	for _, n := range []int{0, 5, headerLen, headerLen + 24, len(saved) / 2, len(saved) - 1} {
		if err := os.WriteFile(p, saved[:n], 0o644); err != nil {
			t.Fatal(err)
		}
		mustFailSafely(t, b, folder, ErrCorrupt)
	}
	// The last file cut short is caught too.
	p3 := fileOf(folder, a, 3)
	saved3, _ := os.ReadFile(p3)
	os.WriteFile(p, saved, 0o644)
	os.WriteFile(p3, saved3[:len(saved3)-1], 0o644)
	mustFailSafely(t, b, folder, ErrCorrupt)
	os.WriteFile(p3, saved3, 0o644)
	if r := b.sync(); r.Pulled.Created != 3 {
		t.Fatalf("after repair: %+v", r.Pulled)
	}
}

func TestTamperedFilesAreRejected(t *testing.T) {
	folder, a, b := threeFiles(t)
	p := fileOf(folder, a, 2)
	saved, _ := os.ReadFile(p)
	// Flip one bit in the header, the nonce, the middle and the authentication tag.
	for _, at := range []int{0, 4, 5, headerLen + 3, len(saved) / 2, len(saved) - 1} {
		bad := append([]byte{}, saved...)
		bad[at] ^= 0x01
		if err := os.WriteFile(p, bad, 0o644); err != nil {
			t.Fatal(err)
		}
		mustFailSafely(t, b, folder, ErrCorrupt)
	}
	// Extra bytes at the end change the tag check too.
	os.WriteFile(p, append(append([]byte{}, saved...), 0), 0o644)
	mustFailSafely(t, b, folder, ErrCorrupt)
}

func TestFilesCannotBeSwappedOrMoved(t *testing.T) {
	folder, a, b := threeFiles(t)
	p2, p3 := fileOf(folder, a, 2), fileOf(folder, a, 3)
	f2, _ := os.ReadFile(p2)
	f3, _ := os.ReadFile(p3)
	os.WriteFile(p2, f3, 0o644)
	os.WriteFile(p3, f2, 0o644)
	mustFailSafely(t, b, folder, ErrCorrupt)
	os.WriteFile(p2, f2, 0o644)
	os.WriteFile(p3, f3, 0o644)

	// A copy of a file from one device's folder in another's is refused too: C writes a file, and
	// it is placed in A's folder as A's next file.
	c := newDev(t, "Phone")
	c.connect(folder)
	c.at(9000).mkNote("C1", "from C")
	c.sync()
	cf, _ := os.ReadFile(fileOf(folder, c, 1))
	os.WriteFile(fileOf(folder, a, 4), cf, 0o644)
	mustFailSafely(t, b, folder, ErrCorrupt)
}

func TestFileFromAnotherPasswordIsRejected(t *testing.T) {
	folder, a, b := threeFiles(t)
	other := deriveKey("some other password", []byte("0123456789abcdef"), fastKDF)
	body := `{"format":1,"kind":"change","device":"` + a.st.Device() + `","seq":2,"schema":13,"changes":[]}`
	data, _ := seal(other, kindChange, a.st.Device(), 2, []byte(body))
	os.WriteFile(fileOf(folder, a, 2), data, 0o644)
	mustFailSafely(t, b, folder, ErrCorrupt)
}

// forge writes a validly encrypted file (the attacker knows the password, or it is a bug in a
// newer version) with the contents given.
func forge(t *testing.T, folder string, a *dev, e *Engine, seq uint64, body fileBody) {
	t.Helper()
	body.Format, body.Kind, body.Device, body.Seq = formatVersion, "change", a.st.Device(), seq
	if body.Schema == 0 {
		body.Schema = 13
	}
	plain := []byte(jsonOf(body))
	data, err := seal(e.key, kindChange, a.st.Device(), seq, plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileOf(folder, a, seq), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOneBadChangeRejectsTheWholeFile(t *testing.T) {
	folder, a, b := threeFiles(t)
	good := store.Change{ID: "01ZZZZZZZZZZZZZZZZZZZZZZZZ", Device: a.st.Device(), HLC: "0001700000000000.00000." + a.st.Device(),
		Command: "x", ObjectType: "lists", ObjectID: "LX", Op: "create",
		After: map[string]any{"id": "LX", "kind": "verses", "name": "x", "created_at": 1, "updated_at": 1}}
	good.HLC = "1700000000000.00000." + a.st.Device()
	bad := good
	bad.ID, bad.ObjectType, bad.ObjectID = "01YYYYYYYYYYYYYYYYYYYYYYYYY", "no_such_table", "Z"
	forge(t, folder, a, a.eng, 4, fileBody{Changes: []store.Change{good, bad}})
	mustFailSafely(t, b, folder, ErrCorrupt)
	if _, ok := b.listName("LX"); ok {
		t.Fatal("the good half of a bad file was applied")
	}
}

func TestChangeFromAnotherDeviceInsideAFileIsRejected(t *testing.T) {
	folder, a, b := threeFiles(t)
	c := store.Change{ID: "01ZZZZZZZZZZZZZZZZZZZZZZZZ", Device: b.st.Device(), HLC: "1700000000000.00000." + b.st.Device(),
		Command: "x", ObjectType: "lists", ObjectID: "LX", Op: "create",
		After: map[string]any{"id": "LX", "kind": "verses", "name": "x", "created_at": 1, "updated_at": 1}}
	forge(t, folder, a, a.eng, 4, fileBody{Changes: []store.Change{c}})
	mustFailSafely(t, b, folder, ErrCorrupt)
}

func TestNewerVersionIsRefused(t *testing.T) {
	folder, a, b := threeFiles(t)
	forge(t, folder, a, a.eng, 4, fileBody{Schema: 99})
	mustFailSafely(t, b, folder, ErrNewerVersion)
}

func TestWrongPassword(t *testing.T) {
	folder := folderOf(t)
	a := newDev(t, "Laptop")
	a.connect(folder)
	a.mkNote("N1", "x")
	a.sync()
	b := newDev(t, "Desktop")
	_, err := Open(context.Background(), b.st, NewFolder(folder), "not the right password")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("Open = %v", err)
	}
	if strings.Contains(err.Error(), "not the right password") {
		t.Fatal("the password is in the error")
	}
	// A tampered key file cannot be used to lower the cost.
	kp := filepath.Join(folder, RootName, keyName)
	raw, _ := os.ReadFile(kp)
	weak := strings.Replace(string(raw), `"memory_kib":8192`, `"memory_kib":8`, 1)
	if weak == string(raw) {
		t.Fatal("test setup: key file layout changed")
	}
	os.WriteFile(kp, []byte(weak), 0o644)
	if _, err := Open(context.Background(), b.st, NewFolder(folder), password); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("weak parameters accepted: %v", err)
	}
	os.WriteFile(kp, []byte("not json"), 0o644)
	if _, err := Open(context.Background(), b.st, NewFolder(folder), password); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("garbage key file: %v", err)
	}
	os.Remove(kp)
	if _, err := Open(context.Background(), b.st, NewFolder(folder), password); !errors.Is(err, ErrNotInitialised) {
		t.Fatalf("missing key file: %v", err)
	}
	if body, _, _ := b.note("N1"); body != "" {
		t.Fatal("a wrong password still read data")
	}
}

func TestCancelledSyncAppliesNothing(t *testing.T) {
	folder, _, b := threeFiles(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := b.dump()
	if _, err := b.eng.Sync(ctx); err == nil {
		t.Fatal("cancelled sync succeeded")
	}
	if b.dump() != before {
		t.Fatal("cancelled sync changed the library")
	}
	_ = folder
}

func TestSnapshotTamperIsRejected(t *testing.T) {
	folder, a, b := threeFiles(t)
	a.eng.CompactAfter = 0
	if err := a.eng.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(folder, RootName, a.st.Device(), snapName(3))
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0xff
	os.WriteFile(p, raw, 0o644)
	// B has never seen A, so it needs the snapshot.
	_, err = b.eng.Sync(context.Background())
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("sync = %v", err)
	}
	if _, _, ok := b.note("N1"); ok {
		t.Fatal("tampered snapshot applied")
	}
}
