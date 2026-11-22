package csl

import (
	"os"
	"strings"
	"testing"
)

// TestDebugMacro renders one macro of a bundled style for one golden item:
// CSL_STYLE=chicago-notes-bibliography CSL_MACRO=author-note CSL_ITEM=edited go test -run TestDebugMacro -v
func TestDebugMacro(t *testing.T) {
	style, macro, item := os.Getenv("CSL_STYLE"), os.Getenv("CSL_MACRO"), os.Getenv("CSL_ITEM")
	if style == "" {
		t.Skip("debug helper")
	}
	p := goldenProcessor(t, style)
	p.prepare()
	r := newRenderer(p, p.style.citation, false, p.items[item])
	r.cite = &citeCtx{label: "page"}
	out := r.macro(macro)
	t.Logf("%q", strings.TrimSpace(plainOf(out)))
}
