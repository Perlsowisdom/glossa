# 0014: Connections

Status: Accepted (approved by the user, 26 November 2026); T40a–T40d. Builds on 0005, 0009 and 0011; **adds Zotero (the app on this computer, and
zotero.org) to the list of network features in ADR 0009.** Addenda add Any (T40c) and Readwise (T40d).

## Context

People keep sources in other programs: a Zotero library, notes in Any, a Calibre shelf. Glossa should let
them cite those sources and move things in and out without a second copy to maintain, and without giving up
the privacy promises of ADR 0009: no telemetry, every network call listed, every one off until the person
turns it on. ADR 0005 keeps sync apart; this record is about *links to other programs*, not about syncing
Glossa's own data.

## Decision

**A connection is a link to one other program or service.** The code is `internal/connect`; each connection
implements one small interface and is registered once. It declares:

| Part | Meaning |
| --- | --- |
| `ID`, `Name` | A short lowercase ID (`zotero`) used by commands and keychain names, and the name people see |
| Sign-in | How the person signs in: a **key** they paste, a **local app** that is running on this computer, or a **folder** |
| Capabilities | Any of **import**, **export** and **cite** (offers sources to cite) |
| Sends | One plain sentence saying exactly what is sent and where; it is shown beside the switch, and `docs/privacy.md` says the same in full |
| Key | What key it takes, if any, and whether it is required or only a fallback |
| Test | A check that it works now, returning one plain sentence that says what to do when it does not |

**The privacy switch is the framework's, not the connection's.** Every connection is **off until the person
turns it on** (`connect.enable`). The framework reads the switch and the key and refuses to call a connection
that is off (`Service.Require`), so a connection cannot forget the check, and nothing is sent while it is off.
Turning it off stops all requests; what was cached stays.

**Secrets.** A key goes to the operating system keychain through `internal/integrations/secrets`, under
`connect.<id>.key`. It is never written to `library.db`, a file or a log, no command returns it, and errors
are written without the request address (a key can be part of one). The interface shows only "saved" or
"not saved".

**Where the switch lives.** In `library.db`'s `settings` table, key `connect.<id>`, value `{"enabled": bool}`.
It is a **per-device setting** (the prefix `connect.` is added to the per-device list in `internal/store`):
the key lives in this computer's keychain, so a switch that synced to another device would turn on a
connection that has no key there.

**Commands.** `connect.list` (never uses the network), `connect.enable`, `connect.disable`,
`connect.set_key`, `connect.clear_key`, `connect.test` (needs the connection on). A connection adds commands
of its own under its ID (`zotero.*`), which check the switch first. Like every feature, the desktop app, the
CLI and the MCP server all reach them through the command registry.

**Interface.** Settings → Connections lists each connection with its switch, its status (Off, Connected,
Needs attention), what it sends, and a Test button. A key is one step away under "More options" when the
connection works without one, and shown directly when it is required. The Privacy page lists each connection
and whether it is on.

### Zotero

The first connection. Read-only: Glossa never changes a Zotero library. Written from Zotero's public
documentation.

- **Prefers the app on this computer.** Zotero 7's local API is at `http://localhost:23119/api/users/0/`. The
  person allows it in Zotero → Settings → Advanced → "Allow other applications on this computer to
  communicate with Zotero". It answers **403** while that is off and **refuses the connection** when Zotero
  is not running; Glossa tells the person which, in plain words, with what to tick. It needs no key, has no
  rate limit and is read-only here.
- **Falls back to the web API (v3)** at `https://api.zotero.org` only when a key is saved and the app cannot
  be reached. The key is sent in the `Zotero-API-Key` header, with `Zotero-API-Version: 3`. The user ID comes
  from `GET /keys/<key>` (not `/keys/current`) once per run, so no library ID is asked for. Paging uses
  `start`, `limit` (at most 100) and `Total-Results`. A `Backoff` header is waited out before the next
  request, and `Retry-After` on 429 and 503 is waited out and retried (twice), then reported plainly; a wait
  longer than 30 seconds is reported rather than held. `If-Modified-Since-Version` with the stored
  `Last-Modified-Version` makes a refresh a single 304 when nothing changed.
