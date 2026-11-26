package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	gosync "sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

const (
	stateFolder  = "folder"
	secretName   = "sync.password"
	rememberFlag = "remember"
	autoOff      = "auto_off"
)

// AutoEvery is how often the app syncs by itself while it is open.
const AutoEvery = 5 * time.Minute

type emitKey struct{}

// WithEmitter gives the sync commands a way to send events (the desktop app's Wails events), such
// as "sync:progress" while books move. Without one, nothing is sent.
func WithEmitter(ctx context.Context, emit func(name string, data any)) context.Context {
	return context.WithValue(ctx, emitKey{}, emit)
}

// setupKDF is the cost sync.setup uses for a new folder; tests lower it.
var setupKDF = DefaultKDF

// Register adds the sync commands. The password is held in memory for the session, and in the
// operating system keychain only when the person asks to remember it; it is never written to
// library.db, a file or a log, and never appears in an error.
func Register(r *command.Registry, keys secrets.Store, where ...Locations) {
	m := &manager{keys: keys}
	if len(where) > 0 {
		m.loc = where[0]
	}
	r.Add(
		command.Define("sync.password.strength", "Rate a sync password: whether it is long enough, and a hint for choosing a better one. Sends nothing anywhere.", false, passwordStrength),
		command.Define("sync.setup", "Choose the folder to sync through and its password. Makes a new sync folder, or joins one another device made. Does not merge anything yet: run sync.preview, then sync.now.", false, m.setup),
		command.Define("sync.unlock", "Enter the sync password again after starting Glossa, so syncing can continue.", false, m.unlock),
		command.Define("sync.preview", "Show what the next sync would do on each side, without changing this library or the folder. Use it before the first sync on a device that already has data.", false, m.preview),
		command.Define("sync.now", "Sync with the folder: read what other devices wrote, then write this device's changes. If any file is damaged or missing, nothing is applied.", true, m.now),
		command.Define("sync.status", "Show whether sync is set up, the folder, the last sync, changes waiting to be sent and the devices seen.", false, m.status),
		command.Define("sync.devices", "List the devices that have written to the sync folder, and the space it uses.", false, m.devices),
		command.Define("sync.forget_device", "Remove another device from the sync folder. Its files are deleted and it must be set up again to rejoin. Changes it already sent stay.", false, m.forget),
		command.Define("sync.compact", "Write a snapshot and remove this device's old change files, so new devices start quickly and deleted-for-good text leaves the folder.", false, m.compact),
		command.Define("sync.conflicts", "List text edits (notes, sermons) that were replaced by a newer edit on another device. The replaced text is kept in the history.", false, m.conflicts),
		command.Define("sync.conflicts.dismiss", "Mark a replaced edit as dealt with. The text stays in the history.", false, m.dismiss),
		command.Define("sync.books", "Show, and with mode change, whether this device downloads every book at sync time (all) or only when a book is opened (opened). Also counts the books and modules shared and how many are on this device.", false, m.books),
		command.Define("sync.fetch", "Download a book that came from another device and is not on this one yet (what opening it does), or with all every book and module not here yet. Reports progress through sync.progress.", false, m.fetch),
		command.Define("sync.progress", "Show how far the current book or module transfer has got: bytes and files done and in total.", false, m.progress),
		command.Define("sync.automatic", "Turn automatic syncing (at start, every few minutes and at quit) on or off. Sync now still works when it is off.", false, m.automatic),
		command.Define("sync.auto", "Sync if sync is set up, switched on, unlocked and the first sync was confirmed. The app calls this at start, every few minutes and at quit; it never fails, it says why it did not run.", true, m.auto),
		command.Define("sync.stop", "Turn sync off on this device. The folder and every library are left as they are; the saved password is forgotten.", false, m.stop),
	)
	r.OnClose(m.close)
}

type manager struct {
	keys secrets.Store
	mu   gosync.Mutex
	eng  *Engine
	st   *store.Store // the library eng works on
	fold string       // its folder
	loc  Locations    // where books and modules from other devices go
	emit func(string, any)
	// lastErr is why the last sync failed, until one succeeds.
	lastErr, lastCode string
	syncing           bool
}

