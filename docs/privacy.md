# What Glossa sends over the network

ADR 0009: Glossa sends no telemetry. Only the features below use the network, each off until you turn
it on, with one exception: checking for updates (direct download only) is on unless you turn it off
(ADR 0016). This page says exactly what each one sends. Other features add their sections when they are built.

## Bible catalogues (T25)

Off until you turn them on: the "Turn on Bible catalogues" button in Bibles → Get Bibles, or
`bible.sources {"enable": true}`. `bible.sources {"enable": false}` turns them off again; the cached
lists stay on disk and can still be browsed offline.

Every request is an HTTPS `GET` (or `HEAD` for a size) with the header `User-Agent: Glossa` and no
cookies, account, identifier, search terms or information about your library. The servers see your IP
address and which files you request, as with any download.

| When | Requests |
| --- | --- |
| You refresh the catalogues (`bible.sources` with `refresh`) | `https://crosswire.org/ftpmirror/pub/sword/masterRepoList.conf`; `mods.d.tar.gz` from each HTTPS repository it lists (for example `https://crosswire.org/ftpmirror/pub/sword/raw/mods.d.tar.gz`); `https://ebible.org/Scriptures/translations.csv` |
| You click Install, before anything is downloaded (`bible.get` with `preview`) | eBible.org only: `HEAD https://ebible.org/Scriptures/<id>_usfm.zip` for the size, and, when the catalogue does not name the licence, `https://ebible.org/<id>/copr.htm` |
| You confirm the install (`bible.get`) | SWORD: the module's data files (`ot.bzs`, `ot.bzv`, `ot.bzz`, `nt.…`, or `ot`, `ot.vss`, `nt`, `nt.vss`), or a dictionary's `<name>.idx`, `.dat`, `.zdx`, `.zdt` from its repository folder; eBible.org: `https://ebible.org/Scriptures/<id>_usfm.zip` |

Listing, searching the catalogues and checking for updates (`bible.available`, `bible.updates`) read the
cache only and never use the network.

The open datasets in Get Bibles (T27) send nothing until you install one, and then only `GET` requests
for its files: STEPBible's from `https://raw.githubusercontent.com/STEPBible/STEPBible-Data/master/…`
(GitHub), OpenBible.info's `https://a.openbible.info/data/cross-references.zip`. Their entries appear
only while the catalogues are on.

## Free books from CCEL (ccel.org)

Off until you turn it on: the switch "Christian Classics Ethereal Library (ccel.org)" in Settings → Privacy, or
`ccel.settings {"enable": true}`. Turning it off stops every request; the cached list stays on disk.

Every request is an HTTPS `GET` (or `HEAD`) with the header `User-Agent: Glossa` and no cookies, account,
identifier, search terms or information about your library. ccel.org sees your IP address and which pages and
files you request. Glossa waits at least ten seconds between requests, as ccel.org's robots.txt asks. Terms and
formats are in `docs/sources/ccel.md`.

| When | Requests |
| --- | --- |
| You turn it on, and later when the list is more than a week old and you open From Logos or press refresh (`ccel.refresh`) | `https://ccel.org/index/title`: one page listing every book (titles and authors) |
| You press Get free copy, before anything is downloaded (`ccel.get` with `preview`) | The first 32 KB of the book's file `https://ccel.org/ccel/<a>/<author>/<book>.xml` for its rights note, and `HEAD https://ccel.org/ccel/<a>/<author>/<book>/cache/<book>.epub` for the size |
| You confirm (`ccel.get`) | The rights note again, then `https://ccel.org/ccel/<a>/<author>/<book>/cache/<book>.epub` |

Only books whose rights note says "Public Domain" are downloaded. They are added to your library as public
domain with a credit to CCEL. Matching From Logos books to CCEL books reads the cached list only and sends
nothing, and nothing about your Logos library leaves your computer.

## Updates (T23, direct download only)

