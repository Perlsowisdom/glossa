# T43: More free sources for your Logos books

Brief for one agent (Sonnet), 27 November 2026.

The user asked to bring their Logos purchases into Glossa. The books themselves can't be taken out:
they are encrypted, and the clean-room rule forbids decrypting or reverse-engineering Logos. What we
can do is find free, legal copies of the same works.

From Logos (Library → From Logos) already reads the *list* of the user's Logos books (catalogue
metadata only, in `internal/logosimport/library.go`) and matches it against CCEL (`internal/ccel`).
This task widens that to more sources, and shows how much of the library each source covers.

Read these first:
- `AGENTS.md`;
- ADR 0009 (privacy) and `docs/privacy.md` (the CCEL section is the pattern);
- `docs/sources/ccel.md`;
- `internal/ccel`;
- `internal/logosimport/library.go` and `matcher.go`;
- `frontend/src/views/fromlogos.ts`;
- `internal/bible/repos` and the SWORD install code;
- `frontend/e2e` (the `fromlogos` and `zccel` files and the `fake-*` servers).

## Decisions (the user chose this direction on 27 November 2026; the details are coordinator defaults)

- **One match screen, several sources.** From Logos shows each Logos book once, with the best free
  copy found and its source. Possible states:
  - **Already in Glossa:** a library book or an installed module matches.
  - **Free copy on CCEL** (as now).
  - **Free copy on Project Gutenberg** (new).
  - **Free as a Bible module:** a CrossWire or eBible module, for commentaries, lexicons, dictionaries,
    general books and Bibles (new; uses the catalogues Get Bibles already caches).
  - **Not free:** with a "Search Internet Archive" link that opens archive.org in the browser. The app
    itself sends nothing.

  Filters (All, Free copy found, Already in Glossa, Not free) and a short summary line, for example
  "312 of your 1,840 Logos books have a free copy". That summary is the coverage measure the user
  wants.
- **Matching stays on this computer.** Every source is matched against a whole catalogue downloaded
  once and cached, never by sending titles one at a time. Nothing about the user's Logos library
  leaves the computer.
- **Project Gutenberg:**
  - Use its official catalogue file: `https://www.gutenberg.org/cache/epub/feeds/pg_catalog.csv.gz`
    (or the documented equivalent). Check the current path and terms in Gutenberg's robot and
    mirroring guidance, and record them in `docs/sources/gutenberg.md`.
  - Refresh at most once a week, and only when the user opens From Logos or presses refresh.
  - Download an EPUB only when the user presses Get free copy and confirms. Use Gutenberg's EPUB3
    (with images) file for that book number. Respect Gutenberg's rules for automated access: one
    request at a time, a polite delay, and a `User-Agent: Glossa` header.
  - Gutenberg works are public domain in the US. Add the book with the licence "Public domain (US)"
    and the credit "Project Gutenberg (gutenberg.org)". Strip or keep the Gutenberg licence header as
    their terms require; record which in the sources doc.
  - Include only text works in the languages the catalogue gives. Skip audio.
- **Bible modules:**
  - Reuse the CrossWire and eBible catalogues Get Bibles already fetches and caches. Make no new
    network call, and add no new switch beyond what Get Bibles has.
  - Match Logos books of the kinds commentary, lexicon, dictionary, encyclopedia, Bible and general
    book to module descriptions, for example "Matthew Henry's Commentary", "Jamieson, Fausset and
    Brown", "Thayer's Greek Lexicon", "Easton's Bible Dictionary" or "Strong's".
  - "Install" uses the existing SWORD install path with its licence handling.
- **Internet Archive:** an outbound link only. It opens a search for the title and author on archive.org
  in the browser, through the existing open-URL path. Glossa makes no API calls there: Internet
  Archive's copyright status is unreliable, and per-title queries would send the user's titles.
- **Switches:**
  - Gutenberg gets its own privacy switch, off until turned on, in the same place and style as CCEL's,
    with one plain sentence on what it sends.
  - Show the CCEL and Gutenberg switches together on the From Logos screen, as a short "Look for free
    copies on: CCEL, Project Gutenberg" row, and in Settings → Privacy.
- **Matching quality:**
  - Normalise titles and authors: case, punctuation, leading articles, subtitles after a colon,
    "Vol."/"Volume" numbers, and author name order ("Spurgeon, Charles H." against "C. H. Spurgeon").
  - Match on title plus author. A title-only match counts only when it is very close and distinctive,
    and is marked "possible match: check".
  - Never offer a free copy for a book whose Logos record is clearly a modern work. A copyright date
    after 1930 means no match, unless the source's own record says public domain.
  - Prefer CCEL first (it often has better theological markup), then Gutenberg, then modules, when
    the same work is in more than one source. The user can see the others under "Other free copies".
  - Go table tests with real-looking titles from each source's format. Write them by hand; never use
    the user's real Logos library.
- **Bulk:**
  - "Get all free copies" lists what will be downloaded, with the total size where known, and asks
    once.
  - It then downloads one at a time with the sources' delays, shows progress, and can be stopped.
  - Books are added to the library with their licence and credit.

## Tests

- **Go:**
  - Gutenberg catalogue parsing (a small hand-made `.csv.gz` fixture).
  - Matching: title and author normalisation, the modern-work guard, the source order and possible
    matches.
  - The module match.
  - Weekly refresh and the switch: nothing is sent while it is off.
  - Download with a fake server.
- **Real app:**
  - Extend or add a `fromlogos` e2e file with a fake Gutenberg server, a fake CCEL (one exists) and the
    fake module catalogue.
  - Check the summary line, the filters, a Gutenberg Get free copy that adds a book, an Install for a
    module match, and the Internet Archive link. Catch the URL; don't open a browser.
  - Check that turning Gutenberg off means no request reaches the fake server.
  - Take screenshots at 1536×816.
  - Run only touched files while working, and the full suite once at the end.
- **Docs:**
  - `docs/privacy.md`: a Project Gutenberg section in the CCEL format (when, and what requests), plus
    the Internet Archive link.
  - `docs/sources/gutenberg.md`.
  - An e2e scope note.
  - If the shape of From Logos changes materially, a short note in `docs/integrations.md`.

## Rules

- Clean room: read only the Logos catalogue metadata the code already reads. Never open, decrypt or
  inspect Logos resource files.
- No real network in tests (fake servers only). Never use the user's real Logos library or Glossa
  library: temp folders and fixtures only.
- Permissive licences only.
- Run `npm ci` in `frontend/`; no junctions.
- Run long commands with timeouts, and close every process you start, including `glossa-e2e.exe` and
  e2e node processes.
- Never touch `%LOCALAPPDATA%\Temp\glossa-check`, `xegesis-check` or `%LOCALAPPDATA%\Programs\Glossa`.
- Don't commit `frontend/dist`. Don't push. Never `git stash`. Never switch branches.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`. End each commit message
  with a blank line, then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Commit after each step.
- Use `scripts\build.ps1` for any app build.
- Plain words in the interface; no blocking questions on the common path.
