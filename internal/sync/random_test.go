package sync

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/store"
)

// TestRandomEditsConverge has three devices make random edits, deletes and purges at random times
// (with skewed clocks) and sync in random orders, with compaction on. After a final round every
// library must be identical. Several fixed seeds keep it reproducible.
func TestRandomEditsConverge(t *testing.T) {
	for seed := int64(1); seed <= 6; seed++ {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			folder := folderOf(t)
			ds := []*dev{newDev(t, "A"), newDev(t, "B"), newDev(t, "C")}
			skew := []int64{0, -7200000, 3600000}
			for _, d := range ds {
				d.connect(folder)
				d.eng.CompactAfter = 4
			}
			ids := []string{"N1", "N2", "N3", "N4"}
			made := map[string]bool{}
			purged := map[string]bool{}
			clock := int64(1000)
			for step := 0; step < 60; step++ {
				clock += int64(rng.Intn(5000))
				i := rng.Intn(len(ds))
				d := ds[i].at(clock + skew[i])
				switch id := ids[rng.Intn(len(ids))]; rng.Intn(8) {
				case 0, 1:
					if _, _, ok := d.note(id); !ok && !purged[id] && !made[id] {
						d.mkNote(id, "first "+id)
						made[id] = true
					}
				case 2, 3, 4:
					if _, del, ok := d.note(id); ok && !del {
						d.editNote(id, fmt.Sprintf("%s by %s step %d", id, d.name, step))
					}
				case 5:
					if _, del, ok := d.note(id); ok && !del {
						d.setNoteTitle(id, fmt.Sprintf("title %d", step))
					}
				case 6:
					if _, del, ok := d.note(id); ok && !del {
						d.deleteNote(id)
					}
				case 7:
					if _, del, ok := d.note(id); ok && del && rng.Intn(3) == 0 {
						d.purgeNote(id)
						purged[id] = true
					}
				}
				if rng.Intn(3) == 0 {
					ds[rng.Intn(len(ds))].sync()
				}
			}
			for round := 0; round < 3; round++ {
				for _, i := range rng.Perm(len(ds)) {
					ds[i].at(clock + 100000).sync()
				}
			}
			sameLibraries(t, ds...)
			// Purged notes are gone everywhere, history included.
			for id := range purged {
				for _, d := range ds {
					if _, _, ok := d.note(id); ok {
						t.Fatalf("%s still has purged %s", d.name, id)
					}
				}
			}
			// Conflict lists agree too, since they come from the shared history.
			var first string
			for i, d := range ds {
				cs, _ := d.st.SyncConflicts(context.Background(), true)
				var lines []string
				for _, c := range cs {
					lines = append(lines, c.ObjectID+"|"+c.ChangeID)
				}
				sort.Strings(lines)
				s := strings.Join(lines, ",")
				if i == 0 {
					first = s
				} else if s != first {
					t.Fatalf("conflict lists differ between devices:\n%s\nvs\n%s", first, s)
				}
			}
		})
	}
}

func TestFolderTransport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	tr := NewFolder(dir)
	if es, err := tr.List(ctx, ""); err != nil || len(es) != 0 {
		t.Fatalf("empty list = %v, %v", es, err)
	}
	if err := tr.Create(ctx, "dev/one.chg", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tr.Create(ctx, "dev/one.chg", []byte("y")); err != ErrExists {
		t.Fatalf("overwrite = %v", err)
	}
	if b, err := tr.Read(ctx, "dev/one.chg"); err != nil || string(b) != "x" {
		t.Fatalf("read = %q, %v", b, err)
	}
	if _, err := tr.Read(ctx, "dev/none"); err != ErrNotExist {
		t.Fatalf("missing read = %v", err)
	}
	for _, bad := range []string{"../escape", "a/../../b", `a\b`} {
		if err := tr.Create(ctx, bad, []byte("x")); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if es, _ := tr.List(ctx, "dev"); len(es) != 1 || es[0].Name != "one.chg" || es[0].Size != 1 {
		t.Fatalf("list = %v (a temp file was left behind?)", es)
	}
	if err := tr.Remove(ctx, "dev/gone"); err != nil {
		t.Fatalf("removing a missing file = %v", err)
	}
	if err := tr.RemoveAll(ctx, ""); err == nil {
		t.Fatal("removed the whole sync folder")
	}
	if err := tr.RemoveAll(ctx, "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, RootName, "dev")); !os.IsNotExist(err) {
		t.Fatal("folder not removed")
	}
	_ = store.Row{}
}
