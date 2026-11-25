# Quality pass audit (branch m3/qol)

How this was done: the real app (the e2e build, on a copy of the scratch library, fakes for Zotero, Any and
citation styles, no network) was driven through every page and settings section, and through create, rename and
delete of notes, folders, tags, sermons, lists and reading plans; the bin; opening a book; search; the
highlights page; history; backups; the sync, connections, citations, updates and model setup screens; and
Print or save. Pages were looked at at 1536 x 816 and 900 x 700, light and dark (the screenshots in
`frontend/e2e/shots/`, which is not committed; `qol-*.png` are the fixed states, `tour-*.png` the audit tour).
The checks that prove each fix are in `frontend/e2e/qol.e2e.mjs`.

Severity: **broken** (does not work), **annoying** (works, gets in the way), **cosmetic** (looks off).

## The three reported bugs (all broken, all fixed, each with a check that failed first)

| Bug | Cause | Fix |
| --- | --- | --- |
| Print or export does nothing | Print called `window.print()`. In the WebView2 window it showed no dialog and blocked the page (a probe call did not return in 8 s). The old check replaced `window.print` with a stub, so it never saw this. | Print makes a PDF (`export.make` with `print: true`, counted against the print limit, kept in a temporary `print` folder in the data folder and swept after a day) and opens it in the system's PDF viewer (`App.OpenFile`, PDFs only). The button says **Open PDF to print**; the dialog says where it went. Saving as PDF, Word, web page and Markdown was checked end to end with an injected save path: the file is written where the dialog said, starts like its format, and the dialog names the path. |
| Right sidebar cannot be closed on Library and Settings | The only button was in the Reader's top bar (Ctrl+. worked everywhere but nobody knows it). | A **Study tools** button in the rail, on every page; it shows pressed while the sidebar is open. The setting is the same remembered one as before. Checked on eight pages, with a reload. |
| Changing the assistant in Settings does not update the sidebar | The sidebar's chat read the assistant's settings only when it was shown. | Settings announces each saved change (`glossa:assistant-changed`); the sidebar re-reads at once when AI chat is the tool shown. |

## Problems found and fixed in this pass (each with a check that failed first)

| # | Severity | Steps | What happened | What should happen |
| --- | --- | --- | --- | --- |
| 1 | annoying | My work, Sermons, open a sermon, click **Sermons** in the left column | Nothing: the sermon stayed open (the address is rewritten to `#sermons`, so the click changes nothing). Only the small "My work" crumb went back. | The list of sermons. Fixed (`main.ts` tells a view when its own link is chosen again). |
| 2 | annoying | My work, delete a folder, then go to Sermons, Lists, Plans | The line `"X" went to the bin. Undo` stayed in the column on every page until the app was reloaded. | It goes away by itself after 15 seconds. Fixed. |
| 3 | cosmetic | Lists, Delete list | Asked "You can get it back from the change history", but the list goes to the Bin, where it has a Restore button. | Says it goes to the bin. Fixed. |

## Problems found in pass 1 and fixed in pass 2 (branch m3/qol2)

Each has a check in `frontend/e2e/qol2.e2e.mjs`, run first against the old build and seen to fail, then after the fix.
Screenshots are `frontend/e2e/shots/qol-<name>-<w>x<h>-<theme>.png` at 1536 x 816 and 900 x 700, light and dark.