func (m *manager) close() {
	m.mu.Lock()
	m.eng, m.st = nil, nil
	m.mu.Unlock()
}

func needStore(env *command.Env) error {
	if env.Store == nil {
		return command.Errorf("unavailable", "no library is open")
	}
	return nil
}

// fail turns a sync error into a command error with a code the interface can act on.
func fail(err error) error {
	if err == nil {
		return nil
	}
	var ce *command.Error
	if errors.As(err, &ce) {
		return err
	}
	code := "failed"
	for target, c := range map[error]string{
		ErrWrongPassword: "wrong_password", ErrFolderMissing: "folder_missing", ErrLocked: "locked",
		ErrCorrupt: "corrupt", ErrMissingFile: "missing_file", ErrNewerVersion: "newer_version",
		ErrClockSkew: "clock_skew", ErrDiskFull: "disk_full", ErrForgotten: "forgotten",
		ErrWeakPassword: "weak_password", ErrNotInitialised: "not_initialised", ErrAlreadyExists: "already_exists",
		context.Canceled: "cancelled",
	} {
		if errors.Is(err, target) {
			code = c
			break
		}
	}
	return command.Errorf(code, "%s", err.Error())
}

// engine returns the unlocked engine for the configured folder, opening it with the password given,
// or the one in memory, or the one saved in the keychain.
func (m *manager) engine(ctx context.Context, env *command.Env, folder, pw string) (*Engine, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.takeEmitter(ctx)
	if folder == "" {
		f, _, err := env.Store.SyncState(ctx, stateFolder)
		if err != nil {
			return nil, "", err
		}
		folder = f
	}
	if folder == "" {
		return nil, "", command.Errorf("not_configured", "sync is not set up; run sync.setup")
	}
	if pw == "" && m.eng != nil && m.st == env.Store && m.fold == folder {
		return m.eng, folder, nil
	}
	if pw == "" {
		saved, err := m.keys.Get(secretName)
		if err != nil {
			return nil, folder, ErrLocked
		}
		pw = saved
	}
	e, err := Open(ctx, env.Store, NewFolder(folder), pw)
	if err != nil {
		return nil, folder, err
	}
	m.attach(e)
	m.eng, m.st, m.fold = e, env.Store, folder
	return e, folder, nil
}

// takeEmitter keeps the interface's event sender, if ctx has one. m.mu is held.
func (m *manager) takeEmitter(ctx context.Context) {
	if fn, ok := ctx.Value(emitKey{}).(func(string, any)); ok {
		m.emit = fn
	}
}

// attach points an engine at this device's folders and at the interface's progress events.
// m.mu is held.
func (m *manager) attach(e *Engine) {
	e.Local = m.loc
	e.OnProgress = func(p BlobProgress) {
		m.mu.Lock()
		emit := m.emit
		m.mu.Unlock()
		if emit != nil {
			emit("sync:progress", p)
		}
	}
}

type StrengthParams struct {
	Password string `json:"password" required:"true" desc:"The password you are thinking of. It is not stored or sent."`
}

type StrengthResult struct {
	Score  int    `json:"score" desc:"0 (too short) to 4 (strong)"`
	OK     bool   `json:"ok" desc:"Long enough to use"`
	Hint   string `json:"hint"`
	MinLen int    `json:"min_length"`
}

func passwordStrength(_ context.Context, _ *command.Env, p StrengthParams) (StrengthResult, error) {
	s, h := Strength(p.Password)
	return StrengthResult{Score: s, OK: CheckPassword(p.Password) == nil, Hint: h, MinLen: MinPasswordLength}, nil
}

