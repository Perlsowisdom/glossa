# 0014: Connections

Status: **Proposed** (T40a). Builds on 0005, 0009 and 0011; **adds Zotero (the app on this computer, and
zotero.org) to the list of network features in ADR 0009.**

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
