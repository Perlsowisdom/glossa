// Package all builds the registry with every Glossa command, so the CLI, the MCP server and the
// desktop app offer exactly the same features.
package all

import (
	"path/filepath"

	"github.com/Perlsowisdom/glossa/internal/assistant"
	"github.com/Perlsowisdom/glossa/internal/backup"
	"github.com/Perlsowisdom/glossa/internal/bible"
	"github.com/Perlsowisdom/glossa/internal/bible/repos"
	"github.com/Perlsowisdom/glossa/internal/ccel"
	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/core"
	"github.com/Perlsowisdom/glossa/internal/connect"
	"github.com/Perlsowisdom/glossa/internal/connect/zotero"
	"github.com/Perlsowisdom/glossa/internal/csl"
	"github.com/Perlsowisdom/glossa/internal/document"
	"github.com/Perlsowisdom/glossa/internal/export"
	"github.com/Perlsowisdom/glossa/internal/folder"
	"github.com/Perlsowisdom/glossa/internal/integrations/apibible"
	"github.com/Perlsowisdom/glossa/internal/integrations/dbl"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/lists"
	"github.com/Perlsowisdom/glossa/internal/logosimport"
	"github.com/Perlsowisdom/glossa/internal/plan"
	"github.com/Perlsowisdom/glossa/internal/reader"
	"github.com/Perlsowisdom/glossa/internal/report"
	"github.com/Perlsowisdom/glossa/internal/search"
	foldersync "github.com/Perlsowisdom/glossa/internal/sync"
	"github.com/Perlsowisdom/glossa/internal/update"
)

// Registry returns every command. dataDir is the folder holding library.db; Bible modules live in
// its modules subfolder, Bible catalogues in catalogues and search indexes in index.
func Registry(dataDir string, s secrets.Store) *command.Registry {
	r := command.NewRegistry()
	core.Register(r)
	library.Register(r)
	bibles := bible.NewLibrary(filepath.Join(dataDir, "modules"))
	r.OnClose(bibles.Close)
	bible.RegisterLibrary(r, bibles)
	repos.Register(r, filepath.Join(dataDir, "catalogues"), bibles)
	apibible.Register(r, s)
	dbl.Register(r, s)
	search.Register(r, dataDir)
	backup.Register(r, dataDir)
	reader.Register(r)
	export.Register(r, dataDir, bibles)
	lists.Register(r)
	plan.Register(r)
	report.Register(r)
	document.Register(r)
	folder.Register(r)
	ccel.Register(r, filepath.Join(dataDir, "ccel"))
	logosimport.Register(r)
	assistant.Register(r, s)
	foldersync.Register(r, s, foldersync.Locations{BooksDir: filepath.Join(dataDir, "synced-books"),
		ModulesDir: filepath.Join(dataDir, "modules"), ReleaseModule: bibles.Release})
	z := zotero.New()
	z.Register(r, connect.Register(r, s, z))
	csl.Register(r, filepath.Join(dataDir, "styles"))
	update.Register(r) // nothing in the Store build
	return r
}
