package obsidian

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/connect"
	"github.com/Perlsowisdom/glossa/internal/document"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// rig is a library in a temp folder, the commands, and a temp "vault". Nothing here touches a real vault or library.
type rig struct {
	t     *testing.T
	env   *command.Env
	reg   *command.Registry
	vault string
	conn  *Conn
}

func newRig(t *testing.T) *rig {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &rig{t: t, env: &command.Env{Store: st}, reg: command.NewRegistry(), vault: t.TempDir(), conn: New()}
	core.Register(g.reg)
	document.Register(g.reg)
	svc := connect.Register(g.reg, secrets.NewMemory(), g.conn)
	g.conn.Register(g.reg, svc)
	t.Cleanup(g.reg.Close)
	return g
}

func (g *rig) run(id string, params, out any) error {
	g.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := g.reg.Run(context.Background(), g.env, id, raw)
	if err != nil {
		return err
	}
	if out != nil {
		reflect.ValueOf(out).Elem().SetZero()
		b, _ := json.Marshal(res)
		return json.Unmarshal(b, out)
	}
	return nil
}

func (g *rig) must(id string, params, out any) {
	g.t.Helper()
	if err := g.run(id, params, out); err != nil {
		g.t.Fatalf("%s: %v", id, err)
	}
}

// on turns the connection on and chooses the vault.
func (g *rig) on() {
	g.t.Helper()
	g.must("connect.enable", map[string]string{"id": "obsidian"}, nil)
	g.must("obsidian.set_vault", VaultParams{Path: g.vault}, nil)
}

type noteT struct {
	ID, Title, Body string
	Tags            []string
}

func (g *rig) note(title, body, ref string, tags ...string) string {
	g.t.Helper()
	var n noteT
	g.must("note.create", map[string]any{"title": title, "body": body, "ref": ref, "tags": tags}, &n)
	return n.ID
}

func (g *rig) sermon(title, body string, passages ...string) string {
	g.t.Helper()
	var d struct{ ID string }
	g.must("document.create", map[string]any{"title": title, "body": body, "passages": passages}, &d)
	return d.ID
}

func (g *rig) getNote(id string) core.Note {
	g.t.Helper()
	var n core.Note
	g.must("note.get", map[string]string{"id": id}, &n)
	return n
}

func (g *rig) setNote(id string, params map[string]any) {
	g.t.Helper()
	params["id"] = id
	g.must("note.update", params, nil)
}

func (g *rig) export(items ...Ref) ExportResult {
	g.t.Helper()
	var r ExportResult
	g.must("obsidian.export", ExportParams{Items: items}, &r)
	return r
}

func (g *rig) exportAll() ExportResult {
	g.t.Helper()
	var r ExportResult
	g.must("obsidian.export", ExportParams{AllNotes: true, AllSermons: true}, &r)
	return r
}

func (g *rig) importFrom(folder string) ImportResult {
	g.t.Helper()
	var r ImportResult
	g.must("obsidian.import", ImportParams{Folder: folder}, &r)
	return r
}

func (g *rig) file(rel string) string {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join(g.vault, filepath.FromSlash(rel)))
	if err != nil {
		g.t.Fatal(err)
	}
	return string(b)
}

func (g *rig) put(rel, text string) {
	g.t.Helper()
	p := filepath.Join(g.vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		g.t.Fatal(err)
	}
}

func (g *rig) noteCount() int {
	g.t.Helper()
	var ns []core.Note
	g.must("note.list", map[string]any{"limit": 1000}, &ns)
	return len(ns)
}

func (g *rig) findNote(title string) *core.Note {
	g.t.Helper()
	var ns []core.Note
	g.must("note.list", map[string]any{"limit": 1000}, &ns)
	for i := range ns {
		if ns[i].Title == title {
			return &ns[i]
		}
	}
	return nil
}

func ref(kind, id string) Ref { return Ref{Kind: kind, ID: id} }

// Layout and front matter -----------------------------------------------------------------------------------

func TestExportLayoutAndFrontMatter(t *testing.T) {
	g := newRig(t)
	g.on()
	n := g.note("Love in John 3", "God so loved the world. [@lib:01HXABC, p. 12]\n\n- one\n- two", "John 3:16", "gospel", "love of god")
	s := g.sermon("Grace alone", "Grace, grace.", "Romans 8:28-30", "Ephesians 2:8-9")
	r := g.export(ref("note", n), ref("sermon", s))
	if r.Written != 2 || r.Folder != "Glossa" || len(r.Conflicts) != 0 || len(r.Skipped) != 0 {
		t.Fatalf("export: %+v", r)
	}
	got := g.file("Glossa/Love in John 3.md")
	for _, want := range []string{"---\ntitle: Love in John 3\n", "glossa_id: " + n + "\n", "glossa_kind: note\n", "passages:\n  - John 3:16\n",
		"tags:\n  - gospel\n  - love-of-god\n", "created: 20", "updated: 20", "---\nGod so loved the world. [@lib:01HXABC, p. 12]\n\n- one\n- two\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("the note's file lacks %q:\n%s", want, got)
		}
	}
	sg := g.file("Glossa/Grace alone.md")
	for _, want := range []string{"glossa_kind: sermon\n", "  - Romans 8:28-30\n", "  - Ephesians 2:8-9\n", "---\nGrace, grace.\n"} {
		if !strings.Contains(sg, want) {
			t.Errorf("the sermon's file lacks %q:\n%s", want, sg)
		}
	}
	if strings.Contains(got, "# Love in John 3") {
		t.Error("the title is repeated as a heading in the text")
	}
	entries, _ := os.ReadDir(filepath.Join(g.vault, "Glossa"))
	if len(entries) != 2 {
		t.Errorf("stray files in the folder: %v", entries)
	}
}

