# Scope of the real-app e2e suite

`npm run e2e` (in `frontend/`) drives the real desktop app. It is slow and depends on a real window, so it keeps
**one journey per area** and leaves logic to the Go and vitest tests. On 21 November the suite went from 153
checks to a core set (see the count at the end); the `layout-sanity.e2e.mjs` checks were added.

Run options: `--shuffle` (random file order, prints the seed), `--seed=N` (repeat an order), a word to run only
files or checks that contain it. Every file starts after `t.reset()`; a failed check prints a state dump.

## Kept, by area

Reader and books (`chapter`, `book`: EPUB and PDF in Read and Study, Scroll mode), Bible and book Bibles (`bibles`,
`bibles2`), Study grid (`layout`, `topbar`), notes and sermons (`smoke`, `markdown`, `formats`, `structure`,
`insert`, `tables`), highlights (`highlights`), ink (`ink`, `pdfink`), search (`bibles2`), library, Get Bibles and
SWORD install (`bibles`), From Logos and the import preview (`fromlogos`, `logos`, `zccel`), two-app sync (`sync`),
the assistant against a fake server (`assistant`), the M2 journey with export and print (`m2`), updates off by
default (`updates`), and the generic layout audit (`layout-sanity`).

## Removed, with the reason

Reasons: **Go** = the same rule is covered by Go tests; **vitest** = by the interface unit tests; **dup** = another
kept check goes through the same path; **minor** = a detail of a control another kept check already uses.

| Check | Reason |
| --- | --- |
| Assistant: a tool call is shown; a copyrighted passage is shortened; a personal-copy book is refused for an online provider | Go (the assistant's tools and licence limits) |
| Assistant: a draft is inserted into a note; Stop ends a reply | dup (streaming answer kept); Go (cancel) |
| CLI assistant: all five checks (Ready / Not found, Claude Code, Codex, Stop kills the tool, turned off) | dup of the fake-server chat; process handling is Go |
| A library book typed Bible is in the chooser; Changing the book's type away from Bible | dup of the verse-numbered Bible book check in `bibles2` |
| Insert → Verse text in a sermon (bibles2) | dup of Insert → Verse text in `insert` |
| Reading the same EPUB again reuses its tab | vitest (grid model) |
| Scroll mode: EPUB flows into the next chapter; link inside an EPUB; a book in a Study pane flows | dup of the PDF Scroll check and the Pages checks |
| Scroll mode: Go to page lands on that page of a PDF | dup of the section field check (T38b 7) |
| Book view is uncluttered (Aa, Highlights, Contents) | minor; layout-sanity audits the toolbar |
| Verse quote: a click puts the caret after it; Notes on this passage; Book highlight overlap | dup / Go (`internal/reader` book ranges) |
| Toolbar in a narrow pane; The / menu lists every format | layout-sanity (narrow toolbar); vitest for the menu list |
| Highlights: spans two verses; overlapping replaces; removing; inserting into a note | Go (highlight ranges, `highlight.create`) and the insert-highlight check in `insert` |
| Highlights: stay on their words after font change and Study mode; other Bible faint display; older whole-verse | Go (range mapping); presentation detail |
| Ink: exports as SVG; pen, highlighter and widths; finger; pen pressure; Handwritten note; sermons hold ink; phone width; cursive strokes | Go (export, stroke engine golden file), vitest (stroke engine); M2 covers export |
| Insert: Note link; the / menu; Rom 8:28 + Tab; line after a trailing quote; three drag-and-drop checks; a plain click on a link | dup of the Insert checks kept; the drag checks were the least reliable |
| T38b 8 (properties fold), T38b 5 (sermon passages), T38b 4 (Turn into) | vitest / Go (`document.turn_into`, passages) |
| Logos import: running it again; Undo | Go (`internal/logosimport` idempotence and undo) |
| M2 step 2 (reports), 4 (versions), 5 (saved list), 7 (reading plan) | Go (reports, versions, lists, plans) |
| Markdown: `##`, `###`, italic, underline, code, numbered list, quote, checkbox, divider, Backspace, paste | vitest (the Markdown rules); `# `, bold and bullet kept as the typing journey |
| PDF ink: alignment at every zoom; Scroll mode; Study pane; Print or export | dup of the first PDF ink check; Go (ink export golden file) |
| Structure: folding in sermons; sermon outline; strip; folded heading edited | dup of the note fold check; layout-sanity |
| Sync: a book added on A; All chosen; a note edited on both devices | Go (`internal/sync`) |
| Tables: `/table` sizes; right-click menu; column resize; minimum width | vitest / Go (table model) |
| Toolbar file (sticky toolbar jump, strip names, popovers): all three | layout-sanity (toolbar and popovers in a short window are audited); scroll logic is vitest |
| Top bar: another pane swaps controls; section number; reference field; narrow bar | layout-sanity (top bar audited narrow); dup |
| Updates: refused while off | Go (`internal/update`) |
