// Package sync keeps a Glossa library in step across devices through a folder the user already
// syncs (OneDrive, Dropbox, Google Drive, iCloud, Syncthing). See ADR 0013.
//
// Each device writes only its own encrypted, append-only change files into
// <folder>/Glossa Sync/<device-id>/ and reads everyone else's, so the user's sync service never sees
// two devices write the same file. Merging is last-writer-wins per field by hybrid logical clock,
// with tombstones for deletes, purges that stay purges, and both versions kept when a text document
// was edited on two devices. Glossa itself sends nothing over the network.
package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/store"
)

const (
	keyName        = "key.json"
	forgottenDir   = "forgotten"
	maxClockAhead  = 24 * time.Hour
	stateExported  = "exported_hlc"
	stateAppliedP  = "applied:"
	stateLastSync  = "last_sync"
	defaultChunk   = 5000
	defaultCompact = 50
)

// textFields are the text fields whose edits on two devices are both kept (ADR 0013).
var textFields = map[string][]string{
	"notes":     {"body_md", "title"},
	"documents": {"body_md", "title"},
}

var (
	deviceRE = regexp.MustCompile(`^[0-9A-Z]{26}$`)
	changeRE = regexp.MustCompile(`^(\d{8})\.chg$`)
	snapRE   = regexp.MustCompile(`^snap-(\d{8})\.snap$`)
)

func changeName(seq uint64) string { return fmt.Sprintf("%08d.chg", seq) }
func snapName(seq uint64) string   { return fmt.Sprintf("snap-%08d.snap", seq) }

// fileBody is the plain text inside a change file or snapshot, before it is compressed and encrypted.
type fileBody struct {
	Format     int               `json:"format"`
	Kind       string            `json:"kind"` // "change" or "snapshot"
	Device     string            `json:"device"`
	DeviceName string            `json:"device_name"`
	Seq        uint64            `json:"seq"` // a snapshot's seq is the last change file of its device it covers
	Schema     int               `json:"schema"`
	Created    int64             `json:"created"`
	Devices    map[string]string `json:"devices,omitempty"` // snapshots: names of the devices in it
	Changes    []store.Change    `json:"changes"`
}

// Engine syncs one library with one folder. Make it with Open after Initialize, or on a folder
// another device has set up.
type Engine struct {
	st  *store.Store
	t   Transport
	key []byte
	mu  gosync.Mutex

	// Now is the wall clock. Tests replace it.
	Now func() time.Time
	// ChunkSize is how many changes go in one file.
	ChunkSize int
	// CompactAfter is how many change files of this device pile up before a snapshot is made and the
	// old files removed.
	CompactAfter int

	// Local says where books and modules from other devices are kept. Without it they are not downloaded.
	Local Locations
	// BlobChunk is the size of one piece of a large file in the folder (default 4 MiB); memory use
	// while sending or receiving is about one piece.
	BlobChunk int
	// BlobGrace is how long a deleted book stays in the folder before it is removed, so the bin can
	// still restore it (default 30 days).
	BlobGrace time.Duration
	// OnProgress, if set, is called as bytes of books and modules move. It must not block.
	OnProgress func(BlobProgress)

	fmu  gosync.Mutex // one download or removal of files at a time
	pmu  gosync.Mutex
	prog BlobProgress
}

