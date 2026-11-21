# 0013: Folder sync

Status: Proposed (T20a and T20b; for the user to accept). Builds on 0004, 0005, 0009 and 0010.

## Context

ADR 0005 makes sync an add-on that exchanges change records. The user chose the first transport on
21 November 2026: a folder they already sync with OneDrive, Dropbox, Google Drive, iCloud or
Syncthing. Glossa has no server and no account, and the user's sync service must never be able to
read their notes.

## Decision

**Where.** Each device writes only to its own folder, `<chosen folder>/Glossa Sync/<device-id>/`, and
reads everyone else's. No file is ever written by two devices, so the sync service sees no write
conflicts. The code is `internal/sync`; the transport is an interface (`List`, `Read`, `Create`,
`Remove`, `RemoveAll`), and the folder is its first implementation. Files are made whole and then moved
into place, so a reader never sees half a file; a `.tmp-` name is ignored.

**Files.**

| File | Holds |
| --- | --- |
| `Glossa Sync/key.json` | Not secret: format, `argon2id`, its cost settings, a random 16-byte salt, and a small encrypted check value that tells a right password from a wrong one |
| `<device-id>/00000001.chg`, `00000002.chg`, ... | This device's changes since the previous file, from the `changes` log (ADR 0004), at most 5000 per file |
| `<device-id>/snap-<n>.snap` | A snapshot of everything this device knows, covering its own change files up to number `n` |
| `forgotten/<device-id>` | Marker: that device was removed (see "Forgetting") |
| `blobs/<sha256>.<n>.c`, `blobs/<sha256>.m` | Books and modules (see "Books, Bibles and modules" below) |

Inside, a file is: `"GLSY"`, format version (1), kind (`C` change, `S` snapshot, `K` key check), a random
24-byte nonce, then the XChaCha20-Poly1305 ciphertext of the gzip-compressed JSON. The header, the device
ID and the file number are the associated data, so a file renamed, moved to another device's folder, given
another number, swapped with another or cut short is refused. The JSON holds the format, kind, device,
device name, number, schema version and the change records exactly as in the `changes` table.

**Crypto.** The key is Argon2id (`golang.org/x/crypto/argon2`) over the password with the folder's salt:
3 passes, 128 MiB, 4 lanes, 32-byte key. The cost is stored in `key.json`, so it can be raised later; costs
read from a folder are bounded (8 MiB to 1 GiB, 1 to 20 passes) so a tampered file cannot choose none or
too much. Files use XChaCha20-Poly1305 (`golang.org/x/crypto/chacha20poly1305`). Both packages are
BSD-3-Clause, which ADR 0003 allows. Passwords must be at least 10 characters, and a strength hint is
shown. The password is never written to `library.db`, a file or a log, or put in an error; it can be saved
in the operating system credential store (`internal/integrations/secrets`) only when the person asks. A
lost password means the synced copy cannot be read; each device's own data is unaffected.

**Applying changes.**

- A device reads, in order, each other device's files it has not applied (a counter per device is kept in
  `sync_state`), decrypts and checks all of them first, and applies them in one database transaction. If
  any file is missing from the middle of the sequence, short, altered, from another password, for a table
  or field this version does not know, or written by a newer Glossa, **nothing is applied**, the folder is
  not changed and the error says why.
- Applying is idempotent: a change record is inserted only if its ID is new, and an object's state is
  rebuilt from its whole history, so the order files arrive in does not matter and applying a file twice
  changes nothing.
- **Clock.** Changes carry the hybrid logical clock already in the log. A device moves its clock past
  every value it reads, so an edit made after reading another device's change always sorts after it, even
  if its wall clock is behind. A file whose clock is more than a day ahead of the reader's is refused, so
  one wrong clock cannot make a device win every conflict.
- **Last writer wins per field**: the change with the latest clock value sets the field. Edits to
  different fields of one object both survive.
