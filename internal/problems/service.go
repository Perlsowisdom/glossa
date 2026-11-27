package problems

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
)

// Files, all in the data folder: the switch that turns the log off, the marker of a running app, and
// the logs folder with the log, its older file, the last problem and the last crash's output.
const (
	offName     = "problem-log.off"
	runningName = "running.marker"
	logsDirName = "logs"
	problemName = "problem.json"
	crashName   = "crash.txt"
)

// Limits: how much of a problem is kept, and how many errors the interface may report a minute.
const (
	maxSummary    = 300
	maxStack      = 8000
	frontendLimit = 10
	frontendSpan  = time.Minute
	recentDays    = 24 * time.Hour
)

// Problem is one error worth a report: a crash of the last run, a Go panic that was recovered, or
// an error the interface caught.
type Problem struct {
	ID       string `json:"id"`
	Time     string `json:"time" desc:"When it happened (RFC 3339)"`
	Kind     string `json:"kind" desc:"crash (the last run ended badly), panic (recovered in Go) or interface (an error in the window)"`
	Summary  string `json:"summary"`
	Stack    string `json:"stack,omitempty"`
	Reviewed bool   `json:"reviewed" desc:"The person has seen the notice about it"`
}

// Service owns the problem log of one data folder.
type Service struct {
	dir, logDir string
	// OnProblem is called, outside any lock, when a problem the person has not seen is recorded.
	OnProblem func(Problem)

	mu      sync.Mutex
	roots   Roots
	log     *Log
	crash   *os.File
	started bool
	webview string
	now     func() time.Time
	seen    map[string]bool // problems already recorded in this run, so each one is noticed once
	rate    []time.Time
	counter int
}

var (
	servicesMu sync.Mutex
	services   = map[string]*Service{}
)

// ForDir returns the service of a data folder; the commands and the app share it.
func ForDir(dataDir string) *Service {
	servicesMu.Lock()
	defer servicesMu.Unlock()
	if s := services[dataDir]; s != nil {
		return s
	}
	home, _ := os.UserHomeDir()
	s := &Service{dir: dataDir, logDir: filepath.Join(dataDir, logsDirName), now: time.Now, seen: map[string]bool{},
		roots: Roots{Home: home, Data: dataDir, Library: filepath.Join(dataDir, "library.db")}}
	services[dataDir] = s
	return s
}

// SetLibrary tells the service where the library file is (it is library.db in the data folder unless
// GLOSSA_LIBRARY says otherwise), so that path is shown as <library>.
func (s *Service) SetLibrary(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roots.Library = path
	if s.log != nil {
		s.log.roots = s.roots
	}
}

// SetWebView records the WebView2 version for reports.
func (s *Service) SetWebView(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webview = v
}

// Roots returns the folders that reports hide.
func (s *Service) Roots() Roots {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roots
}

// LogDir is the folder that holds the log.
func (s *Service) LogDir() string { return s.logDir }

// Enabled says whether the problem log is on (it is, until the person turns it off).
func (s *Service) Enabled() bool {
	_, err := os.Stat(filepath.Join(s.dir, offName))
	return err != nil
}

// Start opens the log, notes a crash of the last run (its marker was never removed), points the Go
// runtime's crash output at a file, and leaves a marker that Stop removes. Nothing happens while the log is off.
func (s *Service) Start() error {
	s.mu.Lock()
	if s.started || !s.Enabled() {
		s.mu.Unlock()
		return nil
	}
	l, err := openLog(s.logDir, maxBytes, s.roots)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.log, s.started = l, true
	_, markerErr := os.Stat(filepath.Join(s.dir, runningName))
	prior := readFile(filepath.Join(s.logDir, crashName))
	s.startCrashOutput()
	_ = os.WriteFile(filepath.Join(s.dir, runningName), []byte(s.now().UTC().Format(time.RFC3339)), 0o644)
	s.mu.Unlock()
	if markerErr == nil { // the last run never removed its marker
		s.Record("crash", "Glossa did not close properly the last time it ran.", prior)
	}
	return nil
}

// startCrashOutput makes a crash that kills the process leave its stack in crash.txt (Go 1.23's
// debug.SetCrashOutput). The caller holds the lock.
func (s *Service) startCrashOutput() {
	f, err := os.OpenFile(filepath.Join(s.logDir, crashName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	if debug.SetCrashOutput(f, debug.CrashOptions{}) != nil {
		f.Close()
		return
	}
	s.crash = f
}

func (s *Service) stopCrashOutput() {
	if s.crash != nil {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		s.crash.Close()
		s.crash = nil
	}
}

// Stop ends a normal run: the marker goes, so the next start knows this one ended well.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	s.started = false
	s.stopCrashOutput()
	_ = os.Remove(filepath.Join(s.dir, runningName))
	_ = os.Remove(filepath.Join(s.logDir, crashName))
	if s.log != nil {
		s.log.Close()
		s.log = nil
	}
}

// Install makes the standard slog logger, and with it the log package, write to the problem log
// (and to standard error, where there is one).
func (s *Service) Install() {
	slog.SetDefault(slog.New(slog.NewTextHandler(tee{s}, &slog.HandlerOptions{Level: slog.LevelInfo})))
}

// tee writes to the problem log and to standard error, ignoring a missing one (a windowed app has none).
type tee struct{ s *Service }

func (t tee) Write(p []byte) (int, error) {
	_, _ = t.s.Write(p)
	_, _ = os.Stderr.Write(p)
	return len(p), nil
}

// Write appends to the log, if it is on. It implements io.Writer.
func (s *Service) Write(p []byte) (int, error) {
	s.mu.Lock()
	l := s.log
	s.mu.Unlock()
	if l != nil {
		return l.Write(p)
	}
	return len(p), nil
}

var _ io.Writer = (*Service)(nil)

// SetEnabled turns the log on or off. Turning it off stops writing and deletes the log, the last
// problem and the markers; turning it on starts it again.
func (s *Service) SetEnabled(on bool) error {
	off := filepath.Join(s.dir, offName)
	if on {
		if err := os.Remove(off); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return s.Start()
	}
	s.mu.Lock()
	s.started = false
	s.stopCrashOutput()
	if s.log != nil {
		s.log.Close()
		s.log = nil
	}
	s.seen = map[string]bool{}
	s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(off, []byte("The problem log is off.\n"), 0o644); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(s.dir, runningName))
	removeLogFiles(s.logDir)
	return nil
}