type SetupParams struct {
	Folder     string `json:"folder" required:"true" desc:"A folder your own sync service (OneDrive, Dropbox, Google Drive, iCloud, Syncthing) already copies between your devices. Glossa makes a 'Glossa Sync' folder inside it."`
	Password   string `json:"password" required:"true" desc:"The sync password: at least 10 characters. If it is lost, the synced copy cannot be read, though the data on each device is fine. Not stored unless remember is true."`
	DeviceName string `json:"device_name,omitempty" desc:"A name for this device, shown to your other devices. Default: this computer's name"`
	Remember   bool   `json:"remember,omitempty" desc:"Save the password in this computer's credential store so sync continues after a restart"`
}

type Status struct {
	Configured     bool          `json:"configured"`
	Folder         string        `json:"folder,omitempty"`
	DeviceID       string        `json:"device_id"`
	DeviceName     string        `json:"device_name"`
	Unlocked       bool          `json:"unlocked" desc:"The password is available, so sync can run"`
	PasswordSaved  bool          `json:"password_saved" desc:"The password is in the credential store"`
	LastSync       int64         `json:"last_sync,omitempty" desc:"UTC milliseconds"`
	PendingChanges int           `json:"pending_changes" desc:"Changes on this device not yet in the folder"`
	Conflicts      int           `json:"conflicts" desc:"Replaced text edits not yet dismissed"`
	FolderBytes    int64         `json:"folder_bytes,omitempty"`
	Devices        []DeviceInfo  `json:"devices,omitempty"`
	FolderError    string        `json:"folder_error,omitempty" desc:"Why the folder could not be read, in plain words"`
	Books          *FileStats    `json:"books,omitempty" desc:"Shared books and modules, and how many are on this device"`
	Progress       *BlobProgress `json:"progress,omitempty" desc:"A book or module transfer under way, or the last one"`
	Automatic      bool          `json:"automatic" desc:"Syncs by itself at start, every few minutes and at quit"`
	Syncing        bool          `json:"syncing" desc:"A sync is running now"`
	LastError      string        `json:"last_error,omitempty" desc:"Why the last sync failed, in plain words"`
	LastErrorCode  string        `json:"last_error_code,omitempty"`
}

func (m *manager) setup(ctx context.Context, env *command.Env, p SetupParams) (Status, error) {
	if err := needStore(env); err != nil {
		return Status{}, err
	}
	if st, err := os.Stat(p.Folder); err != nil || !st.IsDir() {
		return Status{}, fail(ErrFolderMissing)
	}
	t := NewFolder(p.Folder)
	ok, err := IsInitialised(ctx, t)
	if err != nil {
		return Status{}, fail(err)
	}
	if !ok {
		if err := Initialize(ctx, t, p.Password, setupKDF); err != nil {
			return Status{}, fail(err)
		}
	}
	e, err := Open(ctx, env.Store, t, p.Password)
	if err != nil {
		return Status{}, fail(err)
	}
	if p.DeviceName != "" {
		if err := env.Store.SetDeviceName(ctx, p.DeviceName); err != nil {
			return Status{}, err
		}
	}
	if err := env.Store.SetSyncState(ctx, stateFolder, p.Folder); err != nil {
		return Status{}, err
	}
	if err := e.Join(ctx); err != nil {
		return Status{}, fail(err)
	}
	if p.Remember {
		if err := m.keys.Set(secretName, p.Password); err != nil {
			return Status{}, command.Errorf("failed", "the password could not be saved in the credential store")
		}
		_ = env.Store.SetSyncState(ctx, rememberFlag, "1")
	} else {
		_ = m.keys.Delete(secretName)
		_ = env.Store.SetSyncState(ctx, rememberFlag, "")
	}
	m.mu.Lock()
	m.takeEmitter(ctx)
	m.attach(e)
	m.eng, m.st, m.fold = e, env.Store, p.Folder
	m.lastErr, m.lastCode = "", ""
	m.mu.Unlock()
	return m.status(ctx, env, struct{}{})
}

type UnlockParams struct {
	Password string `json:"password" required:"true" desc:"The sync password"`
	Remember bool   `json:"remember,omitempty" desc:"Save it in this computer's credential store"`
}

