package sync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// Books and modules (ADR 0013, T20b). The synced thing is a shared_files row: a file's identity
// (hash, name, size), never a path. The bytes travel as blobs. Each device keeps its own record of
// what it holds in sync_files.

const (
	// stateBooksMode is this device's choice of when to download books.
	stateBooksMode = "books_mode"
	// BooksAll downloads every book at sync time; BooksWhenOpened only when one is opened.
	BooksAll        = "all"
	BooksWhenOpened = "opened"
)

// Locations are the folders on this device that synced files go to.
type Locations struct {
	// BooksDir is the library folder books from other devices are copied into.
	BooksDir string
	// ModulesDir holds modules/<id>.db.
	ModulesDir string
	// ReleaseModule closes a module that is open, so its file can be replaced or removed. Optional.
	ReleaseModule func(id string)
}

var moduleIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// BlobProgress is how far a transfer has got, for the interface to show.
type BlobProgress struct {
	Active     bool   `json:"active"`
	Direction  string `json:"direction,omitempty" enum:"upload,download" desc:"upload: this device's books and modules going to the folder; download: other devices' coming here"`
	Name       string `json:"name,omitempty" desc:"The file being transferred"`
	BytesDone  int64  `json:"bytes_done"`
	BytesTotal int64  `json:"bytes_total"`
	FilesDone  int    `json:"files_done"`
	FilesTotal int    `json:"files_total"`
}

// BlobFailure is a file that could not be transferred.
type BlobFailure struct {
	Name  string `json:"name"`
	Error string `json:"error" desc:"In plain words"`
}

// BlobResult is what the book and module part of a sync did.
type BlobResult struct {
	Uploaded   int           `json:"uploaded" desc:"Files this device added to the folder"`
	Downloaded int           `json:"downloaded" desc:"Files copied from the folder to this device"`
	Linked     int           `json:"linked" desc:"Files that were already on this device, so nothing was downloaded"`
	Removed    int           `json:"removed" desc:"Files taken off this device because they were deleted on another"`
	Collected  int           `json:"collected" desc:"Files removed from the folder because no device uses them any more"`
	OnDemand   int           `json:"on_demand" desc:"Books left in the folder until they are opened"`
	Waiting    int           `json:"waiting" desc:"Files other devices have listed that have not reached the folder yet"`
	Skipped    int           `json:"skipped" desc:"Files not sent because their licence does not allow a stored copy"`
	Failed     []BlobFailure `json:"failed,omitempty"`
}

// FileStats says how many books and modules are shared and how many are on this device.
type FileStats struct {
	Mode       string `json:"mode" enum:"all,opened" desc:"all: download every book at sync time; opened: only when a book is opened"`
	Files      int    `json:"files" desc:"Books and modules in the shared list"`
	Bytes      int64  `json:"bytes"`
	OnDevice   int    `json:"on_device"`
	RemoteOnly int    `json:"remote_only" desc:"Shared files not on this device yet"`
	RemoteSize int64  `json:"remote_bytes"`
}

// Progress is the state of the transfer under way, or the last one.
func (e *Engine) Progress() BlobProgress {
	e.pmu.Lock()
	defer e.pmu.Unlock()
	return e.prog
}

func (e *Engine) report(fn func(p *BlobProgress)) {
	e.pmu.Lock()
	fn(&e.prog)
	p := e.prog
	cb := e.OnProgress
	e.pmu.Unlock()
	if cb != nil {
		cb(p)
	}
}

func (e *Engine) beginRun(dir string, files int, bytes int64) {
	e.report(func(p *BlobProgress) {
		*p = BlobProgress{Active: true, Direction: dir, FilesTotal: files, BytesTotal: bytes}
	})
}

func (e *Engine) endRun() {
	e.report(func(p *BlobProgress) { p.Active, p.Name = false, "" })
}

func (e *Engine) advance(n int64) { e.report(func(p *BlobProgress) { p.BytesDone += n }) }

func (e *Engine) startFile(name string) { e.report(func(p *BlobProgress) { p.Name = name }) }

func (e *Engine) finishFile() { e.report(func(p *BlobProgress) { p.FilesDone++ }) }

