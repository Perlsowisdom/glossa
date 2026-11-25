package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCommandOnlyOpensAnExistingPDF(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "a.pdf")
	exe := filepath.Join(dir, "a.exe")
	os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644)
	os.WriteFile(exe, []byte("x"), 0o644)
	if _, err := openCommand(pdf); err != nil {
		t.Errorf("a PDF should open: %v", err)
	}
	if _, err := openCommand(exe); err == nil {
		t.Errorf("a program must not be opened")
	}
	if _, err := openCommand(filepath.Join(dir, "missing.pdf")); err == nil {
		t.Errorf("a missing file must be refused")
	}
	if _, err := openCommand(dir); err == nil {
		t.Errorf("a folder must be refused")
	}
}