// Initialize sets up an empty folder for sync: it makes the key file with a random salt, and
// refuses a folder that already has one. The password is not stored anywhere.
func Initialize(ctx context.Context, t Transport, password string, p KDFParams) error {
	if err := CheckPassword(password); err != nil {
		return err
	}
	if err := p.valid(); err != nil {
		return err
	}
	if _, err := t.Read(ctx, keyName); err == nil {
		return ErrAlreadyExists
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}
	d, err := newKeyDoc(password, p, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(d)
	if err := t.Create(ctx, keyName, raw); err != nil {
		if errors.Is(err, ErrExists) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

// IsInitialised tells whether the folder already holds sync data.
func IsInitialised(ctx context.Context, t Transport) (bool, error) {
	_, err := t.Read(ctx, keyName)
	if errors.Is(err, ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Open derives the key from the password and checks it against the folder. A wrong password is
// ErrWrongPassword; nothing is read or written until the password is right.
func Open(ctx context.Context, st *store.Store, t Transport, password string) (*Engine, error) {
	raw, err := t.Read(ctx, keyName)
	if errors.Is(err, ErrNotExist) {
		return nil, ErrNotInitialised
	}
	if err != nil {
		return nil, err
	}
	key, err := keyFrom(raw, password)
	if err != nil {
		return nil, err
	}
	return &Engine{st: st, t: t, key: key, Now: time.Now, ChunkSize: defaultChunk, CompactAfter: defaultCompact,
		BlobChunk: defaultBlobChunk, BlobGrace: 30 * 24 * time.Hour}, nil
}

// Summary counts what a merge did, or would do, on this device.
type Summary struct {
	Changes   int `json:"changes" desc:"Change records that were new to this device"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Purged    int `json:"purged"`
	Conflicts int `json:"conflicts" desc:"Text documents edited on two devices: the older edit was kept in the history"`
}

// Result is what a sync did.
type Result struct {
	Pulled    Summary    `json:"pulled"`
	Pushed    int        `json:"pushed" desc:"Changes from this device written to the folder"`
	Files     int        `json:"files" desc:"Change files written"`
	Compacted bool       `json:"compacted"`
	Blobs     BlobResult `json:"blobs" desc:"Books, Bibles and modules: what moved between this device and the folder"`
	At        int64      `json:"at"`
}

// Preview says what the first (or next) sync would do, without doing any of it.
type Preview struct {
	FolderHasData bool           `json:"folder_has_data"`
	Devices       []string       `json:"devices" desc:"Names of the other devices that have written to the folder"`
	ThisDevice    map[string]int `json:"this_device" desc:"Objects on this device now, by kind"`
	InFolder      map[string]int `json:"in_folder" desc:"Objects the folder holds, by kind"`
	AfterMerge    map[string]int `json:"after_merge" desc:"Objects on this device after merging"`
	ToThisDevice  Summary        `json:"to_this_device"`
	ToFolder      int            `json:"to_folder" desc:"Changes from this device the folder does not have yet"`
	NewFolder     bool           `json:"new_folder,omitempty" desc:"The folder has no sync data yet: syncing starts it, with this device's data"`
}

type unit struct {
	device   string
	seq      uint64 // a change file's number, or a snapshot's last covered number
	snapshot bool
}

func (u unit) name() string {
	if u.snapshot {
		return u.device + "/" + snapName(u.seq)
	}
	return u.device + "/" + changeName(u.seq)
}

type fetched struct {
	unit
	body fileBody
}

type devFiles struct {
	changes map[uint64]bool
	snaps   []uint64
	maxSeq  uint64
	bytes   int64
}

func (e *Engine) listDevice(ctx context.Context, dev string) (devFiles, error) {
	es, err := e.t.List(ctx, dev)
	if err != nil {
		return devFiles{}, err
	}
	d := devFiles{changes: map[uint64]bool{}}
	for _, en := range es {
		if en.Dir {
			continue
		}
		var n uint64
		if m := changeRE.FindStringSubmatch(en.Name); m != nil {
			n, _ = strconv.ParseUint(m[1], 10, 64)
			d.changes[n] = true
			d.maxSeq = max(d.maxSeq, n)
		} else if m := snapRE.FindStringSubmatch(en.Name); m != nil {
			n, _ = strconv.ParseUint(m[1], 10, 64)
			d.snaps = append(d.snaps, n)
		} else {
			continue
		}
		d.bytes += en.Size
	}
	sort.Slice(d.snaps, func(i, j int) bool { return d.snaps[i] < d.snaps[j] })
	return d, nil
}

func (e *Engine) forgotten(ctx context.Context) (map[string]bool, error) {
	es, err := e.t.List(ctx, forgottenDir)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, en := range es {
		out[en.Name] = true
	}
	return out, nil
}

func (e *Engine) devices(ctx context.Context) ([]string, error) {
	es, err := e.t.List(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, en := range es {
		if en.Dir && deviceRE.MatchString(en.Name) {
			out = append(out, en.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (e *Engine) cursor(ctx context.Context, dev string) (uint64, error) {
	v, ok, err := e.st.SyncState(ctx, stateAppliedP+dev)
	if err != nil || !ok {
		return 0, err
	}
	var n uint64
	fmt.Sscan(v, &n)
	return n, nil
}

// plan lists, in order, the files this device has not applied yet. A device that is new to
// another's folder starts from its newest snapshot; one that is behind a compaction jumps to it.
func (e *Engine) plan(ctx context.Context) ([]unit, error) {
	forgotten, err := e.forgotten(ctx)
	if err != nil {
		return nil, err
	}
	if forgotten[e.st.Device()] {
		return nil, ErrForgotten
	}
	devs, err := e.devices(ctx)
	if err != nil {
		return nil, err
	}
	var out []unit
	for _, dev := range devs {
		if forgotten[dev] {
			continue
		}
		files, err := e.listDevice(ctx, dev)
		if err != nil {
			return nil, err
		}
		cur, err := e.cursor(ctx, dev)
		if err != nil {
			return nil, err
		}
		covering := func(next uint64) (uint64, bool) {
			for i := len(files.snaps) - 1; i >= 0; i-- {
				if files.snaps[i] >= next {
					return files.snaps[i], true
				}
			}
			return 0, false
		}
		next := cur + 1
		if cur == 0 && len(files.snaps) > 0 {
			top := files.snaps[len(files.snaps)-1]
			out = append(out, unit{dev, top, true})
			next = top + 1
		}
		for {
			if files.changes[next] {
				out = append(out, unit{dev, next, false})
				next++
				continue
			}
			if through, ok := covering(next); ok {
				out = append(out, unit{dev, through, true})
				next = through + 1
				continue
			}
			if files.maxSeq >= next {
				return nil, fmt.Errorf("%w: %s/%s", ErrMissingFile, dev, changeName(next))
			}
			break
		}
	}
	return out, nil
}

func (e *Engine) fetch(ctx context.Context, units []unit) ([]fetched, error) {
	schema, err := e.st.SchemaVersion()
	if err != nil {
		return nil, err
	}
	var out []fetched
	for _, u := range units {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := e.t.Read(ctx, u.name())
		if errors.Is(err, ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrMissingFile, u.name())
		}
		if err != nil {
			return nil, err
		}
		kind := byte(kindChange)
		if u.snapshot {
			kind = kindSnapshot
		}
		plain, err := open(e.key, raw, kind, u.device, u.seq)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", err, u.name())
		}
		var b fileBody
		if err := json.Unmarshal(plain, &b); err != nil || b.Device != u.device || b.Seq != u.seq {
			return nil, fmt.Errorf("%w: %s", ErrCorrupt, u.name())
		}
		if b.Format > formatVersion || b.Schema > schema {
			return nil, fmt.Errorf("%w: %s", ErrNewerVersion, u.name())
		}
		out = append(out, fetched{u, b})
	}
	return out, nil
}

type pullInfo struct {
	sum    Summary
	counts map[string]int // after merge, when asked for
	inFold map[string]int
	names  map[string]string // other devices in the files, by ID
}

// pull applies every new file in one transaction, or, with dryRun, only works out what that would do.
func (e *Engine) pull(ctx context.Context, dryRun bool) (pullInfo, error) {
	info := pullInfo{names: map[string]string{}}
	units, err := e.plan(ctx)
	if err != nil {
		return info, err
	}
	files, err := e.fetch(ctx, units)
	if err != nil {
		return info, err
	}
	self := e.st.Device()
	now := e.Now().UnixMilli()
	var maxHLC, maxOwn string
	created, gone := map[string]bool{}, map[string]bool{}
	err = e.st.ApplyRemote(ctx, dryRun, func(r *store.Remote) error {
		for _, f := range files {
			info.names[f.device] = orID(f.body.DeviceName, f.device)
			if err := r.EnsureDevice(f.device, orID(f.body.DeviceName, f.device)); err != nil {
				return err
			}
			for id, name := range f.body.Devices {
				if err := r.EnsureDevice(id, orID(name, id)); err != nil {
					return err
				}
			}
			for _, c := range f.body.Changes {
				if !f.snapshot && c.Device != f.device {
					return fmt.Errorf("%w: %s", ErrCorrupt, f.name())
				}
				if ms, ok := hlcMillis(c.HLC); !ok {
					return fmt.Errorf("%w: %s", ErrCorrupt, f.name())
				} else if ms > now+maxClockAhead.Milliseconds() {
					return fmt.Errorf("%w (%s)", ErrClockSkew, orID(f.body.DeviceName, f.device))
				}
				if err := r.EnsureDevice(c.Device, c.Device); err != nil {
					return err
				}
				if store.PerDevice(c.ObjectType, c.ObjectID) {
					continue // another device's paths and settings are not ours to apply
				}
				key := c.ObjectType + "\x00" + c.ObjectID
				switch {
				case c.Op == "create":
					created[key] = true
				case c.Op == "delete":
					gone[key] = true
				}
				ins, err := r.Insert(c)
				if err != nil {
					return fmt.Errorf("%w: %s: %v", ErrCorrupt, f.name(), err)
				}
				if ins {
					info.sum.Changes++
				}
				maxHLC = maxStr(maxHLC, c.HLC)
				if c.Device == self {
					maxOwn = maxStr(maxOwn, c.HLC)
				}
			}
		}
		merged, err := r.Materialize(textFields)
		if err != nil {
			return err
		}
		for _, m := range merged {
			switch m.Outcome {
			case store.Created:
				info.sum.Created++
			case store.Updated:
				info.sum.Updated++
			case store.Deleted:
				info.sum.Deleted++
			case store.Purged:
				info.sum.Purged++
			}
			for _, c := range m.Conflicts {
				if c.New {
					info.sum.Conflicts++
				}
			}
		}
		if dryRun {
			if info.counts, err = r.Counts(); err != nil {
				return err
			}
			return nil
		}
		last := map[string]uint64{}
		for _, f := range files {
			last[f.device] = max(last[f.device], f.seq)
		}
		for dev, seq := range last {
			if err := r.SetState(stateAppliedP+dev, fmt.Sprint(seq)); err != nil {
				return err
			}
		}
		if maxOwn != "" {
			old, _, err := r.State(stateExported)
			if err != nil {
				return err
			}
			if maxOwn > old {
				return r.SetState(stateExported, maxOwn)
			}
		}
		return nil
	})
	if err != nil {
		return info, err
	}
	info.inFold = map[string]int{}
	for k := range created {
		if !gone[k] {
			info.inFold[k[:indexNUL(k)]]++
		}
	}
	if !dryRun && maxHLC != "" {
		_ = e.st.ObserveClock(maxHLC)
	}
	return info, nil
}

// Preview works out what the next sync would do on each side and changes nothing: not the library,
// not the folder. Use it before the first sync on a device that already has data.
func (e *Engine) Preview(ctx context.Context) (Preview, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var p Preview
	devs, err := e.devices(ctx)
	if err != nil {
		return p, err
	}
	if p.ThisDevice, err = e.st.CountObjects(ctx); err != nil {
		return p, err
	}
	info, err := e.pull(ctx, true)
	if err != nil {
		return p, err
	}
	p.ToThisDevice, p.AfterMerge, p.InFolder = info.sum, info.counts, info.inFold
	for _, d := range devs {
		if f, err := e.listDevice(ctx, d); d != e.st.Device() && err == nil && len(f.changes)+len(f.snaps) > 0 {
			p.FolderHasData = true
			p.Devices = append(p.Devices, orID(info.names[d], d))
		}
	}
	exported, _, err := e.st.SyncState(ctx, stateExported)
	if err != nil {
		return p, err
	}
	p.ToFolder, err = e.st.CountLocalChanges(ctx, exported)
	return p, err
}

// Sync brings this device and the folder level: it applies what other devices wrote, then writes
// this device's new changes. If any file cannot be read, nothing is applied or written.
func (e *Engine) Sync(ctx context.Context) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var res Result
	pulled, err := e.pull(ctx, false)
	if err != nil {
		return res, err
	}
	res.Pulled = pulled.sum
	if err := e.join(ctx); err != nil {
		return res, err
	}
	if err := e.indexLocal(ctx, &res.Blobs); err != nil {
		return res, err
	}
	if err := e.uploadAll(ctx, &res.Blobs); err != nil {
		return res, err
	}
	pushed, files, purge, err := e.push(ctx)
	res.Pushed, res.Files = pushed, files
	if err != nil {
		return res, err
	}
	if err := e.receiveFiles(ctx, &res.Blobs); err != nil {
		return res, err
	}
	if purge || pulled.sum.Purged > 0 || e.ownFileCount(ctx) > e.CompactAfter {
		if err := e.compact(ctx); err != nil {
			return res, err
		}
		res.Compacted = true
	}
	if err := e.collectBlobs(ctx, &res.Blobs); err != nil {
		return res, err
	}
	res.At = e.Now().UnixMilli()
	err = e.st.SetSyncState(ctx, stateLastSync, fmt.Sprint(res.At))
	return res, err
}

func (e *Engine) ownFileCount(ctx context.Context) int {
	f, err := e.listDevice(ctx, e.st.Device())
	if err != nil {
		return 0
	}
	return len(f.changes)
}

// push writes this device's changes that are not in the folder yet.
func (e *Engine) push(ctx context.Context) (changes, files int, purge bool, err error) {
	self := e.st.Device()
	own, err := e.listDevice(ctx, self)
	if err != nil {
		return 0, 0, false, err
	}
	exported, _, err := e.st.SyncState(ctx, stateExported)
	if err != nil {
		return 0, 0, false, err
	}
	top := own.maxSeq
	if n := len(own.snaps); n > 0 {
		top = max(top, own.snaps[n-1])
	}
	if cur, err := e.cursor(ctx, self); err != nil {
		return 0, 0, false, err
	} else if cur > top {
		// The folder lost files this device had written (a restored folder): write them all again.
		exported = ""
		if err := e.st.SetSyncState(ctx, stateAppliedP+self, fmt.Sprint(top)); err != nil {
			return 0, 0, false, err
		}
	}
	name, _ := e.st.DeviceName(ctx, self)
	schema, err := e.st.SchemaVersion()
	if err != nil {
		return 0, 0, false, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return changes, files, purge, err
		}
		cs, err := e.st.LocalChanges(ctx, exported, e.ChunkSize)
		if err != nil {
			return changes, files, purge, err
		}
		if len(cs) == 0 {
			return changes, files, purge, nil
		}
		top++
		body := fileBody{Format: formatVersion, Kind: "change", Device: self, DeviceName: name, Seq: top,
			Schema: schema, Created: e.Now().UnixMilli(), Changes: cs}
		if err := e.writeFile(ctx, kindChange, body, changeName(top)); err != nil {
			return changes, files, purge, err
		}
		exported = cs[len(cs)-1].HLC
		if err := e.st.SetSyncState(ctx, stateExported, exported); err != nil {
			return changes, files, purge, err
		}
		if err := e.st.SetSyncState(ctx, stateAppliedP+self, fmt.Sprint(top)); err != nil {
			return changes, files, purge, err
		}
		changes += len(cs)
		files++
		for _, c := range cs {
			purge = purge || (c.Op == "delete" && c.Field == "purge")
		}
	}
}

func (e *Engine) writeFile(ctx context.Context, kind byte, body fileBody, name string) error {
	plain, err := json.Marshal(body)
	if err != nil {
		return err
	}
	data, err := seal(e.key, kind, body.Device, body.Seq, plain)
	if err != nil {
		return err
	}
	return e.t.Create(ctx, body.Device+"/"+name, data)
}

// joinedName is the file a device writes when it joins, before it has any change to send.
const joinedName = "joined"

// joinedTmpName is where a replacement marker is written before it is renamed over the old one.
const joinedTmpName = "joined.new"

// Join leaves a small encrypted marker in this device's folder, so the other devices list it (and can
// forget it) as soon as it has joined, not only after its first change file. Doing it again is harmless,
// and a device that was forgotten is not brought back.
func (e *Engine) Join(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.join(ctx)
}

func (e *Engine) join(ctx context.Context) error {
	self := e.st.Device()
	forgotten, err := e.forgotten(ctx)
	if err != nil || forgotten[self] {
		return err
	}
	e.cleanMarkerTemps(ctx, self)
	name, _ := e.st.DeviceName(ctx, self)
	had, ok := e.readJoined(ctx, self)
	if ok && had == name {
		return nil // already there under the current name: nothing to write
	}
	schema, err := e.st.SchemaVersion()
	if err != nil {
		return err
	}
	body := fileBody{Format: formatVersion, Kind: "join", Device: self, DeviceName: name, Schema: schema,
		Created: e.Now().UnixMilli(), Changes: []store.Change{}}
	if !ok {
		if err := e.writeFile(ctx, kindJoin, body, joinedName); err != nil && !errors.Is(err, ErrExists) {
			return err
		}
		return nil
	}
	// Renamed since it joined: the new marker is written under a temporary name and renamed over the old
	// one, so another device listing the folder always finds a marker.
	if err := e.writeFile(ctx, kindJoin, body, joinedTmpName); err != nil {
		return err
	}
	if err := e.t.Replace(ctx, self+"/"+joinedTmpName, self+"/"+joinedName); err != nil {
		_ = e.t.Remove(ctx, self+"/"+joinedTmpName)
		return err
	}
	return nil
}

// cleanMarkerTemps removes temporary files a crashed write left in this device's folder. Listings
// ignore them anyway.
func (e *Engine) cleanMarkerTemps(ctx context.Context, self string) {
	es, err := e.t.List(ctx, self)
	if err != nil {
		return
	}
	for _, en := range es {
		if !en.Dir && (en.Name == joinedTmpName || strings.HasPrefix(en.Name, ".tmp-")) {
			_ = e.t.Remove(ctx, self+"/"+en.Name)
		}
	}
}

// joinedAs reads the name a device gave when it joined; empty if it left no marker or it cannot be read.
func (e *Engine) joinedAs(ctx context.Context, dev string) string {
	name, _ := e.readJoined(ctx, dev)
	return name
}

// readJoined reads a device's marker: its name, and whether a readable marker exists.
func (e *Engine) readJoined(ctx context.Context, dev string) (string, bool) {
	raw, err := e.t.Read(ctx, dev+"/"+joinedName)
	if err != nil {
		return "", false
	}
	plain, err := open(e.key, raw, kindJoin, dev, 0)
	if err != nil {
		return "", false
	}
	var b fileBody
	if json.Unmarshal(plain, &b) != nil || b.Device != dev {
		return "", false
	}
	return b.DeviceName, true
}

// Compact writes a snapshot of everything this device knows and removes this device's older change
// files and snapshots. Other devices that have not read those files yet start from the snapshot.
// Values erased by a purge are gone from the snapshot, so compacting also clears them from the folder.
func (e *Engine) Compact(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.pull(ctx, false); err != nil {
		return err
	}
	if _, _, _, err := e.push(ctx); err != nil {
		return err
	}
	return e.compact(ctx)
}

func (e *Engine) compact(ctx context.Context) error {
	self := e.st.Device()
	own, err := e.listDevice(ctx, self)
	if err != nil {
		return err
	}
	through := own.maxSeq
	if through == 0 {
		return nil
	}
	cs, err := e.st.SnapshotChanges(ctx)
	if err != nil {
		return err
	}
	names, err := e.st.Devices(ctx)
	if err != nil {
		return err
	}
	schema, err := e.st.SchemaVersion()
	if err != nil {
		return err
	}
	body := fileBody{Format: formatVersion, Kind: "snapshot", Device: self, DeviceName: names[self], Seq: through,
		Schema: schema, Created: e.Now().UnixMilli(), Devices: names, Changes: cs}
	if err := e.t.Remove(ctx, self+"/"+snapName(through)); err != nil {
		return err
	}
	if err := e.writeFile(ctx, kindSnapshot, body, snapName(through)); err != nil {
		return err
	}
	for seq := range own.changes {
		if seq <= through {
			if err := e.t.Remove(ctx, self+"/"+changeName(seq)); err != nil {
				return err
			}
		}
	}
	for _, s := range own.snaps {
		if s < through {
			if err := e.t.Remove(ctx, self+"/"+snapName(s)); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeviceInfo describes a device that has written to the folder.
type DeviceInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Self  bool   `json:"self"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// Usage lists the devices seen in the folder and how much space the folder uses.
type Usage struct {
	Devices   []DeviceInfo `json:"devices"`
	Bytes     int64        `json:"bytes" desc:"Everything in the Glossa Sync folder"`
	BlobBytes int64        `json:"blob_bytes" desc:"Of which books and modules"`
}

// Devices lists the devices that have written to the folder, and the space used.
func (e *Engine) Devices(ctx context.Context) (Usage, error) {
	var u Usage
	forgotten, err := e.forgotten(ctx)
	if err != nil {
		return u, err
	}
	devs, err := e.devices(ctx)
	if err != nil {
		return u, err
	}
	names, _ := e.st.Devices(ctx)
	for _, d := range devs {
		if forgotten[d] {
			continue
		}
		f, err := e.listDevice(ctx, d)
		if err != nil {
			return u, err
		}
		name := names[d]
		if name == "" {
			name = e.joinedAs(ctx, d)
		}
		u.Devices = append(u.Devices, DeviceInfo{ID: d, Name: orID(name, d), Self: d == e.st.Device(),
			Files: len(f.changes) + len(f.snaps), Bytes: f.bytes})
		u.Bytes += f.bytes
	}
	top, err := e.t.List(ctx, "")
	if err != nil {
		return u, err
	}
	for _, en := range top {
		if !en.Dir {
			u.Bytes += en.Size
		}
		if en.Dir && en.Name == "blobs" {
			sub, _ := e.t.List(ctx, "blobs")
			for _, s := range sub {
				u.Bytes += s.Size
				u.BlobBytes += s.Size
			}
		}
	}
	return u, nil
}

// Forget removes another device from the folder: its files are deleted and a marker tells every
// device to ignore it. If that device syncs again it is told to set sync up afresh. Changes it
// already sent stay in every library.
func (e *Engine) Forget(ctx context.Context, device string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if device == e.st.Device() {
		return errors.New("this device cannot forget itself; turn sync off instead")
	}
	if !deviceRE.MatchString(device) {
		return fmt.Errorf("%q is not a device ID", device)
	}
	if err := e.t.Create(ctx, forgottenDir+"/"+device, []byte("forgotten")); err != nil && !errors.Is(err, ErrExists) {
		return err
	}
	return e.t.RemoveAll(ctx, device)
}

func orID(name, id string) string {
	if name == "" {
		return id
	}
	return name
}

func maxStr(a, b string) string {
	if b > a {
		return b
	}
	return a
}

func indexNUL(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return i
		}
	}
	return len(s)
}

// hlcMillis reads the wall-clock part of a hybrid clock value (13 digits, a counter, the device).
func hlcMillis(hlc string) (int64, bool) {
	if len(hlc) < 20 || hlc[13] != '.' {
		return 0, false
	}
	var ms int64
	for _, c := range hlc[:13] {
		if c < '0' || c > '9' {
			return 0, false
		}
		ms = ms*10 + int64(c-'0')
	}
	return ms, true
}
