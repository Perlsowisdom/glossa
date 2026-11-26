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

## Items 12 to 15 fixed in pass 3 (branch m3/qol3)

Each has a check in `frontend/e2e/qol3.e2e.mjs` (or `layout-sanity.e2e.mjs`), run first against the old code and seen to
fail, then after the fix. Screenshots are `frontend/e2e/shots/qol-<name>-<w>x<h>-<theme>.png` at 1536 x 816 and 900 x 700,
light and dark (`plan-bin-notice`, `plan-in-bin`, `sync-strength`, `own-dialog`; the Settings audit saves
`ls-settings-900x700-<theme>-<section>-<open|closed>.png`).

| # | Severity | Cause | Fix | Check (failed first) |
| --- | --- | --- | --- | --- |
| 12 | annoying | The bin knew four kinds (folder, note, document, list); a reading plan was soft-deleted with nowhere to be restored from, so batch 2 asked a native "for good" question. | `reading_plans` joins the bin: `bin.list` returns `kind: plan`, `bin.restore` brings it back, `bin.purge` removes the plan and its days, readings and progress rows and erases their history values (ADR 0010). **No schema change**: `reading_plans` already had `deleted_at`, and sync carries a delete, restore (update of `deleted_at`) and purge of a plan and its child rows like the other types. The Plans page uses the shared notice and Undo; the Bin page names it "Reading plan". | Go: `TestPlanGoesToBinRestoresAndPurges`, sync: `TestPlanBinRestoreAndPurgePropagate`. Real app: "Reading plans: Delete goes to the bin ..." (the old build asked a native question and never showed the notice). Replaces the plan half of the batch 2 delete check. |
| 13 | cosmetic | The badge says "Strong" and the hint said "Strong." as well; weaker ones read "Weak Weak: ...". | `strengthAdvice` drops the word the badge already says: "Strong" and "Good" stand alone, "Weak" is followed by the advice only. | vitest `strengthAdvice`; real app "Settings, Sync: the password strength line says each word once" (read "Strong Strong."). |
| 14 | annoying | The page list took 170 to 210 px beside the settings, so with the study tools open (and a 900 px window) the column was 243 px and rows squeezed their text to 0 to 150 px. | The Settings page is a container: below 640 px wide the page list goes on top (a row per group) and each setting's text and control wrap. | New rows in `layout-sanity.e2e.mjs`: every section at 900 x 700, sidebar open and closed, light and dark; column at least 400 px, no sideways scroll, no text block of 30+ characters narrower than 140 px (28 problems before, 0 after). |
| 15 | annoying | Library remove folder, Bible remove, Connections and API.Bible key removal, Sync turn off and forget device, merge an export, delete a collection, Logos import and undo, and turn off the Bible catalogues called the browser's `confirm()`. | A new `ask()` dialog (`ask.ts`, the one the Bin already used) asks in plain words for removing a folder, a Bible or a key, turning sync off, forgetting a device and merging an export. Deleting a collection (it already offered Undo), taking a Logos import back (it goes to the bin), the Logos import (the page now says what it does) and turning off the catalogues (the switch turns them on) just do it. | vitest `nonative.test.ts` (11 hits before) and a real-app row that reads the source; "No native box: Library, Sync, Bibles and Settings ..." (no dialog opened before); the Connections Remove key check in `connect.e2e.mjs` records any native dialog. |

## New things noticed in pass 3 (not fixed)

| # | Severity | Steps | What happens | What should happen |
| --- | --- | --- | --- | --- |
| 16 | cosmetic | Delete a collection | It says `Deleted X. Undo` and brings it back from the change history; the other things say `"X" went to the bin.` | Collections as a bin type, so the notice matches. A Go change like item 12. |
| 17 | cosmetic | Go test `TestDeleteFolderGoesToBinWithItsContents`, rarely | A note deleted in the same millisecond as its folder is counted as deleted with it (the bin compares `deleted_at >= `), so the test failed once in about ten runs. | A note deleted on its own just before its folder should not depend on the clock tick. Not seen by a person. |
| 18 | cosmetic | `plan.mark`, `plan.unmark` or `plan.reschedule` with the id of a plan in the bin | The write happens, then the command answers "no reading plan". | Refuse before writing. Command-line and MCP only. |
| 19 | cosmetic | The batch 2 "Delete ..." check, run alone with a word filter | It needs the Sermons table an earlier check leaves behind and times out. It passes in the file or the full run. | Make it set up what it needs. |
| 20 | cosmetic | Sync, Forget this device | The question is now the app's own dialog but only the code and the vitest grep cover it: it needs a second device. | A two-device check (the `sync` file already has both). |