// Record writes a problem to the log and keeps it as the latest, for a notice and a report. A
// problem like one already recorded in this run is only logged. It returns the problem and whether
// the person should be told about it.
func (s *Service) Record(kind, summary, stack string) (Problem, bool) {
	s.mu.Lock()
	if s.log == nil {
		s.mu.Unlock()
		return Problem{}, false
	}
	summary = clip(redactQuoted(Strip(summary, s.roots)), maxSummary)
	stack = clip(Strip(stack, s.roots), maxStack)
	sum := sha256.Sum256([]byte(kind + "\n" + summary))
	sig := hex.EncodeToString(sum[:8])
	if s.seen[sig] {
		s.mu.Unlock()
		slog.Error("problem again", "kind", kind, "summary", summary)
		return Problem{}, false
	}
	s.seen[sig] = true
	s.counter++
	p := Problem{ID: fmt.Sprintf("%s-%d", s.now().UTC().Format("20060102T150405"), s.counter), Time: s.now().UTC().Format(time.RFC3339),
		Kind: kind, Summary: summary, Stack: stack}
	s.writeProblem(p)
	notify := s.OnProblem
	s.mu.Unlock()
	slog.Error("problem", "kind", kind, "summary", summary, "id", p.ID)
	if notify != nil {
		notify(p)
	}
	return p, true
}

func (s *Service) writeProblem(p Problem) {
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	_ = os.MkdirAll(s.logDir, 0o755)
	// Written to a new file and renamed, so a reader never sees half a problem.
	tmp := filepath.Join(s.logDir, problemName+".tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(s.logDir, problemName))
	}
}

// Latest is the last problem recorded, if there is one.
func (s *Service) Latest() (Problem, bool) {
	var p Problem
	b, err := os.ReadFile(filepath.Join(s.logDir, problemName))
	if err != nil || json.Unmarshal(b, &p) != nil || p.ID == "" {
		return Problem{}, false
	}
	return p, true
}

// Pending is the last problem if the person has not been told about it yet.
func (s *Service) Pending() (Problem, bool) {
	p, ok := s.Latest()
	return p, ok && !p.Reviewed
}

// Dismiss marks the latest problem as seen, so its notice is not shown again. The notice covers every
// problem up to now, so it does not matter which one it was shown for (id is the one it named).
func (s *Service) Dismiss(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.Latest(); ok {
		p.Reviewed = true
		s.writeProblem(p)
	}
}

// Guard runs fn and turns a panic in it into a logged problem and a plain error.
func (s *Service) Guard(name string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = s.Panicked(name, r, debug.Stack())
		}
	}()
	return fn()
}

// Panicked records a recovered panic and returns the error to show the person: no stack, no value.
func (s *Service) Panicked(name string, v any, stack []byte) error {
	s.Record("panic", fmt.Sprintf("Glossa stopped in %s: %v", name, v), string(stack))
	return command.Errorf("failed", "Glossa ran into a problem and stopped that. It is in the problem log; Help → Report a problem lets you review a report.")
}

// FrontendResult says what became of an error the interface reported.
type FrontendResult struct {
	Logged  bool     `json:"logged"`
	Limited bool     `json:"limited,omitempty" desc:"Too many errors in the last minute; this one was dropped"`
	Problem *Problem `json:"problem,omitempty" desc:"Set when this is a problem the person has not been told about"`
}

// LogFrontend records an error from the window. At most frontendLimit a minute are accepted.
func (s *Service) LogFrontend(message, stack, where string) FrontendResult {
	s.mu.Lock()
	cutoff := s.now().Add(-frontendSpan)
	keep := s.rate[:0]
	for _, t := range s.rate {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	s.rate = keep
	if len(s.rate) >= frontendLimit {
		s.mu.Unlock()
		return FrontendResult{Limited: true}
	}
	s.rate = append(s.rate, s.now())
	on := s.log != nil
	s.mu.Unlock()
	if !on {
		return FrontendResult{}
	}
	summary := "Interface error: " + clip(strings.TrimSpace(message), 200)
	if where = strings.TrimSpace(where); where != "" {
		summary += " (" + clip(where, 80) + ")"
	}
	p, isNew := s.Record("interface", summary, stack)
	if isNew {
		return FrontendResult{Logged: true, Problem: &p}
	}
	return FrontendResult{Logged: true}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func readFile(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}