func (e *Engine) blobChunk() int {
	if e.BlobChunk > 0 && e.BlobChunk <= maxBlobChunk {
		return e.BlobChunk
	}
	return defaultBlobChunk
}

// BooksMode is this device's setting for when to download books.
func (e *Engine) BooksMode(ctx context.Context) string {
	v, ok, _ := e.st.SyncState(ctx, stateBooksMode)
	if ok && v == BooksWhenOpened {
		return BooksWhenOpened
	}
	return BooksAll
}

// SetBooksMode chooses when this device downloads books: BooksAll or BooksWhenOpened.
func (e *Engine) SetBooksMode(ctx context.Context, mode string) error {
	if mode != BooksAll && mode != BooksWhenOpened {
		return fmt.Errorf("books can be kept %q or %q", BooksAll, BooksWhenOpened)
	}
	return e.st.SetSyncState(ctx, stateBooksMode, mode)
}

// shared is a shared_files row, with whether the book it belongs to is deleted.
type shared struct {
	ID, Kind, ItemID, ModuleID, Name, Format, SHA string
	Size, DeletedAt, ItemDeletedAt                int64
	HasItem                                       bool
}

func (s shared) live() bool {
	return s.DeletedAt == 0 && (s.Kind == "module" || s.HasItem && s.ItemDeletedAt == 0)
}

// deadSince is when the file stopped being used (zero while it is in use).
func (s shared) deadSince() int64 {
	if s.live() {
		return 0
	}
	return max(s.DeletedAt, s.ItemDeletedAt, 1)
}

func (e *Engine) shared(ctx context.Context) (map[string]shared, error) {
	rows, err := e.st.Query(ctx, `SELECT s.id, s.kind, COALESCE(s.item_id, ''), COALESCE(s.module_id, ''), s.name,
		s.format, s.size_bytes, s.sha256, COALESCE(s.deleted_at, 0), i.id IS NOT NULL, COALESCE(i.deleted_at, 0)
		FROM shared_files s LEFT JOIN items i ON i.id = s.item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]shared{}
	for rows.Next() {
		var s shared
		if err := rows.Scan(&s.ID, &s.Kind, &s.ItemID, &s.ModuleID, &s.Name, &s.Format, &s.Size, &s.SHA,
			&s.DeletedAt, &s.HasItem, &s.ItemDeletedAt); err != nil {
			return nil, err
		}
		out[s.ID] = s
	}
	return out, rows.Err()
}

func liveSorted(all map[string]shared) []shared {
	var out []shared
	for _, s := range all {
		if s.live() {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func idFor(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:16])
}

func bookFileID(item, sha string) string { return "f" + idFor(item, sha) }
func moduleFileID(module string) string  { return "m" + idFor(module) }

func (e *Engine) modulePath(id string) string { return filepath.Join(e.Local.ModulesDir, id+".db") }

// localBooks maps the hash of every book file on this device to a path that exists.
func (e *Engine) localBooks(ctx context.Context) (map[string]string, error) {
	rows, err := e.st.Query(ctx, `SELECT f.sha256, l.path_on_volume, f.rel_path FROM files f
		JOIN library_folders l ON l.id = f.folder_id
		WHERE f.deleted_at IS NULL AND l.deleted_at IS NULL AND f.sha256 IS NOT NULL AND f.sha256 <> ''
		ORDER BY f.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var sha, dir, rel string
		if err := rows.Scan(&sha, &dir, &rel); err != nil {
			return nil, err
		}
		if _, ok := out[sha]; ok {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			out[sha] = p
		}
	}
	return out, rows.Err()
}