- **Items are CSL-JSON.** Requests use `format=csljson`, which both APIs serve. Glossa's own Go type
  (`internal/connect/csljson`, written from the public CSL-JSON schema, tolerant of numbers where text is
  expected and of date parts given as text) is what the citation engine (`internal/csl`, T40b) consumes. An
  item's ID becomes `zotero:<key>` whichever API it came from, so a citation finds it again. Notes and
  attachments are not items to cite and are left out.
- **Commands:** `zotero.items` (a page of the library or one collection), `zotero.search`,
  `zotero.collections`, `zotero.item`, `zotero.cite`, `zotero.cached`, `zotero.refresh`.
- **Kept copies.** `zotero.cite` keeps the item as CSL-JSON in a new table, `cite_items` (schema 15; see
  below), so a document that cites it still renders when Zotero is closed, off or offline. `zotero.item` falls
  back to the kept copy when Zotero cannot be reached, and `zotero.cached` lists them with the connection off.
  `zotero.refresh` asks Zotero whether anything changed (one 304 in the common case) and updates only what
  did; an item deleted in Zotero stays in the kept copies.
- **Test setup.** The two addresses can be changed with `GLOSSA_ZOTERO_LOCAL_URL` and `GLOSSA_ZOTERO_WEB_URL`
  (for tests, and for Zotero on another port). The settings screen does not offer them.

### Schema (a shared contract)

Migration `0015_cite_items.sql` adds `cite_items(id, connection, item_key, csl_json, title, library_version,
cached_at)`, one row per cited item, `id` = `<connection>:<key>`. It is written through the logged store like
other user data, so it syncs (ADR 0013): a document cites the same sources on every device. The `connect.*`
setting rows are per-device (above). No other table changes.

## Consequences

- ADR 0009's list of network features gains **Zotero** (the app on this computer, and zotero.org); the same
  switch rule applies, and `docs/privacy.md` has its section. Later connections (Any, Calibre) are added by
  new records or by amending this one, each with its own sentence and privacy section.
- Adding a connection means one Go type and one registration; the settings screen, the switch, the keychain
  entry and the privacy list come from the framework. No connection has code of its own in the interface.
