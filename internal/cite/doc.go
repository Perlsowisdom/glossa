// Package cite turns what a person cites into sources the CSL engine (internal/csl) can render:
// library books from their catalogue details, Bible versions with SBL book abbreviations, Zotero
// items (kept in cite_items when cited, so a document still renders offline) and manual entries.
// It renders a document's citations and bibliography in a style, and keeps the person's choice of
// style and the optional download of more styles.
//
// A citation in the editor's Markdown is Pandoc-like: [see @lib:01H..., p. 12, for more]. See
// markdown.go for the exact form; the editor (frontend/src/editor/markdown.ts) reads and writes the
// same one.
package cite
