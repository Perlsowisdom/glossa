// Command app is the Glossa desktop app. Its interface calls the same command registry as the CLI;
// it has no features of its own.
package main

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/Perlsowisdom/glossa/frontend"
	"github.com/Perlsowisdom/glossa/internal/appdir"
	"github.com/Perlsowisdom/glossa/internal/assistant"
	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/all"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/problems"
	"github.com/Perlsowisdom/glossa/internal/store"
	foldersync "github.com/Perlsowisdom/glossa/internal/sync"
)

// App is bound to the interface. The interface reaches every feature through Commands and Run.
type App struct {
	ctx   context.Context
	bg    context.Context // ends at shutdown
	reg   *command.Registry
	env   *command.Env
	stop  context.CancelFunc
	wg    sync.WaitGroup
	typed sync.Once // fills in missing book types once per run
	shot  string    // --screenshot: save the window's picture here, then quit
	// problems is the local problem log (T32); nil in tests.
	problems *problems.Service
}

// rescanEvery is how often library folders are rescanned. Unchanged files are skipped, so it is cheap.
const rescanEvery = 10 * time.Minute

func (a *App) startup(ctx context.Context) {
	// The assistant streams its replies to the interface as events.
	emit := func(name string, data any) { runtime.EventsEmit(ctx, name, data) }
	a.ctx = foldersync.WithEmitter(assistant.WithEmitter(ctx, emit), emit)
	if a.problems != nil {
		a.problems.OnProblem = func(p problems.Problem) { emit("problem:new", p) }
	}
	bg, stop := context.WithCancel(foldersync.WithEmitter(context.Background(), emit))
	a.bg, a.stop = bg, stop
	a.every(bg, time.Hour, "backup.auto", nil)
	// Folder sync, if the person set it up: at start, every few minutes, and at quit (shutdown).
	a.every(bg, foldersync.AutoEvery, "sync.auto", func(res any) {
		if r, ok := res.(foldersync.AutoResult); ok {
			emit("sync:done", r)
			if r.Ran && r.Result != nil && (r.Result.Pulled.Changes > 0 || r.Result.Blobs.Downloaded+r.Result.Blobs.Linked+r.Result.Blobs.Removed > 0) {
				emit("library:changed", nil)
			}
		}
	})
	a.every(bg, rescanEvery, "library.rescan", func(res any) {
		if r, ok := res.(library.RescanResult); ok && r.Added+r.Changed+r.Removed > 0 {
			runtime.EventsEmit(ctx, "library:changed")
		}
		a.index()
		// Libraries from before book types existed get them once, without the user asking.
		a.typed.Do(func() {
			if _, err := a.reg.Run(bg, a.env, "facet.suggest", nil); err != nil && bg.Err() == nil {
				log.Printf("facet.suggest: %v", err)
			}
		})
	})
	a.startUpdates(ctx, bg)
	// Files and folders dropped on the window are added to the library by the interface.
	runtime.OnFileDrop(ctx, func(_, _ int, paths []string) { runtime.EventsEmit(ctx, "files:dropped", paths) })
	if a.shot != "" {
		takeScreenshot(ctx, a.shot)
	}
}

// quitSyncLimit bounds the last sync at quit, so closing the app is never slow.
const quitSyncLimit = 8 * time.Second

func (a *App) shutdown(context.Context) {
	a.stop()
	a.wg.Wait()
	if a.problems != nil {
		defer a.problems.Stop() // a normal quit: the next start must not see a crash
	}
	ctx, cancel := context.WithTimeout(context.Background(), quitSyncLimit)
	if _, err := a.reg.Run(ctx, a.env, "sync.auto", nil); err != nil {
		log.Printf("sync.auto: %v", err)
	}
	cancel()
	if _, err := a.reg.Run(context.Background(), a.env, "search.cancel", nil); err != nil {
		log.Printf("search.cancel: %v", err)
	}
	// With nothing running, close the Bible modules and the search indexes that are still open.
	a.reg.Close()
}