func TestExportAllAndChangeSubfolder(t *testing.T) {
	g := newRig(t)
	g.on()
	g.note("One", "a", "")
	g.note("Two", "b", "")
	g.sermon("Three", "c")
	var st StatusResult
	g.must("obsidian.set_vault", VaultParams{Path: g.vault, Subfolder: "Bible/Study"}, &st)
	if st.Subfolder != "Bible/Study" {
		t.Fatalf("subfolder: %+v", st)
	}
	r := g.exportAll()
	if r.Written != 3 || r.Folder != "Bible/Study" {
		t.Fatalf("export: %+v", r)
	}
	for _, f := range []string{"Bible/Study/One.md", "Bible/Study/Two.md", "Bible/Study/Three.md"} {
		g.file(f)
	}
	// Changing only the subfolder keeps the links: nothing is written twice.
	if r := g.exportAll(); r.Written != 0 || r.Unchanged != 3 {
		t.Fatalf("re-export: %+v", r)
	}
}

func TestSafeNames(t *testing.T) {
	cases := map[string]string{
		`What is "truth"?`:          "What is -truth--",
		"a/b\\c:d*e|f<g>h":          "a-b-c-d-e-f-g-h",
		"CON":                       "_CON",
		"nul.txt":                   "_nul.txt",
		"COM1":                      "_COM1",
		"lpt9":                      "_lpt9",
		"   spaced   out  ":         "spaced out",
		"trailing dots...":          "trailing dots",
		".hidden":                   "hidden",
		"":                          "Untitled",
		"???":                       "---",
		"[[link]] #tag ^block":      "--link-- -tag -block",
		"line\none\ttab":            "line one tab",
		strings.Repeat("é", 300):    strings.Repeat("é", maxNameRunes),
		"Ends with space and dot. ": "Ends with space and dot",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
	for _, name := range []string{"Window: 3?", "CON", strings.Repeat("x", 500), "aux.md"} {
		if n := safeName(name) + " (12).md"; len(n) > 120 || isReserved(strings.TrimSuffix(n, ".md")) {
			t.Errorf("%q becomes %q", name, n)
		}
	}
}

func TestDuplicateAndOddTitlesGetOwnFiles(t *testing.T) {
	g := newRig(t)
	g.on()
	a := g.note("Same", "1", "")
	b := g.note("Same", "2", "")
	c := g.note("same", "3", "") // differs only in case: a Windows disk would clash
	d := g.note("CON", "4", "")
	e := g.note("", "Just a thought about grace", "") // untitled: named from its first line
	r := g.export(ref("note", a), ref("note", b), ref("note", c), ref("note", d), ref("note", e))
	if r.Written != 5 {
		t.Fatalf("export: %+v", r)
	}
	var names []string
	for _, it := range r.Items {
		names = append(names, it.Path)
	}
	want := []string{"Glossa/Same.md", "Glossa/Same (2).md", "Glossa/same (3).md", "Glossa/_CON.md", "Glossa/Just a thought about grace.md"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("files: %v\nwant:  %v", names, want)
	}
	// A file that is already there and not Glossa's is never overwritten.
	g.put("Glossa/Mine.md", "my own text\n")
	f := g.note("Mine", "from glossa", "")
	r = g.export(ref("note", f))
	if r.Items[0].Path != "Glossa/Mine (2).md" || g.file("Glossa/Mine.md") != "my own text\n" {
		t.Errorf("a file that is not Glossa's was touched: %+v", r.Items)
	}
}

// Safety ---------------------------------------------------------------------------------------------------

func TestPathsOutsideTheVaultAreRefused(t *testing.T) {
	g := newRig(t)
	g.on()
	for _, sub := range []string{"..", "../x", "a/../../x", `..\x`, "C:/x", "/abs", "a:b", "x/.git", ".obsidian"} {
		err := g.run("obsidian.set_vault", VaultParams{Path: g.vault, Subfolder: sub}, nil)
		if err == nil {
			t.Errorf("subfolder %q was accepted", sub)
		}
	}
	outside := t.TempDir()
	for _, folder := range []string{"..", "../" + filepath.Base(outside), outside, `..\x`} {
		if err := g.run("obsidian.import", ImportParams{Folder: folder}, nil); err == nil {
			t.Errorf("import from %q was accepted", folder)
		}
	}
	// A link row whose path was tampered with (a damaged or hostile library) is not followed out of the vault.
	n := g.note("Escape", "x", "")
	err := g.env.Store.Write(context.Background(), "test", func(tx *store.Tx) error {
		return tx.Create("obsidian_links", store.Row{"kind": "note", "item_id": n, "path": "../" + filepath.Base(outside) + "/evil.md", "synced_at": int64(1)})
	})
	if err != nil {
		t.Fatal(err)
	}
	r := g.export(ref("note", n))
	if r.Written != 0 || len(r.Skipped) != 1 {
		t.Fatalf("export: %+v", r)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("something was written outside the vault: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(g.vault), "evil.md")); err == nil {
		t.Fatal("a file was written next to the vault")
	}
}

// linkDir makes a symbolic link, or on Windows a junction, from vault/rel to target. It skips the test when neither can be made.
func linkDir(t *testing.T, vault, rel, target string) {
	t.Helper()
	link := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		return
	}
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err == nil {
			return
		} else {
			t.Skipf("cannot make a junction: %v %s", err, out)
		}
	}
	t.Skip("cannot make a link here")
}