**On unless you turn it off** (ADR 0016, which changes ADR 0009's "off until you turn it on" for this one
feature). Glossa checks once a day while it is open; there is no question at first start. Turn it off any time in
Settings → Updates (the switch "Check for updates automatically", or `update.settings.set {"enabled": false}`):
then no request is made at all. Someone who answered "No thanks" to the old first-run question stays off; someone
who never answered is on. The Microsoft Store build has no updater at all; the Store installs its updates.

Every request is an HTTPS `GET` with the header `User-Agent: Glossa`, and nothing else: no cookies,
account, identifier, version of this app, or information about your library or this computer. GitHub sees your IP
address and which file you ask for, as with any download.

Settings → Updates has a **channel**: Stable (finished releases only) or Preview (the newest release, test versions
included). Until you choose, a test version of Glossa is on Preview and any other is on Stable. Which version you
are on is not sent; the channel only decides which address is asked.

| When | Requests |
| --- | --- |
| Once a day while Glossa is open and updates are on, or when you press Check for updates (`update.auto`, `update.check`). Stable channel | `https://github.com/Perlsowisdom/glossa/releases/latest/download/latest.json`: version, installer address, SHA-256 and signature of the newest finished release |
| The same, Preview channel | `https://api.github.com/repos/Perlsowisdom/glossa/releases?per_page=10` (GitHub's public list of releases; no key or account), then the `latest.json` of the newest release in it that is not a draft, from `https://github.com/Perlsowisdom/glossa/releases/download/…` (an address anywhere else is refused) |
| You press Install and restart (`update.install`) | The same manifest again, then the installer it names, from the same release (GitHub forwards the download to its own file servers) |

A check that fails (no internet, GitHub down) is tried again within the hour and says nothing outside Settings →
Updates, which shows the last error. If GitHub says it is being asked too often (HTTP 403 or 429, `Retry-After`,
`X-RateLimit-Reset`), Glossa waits at least until the next daily check. The settings (on or off, channel, last
check) stay on this computer and are not synced.

When a newer version is found you see a small notice (Install and restart, or Later) and a marker on the rail.
Nothing is downloaded to install until you press Install and restart. A version that is not newer than the one you
have is never offered, so switching channel never installs an older version.

Before anything runs, the installer's SHA-256 must match `latest.json` and its Ed25519 signature must
verify against the public key built into the app (`internal/update/pubkey.txt`). The setup is not
code-signed, so this signature is what keeps updates safe: a file that fails either check is deleted and
never run. The setup then installs over the old version and starts it again; your library is not touched.

On macOS and Linux the app cannot replace itself, so nothing is downloaded by Glossa: pressing Download only opens the new version's download (the disk image on macOS; on Linux the `.deb` when Glossa was installed from the package; otherwise the release page) in your web browser, and your browser fetches it. The check requests above are the only ones Glossa makes.

## AI assistant (T36a)

Off until you choose a provider in Settings → Assistant → Model (`assistant.config.set`). Choosing one
sends nothing. Your key is kept in the system keychain and sent only to the provider's address, in an
`Authorization: Bearer` header (for Claude also `x-api-key`, with `anthropic-version`). ADR 0011 has the
details.

| Provider | Address (you can change it under More options) |
| --- | --- |
| Claude | `https://api.anthropic.com/v1` (Anthropic's OpenAI-compatible endpoint) |
| OpenAI | `https://api.openai.com/v1` |
| Gemini | `https://generativelanguage.googleapis.com/v1beta/openai` |
| OpenRouter | `https://openrouter.ai/api/v1` |
| Local model | `http://localhost:11434/v1` (Ollama; LM Studio uses `http://localhost:1234/v1`): stays on this computer |
| Custom | the address you give; on this computer only when it is `localhost`, `127.x.x.x` or `::1` |

| When | Requests |
| --- | --- |
| The Model page lists models, after a key is saved (`assistant.models`) | `GET <address>/models` |
| You press Test connection (`assistant.test`) | `GET <address>/models`; when the server has no model list, one `POST <address>/chat/completions` with the message "Reply with the word OK." and nothing of yours |
| You send a question (`assistant.chat`) | `POST <address>/chat/completions`, streamed, once per step of the reply. It holds the model name, Glossa's instructions to the model, the conversation so far (your questions with the passage and the note or sermon open when you asked: their reference, title and ID), the list of read-only tools, and each tool's result |

### Claude Code and Codex (T36b)

Choosing **Claude Code** or **Codex** uses the command-line tool already on this computer (`claude`,
`codex`). **These tools do their own sign-in and their own network calls**: Glossa holds no key for
them, does not see which service or model they use, and cannot limit where they send what they are
given. Your question, the chat so far and what the tools read from your library go to their provider
under its terms, so Glossa treats them as online: personal copies of books are never given to them, and
quoting limits apply.

| When | What happens |
| --- | --- |
| The Model page opens, or you ask `assistant.cli.detect` | Looks for `claude` and `codex` on PATH and runs each with `--version`. No network from Glossa |
| You press Test connection | Nothing is run or sent; it reports the version found |
| You send a question | Runs the tool with no window, in an empty temporary folder, with the question, Glossa's instructions and the chat so far on its standard input, and a config naming Glossa's own `mcp-assistant` mode. That mode offers only the read-only tools, each result passing the licence gate; the tool's own file, shell and web tools are switched off. Pressing Stop ends the tool and everything it started |

The privacy panel shows the exact command and what is sent to it before your first message to each tool.

Tool results are the text the assistant read for you: Bible passages, search snippets and book sections,
your notes, sermons and highlights, and reports. Text from works with a licence goes through the same
rules as quoting into a note: open texts in full, limited texts only up to their quoting limit, and
personal copies of books **never to an online provider**, only to a model on this computer. Each tool
call shows in the chat as a line you can open to see exactly what was sent.

Before your first message to a provider (and again if its address changes), the chat shows what is sent
and where, and sends nothing until you press Send. What the provider does with it is governed by its own
terms. Chats are kept in memory only, until you quit, unless you press "Save as note".

## Connections: Zotero (T40a)

Off until you turn it on: the switch "Use Zotero" in Settings → Connections, or `connect.enable {"id": "zotero"}`.
Turning it off (`connect.disable`) stops every request; the copies of sources you cited stay in your library.
Glossa only ever **reads** from Zotero. ADR 0014 has the design.

Two places can answer, tried in this order:

1. **The Zotero app on this computer.** Requests go to `http://localhost:23119/api/users/0/...` and never leave
   this computer. Zotero answers only after you tick "Allow other applications on this computer to
   communicate with Zotero" in its Settings → Advanced.
2. **zotero.org**, only if you saved a Zotero key (Settings → Connections → More options) and the app could not
   be reached. Requests are HTTPS `GET`s to `https://api.zotero.org` with the headers `Zotero-API-Key: <your key>`,
   `Zotero-API-Version: 3` and `User-Agent: Glossa`, and no cookies. Zotero sees your IP address, your key (so which
   account), and what you ask for: the page of your library or collection you open, your search words, and the
   keys of items you cite or refresh.

| When | Requests |
| --- | --- |
| You turn it on, press Test connection, or open Settings → Connections while it is on (`connect.test`) | The app: `GET .../items/top?limit=1&format=csljson`. If that fails and a key is saved: `GET https://api.zotero.org/keys/<your key>` (once per run, to learn your user ID), then `GET /users/<id>/items/top?limit=1&format=csljson` |
| You browse or search your library (`zotero.items`, `zotero.search`, `zotero.collections`) | `GET .../items/top`, `.../collections/<key>/items/top` or `.../collections`, with `format`, `limit`, `start`, `sort`, `q` (your search words) |
| You cite an item or open one (`zotero.cite`, `zotero.item`) | `GET .../items/<key>?format=csljson` |
| You refresh the copies you kept (`zotero.refresh`) | `GET .../items?itemKey=<keys>&format=csljson` with `If-Modified-Since-Version`; Zotero answers "not modified" when nothing changed |

When Zotero asks Glossa to slow down (`Backoff`, `Retry-After`), Glossa waits before asking again. Your key is kept
in the system keychain (`connect.zotero.key`), never in `library.db`, a file or a log, and is never shown again.
Nothing from your Glossa library (notes, documents, books) is sent to Zotero. The copies of sources you cited (their
title, authors and other details, as Zotero describes them) are kept in `library.db` so documents still show their
citations without Zotero, and they sync with your other devices like the rest of your library (ADR 0013).

## Connections: Readwise (T40d)

Off until you turn it on: the switch "Use Readwise" in Settings → Connections, or `connect.enable {"id": "readwise"}`.
Turning it off (`connect.disable`) stops every request; the notes already made stay. ADR 0014 has the design.

Glossa only ever **reads** from Readwise, and only talks to `https://readwise.io`. Every request is an HTTPS `GET`
with the headers `Authorization: Token <your token>` and `User-Agent: Glossa`, and no cookies. Readwise sees your IP
address, your token (so which account) and what you ask for. Nothing from your Glossa library (books, notes,
documents, the titles of your books) is sent: the matching of Readwise sources to your library books happens on this
computer.

| When | Requests |
| --- | --- |
| You turn it on, press Test connection, or open Settings → Connections while it is on (`connect.test`, `readwise.test`) | `GET https://readwise.io/api/v2/auth/` |
| You press Preview or Import (`readwise.preview`, `readwise.import`) | `GET https://readwise.io/api/v2/export/?includeDeleted=true`, plus `updatedAfter=<time of your last import>` after the first import, plus `pageCursor=<cursor Readwise gave>` for each further page |

The time of the last import is kept in `library.db` on this device only. When Readwise answers "too many requests",
Glossa waits for the number of seconds in its `Retry-After` header before asking again, a few times at most. The token
is kept in the system keychain (`connect.readwise.key`), never in `library.db`, a file or a log, and is never shown
again. The highlights, notes and links Readwise sent are kept in `library.db` as notes, and sync with your other
devices like the rest of your library (ADR 0013); the link tables from Readwise sources to those notes sync too, so a
second device updates the same notes instead of making copies.

## Connections: Obsidian (T40d)

Off until you turn it on: the switch "Use Obsidian" in Settings → Connections, or `connect.enable {"id": "obsidian"}`.
ADR 0014 has the design.

**Nothing is sent over the network.** This connection is files on this computer and nothing else: it has no
network code at all. Glossa only reads and writes Markdown (`.md`) files in the Obsidian vault folder you choose,
and your notes and sermons never go anywhere else. What Obsidian or a sync service does with the vault afterwards
(Obsidian Sync, iCloud, Dropbox) is theirs.

| When | What Glossa does on this computer |
| --- | --- |
| You choose the vault (`obsidian.set_vault`) | Checks the folder exists and can be written to (makes and removes a small `.glossa-check-*` file) |
| You export (`obsidian.export`, or "Send to Obsidian" in a note's or sermon's ⋯ menu) | Writes one `.md` file per note or sermon you chose into one subfolder of the vault (default `Glossa`, changeable), with the title, `glossa_id`, `glossa_kind`, passages, tags and dates as front matter, and the text as Markdown. A file that is already up to date is not touched; one that changed in Obsidian is not overwritten |
| You import (`obsidian.import`) | Reads the `.md` files in that subfolder, another folder of the vault you choose, or the whole vault (not folders starting with a dot, such as `.obsidian`) and makes or updates notes. It never changes a file when importing |

**Where it writes.** Only inside the vault folder you chose: a path with `..`, an absolute path, a drive or stream
name (`:`), or a folder that is a symbolic link or Windows junction is refused, so nothing can be written or read
outside the vault through a link. A file is written to a temporary file in the same folder and renamed, and a
file that is itself a link is never written through. **It never deletes a file**, never renames one, and never
overwrites a file Glossa did not make (a name already in use gets " (2)"). Files over 5 MB and files that are not
UTF-8 text are skipped, with the reason shown.

**What is kept in `library.db`**, on this device only and never synced to your other devices: the vault folder and
subfolder (the setting `connect.obsidian.config`), the time and counts of the last export and import
(`connect.obsidian.sync`), and the table `obsidian_links` (migration `0019`), which remembers for each note or
sermon the file it is linked to (a path relative to the vault) and a fingerprint of what each side held at the last
sync. The fingerprints are what tell Glossa which side changed.

## Citation styles (T40b)

Off until you turn it on: the switch "Download more styles" in Settings → Citations, or `cite.styles_enable`.
The citation styles that ship with Glossa (SBL, Chicago, APA) need no internet. When the switch is on:

- **Searching** asks GitHub for the list of style names in the Citation Style Language project's public
  repository (`api.github.com/repos/citation-style-language/styles/git/trees/...`, two plain requests, kept in
  memory for an hour). **The words you search for are matched on this computer and are never sent.**
- **Downloading** one style fetches its file from `raw.githubusercontent.com/citation-style-language/styles/`
  (and, for a journal's style that follows another, the style it follows). Nothing else is sent: no account, no
  information about you, your library or your documents. As with any web request, GitHub sees this computer's
  internet address and a user agent of "Glossa".
- Downloaded styles go to the `styles` folder of your data folder, unchanged, with a `.licence.txt` beside each
  naming its licence (CC BY-SA 3.0 for the project's styles) and where it came from. Turning the switch off stops
  all requests; the styles stay and keep working.

Citing itself (library books, Bibles, Zotero items kept in your library, manual entries) never uses the network;
Zotero is covered by its own section above and its own switch.

## Connections: Any (T40c)

Off until you turn it on: the switch "Use Any" in Settings → Connections, or `connect.enable {"id": "any"}`. ADR 0014
has the design.

**Everything stays on this computer.** Glossa only talks to the Any app running on this computer, at
`http://127.0.0.1:31009` (the address Any listens on for other programs), and refuses any address that is not
on this computer. Nothing is sent over the internet by Glossa, and nothing goes to anyone but Any. Any then syncs
its own data as it always does; that is Any's business.

| When | Sent to the Any app on this computer |
| --- | --- |
| You turn it on, press Test connection, or open Settings → Connections while it is on (`connect.test`) | `GET /v1/spaces` with your key (or without one, to learn whether you are signed in) |
| You sign in (`any.pair_start`, `any.pair_finish`) | `POST /v1/auth/challenges` with the name "Glossa"; Any shows a 4-digit code; then `POST /v1/auth/api_keys` with that code |
| You pick a space or open the page picker (`any.spaces`, `any.pages`, `any.search`) | `GET /v1/spaces`; `POST /v1/spaces/<space>/search` with the words you type |
| You send a note or sermon (`any.send`) | `GET`/`POST /v1/spaces/<space>/properties` (to make a "Passages" property once), then `POST` (first time) or `PATCH` (after) `/v1/spaces/<space>/objects`: the title, the Markdown text and the passages as plain references; then `GET` the page back |
| You bring a page in (`any.import`) | `GET /v1/spaces/<space>/objects/<id>` |

Every request carries the header `Anytype-Version: 2025-11-08` and, after you sign in, `Authorization: Bearer <key>`.
The key is kept in the system keychain (`connect.any.key`), never in `library.db`, a file or a log, and is never
shown again; "Sign out of Any" removes it. The links between your notes and Any's pages are kept in `library.db` on
this device only and are not synced to your other devices.

## Sync through a folder (T20)

Glossa itself sends nothing over the network for sync, and has no server or account. When you set it up
(`sync.setup`, Settings → Sync), Glossa reads and writes encrypted files in a `Glossa Sync` folder inside a
folder you choose. If that folder is one your own sync service (OneDrive, Dropbox, Google Drive, iCloud,
Syncthing) copies between your devices, that service moves the files, under its own terms. Glossa never
contacts it.

| What is in the folder | Who can read it |
| --- | --- |
| `key.json`: the encryption settings and a random salt (never your password) | Anyone with the folder; it holds nothing about your data |
| Per-device change files and snapshots: your notes, highlights, lists, plans, folders, tags, sermons and settings | Only people who know your sync password. They are encrypted on your computer (XChaCha20-Poly1305, key from your password with Argon2id) before they are written |
| `blobs/`: your books and installed Bibles, commentaries and dictionaries (so each device has them), in pieces | Only people who know your sync password. Each piece is compressed and encrypted on your computer before it is written. Each file keeps its licence, and a book whose licence does not allow a stored copy is not sent |

Your sync service can see that the files exist, their names, sizes and times (for books, only the hash, the number of pieces and their sizes, not the titles), and which of your devices wrote
them (device IDs and file numbers are not encrypted). It cannot read them, and Glossa refuses any file that has
been altered. A lost password cannot be recovered: the synced copy cannot be read, though the data on each
device is fine. The password is kept in memory while Glossa runs, and saved in your computer's credential store
only if you choose "remember". It is never written to `library.db`, a file or a log. Deleting a note for good
also removes its text from the folder's files when Glossa next tidies them. See ADR 0013.

## Problem reports (T32)

**Nothing is sent by Glossa.** There is no crash-reporting service, no analytics and no automatic upload (ADR 0009).
A report is text the app builds for you to read; it goes nowhere unless you open it as a GitHub issue yourself and
press Submit there, and GitHub issues are public.

**What is logged on this computer.** Unless you turn it off, Glossa keeps a small problem log in its data folder
(`logs`, 1 MB, with one older file kept): its own messages (start-up, a command's name and the kind of error it
failed with, never what you typed or the command's parameters), any error Glossa caught in its Go code (with a stack),
errors the window caught (at most ten a minute), and, after a crash that ended the program, the crash's output. A
marker file in the data folder tells the next start that the last run did not end normally. Everything written to
the log has paths, e-mail addresses, URL query strings, keys and long hex or base64 runs removed first. Settings →
Privacy has the switch "Keep a problem log on this computer" (turning it off stops the log and deletes it) and
"Open the log folder". The log is never synced.

**What a report contains.** The text you see in Help → Report a problem (or after "Glossa ran into a problem. Review a
report?"): the Glossa version and channel, the operating system and architecture, the WebView2 version, the error and
its stack if there is a recent one, the last 200 log lines, and what you typed under "What were you doing?". The rules
that strip private content run again when the report is built, in one place (`internal/problems/strip.go`):

| Removed | Replaced by |
| --- | --- |
| Your home folder, Glossa's data folder, the library file (also `\?\` paths and `/`-style Windows paths) | `<home>`, `<data>`, `<library>` (everything under them goes too, so file names such as book titles never appear) |
| Any other absolute path (Windows, UNC, macOS, Linux, `file://`, `~/`) | `<path>` |
| E-mail addresses | `<email>` |
| URL query strings and fragments, and a user name or password in a URL | `?<removed>` |
| `key=`, `token=`, `password=`, `secret=` values, `Bearer` tokens, hex or base64 runs of 32 or more characters (and UUIDs) | `<secret>` |

Glossa also never writes note or document text, book titles, verse lists or search terms to the log. Quoted text in
an error message is shortened to `"…"`. You can still edit or delete anything in the text before it goes out.

**Where it goes.** Three ways out, all started by you: **Open as GitHub issue (public)** opens a pre-filled "new
issue" page on `https://github.com/Perlsowisdom/glossa/issues/new` in your browser (through the system, like any
link; the address holds the text, cut to about 7,000 characters if it is longer, oldest log lines first); **Copy**; and
**Save to file…** (a `.txt` file you choose). The text sent, copied or saved is exactly what the box shows. Glossa makes
no network request for any of this: your browser does, when you open the page, and GitHub sees what any web page
visit shows it.
