# T40: Connections and citations

Briefs for agents (Sonnet), with the user's decisions from 21 November 2026. Read these first:
- `AGENTS.md`;
- ADR 0005 (pluggable), ADR 0009 (privacy) and ADR 0011 (the assistant's licence gate);
- `internal/integrations` (`apibible`, `dbl`, `secrets`);
- `docs/privacy.md`.

## The user's decisions

- **A connections API:** one framework through which Glossa talks to other tools. The first
  connections are Zotero and Any; Readwise and Obsidian follow.
  - **Zotero: the library and citations.** Read the user's Zotero library and use its items as
    citation sources. Attaching Zotero items to notes and sermons, and importing Zotero notes and
    PDF annotations, are optional extras.
  - **Any: two-way, through Any's local API on this computer.** The Any app must be running.
  - **Later:**
    - Readwise imports highlights, with the user's token.
    - Obsidian reads and writes a Markdown vault folder the user chooses, both ways.
- **Citations: any CSL style.**
  - Write **our own Citation Style Language (CSL 1.0.2) processor in Go**. The common engines
    (citeproc-js, citeproc-rs) are AGPL, CPAL or MPL, and this project needs permissive licences
    (ADR 0003).
  - Style and locale files are CC BY-SA data from the official CSL repositories. Keep them with
    their licence and attribution, apart from the code.
  - **Bundle** SBL (Society of Biblical Literature, full note), Chicago notes-bibliography, APA, and
    the en-US and en-GB locales.
  - **Download** any other style on request from the CSL styles repository. This is a new network
    feature, so it needs a switch and stays off until the user chooses a style; list it in privacy.
- **Citing in the editor:**
  - **Insert → Citation**, and a `/cite` command: pick a source and add a locator (page, section,
    verse).
  - Sources: a library book, a Bible, or a Zotero item.
  - The citation shows as a chip in the text.
  - Exports (HTML, Word, PDF and Markdown) render footnotes or author-date by the chosen style, with
    a generated bibliography. "Insert bibliography" places one in the document.

## Parts (one agent each; commit after each step)

### T40a: the framework and Zotero

- **`internal/connect`:**
  - A `Connection` interface: an ID, a name, how it signs in (a key, a local app, or a folder), its
    capabilities (import, export, sources for citing), status and a test.
  - A registry.
  - Secrets go in the OS credential store.
  - A per-connection privacy switch, off by default, saying exactly what it sends and where.
  - Commands under `connect.*`.
- **Settings → Connections:** list the connections, with connect and disconnect, status, what each
  sends, and a test.
- **Zotero:**
  - Prefer the **local Zotero API** on this computer. Check Zotero 7's local API: its port, how it is
    enabled, and that it is read-only. Fall back to the **web API** with the user's key and library
    ID.
  - Browse and search items and collections.
  - Map items to **CSL-JSON**, the item model the citation engine uses.
  - Cache what the user cited, so documents still render offline.
- Write **ADR 0014, "Connections"**, with status Proposed.
- **Tests:** Go tests with fake Zotero servers (local and web), and a real-app check of Settings →
  Connections that connects to a fake Zotero, with screenshots.

### T40b: the citation engine and citing

- **The CSL engine, in `internal/csl`:**
  - Parse styles and locales.
  - Cover the citation and bibliography layouts, macros, choose/if, names (with et-al, initialize
    and sort), dates, numbers, labels, groups, text-case, affixes, delimiters and disambiguation.
  - Support note and in-text (author-date and numeric) classes, and subsequent and ibid citations.
  - **Test against the official CSL test suite** (the citeproc-test fixtures, MIT): fetch them in a
    `go:generate` step or keep a pinned copy, and report the pass rate. Aim for all the fixtures the
    bundled styles rely on.
- **Sources as CSL-JSON:** library books (from their metadata), Bibles (with SBL abbreviations for
  the Bible book and version), Zotero items, and manual entries.
- **The editor:** Insert → Citation and `/cite` (searchable sources, a locator, a prefix and suffix),
  a citation chip node that round-trips through Markdown, and "Insert bibliography".
- **Exports:** render the citations and bibliography in HTML, Word and PDF: footnotes for note
  styles and in-text for the others. The document's style is set in its ⋯ menu, with a default in
  Settings.
- **Styles:** bundle the ones listed above. Settings → Citations: search and download styles (with
  the switch), and choose the default.
- **Tests:** engine tests with the CSL suite, round-trip tests, and real-app checks: cite a book and a
  Zotero item, see the footnote and bibliography in an export, and switch the style.

### T40c: Any (two-way)

- Any's local API: check the port, how a key or pairing is set up in the Any app, and the endpoints.
  The user's Any MCP server shows the API is available.
- Send a note or sermon to an Any space as a page, with Markdown and its passages as properties.
  Import Any pages as notes.
- Keep a link record, so re-sending updates rather than duplicates. When both sides changed, ask.
- **Tests:** a fake Any API server, and a real-app check.

### T40d: Readwise and Obsidian (later)

- **Readwise:** import highlights through the export API (with the token), linked to library books
  where the title and author match, otherwise as notes per source.
- **Obsidian:** export and import Markdown with front matter in a chosen vault folder. Make links back
  to Glossa passages and notes through a `glossa://` link scheme, if the app registers one; otherwise
  plain references.

## Rules (all parts)

- No real network in tests; use fake servers.
- Never use the user's real library, Zotero, Any or vault: temp folders and fakes only.
- Keys go in the credential store, never in logs.
- Run `npm ci`; no junctions.
- Run long commands with timeouts, and close every process you start.
- Never touch `%LOCALAPPDATA%\Temp\glossa-check` or `xegesis-check`.
- Don't commit `frontend/dist`.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`.
- Don't push.
- Run only touched e2e files while working, and the full suite once at the end.
- Use `scripts\build.ps1` for any app build.
