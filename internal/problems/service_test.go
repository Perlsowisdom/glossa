package problems

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Perlsowisdom/glossa/internal/command"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// newService is a service on a temporary data folder, started and stopped with the test.
func newService(t *testing.T) *Service {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Glossa")
	s := &Service{dir: dir, logDir: filepath.Join(dir, logsDirName), now: time.Now, seen: map[string]bool{},
		roots: Roots{Home: filepath.Join(t.TempDir(), "home"), Data: dir, Library: filepath.Join(dir, "library.db")}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s
}

func TestLogRotates(t *testing.T) {
	dir := t.TempDir()
	l, err := openLog(dir, 200, Roots{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for i := 0; i < 30; i++ {
		fmt.Fprintf(l, "line number %02d of the log\n", i)
	}
	for _, name := range []string{logName, oldName} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Size() > 200 {
			t.Errorf("%s is %d bytes, over the limit", name, info.Size())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, logName+".2")); err == nil {
		t.Error("more than one older file is kept")
	}
	lines := l.Lines(100)
	if got := lines[len(lines)-1]; got != "line number 29 of the log" {
		t.Errorf("last line = %q", got)
	}
	if len(lines) < 8 {
		t.Errorf("only %d lines survive rotation", len(lines))
	}
}

func TestLogStripsWhatItWrites(t *testing.T) {
	dir := t.TempDir()
	l, _ := openLog(dir, maxBytes, Roots{Home: `C:\Users\Jane Doe`})
	defer l.Close()
	fmt.Fprintln(l, `opened C:\Users\Jane Doe\Documents\Sermon Notes.docx token=abc123`)
	got := strings.Join(l.Lines(10), "\n")
	if strings.Contains(got, "Jane") || strings.Contains(got, "abc123") || strings.Contains(got, "Sermon") {
		t.Errorf("the log kept private text: %q", got)
	}
}

func TestGuardTurnsAPanicIntoAnError(t *testing.T) {
	s := newService(t)
	var got []Problem
	s.OnProblem = func(p Problem) { got = append(got, p) }
	boom := func() error { panic(`bad note "My Private Title" in ` + s.roots.Home) }
	err := s.Guard("notes.list", boom)
	var ce *command.Error
	if !errors.As(err, &ce) || ce.Code != "failed" {
		t.Fatalf("err = %v, want a command error", err)
	}
	if strings.Contains(err.Error(), "Private") || strings.Contains(err.Error(), "goroutine") {
		t.Errorf("the error shown to the person holds the panic: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "panic" || !strings.Contains(got[0].Summary, "notes.list") {
		t.Fatalf("problems = %+v", got)
	}
	if strings.Contains(got[0].Summary, "Private") || strings.Contains(got[0].Summary, s.roots.Home) {
		t.Errorf("summary holds private text: %q", got[0].Summary)
	}
	if !strings.Contains(got[0].Stack, "TestGuard") {
		t.Errorf("the stack is missing: %q", got[0].Stack)
	}
	// The same panic again is logged but not announced again.
	_ = s.Guard("notes.list", boom)
	if len(got) != 1 {
		t.Errorf("the same problem was announced %d times", len(got))
	}
	// A function that does not panic returns its own error.
	want := errors.New("plain")
	if err := s.Guard("x", func() error { return want }); err != want {
		t.Errorf("err = %v", err)
	}
}

func TestCrashMarker(t *testing.T) {
	s := newService(t)
	if _, ok := s.Latest(); ok {
		t.Fatal("a clean first start reported a crash")
	}
	if _, err := os.Stat(filepath.Join(s.dir, runningName)); err != nil {
		t.Fatalf("a running app leaves a marker: %v", err)
	}
	s.Stop()
	if _, err := os.Stat(filepath.Join(s.dir, runningName)); err == nil {
		t.Fatal("a normal stop must remove the marker")
	}

	// A run that never stopped: its marker is still there, and its crash output is in crash.txt.
	if err := os.WriteFile(filepath.Join(s.dir, runningName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	crash := "fatal error: all goroutines are asleep\n\ngoroutine 1 [running]:\nmain.main() at " + filepath.Join(s.dir, "x.go") + ":9"
	if err := os.WriteFile(filepath.Join(s.logDir, crashName), []byte(crash), 0o644); err != nil {
		t.Fatal(err)
	}
	var told []Problem
	s.OnProblem = func(p Problem) { told = append(told, p) }
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	p, ok := s.Pending()
	if !ok || p.Kind != "crash" || len(told) != 1 {
		t.Fatalf("pending = %+v %v, told %d", p, ok, len(told))
	}
	if !strings.Contains(p.Stack, "all goroutines are asleep") || strings.Contains(p.Stack, s.dir) {
		t.Errorf("stack = %q", p.Stack)
	}
	// Seen once: dismissed, it is not pending, but a report can still use it.
	s.Dismiss(p.ID)
	if _, ok := s.Pending(); ok {
		t.Error("a dismissed problem is still pending")
	}
	if _, ok := s.Latest(); !ok {
		t.Error("the problem should still be there for a report")
	}
}

func TestFrontendErrorsAreRateLimited(t *testing.T) {
	s := newService(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	accepted := 0
	for i := 0; i < 25; i++ {
		if r := s.LogFrontend(fmt.Sprintf("boom %d", i), "at x", "app.js:1"); r.Logged {
			accepted++
		} else if !r.Limited {
			t.Fatalf("error %d neither logged nor limited", i)
		}
	}
	if accepted != frontendLimit {
		t.Errorf("accepted %d, want %d", accepted, frontendLimit)
	}
	now = now.Add(61 * time.Second)
	if r := s.LogFrontend("later", "", ""); !r.Logged || r.Limited {
		t.Errorf("after a minute: %+v", r)
	}
}

func TestFrontendErrorIsNewOnce(t *testing.T) {
	s := newService(t)
	first := s.LogFrontend("Cannot read x", "at a\nat b", "main.js:3")
	if first.Problem == nil || first.Problem.Kind != "interface" {
		t.Fatalf("first = %+v", first)
	}
	if again := s.LogFrontend("Cannot read x", "at a\nat b", "main.js:3"); !again.Logged || again.Problem != nil {
		t.Errorf("a repeat should be logged but not new: %+v", again)
	}
}

func TestTurningTheLogOffDeletesIt(t *testing.T) {
	s := newService(t)
	s.LogFrontend("boom", "at x", "")
	if err := s.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(s.logDir)
	if len(entries) != 0 {
		t.Errorf("files left after turning the log off: %v", entries)
	}
	if s.Enabled() {
		t.Error("still enabled")
	}
	if r := s.LogFrontend("again", "", ""); r.Logged {
		t.Error("an error was logged while the log is off")
	}
	if _, err := s.Write([]byte("nothing\n")); err != nil {
		t.Error(err)
	}
	if entries, _ := os.ReadDir(s.logDir); len(entries) != 0 {
		t.Errorf("wrote while off: %v", entries)
	}
	if err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !s.Enabled() || !s.LogFrontend("on again", "", "").Logged {
		t.Error("the log does not come back on")
	}
}

func TestReportHoldsWhatItShouldAndNothingPrivate(t *testing.T) {
	s := newService(t)
	s.SetWebView("131.0.2903.51")
	fmt.Fprintf(s, "opened %s failed for jane@example.org\n", filepath.Join(s.roots.Home, "Documents", "My Sermon.docx"))
	s.LogFrontend("oops", "at "+filepath.Join(s.dir, "x.js")+":1", "")
	text := s.Report(ReportOptions{Note: "I pressed Save in " + s.roots.Home})
	for _, want := range []string{headNote, headAbout, "- Version: ", "- System: ", "WebView2: 131.0.2903.51", headProblem, "Interface error: oops", headLog, "<home>"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
	for _, private := range []string{s.roots.Home, s.dir, "My Sermon", "jane@example.org"} {
		if strings.Contains(text, private) {
			t.Errorf("the report holds %q:\n%s", private, text)
		}
	}
}

func TestIssueURL(t *testing.T) {
	short := "## What were you doing?\n\nOpening a note & saving it, 100% sure: \"é\" #1\n\n## Recent log\n\n```\nline\n```\n"
	link, cut := IssueURL(short)
	if cut {
		t.Error("a short report was cut")
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme+"://"+u.Host+u.Path != "https://github.com/Perlsowisdom/glossa/issues/new" {
		t.Errorf("link = %s", link)
	}
	if got := u.Query().Get("body"); got != short {
		t.Errorf("the body does not round-trip:\n%q\n%q", got, short)
	}
	if strings.Contains(link, "+") || strings.Contains(link, " ") {
		t.Errorf("the address has raw spaces or plus signs: %s", link)
	}
}

func longReport(logLines, stackLines int) string {
	var b strings.Builder
	b.WriteString(headNote + "\n\nI was typing.\n\n" + headAbout + "\n\n- Version: 1\n\n" + headProblem + "\n\nInterface error: boom (panic)\n```\n")
	for i := 0; i < stackLines; i++ {
		fmt.Fprintf(&b, "frame %03d some/package.function(arg) file.go:%d\n", i, i)
	}
	b.WriteString("```\n\n" + headLog + "\n\n```\n")
	for i := 0; i < logLines; i++ {
		fmt.Fprintf(&b, "2026-11-26T12:00:%02d level=INFO msg=\"entry %03d\" some=value\n", i%60, i)
	}
	b.WriteString("```\n")
	return b.String()
}

func TestIssueURLCutsTheLogFirst(t *testing.T) {
	text := longReport(200, 10)
	link, cut := IssueURL(text)
	if !cut || len(link) > MaxIssueURL {
		t.Fatalf("cut = %v, length %d", cut, len(link))
	}
	body := mustBody(t, link)
	if !strings.Contains(body, "I was typing.") || !strings.Contains(body, "frame 009") {
		t.Error("the note and the whole stack must survive while the log can be cut")
	}
	if !strings.Contains(body, "entry 199") || strings.Contains(body, "entry 000") {
		t.Error("the oldest log lines go first, the newest stay")
	}
	if !strings.Contains(body, "shortened") {
		t.Error("the text does not say it was shortened")
	}
	if !strings.Contains(mustTitle(t, link), "boom") {
		t.Errorf("title = %q", mustTitle(t, link))
	}
}

func TestIssueURLCutsTheStackNext(t *testing.T) {
	text := longReport(5, 600)
	link, cut := IssueURL(text)
	if !cut || len(link) > MaxIssueURL {
		t.Fatalf("cut = %v, length %d", cut, len(link))
	}
	body := mustBody(t, link)
	if strings.Contains(body, "entry 000") {
		t.Error("the log lines should have gone before the stack")
	}
	if !strings.Contains(body, "frame 000") || strings.Contains(body, "frame 599") {
		t.Error("the top of the stack stays, the bottom goes")
	}
}

func TestIssueURLCutsAnythingElseLast(t *testing.T) {
	text := "## What were you doing?\n\n" + strings.Repeat("a very long note é ", 2000)
	link, cut := IssueURL(text)
	if !cut || len(link) > MaxIssueURL {
		t.Fatalf("cut = %v, length %d", cut, len(link))
	}
	if !strings.HasPrefix(mustBody(t, link), "## What were you doing?") {
		t.Error("the start of the text must stay")
	}
}

func mustBody(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("body")
}

func mustTitle(t *testing.T, link string) string {
	t.Helper()
	u, _ := url.Parse(link)
	return u.Query().Get("title")
}

func TestCommands(t *testing.T) {
	s := newService(t)
	r := command.NewRegistry()
	RegisterService(r, s)
	run := func(id string, params any) json.RawMessage {
		t.Helper()
		b, _ := json.Marshal(params)
		res, err := r.Run(context.Background(), &command.Env{}, id, b)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		out, _ := json.Marshal(res)
		return out
	}
	var fr FrontendResult
	if err := json.Unmarshal(run("problems.log", LogParams{Message: "x is undefined", Stack: "at y", Where: "a.js:1"}), &fr); err != nil || fr.Problem == nil {
		t.Fatalf("problems.log: %+v %v", fr, err)
	}
	var st StatusResult
	_ = json.Unmarshal(run("problems.status", struct{}{}), &st)
	if !st.Enabled || st.Pending == nil || st.Pending.ID != fr.Problem.ID {
		t.Fatalf("status = %+v", st)
	}
	run("problems.dismiss", DismissParams{ID: st.Pending.ID})
	st = StatusResult{}
	_ = json.Unmarshal(run("problems.status", struct{}{}), &st)
	if st.Pending != nil {
		t.Error("still pending after dismiss")
	}
	var rep ReportResult
	_ = json.Unmarshal(run("problems.report", ReportParams{Note: "hello"}), &rep)
	if !strings.Contains(rep.Text, "hello") || !strings.Contains(rep.Text, "x is undefined") {
		t.Errorf("report = %s", rep.Text)
	}
	var is IssueResult
	_ = json.Unmarshal(run("problems.issue_url", IssueParams{Text: rep.Text}), &is)
	if !strings.HasPrefix(is.URL, IssueNewURL+"?title=") || is.Shortened {
		t.Errorf("issue = %+v", is)
	}

	out := filepath.Join(t.TempDir(), "report.txt")
	run("problems.save", SaveParams{Path: out, Text: "the edited text"})
	if b, _ := os.ReadFile(out); string(b) != "the edited text" {
		t.Errorf("saved %q", b)
	}
	b, _ := json.Marshal(SaveParams{Path: filepath.Join(t.TempDir(), "x.exe"), Text: "x"})
	if _, err := r.Run(context.Background(), &command.Env{}, "problems.save", b); err == nil {
		t.Error("saving anything but a .txt file must be refused")
	}
	off := false
	b, _ = json.Marshal(SettingsParams{Enabled: &off})
	if _, err := r.Run(context.Background(), &command.Env{}, "problems.settings", b); err != nil {
		t.Fatal(err)
	}
	if s.Enabled() {
		t.Error("problems.settings enabled=false did not turn the log off")
	}
}