// index starts indexing new and changed books in the background; books become searchable one by
// one. It is cheap when nothing has changed, and it rebuilds index files of an older format.
func (a *App) index() {
	if a.bg == nil || a.bg.Err() != nil {
		return
	}
	if _, err := a.reg.Run(a.bg, a.env, "search.index", nil); err != nil {
		log.Printf("search.index: %v", err)
	}
}

// addsBooks reports whether a command can bring new books into the library.
func addsBooks(id string) bool {
	return strings.HasPrefix(id, "library.add") || id == "library.scan" || id == "library.relocate_folder"
}

// every runs a command at start-up and then at each interval until ctx ends: the scheduled backup
// (the command decides whether one is due) and the library rescan.
func (a *App) every(ctx context.Context, interval time.Duration, id string, done func(any)) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			res, err := a.runCommand(ctx, id, nil)
			if err == nil && done != nil {
				done(res)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

// Commands describes every command.
func (a *App) Commands() []command.Description { return a.reg.List() }

// Run runs a command with JSON parameters and returns its JSON result.
func (a *App) Run(id, params string) (string, error) {
	res, err := a.runCommand(a.ctx, id, json.RawMessage(params))
	if err != nil {
		return "", err
	}
	if addsBooks(id) {
		a.index()
	}
	a.afterRun(res)
	b, err := json.Marshal(res)
	return string(b), err
}

// libraryPath is library.db in the user's config folder, or GLOSSA_LIBRARY when set, as for the CLI.
func libraryPath() string {
	if p := appdir.Getenv("GLOSSA_LIBRARY"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Join(appdir.DataDir(dir, log.Printf), "library.db")
}

// memoryLimit is the Go heap's soft limit (ADR 0008) unless GOMEMLIMIT is set. The 10,000-book
// test peaks far below it (docs/performance.md); it makes the collector work harder before the
// app grows past its memory budget.
const memoryLimit = 300 << 20

func setMemoryLimit() int64 {
	if os.Getenv("GOMEMLIMIT") != "" {
		return debug.SetMemoryLimit(-1)
	}
	debug.SetMemoryLimit(memoryLimit)
	return memoryLimit
}

func main() {
	if i := slices.Index(os.Args, "mcp-assistant"); i > 0 {
		// Claude Code and Codex start the app's executable like this to read the library
		// (assistant.CLIAgent); no window opens.
		serveAssistantMCP(os.Args[1:i])
		return
	}
	setMemoryLimit()
	migrateWebViewData()
	lib := libraryPath()
	s, err := store.Open(lib)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	reg := all.Registry(filepath.Dir(lib), secrets.OS())
	// The problem log (T32): Glossa's own messages go there, a panic becomes a plain error, and a crash that
	// ends the process leaves a marker the next start finds. It never leaves the computer by itself.
	pl := problems.ForDir(filepath.Dir(lib))
	pl.SetLibrary(lib)
	pl.SetWebView(problems.WebView2Version())
	if err := pl.Start(); err != nil {
		log.Printf("problem log: %v", err)
	}
	pl.Install()
	slog.Info("Glossa started", "version", core.Version)
	defer pl.Stop()
	app := &App{reg: reg, env: &command.Env{Store: s}, shot: screenshotArg(os.Args[1:]), problems: pl}

	err = wails.Run(&options.App{
		Title:       "Glossa",
		Width:       1200,
		Height:      800,
		MinWidth:    420,
		MinHeight:   360,
		AssetServer: &assetserver.Options{Assets: frontend.Dist},
		OnStartup:   app.startup,
		OnShutdown:  app.shutdown,
		Bind:        []any{app},
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// serveAssistantMCP serves the assistant's read-only tools on standard input and output, for the
// command-line tool the assistant started. flags may hold -library <path>.
func serveAssistantMCP(flags []string) {
	lib := libraryPath()
	if i := slices.Index(flags, "-library"); i >= 0 && i+1 < len(flags) {
		lib = flags[i+1]
	}
	s, err := store.Open(lib)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	reg := all.Registry(filepath.Dir(lib), secrets.OS())
	if err := assistant.ServeMCP(context.Background(), reg, &command.Env{Store: s}, "app", os.Stdin, os.Stdout); err != nil {
		log.Print(err)
	}
}
