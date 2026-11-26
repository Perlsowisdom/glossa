// Package syncutil is what connections that keep a note in step with something outside Glossa share: a
// content hash, the rule for which side wins, and the excerpt that shows where two texts first differ.
// Readwise, Any and Obsidian all work the same way: each remembers what both sides looked like at the last
// sync, and compares today's state with that.
package syncutil

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Hash is a short fingerprint of parts. Parts cannot run into each other.
func Hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Action is what to do with a linked item.
type Action int

const (
	Nothing   Action = iota // neither side changed
	KeepOther               // only the other side changed: bring its text in
	KeepOwn                 // only Glossa changed: send its text out
	Ask                     // both changed: the person decides
)

// Decide compares each side with the fingerprint taken at the last sync. A side whose last fingerprint is
// unknown (empty) counts as changed, unless the two sides are equal now (same is true).
func Decide(glossaNow, glossaThen, otherNow, otherThen string, same bool) Action {
	if same {
		return Nothing
	}
	g := glossaThen == "" || glossaNow != glossaThen
	o := otherThen == "" || otherNow != otherThen
	switch {
	case g && o:
		return Ask
	case g:
		return KeepOwn
	case o:
		return KeepOther
	}
	return Nothing
}

// FromDifference cuts both texts at the line where they first differ, so excerpts show what is different.
func FromDifference(a, b string) (string, string) {
	ra, rb := []rune(a), []rune(b)
	i := 0
	for i < len(ra) && i < len(rb) && ra[i] == rb[i] {
		i++
	}
	for i > 0 && ra[i-1] != '\n' {
		i--
	}
	pre := ""
	if i > 0 {
		pre = "… "
	}
	return pre + string(ra[i:]), pre + string(rb[i:])
}

// Excerpt keeps the first max characters of s, trimmed.
func Excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}