func TestLinkedFolderCannotLeadOutOfTheVault(t *testing.T) {
	g := newRig(t)
	g.on()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.md"), []byte("---\ntitle: Secret\n---\nnot yours\n"), 0o644)
	linkDir(t, g.vault, "Glossa", outside) // the default subfolder is a link to somewhere else
	n := g.note("Trying", "x", "")
	r := g.export(ref("note", n))
	if r.Written != 0 || len(r.Skipped) != 1 {
		t.Fatalf("export through a link: %+v", r)
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 {
		t.Fatalf("something was written through the link: %v", entries)
	}
	// Importing does not read through it either: the link is reported, not followed.
	g.put("Inbox/real.md", "hello\n")
	linkDir(t, g.vault, "Inbox/out", outside)
	res := g.importFrom(".")
	if g.findNote("Secret") != nil {
		t.Fatal("a file outside the vault was imported through a link")
	}
	if g.findNote("real") == nil {
		t.Errorf("the real file was not imported: %+v", res)
	}
	found := false
	for _, s := range res.Skipped {
		found = found || strings.Contains(s.Path, "out") && strings.Contains(s.Reason, "link")
	}
	if !found {
		t.Errorf("the link is not reported: %+v", res.Skipped)
	}
}

func TestFileThatIsALinkIsNeverWrittenThrough(t *testing.T) {
	g := newRig(t)
	g.on()
	outside := filepath.Join(t.TempDir(), "target.md")
	os.WriteFile(outside, []byte("precious\n"), 0o644)
	if err := os.MkdirAll(filepath.Join(g.vault, "Glossa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(g.vault, "Glossa", "Sneaky.md")); err != nil {
		t.Skip("cannot make a symbolic link here")
	}
	n := g.note("Sneaky", "x", "")
	r := g.export(ref("note", n))
	if b, _ := os.ReadFile(outside); string(b) != "precious\n" {
		t.Fatalf("written through a file link: %q", b)
	}
	if r.Items[0].Path != "Glossa/Sneaky (2).md" {
		t.Errorf("did not pick another name: %+v", r.Items)
	}
}

func TestNeverDeletesFiles(t *testing.T) {
	g := newRig(t)
	g.on()
	n := g.note("Keep me", "x", "")
	g.export(ref("note", n))
	g.put("Glossa/Other.md", "mine\n")
	g.must("note.delete", map[string]string{"id": n}, nil)
	g.exportAll()
	g.importFrom("")
	g.file("Glossa/Keep me.md")
	g.file("Glossa/Other.md")
	if g.findNote("Keep me") != nil {
		t.Error("importing brought back a note that was deleted in Glossa")
	}
}

// Idempotence ----------------------------------------------------------------------------------------------

func TestReexportAndReimportChangeNothing(t *testing.T) {
	g := newRig(t)
	g.on()
	n := g.note("Stable", "Body text", "Rom 8:1", "tag one")
	s := g.sermon("Sermon", "Words", "John 1:1")
	g.export(ref("note", n), ref("sermon", s))
	p1, p2 := filepath.Join(g.vault, "Glossa", "Stable.md"), filepath.Join(g.vault, "Glossa", "Sermon.md")
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	for _, p := range []string{p1, p2} {
		os.Chtimes(p, old, old)
	}
	before := g.file("Glossa/Stable.md")
	r := g.exportAll()
	if r.Written != 0 || r.Updated != 0 || r.Unchanged != 2 {
		t.Fatalf("re-export: %+v", r)
	}
	for _, p := range []string{p1, p2} {
		if st, _ := os.Stat(p); !st.ModTime().Equal(old) {
			t.Errorf("%s was rewritten", filepath.Base(p))
		}
	}
	if g.file("Glossa/Stable.md") != before {
		t.Error("the file's text changed")
	}
	nb := g.getNote(n)
	im := g.importFrom("")
	if im.Created != 0 || im.Updated != 0 || im.Unchanged != 2 || im.Files != 2 || g.noteCount() != 1 {
		t.Fatalf("re-import: %+v", im)
	}
	if after := g.getNote(n); after.UpdatedAt != nb.UpdatedAt || after.Body != nb.Body {
		t.Error("importing an unchanged file changed the note")
	}
	if r := g.exportAll(); r.Unchanged != 2 || r.Written+r.Updated != 0 {
		t.Fatalf("export after import: %+v", r)
	}
}

func TestRenamedFileIsFoundByItsID(t *testing.T) {
	g := newRig(t)
	g.on()
	n := g.note("Original name", "first", "")
	g.export(ref("note", n))
	if err := os.MkdirAll(filepath.Join(g.vault, "Archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(g.vault, "Glossa", "Original name.md"), filepath.Join(g.vault, "Archive", "Moved and renamed.md")); err != nil {
		t.Fatal(err)
	}
	// Export finds nothing in its own folder to compare with, so it writes the note back: here the file is outside
	// the subfolder, so the first thing to do is import from the whole vault.
	im := g.importFrom(".")
	if im.Created != 0 || im.Unchanged != 1 || g.noteCount() != 1 {
		t.Fatalf("import after the rename: %+v", im)
	}
	g.put("Archive/Moved and renamed.md", strings.Replace(g.file("Archive/Moved and renamed.md"), "first", "second", 1))
	im = g.importFrom(".")
	if im.Updated != 1 || g.getNote(n).Body != "second" {
		t.Fatalf("edit after the rename: %+v %q", im, g.getNote(n).Body)
	}
	// And exporting follows the file to where it is now, not back to the old place.
	g.setNote(n, map[string]any{"body": "third"})
	r := g.export(ref("note", n))
	if r.Updated != 1 || r.Items[0].Path != "Archive/Moved and renamed.md" {
		t.Fatalf("export after the rename: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(g.vault, "Glossa", "Original name.md")); err == nil {
		t.Error("the old file was made again")
	}
}

func TestRenameInsideTheSubfolderIsFoundByExportToo(t *testing.T) {
	g := newRig(t)
	g.on()
	n := g.note("Before", "text", "")
	g.export(ref("note", n))
	os.Rename(filepath.Join(g.vault, "Glossa", "Before.md"), filepath.Join(g.vault, "Glossa", "After.md"))
	g.setNote(n, map[string]any{"body": "text 2"})
	r := g.export(ref("note", n))
	if r.Updated != 1 || r.Items[0].Path != "Glossa/After.md" || !strings.Contains(g.file("Glossa/After.md"), "text 2") {
		t.Fatalf("export: %+v", r)
	}
	if entries, _ := os.ReadDir(filepath.Join(g.vault, "Glossa")); len(entries) != 1 {
		t.Errorf("a second file was made: %v", entries)
	}
}

func TestRenamedFileWithoutAnIDIsFoundByItsText(t *testing.T) {
	g := newRig(t)
	g.on()
	g.put("Inbox/Plain.md", "Just some words.\n")
	if im := g.importFrom("Inbox"); im.Created != 1 {
		t.Fatalf("first import: %+v", im)
	}
	os.Rename(filepath.Join(g.vault, "Inbox", "Plain.md"), filepath.Join(g.vault, "Inbox", "Plainer.md"))
	im := g.importFrom("Inbox")
	if im.Created != 0 || g.noteCount() != 1 {
		t.Fatalf("a renamed file made a second note: %+v", im)
	}
}

// One side or both changed ----------------------------------------------------------------------------------

func TestOneSideChangedThatSideWins(t *testing.T) {
	g := newRig(t)
	g.on()
	n := g.note("Two ways", "start", "")
	g.export(ref("note", n))
	// Obsidian edits the file, adds a property of its own, and Glossa does nothing.
	f := g.file("Glossa/Two ways.md")
	g.put("Glossa/Two ways.md", strings.Replace(strings.Replace(f, "start", "start, then more", 1), "---\ntitle:", "---\naliases: [Duo]\ncssclasses:\n  - wide\ntitle:", 1))
	r := g.export(ref("note", n))
	if r.ObsidianNewer != 1 || r.Updated != 0 || len(r.Conflicts) != 0 || !strings.Contains(g.file("Glossa/Two ways.md"), "then more") {
		t.Fatalf("export with only the file changed: %+v", r)
	}
	if g.getNote(n).Body != "start" {
		t.Fatal("export changed the note")
	}
	im := g.importFrom("")
	if im.Updated != 1 || g.getNote(n).Body != "start, then more" {
		t.Fatalf("import: %+v %q", im, g.getNote(n).Body)
	}
	// Now Glossa edits and the file does not: import leaves the note, export sends it, and Obsidian's own property survives.
	g.setNote(n, map[string]any{"body": "start, then more, and Glossa's"})
	im = g.importFrom("")
	if im.GlossaNewer != 1 || im.Updated != 0 || g.getNote(n).Body != "start, then more, and Glossa's" {
		t.Fatalf("import with only Glossa changed: %+v", im)
	}
	r = g.export(ref("note", n))
	if r.Updated != 1 {
		t.Fatalf("export: %+v", r)
	}
	out := g.file("Glossa/Two ways.md")
	if !strings.Contains(out, "and Glossa's") || !strings.Contains(out, "aliases: [Duo]\n") || !strings.Contains(out, "cssclasses:\n  - wide\n") {
		t.Errorf("rewritten file: %s", out)
	}
	if r := g.export(ref("note", n)); r.Unchanged != 1 {
		t.Errorf("settled state: %+v", r)
	}
}

func TestBothChangedAsksAndEachChoiceWorks(t *testing.T) {
	for _, choice := range []string{"obsidian", "glossa", "both"} {
		t.Run(choice, func(t *testing.T) {
			g := newRig(t)
			g.on()
			n := g.note("Both", "line one\nline two", "John 1:1", "a")
			g.export(ref("note", n))
			g.put("Glossa/Both.md", strings.Replace(g.file("Glossa/Both.md"), "line two", "line two from Obsidian", 1))
			g.setNote(n, map[string]any{"body": "line one\nline two from Glossa"})
			fileBefore := g.file("Glossa/Both.md")

			ex := g.export(ref("note", n))
			if len(ex.Conflicts) != 1 || ex.Updated != 0 {
				t.Fatalf("export: %+v", ex)
			}
			c := ex.Conflicts[0]
			if c.ID != n || !strings.Contains(c.YourExcerpt, "Glossa") || !strings.Contains(c.ObsidianExcerpt, "Obsidian") || strings.Contains(c.YourExcerpt, "line one") {
				t.Errorf("conflict: %+v", c)
			}
			im := g.importFrom("")
			if len(im.Conflicts) != 1 || im.Updated != 0 {
				t.Fatalf("import: %+v", im)
			}
			var st StatusResult
			g.must("obsidian.status", nil, &st)
			if len(st.Conflicts) != 1 || st.Linked != 1 {
				t.Fatalf("status: %+v", st)
			}
			if g.file("Glossa/Both.md") != fileBefore || g.getNote(n).Body != "line one\nline two from Glossa" {
				t.Fatal("something changed before the person chose")
			}

			var res ResolveResult
			g.must("obsidian.resolve", ResolveParams{Kind: "note", ID: n, Resolve: choice}, &res)
			switch choice {
			case "obsidian":
				if g.getNote(n).Body != "line one\nline two from Obsidian" || g.file("Glossa/Both.md") != fileBefore {
					t.Errorf("use Obsidian's: %q", g.getNote(n).Body)
				}
			case "glossa":
				if g.getNote(n).Body != "line one\nline two from Glossa" || !strings.Contains(g.file("Glossa/Both.md"), "two from Glossa") || strings.Contains(g.file("Glossa/Both.md"), "Obsidian") {
					t.Errorf("keep Glossa's: %s", g.file("Glossa/Both.md"))
				}
			case "both":
				cp := g.findNote("Both (from Obsidian)")
				if cp == nil || cp.Body != "line one\nline two from Obsidian" || res.CopyID != cp.ID {
					t.Fatalf("keep both: %+v %+v", cp, res)
				}
				if g.getNote(n).Body != "line one\nline two from Glossa" || !strings.Contains(g.file("Glossa/Both.md"), "two from Glossa") {
					t.Error("keep both: Glossa's text is not in the note and the file")
				}
			}
			// Settled: nothing is asked again, and nothing is linked to the copy.
			g.must("obsidian.status", nil, &st)
			if len(st.Conflicts) != 0 {
				t.Errorf("still a conflict: %+v", st.Conflicts)
			}
			if ex := g.export(ref("note", n)); len(ex.Conflicts) != 0 || ex.Updated != 0 || ex.Written != 0 {
				t.Errorf("export after resolving: %+v", ex)
			}
			if im := g.importFrom(""); len(im.Conflicts) != 0 || im.Updated != 0 || im.Created != 0 {
				t.Errorf("import after resolving: %+v", im)
			}
		})
	}
}

func TestFileFoundByIDWithOtherTextAsksFirst(t *testing.T) {
	// A library copy has the note, the vault has the file, and no link between them yet: no baseline, so a difference is asked about.
	g := newRig(t)
	g.on()
	n := g.note("Found", "Glossa says this", "")
	g.put("Glossa/Found.md", "---\ntitle: Found\nglossa_id: "+n+"\nglossa_kind: note\n---\nThe file says that\n")
	if im := g.importFrom(""); len(im.Conflicts) != 1 || im.Updated != 0 {
		t.Fatalf("import: %+v", im)
	}
	g.put("Glossa/Found.md", "---\ntitle: Found\nglossa_id: "+n+"\nglossa_kind: note\n---\nGlossa says this\n")
	if im := g.importFrom(""); len(im.Conflicts) != 0 || im.Unchanged != 1 {
		t.Fatalf("same text, no conflict: %+v", im)
	}
}

// Importing -----------------------------------------------------------------------------------------------

func TestImportWikilinksEmbedsAndTags(t *testing.T) {
	g := newRig(t)
	g.on()
	g.put("Study/Hub.md", "---\ntitle: The hub note\ntags: [study, \"deep dive\"]\npassages: [John 3:16]\n---\n"+
		"See [[Spoke]] and [[Spoke|the spoke]] and [[Study/Spoke#Heading]] and [[Elsewhere/Ghost]] and [[Missing note|alias]].\n"+
		"Embed: ![[diagram.png]] and ![[Spoke]]\n"+
		"Tag in text #grace stays. `[[code]]` stays.\n\n```\n[[fenced]]\n```\n")
	g.put("Study/Spoke.md", "---\ntitle: Spoke title\n---\nspoke body\n")
	im := g.importFrom("Study")
	if im.Created != 2 || im.Links != 5 || im.Embeds != 2 {
		t.Fatalf("import: %+v", im)
	}
	hub := g.findNote("The hub note")
	if hub == nil {
		t.Fatal("no hub note")
	}
	want := "See Spoke title and the spoke and Spoke title and Ghost and alias.\n" +
		"Embed: ![[diagram.png]] and ![[Spoke]]\n" +
		"Tag in text #grace stays. `[[code]]` stays.\n\n```\n[[fenced]]\n```"
	if hub.Body != want {
		t.Errorf("body:\n%s\nwant:\n%s", hub.Body, want)
	}
	if !reflect.DeepEqual(hub.Tags, []string{"deep-dive", "study"}) {
		t.Errorf("tags: %v", hub.Tags)
	}
	if hub.Ref == nil || hub.Ref.Display != "John 3:16" {
		t.Errorf("passage: %+v", hub.Ref)
	}
	// The file itself is never touched by an import.
	if !strings.Contains(g.file("Study/Hub.md"), "[[Spoke]]") || strings.Contains(g.file("Study/Hub.md"), "glossa_id") {
		t.Error("the file was changed by importing it")
	}
	// A second import changes nothing, wikilinks and all.
	if im := g.importFrom("Study"); im.Created != 0 || im.Updated != 0 || im.Unchanged != 2 || g.noteCount() != 2 {
		t.Errorf("second import: %+v", im)
	}
}

func TestFilesWithoutFrontMatterUseTheFileName(t *testing.T) {
	g := newRig(t)
	g.on()
	raw := "# A heading\n\nThree words here.\r\nSecond line.\r\n"
	g.put("Notes/Meeting notes 2026.md", raw)
	g.put("Notes/Dashes.md", "---\nthis is a rule, not front matter\n\nstill text\n")
	g.put("Notes/BOM.md", "\xef\xbb\xbf---\ntitle: With a BOM\n---\nbody\n")
	g.put("Notes/Empty.md", "")
	g.put("Notes/Not markdown.txt", "ignored\n")
	g.put(".obsidian/hidden.md", "ignored\n")
	im := g.importFrom(".")
	if im.Created != 4 || im.Files != 4 {
		t.Fatalf("import: %+v", im)
	}
	n := g.findNote("Meeting notes 2026")
	if n == nil || n.Body != "# A heading\n\nThree words here.\nSecond line." {
		t.Fatalf("note: %+v", n)
	}
	if d := g.findNote("Dashes"); d == nil || !strings.HasPrefix(d.Body, "---\nthis is a rule") {
		t.Errorf("a file that starts with a rule: %+v", d)
	}
	if g.findNote("With a BOM") == nil || g.findNote("Empty") == nil {
		t.Error("BOM or empty file not imported")
	}
	if b, _ := os.ReadFile(filepath.Join(g.vault, "Notes", "Meeting notes 2026.md")); string(b) != raw {
		t.Error("the file was rewritten")
	}
}

func TestBadAndHugeFilesAreSkippedWithAMessage(t *testing.T) {
	g := newRig(t)
	g.on()
	g.put("In/good.md", "fine\n")
	g.put("In/latin1.md", "caf\xe9 not utf-8\n")
	huge := strings.Repeat("x", maxFileBytes+1)
	g.put("In/huge.md", huge)
	im := g.importFrom("In")
	if im.Created != 1 || len(im.Skipped) != 2 {
		t.Fatalf("import: %+v", im)
	}
	reasons := map[string]string{}
	for _, s := range im.Skipped {
		reasons[s.Path] = s.Reason
	}
	if !strings.Contains(reasons["In/latin1.md"], "UTF-8") || !strings.Contains(reasons["In/huge.md"], "5 MB") {
		t.Errorf("reasons: %v", reasons)
	}
	if !strings.Contains(im.Message, "2 files were skipped") {
		t.Errorf("message: %s", im.Message)
	}
	if g.noteCount() != 1 {
		t.Error("a skipped file made a note")
	}
	// Export does not overwrite a file it cannot read as its own.
	n := g.note("latin1", "x", "")
	g.put("Glossa/latin1.md", "caf\xe9\n")
	if r := g.export(ref("note", n)); r.Items[0].Path != "Glossa/latin1 (2).md" {
		t.Errorf("export: %+v", r.Items)
	}
}

func TestStaleIDMakesOneNoteNotOnePerImport(t *testing.T) {
	g := newRig(t)
	g.on()
	g.put("Glossa/From elsewhere.md", "---\ntitle: From elsewhere\nglossa_id: 01NOSUCHNOTE\nglossa_kind: note\ntags: [x]\n---\nbody\n")
	g.put("Glossa/A sermon.md", "---\ntitle: A sermon\nglossa_id: 01NOSUCHSERMON\nglossa_kind: sermon\nglossa_doc_kind: lesson\npassages:\n  - Psalm 23\n---\nlesson text\n")
	if im := g.importFrom(""); im.Created != 2 {
		t.Fatalf("first: %+v", im)
	}
	for i := 0; i < 2; i++ {
		if im := g.importFrom(""); im.Created != 0 || im.Unchanged != 2 {
			t.Fatalf("again: %+v", im)
		}
	}
	var docs []struct {
		Kind, Title string
		Passages    []core.RefResult
	}
	g.must("document.list", map[string]any{}, &docs)
	if len(docs) != 1 || docs[0].Kind != "lesson" || docs[0].Title != "A sermon" || len(docs[0].Passages) != 1 {
		t.Fatalf("documents: %+v", docs)
	}
	if g.noteCount() != 1 {
		t.Errorf("notes: %d", g.noteCount())
	}
}

func TestPassagesThatCannotBeAttachedAreSaidSo(t *testing.T) {
	g := newRig(t)
	g.on()
	g.put("In/p.md", "---\ntitle: P\npassages: [\"Hezekiah 99:1\", John 1:1, Romans 8:1]\n---\nx\n")
	im := g.importFrom("In")
	n := g.findNote("P")
	if n == nil || n.Ref == nil || n.Ref.Display != "John 1:1" || len(im.Notes) != 1 || !strings.Contains(im.Notes[0], "2 passages were not attached") {
		t.Fatalf("%+v %+v", n, im)
	}
}

func TestSermonsRoundTrip(t *testing.T) {
	g := newRig(t)
	g.on()
	s := g.sermon("Round trip", "First draft", "Romans 8:28-30")
	g.export(ref("sermon", s))
	g.put("Glossa/Round trip.md", strings.Replace(strings.Replace(g.file("Glossa/Round trip.md"), "First draft", "Second draft", 1), "  - Romans 8:28-30\n", "  - Romans 8:28-30\n  - John 3:16\n", 1))
	im := g.importFrom("")
	if im.Updated != 1 {
		t.Fatalf("import: %+v", im)
	}
	var d struct {
		Body     string
		Passages []core.RefResult
	}
	g.must("document.get", map[string]string{"id": s}, &d)
	if d.Body != "Second draft" || len(d.Passages) != 2 || d.Passages[1].Display != "John 3:16" {
		t.Fatalf("sermon: %+v", d)
	}
	if r := g.exportAll(); r.Unchanged != 1 {
		t.Errorf("export after: %+v", r)
	}
}

// Switch, vault and links ----------------------------------------------------------------------------------

func TestOffDoesNothingAndNeedsAVault(t *testing.T) {
	g := newRig(t)
	n := g.note("N", "x", "")
	for _, call := range []func() error{
		func() error { return g.run("obsidian.export", ExportParams{Items: []Ref{ref("note", n)}}, nil) },
		func() error { return g.run("obsidian.import", ImportParams{}, nil) },
		func() error { return g.run("obsidian.set_vault", VaultParams{Path: g.vault}, nil) },
	} {
		err := call()
		var ce *command.Error
		if err == nil || !asCommandError(err, &ce) || ce.Code != "off" {
			t.Errorf("while off: %v", err)
		}
	}
	if entries, _ := os.ReadDir(g.vault); len(entries) != 0 {
		t.Errorf("the vault was touched while off: %v", entries)
	}
	g.must("connect.enable", map[string]string{"id": "obsidian"}, nil)
	err := g.run("obsidian.export", ExportParams{Items: []Ref{ref("note", n)}}, nil)
	var ce *command.Error
	if !asCommandError(err, &ce) || ce.Code != "no_vault" {
		t.Errorf("without a vault: %v", err)
	}
	var tr connect.TestOutput
	g.must("connect.test", map[string]string{"id": "obsidian"}, &tr)
	if tr.OK || !strings.Contains(tr.Message, "Choose your Obsidian vault") {
		t.Errorf("test without a vault: %+v", tr)
	}
	g.must("obsidian.set_vault", VaultParams{Path: g.vault}, nil)
	g.must("connect.test", map[string]string{"id": "obsidian"}, &tr)
	if !tr.OK || !strings.Contains(tr.Message, "“Glossa”") {
		t.Errorf("test: %+v", tr)
	}
	if entries, _ := os.ReadDir(g.vault); len(entries) != 0 {
		t.Errorf("the check left files behind: %v", entries)
	}
	g.must("obsidian.export", ExportParams{Items: []Ref{ref("note", n)}}, nil)
	if err := os.RemoveAll(g.vault); err != nil {
		t.Fatal(err)
	}
	g.must("connect.test", map[string]string{"id": "obsidian"}, &tr)
	if tr.OK || !strings.Contains(tr.Message, "not there any more") {
		t.Errorf("test after the vault went: %+v", tr)
	}
	err = g.run("obsidian.import", ImportParams{}, nil)
	if !asCommandError(err, &ce) || ce.Code != "no_vault" {
		t.Errorf("import after the vault went: %v", err)
	}
}

func asCommandError(err error, ce **command.Error) bool {
	for err != nil {
		if e, ok := err.(*command.Error); ok {
			*ce = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestChoosingAnotherVaultForgetsTheLinks(t *testing.T) {
	g := newRig(t)
	g.on()
	n := g.note("Moves", "x", "")
	g.export(ref("note", n))
	var st StatusResult
	g.must("obsidian.status", nil, &st)
	if st.Linked != 1 || st.LastExport == nil || st.LastExport.Written != 1 {
		t.Fatalf("status: %+v", st)
	}
	other := t.TempDir()
	g.must("obsidian.set_vault", VaultParams{Path: other}, &st)
	if st.Linked != 0 {
		t.Fatalf("links kept: %+v", st)
	}
	if r := g.export(ref("note", n)); r.Written != 1 {
		t.Fatalf("export to the new vault: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(other, "Glossa", "Moves.md")); err != nil {
		t.Error(err)
	}
	g.must("obsidian.set_vault", VaultParams{}, &st)
	if st.Vault != "" {
		t.Errorf("vault not forgotten: %+v", st)
	}
	// Both files are still where they were.
	g.file("Glossa/Moves.md")
}

func TestDeletedAndSharedLinkRowsDoNotSync(t *testing.T) {
	// The link table is per device (migration 0019); store/perdevice_test.go checks it is never sent. Here: the link holds a
	// path relative to the vault, so it stays meaningful when the vault folder itself moves.
	g := newRig(t)
	g.on()
	n := g.note("Relative", "x", "")
	g.export(ref("note", n))
	l, err := linkOfItem(context.Background(), g.env, "note", n)
	if err != nil || l == nil || l.Path != "Glossa/Relative.md" || filepath.IsAbs(l.Path) {
		t.Fatalf("link: %+v %v", l, err)
	}
	moved := filepath.Join(t.TempDir(), "renamed vault")
	if err := os.Rename(g.vault, moved); err != nil {
		t.Fatal(err)
	}
	g.vault = moved
	g.must("obsidian.set_vault", VaultParams{Path: moved}, nil)
	// A different vault path forgets links, so this is a fresh start, and the existing file is found by its id.
	r := g.export(ref("note", n))
	if r.Written != 0 || r.Unchanged != 1 {
		t.Fatalf("export: %+v", r)
	}
}

// Front matter on its own -------------------------------------------------------------------------------

func TestFrontMatterRoundTripsAwkwardText(t *testing.T) {
	titles := []string{"Plain", "Colon: in the middle", "ends with colon:", "# hash", "has #hash", "\"quoted\"", "it's", "true", "123", "2026-01-02", "- dash", "[brackets]", "{braces}", "tab\there",
		"line\nbreak", " leading", "trailing ", "unicode é ñ 日本", "back\\slash", "a: b: c", "", "null", "~", "*star", "&amp", "!bang", "%pct", "@at", "`tick`", "https://example.com/x?y=1#z"}
	for _, in := range titles {
		it := item{Kind: "note", ID: "01ID", Title: in, Body: "b", Passages: []string{"John 3:16"}, Tags: []string{"a b", "c"}, Created: 1700000000000, Updated: 1700000100000}
		got := view("x.md", render(it, ""))
		want := strings.TrimSpace(in)
		if want == "" {
			want = "x" // no title: the file name
		}
		if got.Title != want || got.ID != "01ID" || got.Kind != "note" || got.Body != "b" || !reflect.DeepEqual(got.Passages, []string{"John 3:16"}) || !reflect.DeepEqual(got.Tags, []string{"a-b", "c"}) {
			t.Errorf("%q came back as %+v", in, got)
		}
	}
}

func TestFrontMatterReadsWhatObsidianWrites(t *testing.T) {
	src := "---\r\ntitle: 'It''s here'\r\ntags:\r\n  - one\r\n  - \"two words\"\r\n  - \"#three\"\r\npassages: [John 3:16, \"Rom 8:28-30\"]\r\nstatus: draft # a comment\r\nlinks:\r\n  - a\r\n  - b\r\nsummary: |\r\n  multi\r\n  line\r\n---\r\nbody\r\n"
	p := parseFile(src)
	if !p.HasFM || p.Fields[keyTitle].Text != "It's here" || !reflect.DeepEqual(p.Fields[keyTags].List, []string{"one", "two words", "#three"}) ||
		!reflect.DeepEqual(p.Fields[keyPassages].List, []string{"John 3:16", "Rom 8:28-30"}) || p.Body != "body\n" {
		t.Fatalf("%+v", p)
	}
	for _, want := range []string{"status: draft # a comment\n", "links:\n  - a\n  - b\n", "summary: |\n  multi\n  line\n"} {
		if !strings.Contains(p.Extra, want) {
			t.Errorf("extra lacks %q: %q", want, p.Extra)
		}
	}
	v := view("n.md", src)
	if !reflect.DeepEqual(v.Tags, []string{"one", "three", "two-words"}) || !reflect.DeepEqual(v.Passages, []string{"John 3:16", "Romans 8:28-30"}) {
		t.Errorf("%+v", v)
	}
	// Not front matter: a rule and some text, an unclosed block, a block of prose.
	for _, s := range []string{"---\nnot: ok: really\n- x\nprose line here\n---\nafter", "---\ntitle: x\nno closing line", "no front matter at all"} {
		if parseFile(s).HasFM {
			t.Errorf("%q was read as front matter", s)
		}
	}
}

// A Windows junction is not a symbolic link, and Go's EvalSymlinks does not resolve it (since Go 1.23), so it is
// checked for by itself: an export into a subfolder that is a junction to somewhere else must write nothing there.
func TestJunctionCannotLeadOutOfTheVault(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions exist on Windows only")
	}
	g := newRig(t)
	g.on()
	outside := t.TempDir()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(g.vault, "Glossa"), outside).CombinedOutput(); err != nil {
		t.Skipf("cannot make a junction: %v %s", err, out)
	}
	n := g.note("Through a junction", "x", "")
	r := g.export(ref("note", n))
	if r.Written != 0 || len(r.Skipped) != 1 {
		t.Fatalf("export: %+v", r)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("written through the junction: %v", entries)
	}
	os.WriteFile(filepath.Join(outside, "x.md"), []byte("secret\n"), 0o644)
	if err := g.run("obsidian.import", ImportParams{}, nil); err == nil || g.findNote("x") != nil {
		t.Fatalf("read through the junction: %v", err)
	}
}

func TestImportAcceptsAFolderInsideTheVaultByItsFullPath(t *testing.T) {
	g := newRig(t)
	g.on()
	g.put("Inbox/a.md", "text\n")
	if im := g.importFrom(filepath.Join(g.vault, "Inbox")); im.Created != 1 || im.Folder != "Inbox" {
		t.Fatalf("import: %+v", im)
	}
	if im := g.importFrom(g.vault); im.Unchanged != 1 {
		t.Fatalf("the vault by its full path: %+v", im)
	}
	if err := g.run("obsidian.import", ImportParams{Folder: filepath.Dir(g.vault)}, nil); err == nil {
		t.Error("the folder above the vault was accepted")
	}
}