### After pass 3

One full run (no `--no-build`, layout-sanity included): 95 passed, 0 failed of 95 checks (six new: the plan bin, the
strength line, the native-box check, the source check, and the two Settings layout rows). `gofmt`, `go vet ./...` and
`go test ./...` were clean; vitest 384 tests passed.

## Items 1 to 6 of batch 4, and 16 to 20 above, fixed in pass 4 (branch m3/qol4)

Each has a check, run first against the old code and seen to fail (item 20 is a new check for something that worked; it was
shown able to fail by putting the browser's `confirm()` back). Real-app checks are in `frontend/e2e/qol4.e2e.mjs`.
Screenshots are `frontend/e2e/shots/qol-<name>-<w>x<h>-<theme>.png` at 1536 x 816 and 900 x 700, light and dark
(`bin-count`, `collection-bin-notice`, `collection-in-bin`).

| # | Severity | Cause | Fix | Check (failed first) |
| --- | --- | --- | --- | --- |
| Bin count (batch 4) | annoying | The Bin entry in My work only counted again when a notes, sermons or folder action told it to. Deleting a reading plan or a list from its own page did not, so the entry said 29 while the Bin page (which asks again every time) said 30. | The shared delete notice (`binned`) tells the column on every delete and every Undo, for every kind, and the column counts again each time it is shown. Both numbers still come from `bin.list`. | Real app "Bin: the count beside Bin ... agrees with the Bin page" (read 0 beside a bin of 1). |
| 16 | cosmetic | Collections had no bin type; Delete said `Deleted X. Undo` and undid from the change history. | `collection` is a bin kind: `collection.delete` already soft-deleted (`collections.deleted_at` existed), so `bin.list` shows it (with the collections inside it), `bin.restore` brings it back with them, and `bin.purge` removes the collections, their book links and the history values (ADR 0010). **No schema change, no new migration.** Sync carries delete, restore and purge like the other kinds. The Library uses the shared notice and Undo; the Bin names it "Collection". | Go `TestCollectionGoesToBinRestoresAndPurges`, sync `TestCollectionBinRestoreAndPurgePropagate`, vitest, and real app "Collections: Delete goes to the bin ..." (old build showed the old notice). |
| 17 | cosmetic | "Deleted together" is inferred from `deleted_at >= the folder's`, and `deleted_at` was the clock in whole milliseconds, so a note deleted just before its folder, in the same millisecond, tied with it and came back with it. This was a real (rare) behaviour bug, not only a test problem. | Soft deletes are stamped strictly later than the previous one (`Store.deletedMS`), so a tie cannot happen; things deleted together are stamped in order, each no earlier than the first. | `TestNoteDeletedJustBeforeItsFolderStaysSeparateOnAFrozenClock` (a frozen delete clock: failed before, deterministic); `TestDeleteFolderGoesToBinWithItsContents` run with `-count=200`: all pass. |
| 18 | cosmetic | `plan.mark` and `plan.unmark` wrote to a plan in the bin; `plan.delete` of a binned plan wrote again. `plan.reschedule` already refused. | A plan in the bin is refused before anything is written ("that reading plan is in the bin; restore it first"). | Go `TestNothingInTheBinCanBeWrittenTo`: every write command for plans, notes, sermons, folders, lists and collections on something in the bin must fail and write no change (failed for plan.mark, plan.unmark, plan.delete; the other kinds already refused). |
| 19 | cosmetic | After the sermon went to the bin the check waited for the sermons table, which only exists when a sermon is left; an earlier check usually left one. | The check starts with `t.reset()`, makes its own data and waits for the page, not the table. | Run alone with a word filter: timed out before, passes after. |
| 20 | cosmetic | Nothing covered Sync, Forget this device. | New two-app check: the app's own question, Cancel changes nothing, Forget removes the device from the folder and the list, and the forgotten device's sync is refused. Temporary folders only. | "Sync, Forget this device ..." in `qol4.e2e.mjs`. |

## New things noticed in pass 4 (not fixed)

| # | Severity | Steps | What happens | What should happen |
| --- | --- | --- | --- | --- |
| 21 | cosmetic | Open the Library with no books, with the Collections column showing | The "Traditions and rules" fold sits in the middle of the page, under the empty state, apart from its column. | Keep it with the Collections column or hide it when there is nothing to describe. |
| 22 | cosmetic | A bin of more than 500 things | `bin.list` stops at 500 by default, so the Bin entry and the Bin page both say 500 (they agree, but are wrong). | Count separately from the list, or page the list. |
| 23 | cosmetic | A device that has not sent any change yet, in the folder | It is not listed under Devices (a device shows up when it has written a file), so it cannot be forgotten. | List a device as soon as it has joined. |
| 24 | cosmetic | Design | Which things were deleted with a folder or collection is worked out from times, not stored. Fixed so ties cannot happen (17), but a stored "deleted with" link would be sturdier. | A column or change field naming the deletion batch. Needs a schema change and an ADR. |

### After pass 4

One full run (no `--no-build`, layout-sanity included): 97 passed, 1 failed of 98 checks. The failure was the new bin-count
check itself (its Undo selector matched a hidden notice from another page); the selector was scoped to that notice and the
file's check then passed on its own. The batch 2 delete check was also run alone with a word filter and passed. `gofmt`,
`go vet ./...` and `go test ./...` were clean; vitest 385 tests passed.

## Items 21 to 24, fixed in pass 5 (branch m3/qol5)

Each has a check, run first against the old code and seen to fail (the Go checks for 22 and 23 failed to build, because
the command and the method they test did not exist yet; item 21 failed in the real app before its fix). Real-app checks are
in `frontend/e2e/qol5.e2e.mjs`. Screenshots are `frontend/e2e/shots/qol-<name>-<w>x<h>-<theme>.png` at 1536 x 816 and
900 x 700, light and dark (`library-empty`, `bin-many`), plus `qol5-joined-device.png`.

| # | Severity | Cause | Fix | Check (failed first) |
| --- | --- | --- | --- | --- |
| 21 | cosmetic | The Traditions and rules fold is the last thing in the Library's catalogue column, so with no books it sat under the empty state, apart from the Collections column. | It is hidden while the whole library has no books (there is nothing for traditions and rules to describe); it shows as soon as there is a book, and in any folder or collection of a library that has books. | Real app "Library, empty with the Collections column ..." (a second app on a book-less copy: the fold was visible in the catalogue column). Shots `library-empty`. |
| 22 | cosmetic | `bin.list` stops at 500 and both numbers were its length. | New `bin.count` (the true number) and an `offset` on `bin.list` (pages of 500). The Bin entry in My work reads `bin.count`; the Bin page shows the true count and loads 200 at a time with **Show more (N left)**; the pages already open stay open when something is restored. | Go `TestBinCountIsTrueAndTheListPages` (620 things: count, first page 500, the rest by offset, no overlap, order). Real app "Bin: a bin of more than 500 things ..." (510 notes deleted: both numbers, the pages, Show more, Restore; the bin is emptied again afterwards). Shots `bin-many`. |
| 23 | cosmetic | A device showed in the folder only once it had written a change file. | Joining (`sync.setup`, and every sync) leaves a small encrypted marker, `joined`, in the device's folder holding its name (kind `J`, like a change file: nothing is readable in the folder). `Devices` lists a device that has only joined, with 0 files; the first-sync preview does not count it as data; forgetting it removes it, and joining again does not bring a forgotten device back. | Go `TestDeviceIsListedAsSoonAsItHasJoined`. Real app "Sync: a device that has joined but not written anything ..." (two apps: B only sets up, A lists, forgets it, B's sync is refused). Shot `qol5-joined-device.png`. |
| 24 | cosmetic | "Deleted together" was worked out from `deleted_at` times. | **Migration 0017** adds `deletion_batch` to folders, notes, documents and collections. `folder.delete` and `collection.delete` stamp one new id on everything they delete (set just before the delete, in the same write). `bin.list`, `bin.restore` and `bin.purge` use the batch; a folder or collection with no batch (deleted before 0017) uses the old time rule. Restoring clears the batch. It syncs as an ordinary field change (not per-device). ADR 0015, status Proposed. | Go: `TestRestoreOfAFolderBringsBackExactlyItsBatch` (a note in the folder stamped later but not in the batch stays in the bin; the old rule brought it back), `TestRestoreOfAnOldFolderFallsBackToTheTimeRule`, `TestCollectionRestoreBringsBackExactlyItsBatch`, sync `TestDeletionBatchRoundTrips` (batch reaches the other device, restore there brings back exactly the batch). |

