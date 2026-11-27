# T41: Follow-ups pass (desktop polish)

Brief for one agent (Sonnet), 27 November 2026. The user wants the desktop app ironed out before any
mobile or KOReader work. Read these first:
- `AGENTS.md`;
- `docs/tasks/followups.md` (the list this pass works through);
- `docs/editor-behaviour.md`, `docs/ui-requirements.md` and `docs/qol-audit.md`.

## Step 1: check the list against the code

`followups.md` is partly stale. Some items were fixed by later work: for example, native `confirm()`
boxes look gone (`frontend/src/ask.ts`), and sermons may already show a Preached chip.

- Go through every **Open** item and check it in the code, or in the real app where it is visible.
- Move each fixed item to **Fixed**, with one line saying what fixed it.
- Leave each item you could not settle in place, with a short note. Don't fix anything in this step.
- Commit.

## Step 2: fix, in this order

Stop after any item if the work grows far beyond the item; note it and go on. Commit after each item
(or each small group), and move it to Fixed in `followups.md`.

1. **Sermons:**
   - The editor shows and edits **date preached** and **theme**. `document.update` already takes
     `preached_on` and `theme`. Check what is there and finish it.
   - The two must be easy to see and set, without crowding the toolbar.
2. **App dialogs everywhere:**
   - No "wails.localhost says" box may be left anywhere: no `confirm`, `alert` or `prompt`, including
     in rarely used paths.
   - Use `ask.ts` or the app's dialog.
3. **Notes:**
   - After Restore in Recently deleted, go back to the notes list.
   - Fold the four always-shown filter menus under "More filters", as in the Library.
   - The "Notes on this passage" Save button must never fall below the window. Keep it in view, or
     give the form its own scroll area.
4. **Plans:** the green "Started … finishing …" message keeps the old finish date after days are
   marked. Refresh it or clear it.
5. **Pane ⋯ menu:**
   - Tidy the long list. Group the items, and drop "Unlink", which duplicates the link button.
   - Keep every action reachable.
6. **Keyboard:** tagged words in Bibles can be reached and opened with the keyboard (Tab or arrow
   keys, then Enter), with a visible focus ring.
7. **Search:** with every Bible chip checked, a listed but unsearchable module makes `bible.search`
   fail with `not_found` and hides the book hits too. Skip the module that can't be searched and
   say so quietly; never fail the whole search.
8. **Migrations:**
   - Record each applied migration (a `schema_migrations` table with its number and the time it was
     applied), instead of trusting `PRAGMA user_version` alone.
   - Upgrade existing libraries safely: every migration up to the current `user_version` counts as
     applied.
   - Keep `user_version` in step for older builds.
   - Go tests: an old library upgrades; a gap (a missing middle migration) is applied; nothing runs
     twice.
9. **Quality audit items 31 and 32** (`docs/qol-audit.md`):
   - Remove a stale `joined.new` the next time sync runs on that device.
   - The Library's "Look again" button no longer calls `library.items` on every refresh.
10. **Secondary forms:** replace the native selects and tick-box lists that remain (export options,
    plan weekdays, licence choices, Move to…) with the app's own controls. Use those already used
    elsewhere; don't invent new ones.
11. **Ink:** pen, colour and width are remembered after a restart (per device).
12. **Highlights tool for a book:** give the notes listed there the same colour filter and search
    the highlights have.

## Step 3: check the screens

- **Light and dark themes:** take screenshots at 1536×816 in Light and Dark of the main screens:
  - Read;
  - Study;
  - Library;
  - My work;
  - a note;
  - a sermon;
  - Settings;
  - the right sidebar.

  Fix any text or control the colour tokens miss, such as unreadable text or white boxes in dark.
- **Nothing overlaps content:** check that no button or bar covers text or headers, in particular
  the auto-hide rail's show button and the right rail.

## Tests

- **Go:** migrations, the search skip, and the `joined.new` clean-up.
- **Real app:**
  - Add or extend `*.e2e.mjs` checks for the sermon fields, notes restore and filters, and keyboard
    access to tagged words.
  - Add a check that no native dialog appears: hook `window.confirm`, `alert` and `prompt` to throw
    during the run.
  - Run only the files you touch while working, then the full suite once at the end.
  - Report the exact pass count.

## Rules

- No real network in tests, and never use the user's real library: temp folders only.
- Permissive licences only.
- Run `npm ci` in `frontend/`; no junctions.
- Run long commands with timeouts, and close every process you start, including `glossa-e2e.exe`
  and e2e node processes.
- Never touch `%LOCALAPPDATA%\Temp\glossa-check`, `xegesis-check` or `%LOCALAPPDATA%\Programs\Glossa`.
- Don't commit `frontend/dist`. Don't push. Never `git stash`. Never switch branches.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`. End commit messages with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Use `scripts\build.ps1` for any app build.
- Plain words in the interface: no jargon, and no blocking questions on the common path.