// noStoreItems are the books whose licence does not allow keeping a copy anywhere else.
func (e *Engine) noStoreItems(ctx context.Context) (map[string]bool, error) {
	rows, err := e.st.Query(ctx, `SELECT item_id FROM licences WHERE store_allowed = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func moduleIDs(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, en := range es {
		if id, ok := strings.CutSuffix(en.Name(), ".db"); ok && !en.IsDir() && moduleIDRE.MatchString(id) {
			out = append(out, id)
		}
	}
	return out
}

// indexLocal lists this device's own books and modules in shared_files, so other devices learn of
// them. It only adds or updates: a book that is not on this device (on-demand) is not "removed".
// A module taken out of this device is the one thing that is marked deleted.
func (e *Engine) indexLocal(ctx context.Context, res *BlobResult) error {
	all, err := e.shared(ctx)
	if err != nil {
		return err
	}
	noStore, err := e.noStoreItems(ctx)
	if err != nil {
		return err
	}
	type plan struct {
		create, revive bool
		row            store.Row
		id             string
		update         store.Row
		del            []string
		rec            *store.SyncFile
	}
	var plans []plan

	// Books.
	rows, err := e.st.Query(ctx, `SELECT f.item_id, f.rel_path, f.format, COALESCE(f.size_bytes, 0), f.sha256
		FROM files f JOIN items i ON i.id = f.item_id
		WHERE f.deleted_at IS NULL AND i.deleted_at IS NULL AND f.sha256 IS NOT NULL AND f.sha256 <> ''
		ORDER BY f.item_id, f.rel_path`)
	if err != nil {
		return err
	}
	type bf struct {
		item, rel, format, sha string
		size                   int64
	}
	var books []bf
	for rows.Next() {
		var b bf
		if err := rows.Scan(&b.item, &b.rel, &b.format, &b.size, &b.sha); err != nil {
			rows.Close()
			return err
		}
		books = append(books, b)
	}
	rows.Close()
	planned := map[string]bool{}
	for _, b := range books {
		if noStore[b.item] {
			res.Skipped++
			continue
		}
		id := bookFileID(b.item, b.sha)
		if planned[id] {
			continue
		}
		planned[id] = true
		name := filepath.Base(filepath.FromSlash(b.rel))
		cur, ok := all[id]
		switch {
		case !ok:
			p := plan{create: true, id: id, row: store.Row{"id": id, "kind": "book", "item_id": b.item, "name": name,
				"format": b.format, "size_bytes": b.size, "sha256": b.sha}}
			for _, o := range all { // an older version of the same file is replaced
				if o.Kind == "book" && o.ItemID == b.item && o.Name == name && o.SHA != b.sha && o.DeletedAt == 0 {
					p.del = append(p.del, o.ID)
				}
			}
			sort.Strings(p.del)
			plans = append(plans, p)
		case cur.DeletedAt != 0:
			plans = append(plans, plan{revive: true, id: id})
		}
	}

	// Modules.
	if e.Local.ModulesDir != "" {
		have := map[string]bool{}
		for _, mid := range moduleIDs(e.Local.ModulesDir) {
			have[mid] = true
			id := moduleFileID(mid)
			st, err := os.Stat(e.modulePath(mid))
			if err != nil {
				continue
			}
			rec, hasRec, err := e.st.SyncFile(ctx, id)
			if err != nil {
				return err
			}
			sha, size, mtime := rec.SHA256, st.Size(), st.ModTime().UnixMilli()
			if !hasRec || rec.Size != size || rec.MTime != mtime {
				if sha, size, err = hashFile(e.modulePath(mid)); err != nil {
					continue
				}
			}
			newRec := store.SyncFile{FileID: id, SHA256: sha, Size: size, MTime: mtime, Origin: "local"}
			if hasRec {
				newRec.Origin = rec.Origin
			}
			cur, ok := all[id]
			switch {
			case !ok:
				plans = append(plans, plan{create: true, id: id, rec: &newRec, row: store.Row{"id": id, "kind": "module",
					"module_id": mid, "name": mid + ".db", "format": "module", "size_bytes": size, "sha256": sha}})
			case cur.DeletedAt != 0 && !hasRec:
				// Installed again here after it was removed elsewhere.
				plans = append(plans, plan{revive: true, id: id, rec: &newRec,
					update: store.Row{"sha256": sha, "size_bytes": size}})
			case cur.DeletedAt != 0:
				// Removed on another device: cleanup takes it off this one.
			case cur.SHA != sha && (!hasRec || rec.SHA256 != sha):
				// Changed here (a new licence, say), or a different build under the same ID: send ours.
				plans = append(plans, plan{id: id, rec: &newRec, update: store.Row{"sha256": sha, "size_bytes": size}})
			case cur.SHA == sha && (!hasRec || rec.SHA256 != sha):
				plans = append(plans, plan{id: id, rec: &newRec})
			}
		}
		// A module removed from this device: remove it from the list.
		recs, err := e.st.SyncFiles(ctx)
		if err != nil {
			return err
		}
		for _, r := range recs {
			cur, ok := all[r.FileID]
			if !ok || cur.Kind != "module" || cur.DeletedAt != 0 || have[cur.ModuleID] {
				continue
			}
			plans = append(plans, plan{id: r.FileID, del: []string{r.FileID}, rec: &store.SyncFile{FileID: r.FileID}})
		}
	}
	if len(plans) == 0 {
		return nil
	}
	err = e.st.Write(ctx, "sync.index", func(tx *store.Tx) error {
		for _, p := range plans {
			switch {
			case p.create:
				for _, old := range p.del {
					if err := tx.Delete("shared_files", store.ByID(old)); err != nil {
						return err
					}
				}
				if err := tx.Create("shared_files", p.row); err != nil {
					return err
				}
			case p.revive:
				u := store.Row{"deleted_at": nil}
				for k, v := range p.update {
					u[k] = v
				}
				if err := tx.Update("shared_files", store.ByID(p.id), u); err != nil {
					return err
				}
			case len(p.del) > 0:
				if err := tx.Delete("shared_files", store.ByID(p.id)); err != nil {
					return err
				}
			case len(p.update) > 0:
				if err := tx.Update("shared_files", store.ByID(p.id), p.update); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range plans {
		switch {
		case p.rec != nil && len(p.del) > 0 && !p.create:
			if err := e.st.DeleteSyncFile(ctx, p.id); err != nil {
				return err
			}
		case p.rec != nil:
			if err := e.st.PutSyncFile(ctx, *p.rec); err != nil {
				return err
			}
		}
	}
	return nil
}

// uploadAll puts every file this device holds that the folder does not have yet into the folder.
func (e *Engine) uploadAll(ctx context.Context, res *BlobResult) error {
	all, err := e.shared(ctx)
	if err != nil {
		return err
	}
	have, err := e.blobsInFolder(ctx)
	if err != nil {
		return err
	}
	books, err := e.localBooks(ctx)
	if err != nil {
		return err
	}
	noStore, err := e.noStoreItems(ctx)
	if err != nil {
		return err
	}
	type up struct{ name, sha, path string }
	var todo []up
	var total int64
	seen := map[string]bool{}
	for _, s := range liveSorted(all) {
		if _, ok := have[s.SHA]; ok || seen[s.SHA] {
			continue
		}
		var path string
		if s.Kind == "book" {
			if noStore[s.ItemID] {
				continue
			}
			path = books[s.SHA]
		} else if rec, ok, _ := e.st.SyncFile(ctx, s.ID); ok && rec.SHA256 == s.SHA && e.Local.ModulesDir != "" {
			path = e.modulePath(s.ModuleID)
		}
		if path == "" {
			continue
		}
		seen[s.SHA] = true
		todo = append(todo, up{s.Name, s.SHA, path})
		total += s.Size
	}
	if len(todo) == 0 {
		return nil
	}
	e.beginRun("upload", len(todo), total)
	defer e.endRun()
	for _, u := range todo {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.startFile(u.name)
		wrote, err := e.putBlob(ctx, u.sha, u.path, e.advance)
		switch {
		case errors.Is(err, errChanged) || errors.Is(err, os.ErrNotExist):
			// Edited or moved since it was listed: the next sync lists it again.
		case err != nil:
			if errors.Is(err, ErrFolderMissing) || errors.Is(err, context.Canceled) {
				return err
			}
			res.Failed = append(res.Failed, BlobFailure{Name: u.name, Error: plain(err)})
		case wrote:
			res.Uploaded++
		}
		e.finishFile()
	}
	return nil
}

func plain(err error) string {
	switch {
	case errors.Is(err, ErrCorrupt):
		return "its copy in the sync folder is damaged, incomplete or has been changed, so it was not used"
	case errors.Is(err, ErrMissingFile):
		return "it has not finished arriving in the sync folder yet"
	case errors.Is(err, ErrDiskFull):
		return "the disk is full"
	}
	return err.Error()
}

// presentBook tells whether this device has the file of a book.
func (e *Engine) presentBook(ctx context.Context, s shared) (bool, error) {
	var n int
	err := e.st.QueryRow(ctx, `SELECT COUNT(*) FROM files WHERE item_id = ? AND sha256 = ? AND deleted_at IS NULL`,
		s.ItemID, s.SHA).Scan(&n)
	return n > 0, err
}

func (e *Engine) presentModule(ctx context.Context, s shared) bool {
	if e.Local.ModulesDir == "" || !moduleIDRE.MatchString(s.ModuleID) {
		return true // nowhere to put it: not a thing to fetch
	}
	if _, err := os.Stat(e.modulePath(s.ModuleID)); err != nil {
		return false
	}
	rec, ok, _ := e.st.SyncFile(ctx, s.ID)
	return ok && rec.SHA256 == s.SHA
}

func (e *Engine) present(ctx context.Context, s shared) (bool, error) {
	if s.Kind == "module" {
		return e.presentModule(ctx, s), nil
	}
	return e.presentBook(ctx, s)
}

// downloadAll brings books (all, or none in on-demand mode) and modules from the folder.
func (e *Engine) downloadAll(ctx context.Context, res *BlobResult) error {
	all, err := e.shared(ctx)
	if err != nil {
		return err
	}
	mode := e.BooksMode(ctx)
	var todo []shared
	var total int64
	for _, s := range liveSorted(all) {
		ok, err := e.present(ctx, s)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if s.Kind == "book" && mode == BooksWhenOpened {
			res.OnDemand++
			continue
		}
		todo = append(todo, s)
		total += s.Size
	}
	if len(todo) == 0 {
		return nil
	}
	e.beginRun("download", len(todo), total)
	defer e.endRun()
	for _, s := range todo {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.startFile(s.Name)
		linked, err := e.fetchFile(ctx, s)
		switch {
		case errors.Is(err, ErrMissingFile):
			res.Waiting++
		case err != nil:
			if errors.Is(err, ErrFolderMissing) || errors.Is(err, context.Canceled) {
				return err
			}
			res.Failed = append(res.Failed, BlobFailure{Name: s.Name, Error: plain(err)})
		case linked:
			res.Linked++
		default:
			res.Downloaded++
		}
		e.finishFile()
	}
	return nil
}

// fetchFile puts one shared file on this device. A book whose hash is already here is copied, not
// downloaded. Nothing is put in place until the whole file has been checked; a failure leaves
// nothing behind.
func (e *Engine) fetchFile(ctx context.Context, s shared) (linked bool, err error) {
	if s.Kind == "module" {
		return false, e.fetchModule(ctx, s)
	}
	dir := e.Local.BooksDir
	if dir == "" {
		return false, errors.New("there is no folder to keep books in")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, wrapDisk(err)
	}
	var tmp string
	if books, err := e.localBooks(ctx); err == nil && books[s.SHA] != "" {
		if tmp, err = copyChecked(books[s.SHA], dir, s.SHA, e.advance); err == nil {
			linked = true
		} else {
			tmp = ""
		}
	}
	if tmp == "" {
		if tmp, err = e.getBlob(ctx, s.SHA, dir, e.advance); err != nil {
			return false, err
		}
	}
	name := uniqueName(dir, s.Name)
	final := filepath.Join(dir, name)
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return false, wrapDisk(err)
	}
	fid, err := library.AttachFile(ctx, e.st, s.ItemID, dir, name, s.Format, s.SHA, s.Size)
	if err != nil {
		os.Remove(final)
		return false, err
	}
	return linked, e.st.PutSyncFile(ctx, store.SyncFile{FileID: s.ID, SHA256: s.SHA, Size: s.Size, Origin: "synced", LocalFileID: fid})
}

func (e *Engine) fetchModule(ctx context.Context, s shared) error {
	if !moduleIDRE.MatchString(s.ModuleID) {
		return fmt.Errorf("%w: a module has a bad name", ErrCorrupt)
	}
	tmp, err := e.getBlob(ctx, s.SHA, e.Local.ModulesDir, e.advance)
	if err != nil {
		return err
	}
	if e.Local.ReleaseModule != nil {
		e.Local.ReleaseModule(s.ModuleID)
	}
	final := e.modulePath(s.ModuleID)
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("the module %s could not be replaced (is it in use?): %w", s.ModuleID, wrapDisk(err))
	}
	var mtime int64
	if st, err := os.Stat(final); err == nil {
		mtime = st.ModTime().UnixMilli()
	}
	return e.st.PutSyncFile(ctx, store.SyncFile{FileID: s.ID, SHA256: s.SHA, Size: s.Size, MTime: mtime, Origin: "synced"})
}

// copyChecked copies a local file into dir as a temporary file, checking that it is the file the
// hash names.
func copyChecked(src, dir, sha string, advance func(int64)) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".glossa-download-*")
	if err != nil {
		return "", wrapDisk(err)
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			if _, err := out.Write(buf[:n]); err != nil {
				out.Close()
				os.Remove(out.Name())
				return "", wrapDisk(err)
			}
			advance(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(out.Name())
			return "", rerr
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", wrapDisk(err)
	}
	if hex.EncodeToString(h.Sum(nil)) != sha {
		os.Remove(out.Name())
		return "", errChanged
	}
	return out.Name(), nil
}

// removeDeleted takes files off this device that were deleted on another: modules, and books sync
// itself put here. A file the person added themselves is never deleted by sync.
func (e *Engine) removeDeleted(ctx context.Context, res *BlobResult) error {
	all, err := e.shared(ctx)
	if err != nil {
		return err
	}
	recs, err := e.st.SyncFiles(ctx)
	if err != nil {
		return err
	}
	for _, r := range recs {
		s, ok := all[r.FileID]
		if !ok || s.live() {
			continue
		}
		if s.Kind == "module" {
			if moduleIDRE.MatchString(s.ModuleID) && e.Local.ModulesDir != "" {
				if e.Local.ReleaseModule != nil {
					e.Local.ReleaseModule(s.ModuleID)
				}
				if err := os.Remove(e.modulePath(s.ModuleID)); err != nil && !errors.Is(err, os.ErrNotExist) {
					continue // in use: try again next time
				}
				res.Removed++
			}
		} else if r.Origin == "synced" && r.LocalFileID != "" {
			var dir, rel string
			err := e.st.QueryRow(ctx, `SELECT l.path_on_volume, f.rel_path FROM files f JOIN library_folders l ON l.id = f.folder_id
				WHERE f.id = ? AND f.deleted_at IS NULL`, r.LocalFileID).Scan(&dir, &rel)
			if err == nil {
				if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err := e.st.Write(ctx, "sync.book.remove", func(tx *store.Tx) error {
					return tx.Delete("files", store.ByID(r.LocalFileID))
				}); err != nil {
					return err
				}
				res.Removed++
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if err := e.st.DeleteSyncFile(ctx, r.FileID); err != nil {
			return err
		}
	}
	return nil
}

// collectBlobs removes blobs no device uses any more: every file that names the blob is deleted (or
// its book is) and has been for longer than the grace period, which keeps the bin able to restore
// a book. A blob this library has never heard of is left alone: it may be another device's upload
// that has not been announced yet.
func (e *Engine) collectBlobs(ctx context.Context, res *BlobResult) error {
	all, err := e.shared(ctx)
	if err != nil {
		return err
	}
	have, err := e.blobsInFolder(ctx)
	if err != nil {
		return err
	}
	type use struct {
		live  bool
		since int64
	}
	uses := map[string]*use{}
	for _, s := range all {
		u := uses[s.SHA]
		if u == nil {
			u = &use{}
			uses[s.SHA] = u
		}
		if s.live() {
			u.live = true
		} else {
			u.since = max(u.since, s.deadSince())
		}
	}
	grace := e.BlobGrace
	if grace < 0 {
		grace = 0
	}
	now := e.Now().UnixMilli()
	shas := make([]string, 0, len(have))
	for sha := range have {
		shas = append(shas, sha)
	}
	sort.Strings(shas)
	for _, sha := range shas {
		u := uses[sha]
		if u == nil || u.live || now-u.since < grace.Milliseconds() {
			continue
		}
		if err := e.removeBlob(ctx, sha); err != nil {
			return err
		}
		res.Collected++
	}
	return nil
}

// Stats counts the shared books and modules, and how many are on this device.
func (e *Engine) Stats(ctx context.Context) (FileStats, error) {
	st := FileStats{Mode: e.BooksMode(ctx)}
	all, err := e.shared(ctx)
	if err != nil {
		return st, err
	}
	for _, s := range liveSorted(all) {
		st.Files++
		st.Bytes += s.Size
		ok, err := e.present(ctx, s)
		if err != nil {
			return st, err
		}
		if ok {
			st.OnDevice++
		} else {
			st.RemoteOnly++
			st.RemoteSize += s.Size
		}
	}
	return st, nil
}

// Fetched says what opening a book had to do.
type Fetched struct {
	Downloaded int  `json:"downloaded" desc:"Files copied from the sync folder"`
	Linked     int  `json:"linked" desc:"Files already on this device under another book, copied without downloading"`
	OnDevice   bool `json:"on_device" desc:"The book was already here"`
}

// ErrNotShared is returned for a book that no device has put in the sync folder.
var ErrNotShared = errors.New("this book is not in the sync folder")

// EnsureBook makes sure the files of a library book are on this device, downloading them if the
// book came from another device and has not been opened here yet. Call it when a book is opened.
func (e *Engine) EnsureBook(ctx context.Context, itemID string) (Fetched, error) {
	e.fmu.Lock()
	defer e.fmu.Unlock()
	var f Fetched
	all, err := e.shared(ctx)
	if err != nil {
		return f, err
	}
	var todo []shared
	var total int64
	rows := 0
	for _, s := range liveSorted(all) {
		if s.Kind != "book" || s.ItemID != itemID {
			continue
		}
		rows++
		ok, err := e.present(ctx, s)
		if err != nil {
			return f, err
		}
		if !ok {
			todo = append(todo, s)
			total += s.Size
		}
	}
	if rows == 0 {
		var n int
		if err := e.st.QueryRow(ctx, `SELECT COUNT(*) FROM files WHERE item_id = ? AND deleted_at IS NULL`, itemID).Scan(&n); err != nil {
			return f, err
		}
		if n > 0 {
			f.OnDevice = true
			return f, nil
		}
		return f, ErrNotShared
	}
	if len(todo) == 0 {
		f.OnDevice = true
		return f, nil
	}
	e.beginRun("download", len(todo), total)
	defer e.endRun()
	var firstErr error
	for _, s := range todo {
		e.startFile(s.Name)
		linked, err := e.fetchFile(ctx, s)
		e.finishFile()
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
		case linked:
			f.Linked++
		default:
			f.Downloaded++
		}
	}
	if f.Downloaded+f.Linked > 0 {
		return f, nil // some of its files are here; the rest will come with the next sync
	}
	return f, firstErr
}

// FetchAll downloads every book and module now, whatever the mode.
func (e *Engine) FetchAll(ctx context.Context) (BlobResult, error) {
	e.fmu.Lock()
	defer e.fmu.Unlock()
	var res BlobResult
	all, err := e.shared(ctx)
	if err != nil {
		return res, err
	}
	var todo []shared
	var total int64
	for _, s := range liveSorted(all) {
		if ok, err := e.present(ctx, s); err != nil {
			return res, err
		} else if !ok {
			todo = append(todo, s)
			total += s.Size
		}
	}
	e.beginRun("download", len(todo), total)
	defer e.endRun()
	for _, s := range todo {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		e.startFile(s.Name)
		linked, err := e.fetchFile(ctx, s)
		e.finishFile()
		switch {
		case errors.Is(err, ErrMissingFile):
			res.Waiting++
		case err != nil:
			res.Failed = append(res.Failed, BlobFailure{Name: s.Name, Error: plain(err)})
		case linked:
			res.Linked++
		default:
			res.Downloaded++
		}
	}
	return res, nil
}

// receiveFiles is the download half of a sync: it takes off this device what was deleted elsewhere,
// then downloads what is new (modules always, books according to the setting).
func (e *Engine) receiveFiles(ctx context.Context, res *BlobResult) error {
	e.fmu.Lock()
	defer e.fmu.Unlock()
	if err := e.removeDeleted(ctx, res); err != nil {
		return err
	}
	return e.downloadAll(ctx, res)
}
