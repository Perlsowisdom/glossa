package problems

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Log file names: the current file and the one older file that is kept.
const (
	logName  = "glossa.log"
	oldName  = "glossa.log.1"
	maxBytes = 1 << 20
)

// Log is a small rotating log file. Everything written to it is stripped of private content first,
// so the file never holds a path, address or key in the first place.
type Log struct {
	mu    sync.Mutex
	dir   string
	max   int64
	roots Roots
	f     *os.File
	size  int64
}

// openLog opens (creating it if needed) the log in dir. When the file would grow past max bytes it
// is renamed to the old file, replacing the one before.
func openLog(dir string, max int64, roots Roots) (*Log, error) {
	l := &Log{dir: dir, max: max, roots: roots}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Log) open() error {
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(l.dir, logName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, info.Size()
	return nil
}

// Write strips p and appends it. It reports len(p) written even when it cannot write: a full disk
// must never break the app that is only trying to log.
func (l *Log) Write(p []byte) (int, error) {
	line := Strip(string(p), l.roots)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return len(p), nil
	}
	if l.size > 0 && l.size+int64(len(line)) > l.max {
		l.rotate()
		if l.f == nil {
			return len(p), nil
		}
	}
	n, _ := l.f.WriteString(line)
	l.size += int64(n)
	return len(p), nil
}

func (l *Log) rotate() {
	l.f.Close()
	l.f = nil
	_ = os.Remove(filepath.Join(l.dir, oldName))
	_ = os.Rename(filepath.Join(l.dir, logName), filepath.Join(l.dir, oldName))
	_ = l.open()
}

// Lines returns the last n lines, oldest first, across the old and the current file.
func (l *Log) Lines(n int) []string { return readLines(l.dir, n) }

// readLines reads the last n lines of the log in dir; a missing log has none.
func readLines(dir string, n int) []string {
	var all []string
	for _, name := range []string{oldName, logName} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			if t := strings.TrimRight(sc.Text(), "\r"); t != "" {
				all = append(all, t)
			}
		}
		f.Close()
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// Close closes the file.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// removeAll closes the log and deletes the whole log folder's files.
func (l *Log) removeAll() {
	l.Close()
	removeLogFiles(l.dir)
}

func removeLogFiles(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