func (m *manager) unlock(ctx context.Context, env *command.Env, p UnlockParams) (Status, error) {
	if err := needStore(env); err != nil {
		return Status{}, err
	}
	if _, _, err := m.engine(ctx, env, "", p.Password); err != nil {
		return Status{}, fail(err)
	}
	if p.Remember {
		if err := m.keys.Set(secretName, p.Password); err != nil {
			return Status{}, command.Errorf("failed", "the password could not be saved in the credential store")
		}
		_ = env.Store.SetSyncState(ctx, rememberFlag, "1")
	}
	return m.status(ctx, env, struct{}{})
}

type PreviewParams struct {
	Folder   string `json:"folder,omitempty" desc:"The folder to preview. Default: the one set up"`
	Password string `json:"password,omitempty" desc:"Its password. Default: the one already entered"`
}

func (m *manager) preview(ctx context.Context, env *command.Env, p PreviewParams) (Preview, error) {
	if err := needStore(env); err != nil {
		return Preview{}, err
	}
	if p.Folder != "" && p.Password != "" {
		t := NewFolder(p.Folder)
		ok, err := IsInitialised(ctx, t)
		if err != nil {
			return Preview{}, fail(err)
		}
		if !ok { // a new folder: nothing to merge yet
			if err := CheckPassword(p.Password); err != nil {
				return Preview{}, fail(err)
			}
			pv := Preview{NewFolder: true}
			if pv.ThisDevice, err = env.Store.CountObjects(ctx); err != nil {
				return pv, err
			}
			pv.AfterMerge, pv.InFolder = pv.ThisDevice, map[string]int{}
			pv.ToFolder, err = env.Store.CountLocalChanges(ctx, "")
			return pv, err
		}
		e, err := Open(ctx, env.Store, t, p.Password)
		if err != nil {
			return Preview{}, fail(err)
		}
		pv, err := e.Preview(ctx)
		return pv, fail(err)
	}
	e, _, err := m.engine(ctx, env, p.Folder, p.Password)
	if err != nil {
		return Preview{}, fail(err)
	}
	pv, err := e.Preview(ctx)
	return pv, fail(err)
}

func (m *manager) now(ctx context.Context, env *command.Env, _ struct{}) (Result, error) {
	e, _, err := m.engine(ctx, env, "", "")
	if err != nil {
		return Result{}, m.note(fail(err))
	}
	m.mu.Lock()
	m.syncing = true
	m.mu.Unlock()
	res, err := e.Sync(ctx)
	m.mu.Lock()
	m.syncing = false
	m.mu.Unlock()
	return res, m.note(fail(err))
}

// note remembers why a sync failed (or that it worked) for sync.status, and passes err on.
func (m *manager) note(err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastErr, m.lastCode = "", ""
	var ce *command.Error
	if errors.As(err, &ce) {
		m.lastErr, m.lastCode = ce.Message, ce.Code
	}
	return err
}

type AutomaticParams struct {
	Enabled bool `json:"enabled" desc:"true: sync by itself; false: only when asked"`
}

func (m *manager) automatic(ctx context.Context, env *command.Env, p AutomaticParams) (Status, error) {
	if err := needStore(env); err != nil {
		return Status{}, err
	}
	v := "1"
	if p.Enabled {
		v = ""
	}
	if err := env.Store.SetSyncState(ctx, autoOff, v); err != nil {
		return Status{}, err
	}
	return m.status(ctx, env, struct{}{})
}