## New things noticed in pass 5 (not fixed)

| # | Severity | Steps | What happens | What should happen |
| --- | --- | --- | --- | --- |
| 25 | cosmetic | Bin page with more than one page of things, delete or restore something in another window or by sync, then Show more | The list is paged by offset, so a page can repeat or skip a row when the bin changes between two loads (the count is always right). | Page by "older than the last row shown" instead of by offset, or reload the open pages on any change (it does already for this window's own Restore and Delete). |
| 26 | cosmetic | Rename a device in Sync, before it has written a change file | Other devices list it by the name in its `joined` marker, which is written once, so they show the old name until the device writes a change file (which carries the current name). Each sync also tries to create the marker again (a cheap, harmless no-op). | Rewrite the marker when the device is renamed, and skip the attempt once it has written a change file. |
| 27 | cosmetic | A new library with no books: Library | The Traditions and rules fold is hidden until the first book is added (item 21), so traditions and rules cannot be set up before importing. | Move the fold to Settings (or the Collections column) so it is reachable any time. |
| 28 | cosmetic | Restore a folder deleted before migration 0017, in a library where times were rewritten by sync | Old rows have no batch and still use the time rule, so the old rare mix-up (a note deleted separately at nearly the same time as the folder on another device) can still happen for them. | Nothing is planned: such folders leave the bin when it is emptied. Mentioned in ADR 0015. |

### After pass 5

One full run (no `--no-build`, layout-sanity included): 101 passed, 0 failed of 101 checks (three new in `qol5.e2e.mjs`). `gofmt`,
`go vet ./...` and `go test ./...` were clean; vitest 385 tests passed. Migration 0017 is the new schema version.

## Items 25 to 27, fixed in pass 6 (branch m3/qol6)

Each has a check, run first against the old code and seen to fail. Item 25 and 26 have Go checks; item 27 has real-app checks
in `frontend/e2e/qol6.e2e.mjs`. Screenshots are `frontend/e2e/shots/qol-<name>-<w>x<h>-<theme>.png` at 1536 x 816 and
900 x 700, light and dark (`qol6-library-empty`, `qol6-library-books`), plus `qol6-library-empty.png` and `qol6-library-books.png`.
No schema change.

| # | Severity | Cause | Fix | Check (failed first) |
| --- | --- | --- | --- | --- |
| 25 | cosmetic | `bin.list` paged by offset, so a page could repeat or skip a row when the bin changed between loads. | `bin.list` takes `after_deleted_at` and `after_id` ("older than the last row shown": deleted time, then id as the tiebreak). The Bin page's Show more uses them and appends; the count stays `bin.count`. `offset` still works but is no longer used by the page. | Go `TestBinPagesByCursorWhileTheBinChanges` (30 things with ties; two restored and one newer deleted between the pages: no repeat, no gap, true count). Before the fix it failed with `unknown field "after_deleted_at"` (the cursor did not exist). |
| 26 | cosmetic | The `joined` marker held the name once, and every sync tried to create it again. | `Engine.join` reads the marker: the same name means nothing is written; a different name replaces it (remove, then create); none creates it. | Go `TestRenamedDeviceIsListedUnderTheNewNameBeforeAnyChangeFile` (failed: the other device listed "Desktop", not the new name) and `TestSyncWritesNoJoinMarkerWhenNothingChanged` (failed: 4 creates on two syncs and two joins, want 0). |
| 27 | cosmetic | The fold was hidden in an empty Library and, with books, sat under the book list. | The Traditions and rules fold is in the Collections column, under My collections and its New collection button, whether or not there are books. | Real app "Library, empty: the Traditions and rules fold is in the Collections column ..." (failed: fold not shown) and "Library, with books ..." (failed: fold not in the Collections column). Both pass, and a tradition can be added and removed before any import. |

### After pass 6

One full run (no `--no-build`, layout-sanity included): 103 passed, 0 failed of 103 checks (two new in `qol6.e2e.mjs`). `gofmt`,
`go vet ./...` and `go test ./...` were clean; vitest 385 tests passed.

## New things noticed in pass 6 (not fixed)

| # | Severity | Steps | What happens | What should happen |
| --- | --- | --- | --- | --- |
| 29 | cosmetic | Rename a device in Sync while another device lists devices at that moment | The marker is replaced by remove then create, so for a moment the folder holds no marker for it (the device then shows under its id until the new one lands). | Write the new marker under a temporary name and rename over the old one (needs a replace call on the transport). |
| 30 | cosmetic | Library with no books, open Traditions and rules, press "Look again at books without a type" | Not checked: the button now shows in an empty library, where there is nothing for it to look at. | Hide it or say there are no books. |

## Items 29 and 30, fixed in pass 7 (branch m3/qol7)

Each has a check, run first against the old code and seen to fail. Item 29 has Go checks; item 30 has real-app checks in
`frontend/e2e/qol7.e2e.mjs`. Screenshots are `frontend/e2e/shots/qol-<name>-<w>x<h>-<theme>.png` at 1536 x 816 and 900 x 700,
light and dark (`qol7-library-empty`, `qol7-library-books`), plus `qol7-library-empty.png` and `qol7-library-books.png`.
No schema change.

| # | Severity | Cause | Fix | Check (failed first) |
| --- | --- | --- | --- | --- |
| 29 | cosmetic | A rename replaced the `joined` marker by remove then create, so between the two steps the folder held no marker. | New transport call `Replace(from, to)` (rename over an existing file; `FolderTransport` uses `os.Rename`, which replaces an existing file on Windows too; the only other implementations in tests embed the transport). `Engine.join` writes the new marker as `joined.new` and renames it over `joined`. Listings ignore any file that is not a change file or snapshot, and `join` removes leftover `joined.new` and `.tmp-*` files in the device's own folder. | Go `TestRenameNeverLeavesTheDeviceWithoutAName` (failed: after the remove, another device listed the device under its id, "01M8C89M..."; passes: always "Desktop" or "Study PC") and `TestLeftoverMarkerTempFilesAreIgnoredAndCleanedUp` (failed: leftover `.tmp-123` stayed). |
| 30 | cosmetic | The button was always added to the fold. Before the fix, pressing it in an empty library ran the lookup and said "Set 0 books automatically (0 had nothing to go on). Anything you chose yourself was left alone." | The button is hidden while the whole library (not just the folder or collection on screen) has no books, and shows as soon as one is added. | Real app "Library, empty: ... no Look again button" (failed: button shown) and "Library, with books: the Look again button shows ..." (passes before and after). |

### After pass 7

One full run (layout-sanity included): 105 checks, 103 passed on the first pass; the two Any checks failed because an earlier full run was still alive in the background and shared the app and its fake server, and both passed when `any.e2e.mjs` was rerun alone (4 of 4). `qol7.e2e.mjs` adds two checks. `gofmt`, `go vet ./...` and `go test ./...` were clean; vitest 385 tests passed.

## New things noticed in pass 7 (not fixed)

| # | Severity | Steps | What happens | What should happen |
| --- | --- | --- | --- | --- |
| 31 | cosmetic | A device crashes between writing `joined.new` and renaming it, and never syncs again | The temporary file stays (it is ignored and harmless, but counts for nothing) until that device next joins. | Nothing planned; another device could clean it up when it forgets the device (Forget already removes the whole folder). |
| 32 | cosmetic | Library, a folder or collection selected, many books | The button's visibility asks `library.items` for one book each time the list refreshes (a cheap extra call). | Keep the whole-library count from the facet vocabulary or the folders list instead. |
