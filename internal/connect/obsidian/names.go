package obsidian

import (
	"fmt"
	"strings"
	"unicode"
)

const maxNameRunes = 100 // leaves room for " (2)" and ".md" within a path Windows accepts

// A name a Windows program cannot use for a file, whatever its extension.
var reserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CLOCK$": true}

func isReserved(base string) bool {
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	base = strings.ToUpper(strings.TrimRight(base, " "))
	if reserved[base] {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		return true
	}
	return false
}

// safeName turns a title into a file name (without the .md) that is allowed on Windows, macOS and Linux and
// that Obsidian can link to: no \ / : * ? " < > |, no # ^ [ ] (they break links), no control characters, no dots or
// spaces at the end, not a reserved name, and not too long.
func safeName(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case strings.ContainsRune(`\/:*?"<>|#^[]`, r):
			b.WriteRune('-')
		case unicode.IsControl(r), r == ' ', r == ' ':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	name := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(name); len(r) > maxNameRunes {
		name = string(r[:maxNameRunes])
	}
	name = strings.TrimRight(name, ". ")
	name = strings.TrimLeft(name, ". ") // a leading dot would hide it
	if name == "" {
		name = "Untitled"
	}
	if isReserved(name) {
		name = "_" + name // a leading mark: a trailing one would leave "nul.txt_" reserved
	}
	return name
}

// claims keeps the names given out in one run, so two notes with the same title get different files.
type claims map[string]bool

func fold(p string) string { return strings.ToLower(p) }

// newPath picks a free path in dir for a file called name: "dir/name.md", then "dir/name (2).md" and so on. A path
// is taken when something is on disk there (any case) or another item in this run has it.
func (c claims) newPath(v *vault, dir, name string) string {
	for n := 1; ; n++ {
		base := name
		if n > 1 {
			base = fmt.Sprintf("%s (%d)", name, n)
		}
		rel := base + ".md"
		if dir != "" {
			rel = dir + "/" + rel
		}
		if !c[fold(rel)] && !v.exists(rel) {
			c[fold(rel)] = true
			return rel
		}
	}
}
