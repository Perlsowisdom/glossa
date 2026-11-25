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

## Problems found and not fixed (for the next batch)

| # | Severity | Steps | What happens | What should happen |
| --- | --- | --- | --- | --- |
| 4 | annoying | Open a sermon from My work (`#sermons/<id>`), reload the app | The address drops the sermon (it becomes `#sermons`), so a reload, or Back, does not return to that sermon. | The address keeps the sermon while it is open. (Needs the sermon view to keep and clear it; the checks that open sermons by address should be run after.) |
| 5 | annoying | Reader, ⋯, **Print or save…**, with a pane selected that has nothing to print | Nothing happens and nothing is said (`menu.print?.()` is skipped). | Say "Select a pane with a passage, book or note to print." (code reading; not run) |
| 6 | annoying | Ctrl+P on a page with no printing (Settings, Library) | Not checked in the app. The key is handled only inside a pane that can print; elsewhere the web view's own print may react, and `window.print()` was found to block the window. | Ctrl+P is ignored (or opens Print or save for what is open) on every page. |
| 7 | annoying | Delete a reading plan or a list, remove a book folder, turn off sync | Native `confirm()` boxes, while notes, sermons and folders delete at once with Undo and a Bin. Plans are not in the Bin ("progress is kept in its history"), lists are. Three different ways to delete. | One way: delete, tell, Undo. (The boxes could only be accepted, not looked at, in the test window.) |
| 8 | cosmetic | Settings, Model, with the right sidebar open, at 900 x 700 | The Provider value ("Claude Code, signed in on this computer") wraps over five lines in a narrow column. | Let the value use the row's width, or hide the long form. See `qol-assistant-sync-on-900x700-dark.png`. |
| 9 | cosmetic | Settings, Your data, at 900 x 700, dark | The page title and the card both say "Your data"; the "Documents and sermons" chip touches the "Include the change history" switch. | One heading; a gap before the switch. |
| 10 | cosmetic | Settings, Sync, before typing anything | The yellow "Keep this password safe" warning shows with an empty password; the password box is narrower than the folder box. | Show the warning once a password is typed; same width. |
| 11 | cosmetic | A console error "Failed to load resource: 404" is logged once while working in notes, folders, sermons, lists and plans | Not tracked down (no visible effect). | None. |

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