- **Deletes** are tombstones: a soft delete sets `deleted_at` like any other field, so a later restore
  from the bin wins over an earlier delete, and an edit to another field survives a delete. Hard deletes
  remove the row.
- **Purges** (ADR 0010) propagate as purges: the receiver removes the row and erases the values from its
  copy of the object's history, and any later change to it arrives with its values erased. A device that
  writes a purge, or applies one, compacts (below) so the old values also leave its files in the folder.
- **Text documents** (note and document titles and bodies): when two devices edited the same field from
  different starting texts, the newer edit wins and the older one is kept. It stays in the object's change
  history (restorable) and is listed by `sync.conflicts` with the note "Changed on <device> too".
  Conflicts are worked out from the whole history, so every device reaches the same list.

**Snapshots and compaction.** A snapshot holds, for every object, its create, the latest change to each
field and its latest delete or purge, from all devices, with the older "before" values dropped. A device
that is new to a folder starts from the newest snapshot of each device, then reads the files after it. A
device compacts when it has more than 50 change files of its own, when a purge passes through, or on
request: it writes a snapshot and deletes only its own older change files and snapshots. A device that was
behind a compaction jumps to the snapshot. Each device keeps its full local history.

**Preview.** `sync.preview` runs the merge in a transaction that is rolled back and reports, per kind,
the objects on this device, in the folder and after merging, what would be created, updated, deleted,
purged or kept as conflicts here, and how many changes would be sent. It changes neither side. Setting
sync up does not merge anything; merging starts only when the person runs `sync.now` after the preview.

**Forgetting.** `sync.forget_device` writes `forgotten/<device-id>` and deletes that device's folder.
Every device ignores it from then on, and if it syncs again it is told to set up sync afresh. Changes it
already sent stay in every library. If the folder loses a device's own files (for example an older copy
is restored), that device writes them again.

**Commands.** `sync.setup`, `sync.unlock`, `sync.preview`, `sync.now`, `sync.status`, `sync.devices`,
`sync.forget_device`, `sync.compact`, `sync.conflicts`, `sync.conflicts.dismiss`, `sync.stop` and
`sync.password.strength`, so the interface, the CLI and MCP all use the same code. Errors have codes
(`wrong_password`, `folder_missing`, `corrupt`, `missing_file`, `disk_full`, ...) with plain messages.

**Privacy.** Glossa makes no network request for sync; the user's own sync service moves the folder.
`docs/privacy.md` says so.

## What it does not do yet

- The Settings screen, background sync at start, every few minutes and on quit, and real-app checks (T20c).
- Changing the password (re-encrypting every device's files needs every device's cooperation; for now, set
  up a new folder). Forgetting a device does not change the key, so a device that has the password and a
  copy of the folder can still read it.
- A local backup offer before the first sync (T29 exists; T20c adds the prompt).
- Hiding file sizes, change counts and write times from the sync service: it sees that files exist and how
  big they are. It cannot read or alter them undetected.
- Detecting a sync service that silently rolls back or hides a device's newest files; only gaps in the
  middle of a sequence are detected.

## Books, Bibles and modules (T20b)

Status stays Proposed.

**What is per device and what is shared.** A book's file lives at a path that exists on one computer, so
`files` (item, library folder, relative path, hash) and `library_folders` (marker ID, drive, path) are
**per-device tables**: writes to them are still logged, but they are never exported, never put in a snapshot
and are ignored if they arrive. The `items`, `item_people`, `item_facets`, `licences`, collections and the rest
of the catalogue are shared as before, so a book appears in every library with its title, authors and
licence. A few settings also mean something on one device only and are never sent: `library.search_roots`,
`library.scanned_at.*`, `backup.schedule` and `device_id`. (`sync_*` tables, `changes` and `devices` stay local
as in T20a.)

