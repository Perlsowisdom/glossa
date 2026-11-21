# T20: Sync through a folder you already sync

Brief for agents (Sonnet), with the user's decisions from 21 November 2026. Read these first:
- `AGENTS.md`;
- ADR 0004 (change history), ADR 0005 (pluggable sync), ADR 0009 (privacy);
- `internal/store`, especially the `changes` log and commands.

## The user's decisions

- **Transport:** a folder the user already syncs with OneDrive, Dropbox, Google Drive, iCloud or
  Syncthing. Glossa has no server and no account. Keep the transport behind an interface, so other
  transports can be added later (ADR 0005).
- **What syncs: everything.**
  - The user's work and settings: notes, highlights, ink, sermons, lists, plans, folders, tags and
    preferences.
  - **Their books and installed Bibles and modules too.** These are copied between the user's own
    devices for personal use. That is not sharing, so personal-copy licences allow it. Keep each
    file's licence with it.
- **Encryption:** end to end. The key is derived from a **password** the user chooses.
  - Use **Argon2id** (`golang.org/x/crypto/argon2`, BSD) with strong parameters and a random salt
    stored in the folder.
  - Use XChaCha20-Poly1305 (`golang.org/x/crypto`) for the files.
  - Require a minimum length and show a strength hint.
  - Explain plainly that a lost password means the synced copy can't be read, though local data is
    fine. Offer to save it to the OS credential store on each device.

## Design

- **Change files:**
  - Each device writes its own append-only, encrypted change files into
    `<folder>/Glossa Sync/<device-id>/`, built from the `changes` log (ADR 0004).
  - Other devices read them and apply what's new.
  - No device ever writes another device's files, so the cloud service never sees write conflicts.
- **Merging:**
  - Last-writer-wins per field, by a hybrid logical clock.
  - Deletes are tombstones, and a purge (ADR 0010) propagates as a purge.
  - Text documents (notes, sermons) whose text was edited on two devices since the last sync keep
    both versions: the newer one wins, and the other is saved in version history with a "changed
    on <device> too" notice.
- **Blobs (books, modules, ink-heavy items):**
  - Store them content-addressed (by SHA-256), encrypted, in `<folder>/Glossa Sync/blobs/`.
  - Upload each blob once; other devices download it when they need it.
  - Show progress and the total size, with an option to keep books on demand only (download when
    opened).
- **Snapshots:** a periodic snapshot, so a new device doesn't replay everything, and old change files
  can be compacted.
- **Safety:**
  - Back up first: offer a local backup (T29) before the first sync on a device that already has
    data.
  - **Never** touch the user's existing library until the first sync's preview is confirmed:
    "This device has X notes, the synced folder has Y; they'll be merged."
- **Interface:** Settings → Sync.
  - **Setup:** choose the folder, set or enter the password, show the device name, and preview the
    merge.
  - **Afterwards:** sync on/off, "Sync now", the last sync time, the size used, and the devices seen,
    with "Forget a device" and "Change password", which re-encrypts.
  - Sync runs at start, every few minutes while open, and on quit.
  - Errors are shown in plain words: the folder is missing, the password is wrong, the disk is full.
- **Privacy:** Glossa itself sends nothing over the network for sync; the user's own sync service
  moves the folder. Update `docs/privacy.md` and write **ADR 0013, "Folder sync"**, with status
  Proposed, for the user to accept.

## Parts (one agent each, commit after each step)

- **T20a: the core.**
  - Crypto, key derivation, the change-file format, the clock, merging, tombstones, snapshots.
  - Two simulated devices in Go tests, each with a temp library and one shared temp folder. Test
    concurrent edits, deletes, purges, text conflicts, a wrong password, a missing or truncated or
    tampered file (it must fail safely), and a new device joining from a snapshot.
- **T20b: blobs and books.** Content-addressed encrypted blobs, books, modules and Bibles, on-demand
  download, progress, and deduplication. Go tests with the same two-device setup.
- **T20c: the interface and real-app checks.**
  - Settings → Sync, setup, the merge preview and status.
  - A real-app e2e check that runs **two app instances**, each with its own temp library and one
    shared temp folder: a note made on A appears on B; a book added on A opens on B; an edit on both
    keeps both versions. Take screenshots.

## Rules

- No network in tests.
- Never use the user's real library or real cloud folders: temp folders only.
- Run `npm ci`; no junctions.
- Run long commands with timeouts, and close every process you start.
- Don't commit `frontend/dist`.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`.
- Don't push.
- Run only touched e2e files while working, and the full suite once at the end.