// AutoResult says whether sync.auto synced, and if not, why.
type AutoResult struct {
	Ran    bool    `json:"ran"`
	Reason string  `json:"reason,omitempty" enum:"not_set_up,off,first_sync_pending,locked,failed" desc:"Why nothing was synced"`
	Result *Result `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}

func (m *manager) auto(ctx context.Context, env *command.Env, _ struct{}) (AutoResult, error) {
	if env.Store == nil {
		return AutoResult{Reason: "not_set_up"}, nil
	}
	folder, _, err := env.Store.SyncState(ctx, stateFolder)
	if err != nil || folder == "" {
		return AutoResult{Reason: "not_set_up"}, nil
	}
	if v, _, _ := env.Store.SyncState(ctx, autoOff); v != "" {
		return AutoResult{Reason: "off"}, nil
	}
	// The first sync is the person's decision, made after the preview; never automatic.
	if v, ok, _ := env.Store.SyncState(ctx, stateLastSync); !ok || v == "" {
		return AutoResult{Reason: "first_sync_pending"}, nil
	}
	res, err := m.now(ctx, env, struct{}{})
	if err != nil {
		var ce *command.Error
		if errors.As(err, &ce) && ce.Code == "locked" {
			return AutoResult{Reason: "locked"}, nil
		}
		return AutoResult{Reason: "failed", Error: err.Error()}, nil
	}
	return AutoResult{Ran: true, Result: &res}, nil
}

func (m *manager) status(ctx context.Context, env *command.Env, _ struct{}) (Status, error) {
	var s Status
	if err := needStore(env); err != nil {
		return s, err
	}
	s.DeviceID = env.Store.Device()
	s.DeviceName, _ = env.Store.DeviceName(ctx, s.DeviceID)
	folder, _, err := env.Store.SyncState(ctx, stateFolder)
	if err != nil {
		return s, err
	}
	s.Folder, s.Configured = folder, folder != ""
	if !s.Configured {
		return s, nil
	}
	if v, _, _ := env.Store.SyncState(ctx, autoOff); v == "" {
		s.Automatic = true
	}
	m.mu.Lock()
	s.Syncing, s.LastError, s.LastErrorCode = m.syncing, m.lastErr, m.lastCode
	m.mu.Unlock()
	_, s.PasswordSaved = func() (string, bool) { v, err := m.keys.Get(secretName); return v, err == nil }()
	m.mu.Lock()
	s.Unlocked = m.eng != nil && m.st == env.Store && m.fold == folder
	m.mu.Unlock()
	s.Unlocked = s.Unlocked || s.PasswordSaved
	if v, ok, _ := env.Store.SyncState(ctx, stateLastSync); ok {
		s.LastSync, _ = strconv.ParseInt(v, 10, 64)
	}
	exported, _, _ := env.Store.SyncState(ctx, stateExported)
	s.PendingChanges, _ = env.Store.CountLocalChanges(ctx, exported)
	if cs, err := env.Store.SyncConflicts(ctx, false); err == nil {
		s.Conflicts = len(cs)
	}
	if s.Unlocked {
		if e, _, err := m.engine(ctx, env, "", ""); err != nil {
			s.FolderError = err.Error()
		} else if u, err := e.Devices(ctx); err != nil {
			s.FolderError = err.Error()
		} else {
			s.Devices, s.FolderBytes = u.Devices, u.Bytes
		}
		if e, _, err := m.engine(ctx, env, "", ""); err == nil {
			if fs, err := e.Stats(ctx); err == nil {
				s.Books = &fs
			}
			p := e.Progress()
			s.Progress = &p
		}
	}
	return s, nil
}

func (m *manager) devices(ctx context.Context, env *command.Env, _ struct{}) (Usage, error) {
	if err := needStore(env); err != nil {
		return Usage{}, err
	}
	e, _, err := m.engine(ctx, env, "", "")
	if err != nil {
		return Usage{}, fail(err)
	}
	u, err := e.Devices(ctx)
	return u, fail(err)
}

type ForgetParams struct {
	DeviceID string `json:"device_id" required:"true" desc:"The device to remove, from sync.devices"`
}

type Done struct {
	Done bool `json:"done"`
}

func (m *manager) forget(ctx context.Context, env *command.Env, p ForgetParams) (Done, error) {
	if err := needStore(env); err != nil {
		return Done{}, err
	}
	e, _, err := m.engine(ctx, env, "", "")
	if err != nil {
		return Done{}, fail(err)
	}
	if err := e.Forget(ctx, p.DeviceID); err != nil {
		return Done{}, fail(err)
	}
	return Done{true}, nil
}

func (m *manager) compact(ctx context.Context, env *command.Env, _ struct{}) (Done, error) {
	if err := needStore(env); err != nil {
		return Done{}, err
	}
	e, _, err := m.engine(ctx, env, "", "")
	if err != nil {
		return Done{}, fail(err)
	}
	if err := e.Compact(ctx); err != nil {
		return Done{}, fail(err)
	}
	return Done{true}, nil
}

type ConflictsParams struct {
	All bool `json:"all,omitempty" desc:"Include ones already dismissed"`
}

func (m *manager) conflicts(ctx context.Context, env *command.Env, p ConflictsParams) ([]store.SyncConflict, error) {
	if err := needStore(env); err != nil {
		return nil, err
	}
	return env.Store.SyncConflicts(ctx, p.All)
}

type DismissParams struct {
	ID string `json:"id" required:"true" desc:"The conflict's id, from sync.conflicts"`
}

func (m *manager) dismiss(ctx context.Context, env *command.Env, p DismissParams) (Done, error) {
	if err := needStore(env); err != nil {
		return Done{}, err
	}
	if err := env.Store.DismissSyncConflict(ctx, p.ID); err != nil {
		return Done{}, err
	}
	return Done{true}, nil
}

func (m *manager) stop(ctx context.Context, env *command.Env, _ struct{}) (Done, error) {
	if err := needStore(env); err != nil {
		return Done{}, err
	}
	m.mu.Lock()
	m.eng, m.st, m.fold = nil, nil, ""
	m.mu.Unlock()
	_ = m.keys.Delete(secretName)
	for _, k := range []string{stateFolder, rememberFlag, stateLastSync} {
		if err := env.Store.SetSyncState(ctx, k, ""); err != nil {
			return Done{}, fmt.Errorf("sync.stop: %w", err)
		}
	}
	return Done{true}, nil
}

type BooksParams struct {
	Mode string `json:"mode,omitempty" enum:"all,opened" desc:"Keep books on this device: all (download at sync time) or opened (download when opened). Leave out to just look"`
}

func (m *manager) books(ctx context.Context, env *command.Env, p BooksParams) (FileStats, error) {
	if err := needStore(env); err != nil {
		return FileStats{}, err
	}
	e, _, err := m.engine(ctx, env, "", "")
	if err != nil {
		return FileStats{}, fail(err)
	}
	if p.Mode != "" {
		if err := e.SetBooksMode(ctx, p.Mode); err != nil {
			return FileStats{}, command.Errorf("bad_params", "%v", err)
		}
	}
	st, err := e.Stats(ctx)
	return st, fail(err)
}

type FetchParams struct {
	ItemID string `json:"item_id,omitempty" desc:"The book to download, from library.items"`
	All    bool   `json:"all,omitempty" desc:"Download every book and module that is not on this device yet"`
}

type FetchResult struct {
	Book *Fetched    `json:"book,omitempty"`
	All  *BlobResult `json:"all,omitempty"`
}

func (m *manager) fetch(ctx context.Context, env *command.Env, p FetchParams) (FetchResult, error) {
	var out FetchResult
	if err := needStore(env); err != nil {
		return out, err
	}
	if p.ItemID == "" && !p.All {
		return out, command.Errorf("bad_params", "give item_id, or all")
	}
	e, _, err := m.engine(ctx, env, "", "")
	if err != nil {
		return out, fail(err)
	}
	if p.All {
		r, err := e.FetchAll(ctx)
		out.All = &r
		return out, fail(err)
	}
	f, err := e.EnsureBook(ctx, p.ItemID)
	if errors.Is(err, ErrNotShared) {
		return out, command.Errorf("not_found", "%s", err.Error())
	}
	out.Book = &f
	return out, fail(err)
}

func (m *manager) progress(ctx context.Context, env *command.Env, _ struct{}) (BlobProgress, error) {
	m.mu.Lock()
	e := m.eng
	ok := e != nil && m.st == env.Store
	m.mu.Unlock()
	if !ok {
		return BlobProgress{}, nil
	}
	return e.Progress(), nil
}