| # | Severity | Cause | Fix | Check (failed first) |
| --- | --- | --- | --- | --- |
| 4 | annoying | The sermon view rewrote `#sermons/<id>` to `#sermons` as soon as it opened the sermon. | The open sermon stays in the address (a reload, Back and Forward return to it; the page's own Back clears it; My work remembers the open sermon like a folder). Choosing Sermons while one is open still shows the list. | "Sermons: the open sermon stays in the address" (the address was `#sermons`). Shots `sermon-address`. |
| 5 | annoying | The Reader's top-bar menu item called `menu.print?.()`, which does nothing for panes with nothing to print. | The item is disabled and a line under it says "Select a pane with a passage, book or note to print." | "Reader: Print or save in the menu is disabled" (it was enabled). Shots `print-disabled`. |
| 6 | annoying | Ctrl+P was handled only inside a pane that can print. | One rule on every page: the key is always taken (the window's own print is never reached). It prints what is open: the Reader's selected pane (Bible, book, and now a note or sermon in a pane), or the note or sermon open in My work. In the Reader with nothing printable it shows the same plain notice. On every other page nothing happens. | "Ctrl+P ..." (ten pages left the key to the window). |
| 7 | annoying | Notes, sermons, folders and lists each built their own notice (three different Undo words, only folders cleared it), lists and plans used native boxes. | One helper (`deletenotice.ts`, words in `deletewords.ts`): what the bin holds (notes, sermons, folders, lists) is deleted at once and says `"X" went to the bin.` with Undo (`"X" is back.`), cleared after 15 seconds. A reading plan cannot go to the bin (the bin has no plan type, ADR 0010 only purges what is in the bin), so it asks `Delete "X" for good? Reading plans are not kept in the bin, so it cannot be brought back.` and then says it was deleted for good. | "Delete: notes, sermons, folders and lists ..." (the list asked a native question and showed no notice). Replaces batch 1's list-question check. Shots `delete-notice`. |
| 8 | cosmetic | The Provider row was a non-wrapping flex row, so the value took the leftover few pixels. | The row wraps; the value takes the width (two lines at 900 wide with the sidebar open, was five). | "Settings, Model: the Provider row ..." (5 lines). Shots `provider-row`. |
| 9 | cosmetic | The page title and the card both said "Your data"; chips and switch sat in one run of inline items. | The card heading is "Export and import"; chips are a wrapping row with a gap, the switch is on its own line below. | "Settings, Your data" (two headings). Shots `your-data`. |
| 10 | cosmetic | The warning was always in the form. | It shows once something is typed in the password box. (The two boxes were already the same width, 512 px, so that half of the report was not reproduced.) | "Settings, Sync: the password warning ..." (visible with an empty box). Shots `sync-empty`, `sync-typed`. |
| 11 | cosmetic | Not found. About 25 views and flows were driven with response, console and resource-timing listeners (every page, every Settings section, the Study tabs, history, the Bin, notes, folders, sermons, lists and plans): no request answered 404 and no console error appeared. The one 404 that can be made is the window asking for `/favicon.ico`, which the app does not serve (a hand `fetch` of it logs exactly "Failed to load resource ... 404"). The first load is before the test can listen. | The page now names its own icon (a small inline SVG), so there is nothing to ask for. | "Loading the app logs no console error, and the page names its own icon" (no icon link before). Harmless either way; the cause was not seen directly. |

## New things noticed in pass 2 (not fixed)

| # | Severity | Steps | What happens | What should happen |
| --- | --- | --- | --- | --- |
| 12 | annoying | Reading plans, Delete | A plan is soft-deleted in the database (history keeps it), but the bin has no plan type, so the interface can only say "deleted for good" and cannot offer Undo or Restore. Its values stay readable in the history, which is not what ADR 0010 means by "for good". | Add plans to the bin (`bin.list`, `bin.restore`, `bin.purge`), then plans get the same notice and Undo as the others. A Go change in `internal/folder` and `internal/plan`. |
| 13 | cosmetic | Settings, Sync, type a password | The strength line reads "Strong. Strong." (the badge and the hint both say it). | Show the hint only when it adds something. |
| 14 | cosmetic | Settings, Model at 900 x 700 with the sidebar open | The settings column is about 260 px wide, so the explanation text and buttons stack in a narrow strip. | Let the right sidebar overlay or the settings list collapse at that width. |
| 15 | annoying | Library, Remove folder; Sync, Turn off; Bible catalogues, Remove a Bible; Connections, Remove a key | Still native `confirm()` boxes (outside the types in item 7, left as they were). | A plain in-app question like the plan one, or Undo where the thing can come back. |

Nothing else in the tour was broken: every page and settings section opened and filled, search (Bibles) found
and said "No matches", the notes, folder, sermon, list and plan flows worked, the bin restored, history showed
the title change, a backup was written, and the Library showed and opened the test books.

## Not covered here

The real operating-system dialogs (save-as, and the viewer that opens for Print) cannot be driven by the
tests: the save path and the opener are injected, so Windows launching a viewer from `rundll32` is checked by
reading and by Go tests of what may be opened, not seen. Zotero, Any, sync between two devices, importing from
Logos, highlights made by selecting text, and the Library, Books-citing-a-passage search tabs were left to the
existing checks (`connect`, `any`, `sync`, `logos`, `highlights`, `cite`); the audit did not go beyond them.

## Full real-app suite

One full run (no `--no-build`) after the fixes: 81 passed, 1 failed of 82 checks. The one failure was the new
"choosing Sermons" check itself (its link selector also matched a "Sermons from Logos" folder that an earlier
file in the full run leaves behind); the selector was made exact and that check then passed on its own. The
full suite was not run a second time. Go (`go vet`, `go test ./...`) and vitest (381 tests) passed.

### After pass 2

One full run (no `--no-build`, layout-sanity included): 89 passed, 0 failed of 89 checks (the seven new checks in
`qol2.e2e.mjs` passed first time in that run; each had been seen to fail on the old build before its fix, and the
batch 1 list-question check was replaced by the delete check). `go vet ./...`, `go test ./...` and `gofmt` were
clean; vitest 383 tests passed.