- A connection that needs a long-lived process or a local server of its own is out of scope here.
- The group libraries of Zotero are not browsed yet; the user library (`users/0`, or the key's own user) is.
- Open question for T40b: whether the engine should read `zotero.cached` directly or be handed items by the
  editor. This record only guarantees that the cached CSL-JSON is complete enough to render a citation.

## Amendment: citation styles download (T40b part 2)

ADR 0009's list of network features gains **downloading citation styles** from the Citation Style Language
project's repository (`github.com/citation-style-language/styles`). It is not a connection (no sign-in, no key)
but follows the same rule: **off until the person turns it on** (Settings → Citations, `cite.styles_enable`),
stored as the per-device setting `connect.csl-styles` (under the `connect.` prefix, so it is not synced).
`internal/cite` refuses to search or download while it is off. It reads the list of style names through GitHub's
tree API and one raw style file at a time; the search words are matched locally and never sent. The bundled
styles and everything else about citing work offline. Downloaded styles are saved unchanged to
`<data>/styles/<id>.csl` with a `<id>.licence.txt` (licence, source, authors); a dependent style fetches its
parent. `GLOSSA_CSL_LIST_URL` and `GLOSSA_CSL_RAW_URL` point the feature at test servers. `docs/privacy.md` has
the section. No schema change: manual sources and kept Zotero items share `cite_items`; the default style is the
setting `cite.style` and a document's own choice `cite.doc_style.<id>`.

## Addendum: Any, two-way (T40c)

ADR 0009's list of network features gains **Any** (the Any app, formerly Anytype), reached **only on this
computer**. Nothing leaves the computer: Glossa talks to the app's local API on the loopback address, and
`internal/connect/any` refuses an address that is not loopback. It is a connection like Zotero (switch off by
default, one sentence of what is sent, key in the keychain, a Test), with sign-in "a local app".

**What was confirmed** (from Any's developer documentation, `developers.anytype.io`, and the OpenAPI description
`openapi-2025-11-08.yaml` in `github.com/anyproto/anytype-api`; no Any source code was read, and the person's
real Any app was never contacted, only a fake):

| Part | Confirmed |
| --- | --- |
| Version | The dated API version is **2025-11-08** (older: 2025-05-20, 2025-04-22). Every request carries the header `Anytype-Version: 2025-11-08`. |
| Address | The OpenAPI server is `http://127.0.0.1:31009`; the examples use `localhost:31009`. Glossa uses `http://127.0.0.1:31009`; `GLOSSA_ANY_URL` points it elsewhere on this computer (tests). |
| Pairing | `POST /v1/auth/challenges` with `{"app_name": "Glossa"}` returns `{"challenge_id"}` and makes the Any app show a 4-digit code; the person types it into Glossa; `POST /v1/auth/api_keys` with `{"challenge_id", "code"}` returns `{"api_key"}`. |
| Authentication | `Authorization: Bearer <api_key>` on every other call; a 401 means the key is no longer good (the person is asked to sign in again). |
| Spaces | `GET /v1/spaces?offset&limit` returns `{data: [{id, name, ...}], pagination: {total, offset, limit, has_more}}`. All list calls page the same way. |
| Search and list | `POST /v1/spaces/{space}/search?offset&limit` with `{query, types: ["page"], sort}`; objects carry `id`, `name`, `snippet`, `archived`, `type.key` and `properties` (including `last_modified_date`, a date property). |
| Read | `GET /v1/spaces/{space}/objects/{id}?format=md` returns `{object: {..., markdown}}`. |
| Create | `POST /v1/spaces/{space}/objects` with `{type_key: "page", name, body, properties: [{key, text}]}` (the text field is `body` when creating). |
| Update | `PATCH /v1/spaces/{space}/objects/{id}` with `{name, markdown, properties}` (the text field is `markdown` when updating). |
| Properties | `GET`/`POST /v1/spaces/{space}/properties`: Glossa makes a text property `glossa_passages` in the space once and sets it. If Any refuses, the page is still sent without it. |

**What is sent and where.** Only a note or sermon the person chooses to send (title, Markdown text, its
passages as plain references such as "Romans 8:1; John 3:16"), and searches for pages they pick, and only to
the Any app on this computer. Nothing is sent to anyone else, and nothing is sent while the switch is off.
Anything Any itself then syncs is Any's business, not Glossa's.

**Links.** Migration `0016_any_links.sql` adds `any_links(kind, item_id, space_id, object_id, glossa_hash,
any_hash, any_modified, synced_at)`, one row per linked note or sermon. Sending again updates the linked page
(no duplicate); importing a linked page again updates the same note. **The table is per device**, like
`files`: it is logged but never sent to other devices (`perDeviceTable` in `internal/store/remote.go`). Page
IDs belong to the Any app, and the account, on this computer; another device may have no Any, or another
account, and a synced link there would update the wrong page or none.

**Two sides changed.** At each sync Glossa compares what each side looks like now with what it recorded at the
last sync (a hash of Glossa's title, text and passages; Any's last-modified time and a hash of the page text as
Any returned it). If only one side changed it carries the change across; if both changed (or Any changed and
the person is sending) it **changes nothing** and returns `both_changed` (or `any_changed`). The person then
chooses: keep Glossa's (the page is replaced), keep Any's (the note or sermon is replaced), or keep both (Any's
text is saved as a new note, then Glossa's goes to Any). A page deleted in Any is made again on the next send.

**Commands.** `any.pair_start`, `any.pair_finish`, `any.unpair`, `any.spaces`, `any.set_space`, `any.pages`,
`any.search`, `any.send`, `any.import`, `any.sync_status`. The key is stored once, in the keychain
(`connect.any.key`), and never returned, logged or shown. The default space is the per-device setting
`connect.any.space`.

**Not done.** Sermon outlines are not sent separately (the text is); a page imported becomes a note, never a
sermon; pictures, files and links inside pages are not carried; no automatic or background syncing.

## Addendum (Proposed), 27 November 2026: finding Any, and connecting with no key (T44)

**Why.** The person's Any app listens on `127.0.0.1:31310`, not on 31009 (which is Anytype's own API port, the one
in the OpenAPI description this addendum's predecessor was written from). Nothing listened on 31009, so Glossa said
"the Any app is not running". And that Any answers `GET /v1/spaces` with 200 and no key, so pairing was not needed
(whether this build offers `/v1/auth/challenges` at all is not known; Glossa does not call it unless Any asks for a key).
The part of the earlier addendum's table that says the address is `31009` and that every call needs a key is replaced by this.

**Finding.** With no address saved, Glossa probes `http://127.0.0.1:31310`, then `http://127.0.0.1:31009`, with
`GET /v1/spaces?limit=1` (no side effects, no key), waiting a second for each. An answer of 200, 401 or 403 means
Any; anything else (a 404 from some other program, no answer) does not. The address that answered is remembered in
the per-device setting `connect.any.address` and tried first next time; if it stops answering Glossa tries the usual
ones again. Nothing answering gives one message: "Glossa couldn't find the Any app on this computer. Open Any, or enter
its address under Advanced." **Address field.** Settings → Connections → Any has an Address field under Advanced
(open by itself when Any was not found), and `any.address` / `any.set_address` do the same for scripts and agents. A typed
address is kept as typed (`manual`), is the only one tried, and is never replaced by finding; an empty one goes back to finding.
It must be on this computer: `localhost` or a loopback IP, as `NewClient` already requires; anything else is refused
with "Glossa only talks to the Any app on this computer, and the address given is not on this computer", and is not saved.
Tests start the app with `GLOSSA_ANY_CANDIDATES` (a comma-separated list replacing the usual two) or `GLOSSA_ANY_URL`
(one address, no finding), so they never reach the real ports.

**Signing in only when asked.** The probe tells Glossa whether Any asks for a key. 200: Glossa connects with no key and
the status says "Connected (Any didn't ask for a key)"; the sign-in is not shown. 401 or 403 and no saved key: "Any is
running, but Glossa is not signed in yet", and the pairing flow (challenge, code, key) is offered as before. A saved key
is always sent. If Any answers 401 or 403 to a request that carried a key, the message is "Any no longer accepts Glossa's key.
Choose "Sign in to Any" to pair again." (`ErrKeyRejected`); the key is left in the keychain until the person pairs again or
signs out. `connect.test` reports which of these it found in a new `auth` field (`none_needed`, `ok`, `needed`, `stale`).

**Privacy.** The probe is one more keyless `GET` to the same loopback address, listed in `docs/privacy.md`. No key is ever
sent to an address that has not answered the probe, and a key is never logged.

**Not decided.** Whether to also look for the address in Any's own files or settings (no: that would read another
program's data). Another port for Any is the person's to type.

## Addendum: Readwise, import (T40d part 1)

ADR 0009's list of network features gains **Readwise** (`https://readwise.io` only), off until the switch is
turned on, with its own section in `docs/privacy.md`. Glossa only reads. The token is the connection's key
(required) and lives in the keychain (`connect.readwise.key`).

**What was confirmed** (from `https://readwise.io/api_deets`; the real API was never called, only a fake):

| Part | Detail |
| --- | --- |
| Auth | `GET /api/v2/auth/` with `Authorization: Token <token>`; 204 when good, 401 when not |
| Export | `GET /api/v2/export/`; optional `updatedAfter` (ISO 8601, UTC), `pageCursor` (the `nextPageCursor` of the last answer, until it is null), `includeDeleted=true` (also returns deleted highlights, so deletions can be synced) |
| Answer | `{count, nextPageCursor, results}`; each result is a source (`user_book_id, title, author, category, readwise_url, source_url, book_tags, document_note, highlights`), each highlight has `id, text, note, location, location_type, highlighted_at, updated_at, is_discard, is_deleted, readwise_url`. The docs do not say whether `updatedAfter` trims each source's `highlights`; Glossa assumes it does and keeps every highlight it has seen, so either works |
| Limits | 20 requests a minute on the list endpoints, 240 on the rest; on 429 `Retry-After` gives the seconds to wait. Glossa waits that long (at most three times, never more than 90 seconds) and otherwise stops with a plain message |

**Commands.** `readwise.test`, `readwise.preview`, `readwise.import`, `readwise.status`, `readwise.resolve`.
The first import is complete; later ones send `updatedAfter` with the time the last successful import *started*
(kept per device in the setting `connect.readwise.sync`), and `full` asks for everything again, which is also what
removes highlights that were deleted in Readwise without being flagged. An import that fails part-way does not
move that time; it can simply be run again.

**Matching.** A Readwise source of the category "books" matches a library book when the **title** and the
**authors** both agree after plain tidying: letter case, accents, punctuation, a leading "The/A/An", "Last, First"
against "First Last", and the part after a colon (a subtitle one side lacks is ignored; two different subtitles
do not match). The authors must be the same set. Nothing fuzzier: no spelling guesses, no initials against full
names, no title-only matches, and when two library books fit, none is chosen. Only titles and authors are read
from the library, on this computer.

**Where highlights go.** One note per Readwise source, titled "Title — Author", tagged `readwise` and the source's
tags, holding each highlight as a quote block, then the reader's own note, the place (page or location) and a
link back to Readwise. A matched source's note is **attached to the library book** (as a note on its first
section, which the book's notes panel lists). Glossa's own book highlights are anchored to a text position;
Readwise's places are Kindle locations that cannot be mapped to a position in a file here, and a guessed position
would put a mark in the wrong place, so they are not made. Unmatched sources become free-standing notes.

**Links and sync.** Migration `0018_readwise.sql` adds `readwise_sources(source_id, note_id, item_id, state,
body_hash, meta_json, synced_at)` and `readwise_highlights(highlight_id, source_id, block, loc, at, updated_at)`.
The highlights are kept as Markdown blocks so a note can be rebuilt when an incremental import sends only some of
its highlights. **Both tables sync** (unlike `any_links`): the notes they point to sync, so a second device that
imports finds the links and updates the same notes instead of making copies. The time of the last import is a
`connect.` setting and stays on its device, so each device's first import is a complete one that finds every note
already up to date.

**Discarded and deleted highlights** (`is_discard`, `is_deleted`) are taken out of the note on the next import.

**Edited notes.** `body_hash` is the title and text as Glossa last wrote them. A note whose hash still matches is
updated; one the person changed is **never overwritten**. The import lists it under `conflicts` (with the
differing text of both sides) and changes nothing in it, while still keeping Readwise's newer highlights, so
`readwise.resolve` needs no network: `readwise` replaces the note with Readwise's text, `mine` keeps the person's
note and stops updating it, `both` leaves the person's note and puts Readwise's text in a new, kept-up-to-date
note. A note the person deleted is not made again.

**Not done.** A source that only later matches a library book is not moved onto it; sources deleted whole in
Readwise leave their notes; no automatic or background import; no sending to Readwise.

## Addendum: Obsidian, export and import (T40d part 2)

Obsidian is a connection with sign-in kind `folder` and the capabilities import and export. It has no network code:
`Sends()` says it only reads and writes Markdown files in the chosen vault folder. The vault folder and the
subfolder in it (default `Glossa`) are the per-device setting `connect.obsidian.config`, chosen with the folder dialog
(`obsidian.set_vault`); the frontend calls the same `pick("folder")` as the sync folder, and a test replaces the dialog by
setting `window.go.main.App.PickFolder` in the page, as `sync.e2e.mjs` does.

**Files.** `obsidian.export` writes one `.md` per note or sermon: front matter `title`, `glossa_id`, `glossa_kind`
(`note` or `sermon`), `glossa_doc_kind` (only for a lesson or study), `passages` (plain references such as "John 3:16"),
`tags` (spaces turned into hyphens, because Obsidian tags have none), `created`, `updated`, and for a sermon `preached`
and `theme`; then the stored Markdown text as it is (the citation chip is already `[@lib:ID, p. 12]` there). The
title is not repeated as a heading. Front matter is written and read by a small hand-written reader of the subset
Obsidian itself writes (text, quoted text, `[a, b]` and `- item` lists); no YAML library was added. Keys Glossa does not
write (`aliases`, `cssclasses`, other plugins' keys) are kept as they were when a file is rewritten. File names are
the title made safe for Windows, macOS and Linux and for Obsidian links (reserved names, `\/:*?"<>|`, `#^[]`, length,
trailing dots), and a taken name gets " (2)". `obsidian.import` reads `.md` files from the subfolder, another folder or the
whole vault. Front matter `title`, `tags` and `passages` are used; otherwise the file name is the title. `[[wikilinks]]`
become plain text (the alias, else the title of the note the target file becomes in the same import, else the target's
name); `![[embeds]]` and `#tags` stay as text, and the result counts the embeds so the interface can say so. A note
holds one passage, so only the first readable one is attached and the rest are reported.

**No `glossa://` scheme.** Glossa registers no URL scheme, so passages and notes are linked by plain references (the
passage as text, a note by its title). A `glossa://` scheme (open a passage or note from Obsidian) is a possible
follow-up: it needs the app to register the scheme with the operating system and handle the link at start-up.

**Links, and why they are per device.** Migration `0019_obsidian_links.sql` adds `obsidian_links(kind, item_id, path,
glossa_hash, file_hash, synced_at)`. It is a per-device table, like `any_links` (`store/remote.go`): the path is relative
to the vault chosen on this computer, and another computer has another vault, or none, so a link made here would be
wrong there. (Readwise's links sync because they point at notes that exist everywhere; these point at files on one disk.)
The vault setting and the sync times are `connect.` settings, per device too. Choosing a different vault drops the links
(the files stay). A note's file is found again after a rename or move by the `glossa_id` in its front matter (a file
without one, by the text it had at the last sync).

**Which side wins.** `glossa_hash` and `file_hash` are the same fingerprint of title, text, passages and tags, taken from
each side when they last matched. Exporting or importing compares today's fingerprints with them (the shared rule is
`internal/connect/syncutil`, which Readwise now uses too for its hash and excerpt): unchanged on both sides, nothing; only
Glossa changed, export rewrites the file and import leaves it; only the file changed, import updates the note and export
leaves the file; both changed, nothing is changed and the item is listed under `conflicts`. `obsidian.resolve` settles it
with `obsidian`, `glossa` or `both` (the file's text becomes a new note, then Glossa's goes to the file), the same three
choices as Readwise and Any. Re-exporting an unchanged note does not rewrite its file, and re-importing an unchanged file
changes nothing. A file found by `glossa_id` with no baseline is compared as if both sides changed.

**Safety.** Every path goes through one check: no `..`, no absolute path, no `:` (drive or stream names), and no folder on
the way that is a symbolic link or a Windows junction (Go's `EvalSymlinks` does not follow junctions since Go 1.23, so
they are looked for directly), so nothing is read or written outside the vault. A file that is itself a link is never
written through. Writes go to a temporary file that is renamed. Glossa never deletes or renames a file, and never
overwrites a file it did not make. Files over 5 MB and files that are not UTF-8 are skipped with the reason.

**Not done.** No background or automatic sync, no watching the vault; a sermon's outline points are not exported (only
title, passages, text, details); a note's other passages beyond the first are not attached; a title change in Glossa does
not rename the file; wikilinks rewritten to plain text are not turned back into links when Glossa rewrites the file.
