# What Glossa sends over the network

ADR 0009: Glossa sends no telemetry. Only the features below use the network, each off until you turn
it on. This page says exactly what each one sends. Other features add their sections when they are built.

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

Off until you turn it on: the first time Glossa starts, it asks once whether to check for updates, and
"No thanks" leaves it off. Turn it on or off later in Settings → Updates (`update.settings.set`).
The Microsoft Store build has no updater at all; the Store installs its updates.

Every request is an HTTPS `GET` with the header `User-Agent: Glossa`, and nothing else: no cookies,
account, identifier or information about your library or this computer. GitHub sees your IP address and
which file you ask for, as with any download.

| When | Requests |
| --- | --- |
| Once a day while Glossa is open and updates are on, or when you press Check for updates (`update.auto`, `update.check`) | `https://github.com/Perlsowisdom/glossa/releases/latest/download/latest.json`: version, installer address, SHA-256 and signature of the newest published release |
| You press Restart to update (`update.install`) | The installer named in `latest.json`, from the same release (GitHub forwards the download to its own file servers) |

Before anything runs, the installer's SHA-256 must match `latest.json` and its Ed25519 signature must
verify against the public key built into the app (`internal/update/pubkey.txt`). The setup is not
code-signed, so this signature is what keeps updates safe: a file that fails either check is deleted and
never run. The setup then installs over the old version and starts it again; your library is not touched.

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

Your sync service can see that the files exist, their names, sizes and times, and which of your devices wrote
them (device IDs and file numbers are not encrypted). It cannot read them, and Glossa refuses any file that has
been altered. A lost password cannot be recovered: the synced copy cannot be read, though the data on each
device is fine. The password is kept in memory while Glossa runs, and saved in your computer's credential store
only if you choose "remember". It is never written to `library.db`, a file or a log. Deleting a note for good
also removes its text from the folder's files when Glossa next tidies them. See ADR 0013.
