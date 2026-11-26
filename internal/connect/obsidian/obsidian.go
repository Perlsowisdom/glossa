// Package obsidian is the Obsidian connection (ADR 0014): it writes notes and sermons as Markdown files with
// front matter into a folder of an Obsidian vault, and brings Markdown files from the vault in as notes. It is
// all files: it never uses the network, never writes outside the vault folder the person chose, and never
// deletes a file.
package obsidian

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/connect"
)

// DefaultSubfolder is where notes and sermons go in the vault until the person chooses another place.
const DefaultSubfolder = "Glossa"

// Conn is the Obsidian connection. It implements connect.Connection.
type Conn struct{}

// New makes the Obsidian connection.
func New() *Conn { return &Conn{} }

func (c *Conn) ID() string   { return "obsidian" }
func (c *Conn) Name() string { return "Obsidian" }

func (c *Conn) SignIn() connect.SignIn { return connect.SignInFolder }

func (c *Conn) Capabilities() []connect.Capability {
	return []connect.Capability{connect.CanImport, connect.CanExport}
}

func (c *Conn) Sends() string {
	return "Glossa only reads and writes Markdown files in the Obsidian vault folder you choose (the notes and sermons you pick go into one subfolder of it, " +
		"and the files you point it at are read); it never uses the network and never deletes a file."
}

// Key is nil: the connection is a folder, not a key.
func (c *Conn) Key() *connect.KeyInfo { return nil }

// Test is used only when no library is open; the real check is TestEnv.
func (c *Conn) Test(_ context.Context, _ connect.Config) (connect.TestResult, error) {
	return connect.TestResult{Message: "Choose your Obsidian vault folder first."}, nil
}

// TestEnv checks the chosen folder: that it is chosen, is there and can be written to.
func (c *Conn) TestEnv(ctx context.Context, env *command.Env, _ connect.Config) (connect.TestResult, error) {
	if env == nil || env.Store == nil {
		return connect.TestResult{Message: "No library is open."}, nil
	}
	cfg, err := loadConfig(ctx, env)
	if err != nil {
		return connect.TestResult{}, err
	}
	if cfg.Vault == "" {
		return connect.TestResult{Message: "Choose your Obsidian vault folder below."}, nil
	}
	v, err := openVault(cfg.Vault)
	if err != nil {
		return connect.TestResult{Message: Explain(err)}, nil
	}
	if err := v.canWrite(); err != nil {
		return connect.TestResult{Message: Explain(err)}, nil
	}
	return connect.TestResult{OK: true, Via: "a folder on this computer",
		Message: fmt.Sprintf("Ready. Notes and sermons go into the “%s” folder of your vault.", displaySub(cfg.Subfolder))}, nil
}

// Explain says what a failure means and what to do, in plain words.
func Explain(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, fs.ErrNotExist):
		return "The vault folder is not there any more. Choose it again."
	case errors.Is(err, fs.ErrPermission):
		return "Glossa is not allowed to write in the vault folder. Check its permissions or choose another folder."
	}
	return strings.TrimPrefix(err.Error(), "obsidian: ")
}
