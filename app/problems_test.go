package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/problems"
)

// While GLOSSA_BROWSER_LOG is set, a report address is written to that file and no browser starts.
func TestOpenReportURLOnlyOpensTheIssuePage(t *testing.T) {
	log := filepath.Join(t.TempDir(), "opened.txt")
	t.Setenv(browserLogEnv, log)
	a := &App{}
	good := problems.IssueNewURL + "?title=Problem%20report&body=hello"
	if err := a.OpenReportURL(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"https://example.org/",
		"https://github.com/Perlsowisdom/glossa/issues/new", // no pre-filled text
		"https://github.com/Perlsowisdom/other/issues/new?title=x",
		"file:///C:/Windows/System32/calc.exe",
		problems.IssueNewURL + "?body=" + strings.Repeat("a", problems.MaxIssueURL+300),
	} {
		if err := a.OpenReportURL(bad); err == nil {
			t.Errorf("%.60q was opened", bad)
		}
	}
	b, _ := os.ReadFile(log)
	if got := strings.TrimSpace(string(b)); got != "url "+good {
		t.Errorf("opened %q", got)
	}
}

// A panic in a command becomes a plain error and a logged problem, and the app goes on.
func TestRunRecoversAPanic(t *testing.T) {
	dir := t.TempDir()
	pl := problems.ForDir(dir)
	if err := pl.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pl.Stop)
	reg := command.NewRegistry()
	reg.Add(command.Define("test.panic", "Panic.", false, func(context.Context, *command.Env, struct{}) (int, error) {
		var m map[string]int
		m["x"] = 1
		return 0, nil
	}))
	a := &App{ctx: context.Background(), reg: reg, env: &command.Env{}, problems: pl}
	_, err := a.Run("test.panic", "{}")
	if err == nil || !strings.Contains(err.Error(), "problem log") {
		t.Fatalf("err = %v", err)
	}
	p, ok := pl.Pending()
	if !ok || p.Kind != "panic" || !strings.Contains(p.Summary, "test.panic") {
		t.Errorf("pending = %+v %v", p, ok)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), dir) {
		t.Errorf("the problem holds the data folder: %s", b)
	}
}
