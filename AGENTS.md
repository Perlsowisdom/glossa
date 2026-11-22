# Working on Glossa

Instructions for every contributor, human or AI agent. Read this before changing anything.

## What Glossa is

A free, open-source Bible study app for Windows, macOS and Linux. It is fast, works fully offline, keeps
user data in open formats, and can be driven by people, scripts and AI agents through the same commands.

## Design philosophy

"Simple things should be simple, complex things should be possible." (Alan Kay)

- The common path takes no decisions the app can make itself: sensible defaults, no blocking questions.
- Details, options and exact control are one step away (a "change" link, "More options"), never in the way.
- Plain words in the interface: no command names, no internal type names, no jargon a reader would not use.

## Clean-room rules (non-negotiable)

Glossa is designed from public sources and open data only.

- Do not use, run, study, screenshot or copy the Logos Bible Software app, its files, file formats,
  databases, network traffic or content. Do not decompile any commercial Bible software.
- Design from: the Phase 1 functional survey, open-source projects (AndBible, BibleTime, Xiphos,
  Ezra Bible App, STEPBible, the CrossWire SWORD Project, Calibre, KOReader), published specifications
  such as CrossWire's SWORD module documentation, and open datasets. Take ideas and workflows from
  these projects, never their code: most are GPL, and translating GPL code into Go is still copying it.
- Describe features in generic terms. Do not reuse another product's screen names, layouts or wording.
- When you add a feature, note its source (Phase 1 row, specification or open project) in the pull request.
- The only code that may read Logos data is the separate own-data importer (task T22,
  `internal/logosimport`). It reads the user's own data files (notes, highlights, clippings, lists,
  plans, sermons) and the library catalogue's metadata only (title, author, resource ID, type and
  licence or rights fields), always from a read-only copy. It never opens a Logos resource content
  file and never decrypts or bypasses anything. Nothing else in Glossa reads Logos data.

## Licence rules

- Glossa is licensed MIT OR Apache-2.0.
- Dependencies must use a permissive licence: MIT, Apache-2.0, BSD-2/3-Clause, ISC, zlib or similar.
- No GPL, LGPL, AGPL, SSPL or "source available" code, including as a linked library. This rules out
  MuPDF, libsword and anytype-heart. Ask the coordinator if unsure.
- Bundled data (Bibles, lexicons) must be public domain or openly licensed, with its licence recorded.

## Architecture

- **Go core** in `internal/`: every feature is a registered command (`internal/command`).
- **Interfaces call commands**: the desktop app (`app/`, Wails), the CLI and MCP server (`cmd/glossa`).
  No feature may be reachable only from the interface.
- **Storage**: SQLite through `modernc.org/sqlite`. `library.db` holds user data and the change history;
  `index/<folder>.db` holds one library folder's search index; `modules/<id>.db` holds one Bible or dataset.
- **Every write is a command and is logged** in the `changes` table. Never write to `library.db` outside
  `internal/store`.
- **Verse IDs** are integers `BBCCCVVV` (`internal/verse`). Never store references as free text.
- **Secrets** (API keys) go in the operating system keychain, never in a database or config file.
- **Privacy** (ADR 0009): no telemetry or analytics. Only the features listed in ADR 0009 may use the
  network, each off until the user enables it. Crash reports are sent only after the user reviews them.
- Decisions are recorded in `docs/adr/`. Changing one needs a new ADR.

## Layout

| Path | Contents |
| --- | --- |
| `cmd/glossa` | CLI and MCP server |
| `app/` | Wails desktop app (Go side) |
| `frontend/` | Desktop interface (web) |
| `internal/command` | Command registry, parameters, results |
| `internal/store` | `library.db`, migrations, change history |
| `internal/verse` | Verse IDs and numbering mappings |
| `internal/licence` | Licence records and print/export rules |
| `internal/library` | Library folders, import, catalogue |
| `internal/bible` | Bible modules (SWORD, STEPBible) |
| `internal/search` | Per-folder indexes and merged search |
| `internal/csl` | Citation engine (CSL 1.0.2): styles, locales, citations, bibliographies; bundled styles under `styles/` |
| `internal/integrations` | API.Bible, Digital Bible Library |
| `docs/` | ADRs, schema, specifications |

## Working rules for agents

- Work only in the directories your brief assigns. Changes to shared contracts (`internal/command`,
  `internal/store` schema, `internal/verse`) go through the coordinator.
- Every change has tests. Run `go vet ./...` and `go test ./...` before reporting done.
- Code that starts goroutines has a leak test (`go.uber.org/goleak`), and every database row set,
  file and connection is closed. Glossa also runs on phones (ADR 0008), so watch memory.
- Format with `gofmt`. Keep functions small and comments sparse and useful.
- Commit on your own branch; never push to `main` or rewrite history.
- End commit messages with the attribution line you are given.
