# 0013: Folder sync

Status: Proposed (T20a; for the user to accept). Builds on 0004, 0005, 0009 and 0010.

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
| `blobs/` | Reserved for T20b (books and modules, content-addressed) |

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

- Books, Bibles and modules (T20b: encrypted blobs in `blobs/`, on-demand download, progress).
- The Settings screen, background sync at start, every few minutes and on quit, and real-app checks (T20c).
- Changing the password (re-encrypting every device's files needs every device's cooperation; for now, set
  up a new folder). Forgetting a device does not change the key, so a device that has the password and a
  copy of the folder can still read it.
- A local backup offer before the first sync (T29 exists; T20c adds the prompt).
- Hiding file sizes, change counts and write times from the sync service: it sees that files exist and how
  big they are. It cannot read or alter them undetected.
- Detecting a sync service that silently rolls back or hides a device's newest files; only gaps in the
  middle of a sequence are detected.
- Device-specific tables (`files`, `library_folders`) are synced like everything else for now; T20b decides
  what is per-device.

## Consequences

- The `changes` table is now the wire format; its meaning cannot change without a new format version.
- Schema version 13 adds `sync_state` and `sync_conflicts`, which are local and never synced.
- A device with a newer schema than another writes files the older one refuses until it updates.
