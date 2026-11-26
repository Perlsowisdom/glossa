package obsidian

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// The vault is the one place Glossa writes. Everything goes through resolve, which refuses a path that leaves
// the vault: "..", an absolute path, a drive or stream name, and any folder that is a link (symbolic link or
// Windows junction) pointing outside it.

const (
	maxFileBytes = 5 << 20 // a Markdown file bigger than this is skipped
	maxFiles     = 20000   // files looked at in one import
)

type vault struct {
	root string // the folder, absolute, with links resolved
}

func openVault(path string) (*vault, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("obsidian: no vault folder is chosen")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("obsidian: the vault is not a folder")
	}
	return &vault{root: real}, nil
}

// canWrite checks that a file can be made in the vault, and removes it again.
func (v *vault) canWrite() error {
	f, err := os.CreateTemp(v.root, ".glossa-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

var (
	errOutside = errors.New("obsidian: that path is outside the vault folder")
	errLink    = errors.New("obsidian: a folder on that path is a link, and links are not followed")
)

// isLink tells whether a file or folder is a link: a symbolic link, or on Windows a junction (which Go reports as irregular).
func isLink(st fs.FileInfo) bool { return st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 }

// cleanRel turns a path inside the vault into a clean slash-separated one, or says why it cannot be used.
func cleanRel(rel string) (string, error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") {
		return "", errOutside
	}
	var parts []string
	for _, p := range strings.Split(rel, "/") {
		switch {
		case p == "" || p == ".":
		case p == ".." || strings.Contains(p, ":"):
			return "", errOutside
		default:
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/"), nil
}

// resolve is the absolute path of rel in the vault. No folder on the way may be a link (a symbolic link or a junction),
// and the nearest part that exists must really be inside the vault. The last part may be a link: read and write look at it.
func (v *vault) resolve(rel string) (string, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(v.root, filepath.FromSlash(rel))
	parts := strings.Split(rel, "/")
	cur := v.root
	for i, p := range parts {
		if p == "" {
			continue
		}
		cur = filepath.Join(cur, p)
		st, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if isLink(st) && i < len(parts)-1 {
			return "", errLink
		}
	}
	probe := filepath.Dir(full)
	for within(v.root, probe) {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if !within(v.root, real) {
				return "", errOutside
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		next := filepath.Dir(probe)
		if next == probe {
			break
		}
		probe = next
	}
	return full, nil
}

func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return r == "." || (r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r))
}

// stat says what is at rel: nil error and a regular file, or an error (fs.ErrNotExist when there is none).
// A link is never followed: it counts as "not a regular file".
func (v *vault) stat(rel string) (fs.FileInfo, error) {
	full, err := v.resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.Lstat(full)
}

// exists tells whether anything is at rel (a file, folder or link), case-insensitively on case-insensitive disks.
func (v *vault) exists(rel string) bool {
	_, err := v.stat(rel)
	return err == nil
}

// tooBig and notText are why a file is skipped.
var (
	errTooBig  = errors.New("it is bigger than 5 MB")
	errNotText = errors.New("it is not valid UTF-8 text")
	errNotFile = errors.New("it is not an ordinary file (it may be a link)")
)

// read returns a Markdown file's text. It skips (with a reason) files that are links, huge or not UTF-8.
func (v *vault) read(rel string) (string, error) {
	full, err := v.resolve(rel)
	if err != nil {
		return "", err
	}
	st, err := os.Lstat(full)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errNotFile
	}
	if st.Size() > maxFileBytes {
		return "", errTooBig
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", errNotText
	}
	return string(b), nil
}

// write puts a file in the vault, making the folders it needs. It writes to a temporary file in the same
// folder and renames it, so a file is never left half written, and it refuses to write over a link.
func (v *vault) write(rel string, data []byte) error {
	full, err := v.resolve(rel)
	if err != nil {
		return err
	}
	if st, err := os.Lstat(full); err == nil && !st.Mode().IsRegular() {
		return errNotFile
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := v.resolve(rel); err != nil { // the folders exist now: check them again
		return err
	}
	// Folders were made (or follow links): check again now that they exist.
	if real, err := filepath.EvalSymlinks(dir); err != nil || !within(v.root, real) {
		return errOutside
	}
	tmp, err := os.CreateTemp(dir, ".glossa-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(name)
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(name, full); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Skip is a file that was left out, and why.
type Skip struct {
	Path   string `json:"path" desc:"The file, inside the vault"`
	Reason string `json:"reason" desc:"Why it was left out, in a sentence"`
}

// scan lists the Markdown files under rel (a folder in the vault; "" is the whole vault), as paths relative to the vault.
// Folders whose name starts with a dot (.obsidian, .git, .trash) and links are not entered.
func (v *vault) scan(rel string) (files []string, skips []Skip, err error) {
	rel, err = cleanRel(rel)
	if err != nil {
		return nil, nil, err
	}
	start, err := v.resolve(rel)
	if err != nil {
		return nil, nil, err
	}
	if st, err := os.Lstat(start); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	} else if isLink(st) {
		return nil, nil, errLink
	} else if !st.IsDir() {
		return nil, nil, errors.New("obsidian: that is not a folder")
	}
	err = filepath.WalkDir(start, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			if p == start {
				return werr
			}
			skips = append(skips, Skip{Path: v.rel(p), Reason: "it could not be read"})
			return nil
		}
		if p == start {
			return nil
		}
		name := d.Name()
		switch {
		case d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
			if fi, err := os.Stat(p); (err == nil && fi.IsDir()) || strings.EqualFold(filepath.Ext(name), ".md") {
				skips = append(skips, Skip{Path: v.rel(p), Reason: "it is a link, and links are not followed"})
			}
			return nil
		case d.IsDir():
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		case strings.EqualFold(filepath.Ext(name), ".md") && !strings.HasPrefix(name, "."):
			if len(files) >= maxFiles {
				return fs.SkipAll
			}
			files = append(files, v.rel(p))
		}
		return nil
	})
	sort.Strings(files)
	if len(files) >= maxFiles {
		skips = append(skips, Skip{Path: rel, Reason: fmt.Sprintf("only the first %d files were looked at", maxFiles)})
	}
	return files, skips, err
}

// rel is the path of an absolute path in the vault, with forward slashes.
func (v *vault) rel(abs string) string {
	r, err := filepath.Rel(v.root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(r)
}
