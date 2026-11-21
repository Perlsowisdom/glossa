package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func ownFiles(t *testing.T, folder string, d *dev) (changes, snaps int) {
	t.Helper()
	es, err := os.ReadDir(filepath.Join(folder, RootName, d.st.Device()))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		switch {
		case changeRE.MatchString(e.Name()):
			changes++
		case snapRE.MatchString(e.Name()):
			snaps++
		}
	}
	return
}

func TestNewDeviceJoinsFromASnapshot(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	ea := a.connect(folder)
	b.connect(folder)
	ea.CompactAfter = 3

	a.at(1000).mkNote("N1", "one")
	a.sync()
	b.at(1500).sync() // B reads file 1, then falls behind
	for i := 2; i <= 6; i++ {
		a.at(int64(1000*i)).editNote("N1", "edit "+string(rune('0'+i)))
		a.mkList("L"+string(rune('0'+i)), "list")
		r := a.sync()
		_ = r
	}
	a.deleteNote("N1")
	a.mkNote("N2", "second note")
	a.sync()
	if err := a.eng.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	changes, snaps := ownFiles(t, folder, a)
	if snaps != 1 || changes != 0 {
		t.Fatalf("after compaction A has %d change files and %d snapshots; want 0 and 1", changes, snaps)
	}

	// C is new: it starts from the snapshot, not from file 1 (which is gone).
	c := newDev(t, "Phone")
	c.connect(folder)
	r, err := c.eng.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Pulled.Created == 0 {
		t.Fatalf("C pulled %+v", r.Pulled)
	}
	sameLibraries(t, a, c)
	if _, del, ok := c.note("N1"); !ok || !del {
		t.Fatal("the snapshot lost the tombstone")
	}
	// Starting from a snapshot does not invent text conflicts, though earlier edits are not in it.
	if cs, _ := c.st.SyncConflicts(context.Background(), true); len(cs) != 0 {
		t.Fatalf("snapshot produced conflicts: %v", cs)
	}

	// B had read only file 1; the files after it are gone, so it jumps to the snapshot too.
	b.sync()
	sameLibraries(t, a, b, c)

	// And everyone carries on from there.
	c.at(20000).mkNote("N3", "from the phone")
	c.sync()
	a.at(21000).sync()
	b.at(21000).sync()
	sameLibraries(t, a, b, c)
	if _, _, ok := a.note("N3"); !ok {
		t.Fatal("A did not get C's note after the snapshot")
	}
}

func TestAutomaticCompaction(t *testing.T) {
	folder := folderOf(t)
	a := newDev(t, "Laptop")
	ea := a.connect(folder)
	ea.CompactAfter = 4
	compacted := 0
	for i := 0; i < 12; i++ {
		a.at(int64(1000*(i+1))).mkList("L"+string(rune('A'+i)), "x")
		if a.sync().Compacted {
			compacted++
		}
	}
	if compacted == 0 {
		t.Fatal("never compacted")
	}
	if changes, snaps := ownFiles(t, folder, a); changes > 5 || snaps != 1 {
		t.Fatalf("A has %d change files and %d snapshots", changes, snaps)
	}
	// A device that joins now gets all twelve lists.
	b := newDev(t, "Desktop")
	b.connect(folder)
	b.at(30000).sync()
	sameLibraries(t, a, b)
}

func TestSnapshotCarriesOtherDevicesChanges(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "from A")
	a.sync()
	b.at(2000).sync()
	b.mkNote("N2", "from B")
	b.editNote("N1", "B changed A's note")
	b.sync()
	a.at(3000).sync()
	if err := a.eng.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A's snapshot holds B's changes too, so a new device that reads only A's folder has them.
	c := newDev(t, "Phone")
	c.connect(folder)
	c.at(9000).sync()
	sameLibraries(t, a, b, c)
}

func TestLostFolderFilesAreWrittenAgain(t *testing.T) {
	folder := folderOf(t)
	a, b := newDev(t, "Laptop"), newDev(t, "Desktop")
	a.connect(folder)
	b.connect(folder)
	a.at(1000).mkNote("N1", "one")
	a.sync()
	a.mkNote("N2", "two")
	a.sync()
	// The sync service loses A's folder (a restored older copy of it).
	if err := os.RemoveAll(filepath.Join(folder, RootName, a.st.Device())); err != nil {
		t.Fatal(err)
	}
	a.at(2000).mkNote("N3", "three")
	r := a.sync()
	if r.Pushed != 3 {
		t.Fatalf("A wrote %d changes; want all 3 again", r.Pushed)
	}
	b.at(3000).sync()
	sameLibraries(t, a, b)
}