What travels for the files themselves is the new shared table **`shared_files`** (schema 14): an ID derived from
the book and hash, or from the module ID (so two devices that list the same thing make the same row), `kind`
(`book` or `module`), `item_id` or `module_id`, `name`, `format`, `size_bytes`, `sha256` and `deleted_at`. It
is merged like any other table. Each device also keeps a local, unsynced `sync_files` record (hash, size,
modified time, whether the file is its own or came from the folder, and its own `files` row) so it knows what
it holds, does not re-hash an unchanged module at every sync, and takes off only files that sync itself put
there. Bible modules are not in `items` at all (they are `modules/<id>.db` files), so a module's identity is its
ID. A module's licence is inside the module file, so it travels with it; a book's licence is the `licences`
row. A book whose licence has `store_allowed` off is never uploaded.

**Blobs.** A file is stored under the SHA-256 of the plain file. It is cut into pieces of at most 4 MiB
(`BlobChunk`), and each piece is compressed and encrypted separately with the folder's key as
`blobs/<sha>.<000000>.c` (kind byte `B`; the hash and the piece number are authenticated, so a piece cannot be
moved to another blob or position). The last file written is `blobs/<sha>.m`, a manifest (kind byte `M`) with
the size, the piece size and the hash of every piece. **The manifest is what makes a blob exist.** A device
that was cut short leaves pieces and no manifest, so nobody mistakes it for a whole blob, and the next upload
keeps the pieces that are right, rewrites any that are wrong and writes the manifest. Each file is made whole
and moved into place by the transport, as for change files. Memory use is about one piece, for sending and
receiving. A file is sent once: a device first looks for the manifest, so the same book on two devices is one
blob.

**Downloading.** Pieces are checked against the manifest as they arrive and the finished file against the
blob's name before it is moved into place; the book is then added to the library as a `files` row for the
synced item in a local library folder (`synced-books` in the app data folder, which is not itself synced), so
scans, search and the reader treat it like any other. A missing piece or manifest is "waiting" (the sync
service may still be copying it); an altered, shortened or swapped piece, or a bad manifest, is a failure that
names the file. In both cases nothing is installed: the temporary file is deleted and no library record is
made. The rest of the sync carries on. If the same hash is already on this device (the same book added on both
devices), the local file is copied and nothing is downloaded; the two items stay two items, one per device
(merging them is not done).

**When.** Per device, `sync.books`: **all** (default) downloads every book at sync time; **opened** lists the
books but downloads one only when it is opened (`sync.fetch`, which the reader calls). Modules always come at
sync time. `sync.status` and `sync.progress` give bytes and files done and in total, and the engine has an
`OnProgress` callback, so the interface can show them.

**Deleting.** Deleting a book is the item's deletion, which already propagates; a device removes the copy
that sync put there (never a file the person added themselves). Removing a module from a device marks its row
deleted, and other devices remove it. A blob is removed from the folder when no live file names it and every
file that did has been deleted for 30 days (`BlobGrace`), so the bin can still restore a book; any device can
do this, and a blob this library has never heard of is left alone because it may be another device's upload
that is not announced yet. A device that has been offline longer than the grace period loses nothing it already
has.

**Limits.**
- A module or book edited in place on one device is sent as a new blob; for a book the old blob stays until the
  book is deleted. Two devices that have a module with the same ID but different content settle on the one
  changed last.
- A device offline for longer than the grace period may find a deleted-and-restored book's blob gone; it then
  keeps (or lacks) whatever it had.
- Sizes of blobs and their number of pieces are visible to the sync service.
- Hashing and sending a large library takes time; the first sync on a big library sends everything.

## Consequences

- The `changes` table is now the wire format; its meaning cannot change without a new format version.
- Schema version 13 adds `sync_state` and `sync_conflicts`, which are local and never synced.
- Schema version 14 adds `shared_files` (synced) and `sync_files` (local), and makes `files` and `library_folders`
  per-device.
- A device with a newer schema than another writes files the older one refuses until it updates.
