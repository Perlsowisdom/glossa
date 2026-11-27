package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/problems"
)

// browserLogEnv names a file the interface tests set. While it is set, nothing is opened: the address
// or folder is appended to that file instead, so a test sees what the app would have opened without a
// browser ever starting.
const browserLogEnv = "GLOSSA_BROWSER_LOG"

var browserLogMu sync.Mutex

// guard runs fn; a panic in it is logged as a problem and becomes a plain error (T32).
func (a *App) guard(name string, fn func() error) error {
	if a.problems == nil {
		return fn()
	}
	return a.problems.Guard(name, fn)
}

// runCommand runs a command for the interface, the timers and the shutdown. A panic in the command
// is recovered; a failure is logged by name and code only, never its message or parameters.
func (a *App) runCommand(ctx context.Context, id string, params json.RawMessage) (res any, err error) {
	err = a.guard(id, func() (e error) {
		res, e = a.reg.Run(ctx, a.env, id, params)
		return e
	})
	if err != nil && ctx.Err() == nil {
		var ce *command.Error
		code := "error"
		if errors.As(err, &ce) {
			code = ce.Code
		}
		slog.Warn("command failed", "command", id, "code", code)
	}
	return res, err
}

// OpenReportURL opens a problem report's pre-filled issue page in the browser. Only the new-issue page
// of Glossa's own repository is opened, so a page cannot use this to open anything else. The person reads
// the issue there and submits it themselves; the app sends nothing.
func (a *App) OpenReportURL(url string) error {
	if !strings.HasPrefix(url, problems.IssueNewURL+"?") || len(url) > problems.MaxIssueURL+200 {
		return errors.New("that is not a report address")
	}
	return openExternal(a.ctx, url, false)
}

// OpenLogFolder shows the problem log's folder in the file manager.
func (a *App) OpenLogFolder() error {
	if a.problems == nil {
		return errors.New("no problem log")
	}
	dir := a.problems.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return openExternal(a.ctx, dir, true)
}

func openExternal(ctx context.Context, target string, folder bool) error {
	if path := os.Getenv(browserLogEnv); path != "" {
		browserLogMu.Lock()
		defer browserLogMu.Unlock()
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		kind := "url"
		if folder {
			kind = "folder"
		}
		_, err = f.WriteString(kind + " " + target + "\n")
		return err
	}
	if !folder {
		runtime.BrowserOpenURL(ctx, target)
		return nil
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", filepath.Clean(target))
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap the helper; the file manager outlives it
	return nil
}
