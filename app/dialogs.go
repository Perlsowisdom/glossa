package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// PickFile shows the system's open-file dialog and returns the chosen path, or "" if cancelled.
// It only returns a path; what is done with it is up to a command.
func (a *App) PickFile(title string) (string, error) {
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: title})
}

// PickFiles shows the open-file dialog for several book files and returns the chosen paths.
func (a *App) PickFiles(title string) ([]string, error) {
	return wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{Title: title, Filters: []wruntime.FileFilter{
		{DisplayName: "Books (EPUB, PDF, Word, HTML, Markdown, text)", Pattern: "*.epub;*.pdf;*.docx;*.html;*.htm;*.xhtml;*.md;*.markdown;*.txt"},
		{DisplayName: "All files", Pattern: "*.*"},
	}})
}

// PickFolder shows the system's choose-folder dialog and returns the chosen path, or "".
func (a *App) PickFolder(title string) (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: title, CanCreateDirectories: false})
}

// SaveFile shows the system's save-file dialog for an export and returns the chosen path, or "".
// format is the file extension, such as pdf or docx; name is the suggested file name.
func (a *App) SaveFile(title, name, format string) (string, error) {
	label := map[string]string{"pdf": "PDF", "docx": "Word document", "html": "Web page", "md": "Markdown"}[format]
	if label == "" {
		label = format
	}
	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: title, DefaultFilename: name,
		Filters: []wruntime.FileFilter{{DisplayName: label + " (*." + format + ")", Pattern: "*." + format}}})
}

// OpenFile opens a PDF in the system's own viewer, where it can be read and printed. The Wails WebView
// cannot reliably print a page itself (window.print can freeze the window), so Print makes a PDF and
// hands it over. Only an existing PDF file is opened, so the page cannot start other programs.
func (a *App) OpenFile(path string) error {
	cmd, err := openCommand(path)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", filepath.Base(path), err)
	}
	go cmd.Wait() // reap the helper; the viewer outlives it
	return nil
}

// openCommand is the system command that opens a PDF in its default viewer.
func openCommand(path string) (*exec.Cmd, error) {
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
		return nil, errors.New("only a PDF can be opened here")
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("that file is not there")
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path), nil
	case "darwin":
		return exec.Command("open", path), nil
	default:
		return exec.Command("xdg-open", path), nil
	}
}
