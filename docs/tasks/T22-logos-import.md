# T22: Import your own data from Logos

Brief for agents (Sonnet), approved by the user on 20 November 2026. Read `AGENTS.md` and ADR 0007
(clean room) first.

## The user's decisions

- **What to import:**
  - notes and highlights;
  - sermons;
  - lists and plans (passage lists, word lists, prayer lists, reading-plan progress);
  - clippings.
- **The Logos library:**
  - at least a **list of the books in the user's Logos library**, each with a link that opens the
    book in Logos;
  - where a book is **public domain**, match it to a free copy Glossa can get, and offer to get it.
- **How:** a one-off import with a preview, in Settings → Your data → Import from Logos.
  - Glossa finds the user's Logos data, shows counts and a preview, and imports after the user
    confirms.
  - Running it again adds only what is new or changed.
  - **Nothing in Logos is ever changed:** every Logos file is opened read-only, from a copy.

## Hard limits (unchanged)

- **Never decrypt, reverse-engineer or bypass Logos resource encryption.** Logos books themselves
  can't be imported, and the importer never opens their content files.
- The importer reads only the user's own data files and the library catalogue's **metadata**:
  title, author, resource ID, type and licence or rights fields. Update the T22 line in `AGENTS.md`
  to say exactly that; the user asked for the library list.
- The importer is its own module (`internal/logosimport`). Nothing else in Glossa reads Logos data.

## Where the data is

The user's own Logos MCP server (`C:\Users\perlj\Documents\Claude\Projects\Logos`,
`src/logos.js`) already finds and reads these files:
- `notestool.db`, `Clippings.db`, `PassageList.db`, `wordlist.db`, `PrayerList.db`;
- `ReadingPlan.db`, `readingprogressmanager.db`, `Sermon.db`, `catalog.db`.

It is the user's own code, so use it to learn the locations, tables and rich-text format, and write
the Go importer from that. Don't copy Logos's own code, and don't read any other Logos files.

## Parts (one agent each, in order)

### T22a: notes, highlights, clippings, lists and plans

**Notes:**
- Keep notebooks (as Glossa folders), tags, created and changed dates, and Bible anchors (as verse
  ranges).
- Keep the rich text as Glossa Markdown: bold, italic, underline, lists, links.
- A note on a Logos book (not a Bible) becomes a note whose reference reads, for example, "Logos:
  *Title*, section". It carries a link that opens that place in Logos, and it is tagged
  `from-logos`.

**Highlights:**
- Bible highlights become Glossa highlights, whole verse or partial where the offsets map.
- Map the Logos highlight styles to the nearest Glossa colour, and keep the name of the original
  style.
- Highlights on books, not Bibles, are listed in the import report rather than placed.

**Clippings** become notes, with the clipping folders as Glossa folders.

**Lists and plans:**
- Passage lists become saved lists.
- Prayer lists become notes in a "Prayer lists" folder, unless Glossa has a better fit.
- Word lists become saved lists of words.
- Reading-plan progress becomes a Glossa plan where the plan maps, otherwise a note.

**Preview and confirm:**
- Counts per kind, a sample of each, and what can't be mapped, with the reason.
- "Import" runs it through commands, so history and undo work.
- An import log is stored, so running it again skips what's already there (keyed by Logos IDs and
  changed time).

### T22b: sermons

Sermon Builder documents become Glossa sermons:
- title, passages (several allowed), date preached, theme or series;
- the outline as headings, and body text;
- verse quotes as quote blocks with the Bible's credit line, following quote licences.

Anything unmapped is reported.

### T22c: the Logos library

- **The book list:** Library → "From Logos" shows the books in the user's Logos library, with
  title, author, type and Logos ID. Each has **Open in Logos**, which uses the Logos link scheme
  (`logosres:` or `https://ref.ly/logosres/<id>`) through `BrowserOpenURL`. It is metadata only,
  with no book content.
- **Public-domain matches:**
  - Find books that are public domain by their catalogue rights field, and by publication date and
    author where that is clearer.
  - Match them to free sources Glossa already knows: the Get Bibles catalogues (CrossWire SWORD,
    eBible.org) for Bibles, commentaries and dictionaries, and well-known public-domain texts.
  - Show "Get free copy" for each match. **Never fetch anything from Logos.**
  - Report matches as likely or certain, and let the user confirm before anything downloads.

## Testing

- **Fixtures:** small SQLite databases the tests build with the same table shapes. Never commit the
  user's real Logos files.
- **Dry run on the real data:**
  - copy the user's Logos data files into a temporary folder;
  - run a preview against the copy, importing into the e2e harness's temporary Glossa library;
  - report the counts.

  Never write to the user's real Glossa or Logos data.
- **Tests:**
  - Go tests per mapper;
  - a real-app e2e check for the preview-and-import flow on the fixtures, with screenshots;
  - only touched e2e files while working, and the full suite once at the end.

## Rules

- Run `npm ci`; no junctions.
- Run long commands with timeouts, and close every process you start.
- Never touch `%LOCALAPPDATA%\Temp\glossa-check` or `xegesis-check`.
- Don't commit `frontend/dist`.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`.
- Don't push.
- Commit after each working step.
