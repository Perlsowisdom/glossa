package sync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// Entry is one file or folder in a transport.
type Entry struct {
	Name string
	Dir  bool
	Size int64
}

// Transport is where the encrypted files live. Names are slash-separated paths below the sync
// root ("Glossa Sync"). A transport never sees a password, a key or plain text. The folder
// transport below is the only one so far; others (ADR 0005) implement the same few calls.
type Transport interface {
	// List returns the entries of a folder. A folder that does not exist is empty, but a transport
	// whose whole location is gone returns ErrFolderMissing.
	List(ctx context.Context, dir string) ([]Entry, error)
	// Read returns a file, or ErrNotExist.
	Read(ctx context.Context, name string) ([]byte, error)
	// Create writes a new file all at once, so a reader never sees half of it, and refuses to
	// replace one that exists (ErrExists).
	Create(ctx context.Context, name string, data []byte) error
	// Replace renames a file in one step, replacing a file already at the new name, so a reader sees the
	// old file or the new one and never neither (ErrNotExist if from is missing).
	Replace(ctx context.Context, from, to string) error
	// Remove deletes a file. A file that is not there is not an error.
	Remove(ctx context.Context, name string) error
	// RemoveAll deletes a folder and everything in it.
	RemoveAll(ctx context.Context, dir string) error
}

// RootName is the folder Glossa makes inside the folder the user chose.
const RootName = "Glossa Sync"

// FolderTransport keeps the files in <folder>/Glossa Sync/, a folder the user's own sync service
// (OneDrive, Dropbox, Google Drive, iCloud, Syncthing) copies between their devices.
type FolderTransport struct {
	folder string
	root   string
}

// NewFolder makes a transport for a folder the user chose. The folder itself must exist; the
// Glossa Sync folder inside it is made when first needed.
func NewFolder(folder string) *FolderTransport {
	return &FolderTransport{folder: folder, root: filepath.Join(folder, RootName)}
}

// Folder is the folder the user chose.
func (f *FolderTransport) Folder() string { return f.folder }

func (f *FolderTransport) check() error {
	st, err := os.Stat(f.folder)
	if err != nil || !st.IsDir() {
		return ErrFolderMissing
	}
	return nil
}

func (f *FolderTransport) abs(name string) (string, error) {
	clean := path.Clean("/" + name)
	if strings.Contains(name, "\\") || strings.Contains(name, "..") || clean == "/" && name != "" && name != "." {
		return "", fmt.Errorf("bad sync file name %q", name)
	}
	return filepath.Join(f.root, filepath.FromSlash(strings.TrimPrefix(clean, "/"))), nil
}

func (f *FolderTransport) List(ctx context.Context, dir string) ([]Entry, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	p, err := f.abs(dir)
	if err != nil {
		return nil, err
	}
	des, err := os.ReadDir(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		e := Entry{Name: de.Name(), Dir: de.IsDir()}
		if info, err := de.Info(); err == nil {
			e.Size = info.Size()
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *FolderTransport) Read(ctx context.Context, name string) ([]byte, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	p, err := f.abs(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotExist
	}
	return b, err
}

func (f *FolderTransport) Create(ctx context.Context, name string, data []byte) error {
	if err := f.check(); err != nil {
		return err
	}
	p, err := f.abs(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); err == nil {
		return ErrExists
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return wrapDisk(err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return wrapDisk(err)
	}
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return wrapDisk(werr)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return wrapDisk(err)
	}
	return nil
}

// Replace renames over an existing file; on Windows Go's os.Rename replaces it atomically too.
func (f *FolderTransport) Replace(ctx context.Context, from, to string) error {
	if err := f.check(); err != nil {
		return err
	}
	pf, err := f.abs(from)
	if err != nil {
		return err
	}
	pt, err := f.abs(to)
	if err != nil {
		return err
	}
	if err := os.Rename(pf, pt); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotExist
		}
		return wrapDisk(err)
	}
	return nil
}

func (f *FolderTransport) Remove(ctx context.Context, name string) error {
	if err := f.check(); err != nil {
		return err
	}
	p, err := f.abs(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (f *FolderTransport) RemoveAll(ctx context.Context, dir string) error {
	if err := f.check(); err != nil {
		return err
	}
	p, err := f.abs(dir)
	if err != nil {
		return err
	}
	if p == f.root {
		return errors.New("refusing to remove the whole sync folder")
	}
	return os.RemoveAll(p)
}

// wrapDisk turns "no space left" into ErrDiskFull.
func wrapDisk(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == syscall.ENOSPC || errno == 112 || errno == 39) {
		return fmt.Errorf("%w: %v", ErrDiskFull, err)
	}
	return err
}
