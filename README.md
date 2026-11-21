# Glossa

A free, open-source Bible study app for Windows, macOS and Linux: fast, offline-first, and drivable by both people and AI agents. Glossa was called Xegesis until version 0.4.

Glossa keeps your library, notes and sermons on your own computer in open formats. It has no accounts and no telemetry; the few features that use the internet are off until you turn them on (see [what Glossa sends](docs/privacy.md)).

Status: under active development. Windows and macOS are released together; a Linux build follows.

## Installing (Windows)

- **Microsoft Store**: search for Glossa once it is listed there. The Store installs updates.
- **Direct download**: take `Glossa-<version>-windows-x64-setup.exe` from the [Releases page](https://github.com/Perlsowisdom/glossa/releases). It installs for your user only (no administrator prompt). The setup is not code-signed, so Windows SmartScreen may ask you to confirm: choose More info, then Run anyway. Compare the file with `SHA256SUMS` on the release page if you like. This build can check for updates once a day, but only if you turn that on; each update is verified with the project's own signature before it runs.

Uninstall it from Windows Settings, Apps. Your library (`%APPDATA%\Glossa`) is kept unless you tick "Also remove my library".

## Installing (macOS)

Take `Glossa-<version>-macos-universal.dmg` from the [Releases page](https://github.com/Perlsowisdom/glossa/releases). It runs natively on Apple Silicon and Intel Macs with macOS 11 or later. Open the disk image and drag Glossa onto Applications.

Glossa is not signed by an Apple Developer account yet, so macOS asks before the first open. Do this once:

1. In Applications, right-click (or Control-click) Glossa, choose **Open**, then **Open** again.
2. If there is no Open button, try to open Glossa once, then go to **System Settings, Privacy & Security**, scroll down to "Glossa was blocked" and choose **Open Anyway**.
3. As a last resort, in Terminal: `xattr -dr com.apple.quarantine /Applications/Glossa.app`

You can compare the file with `SHA256SUMS` on the release page. Your library is in `~/Library/Application Support/Glossa`; Claude Code and Codex are found in the usual places (`~/.local/bin`, Homebrew, nvm). An unsigned app cannot replace itself, so updates on macOS are a download: if you turn on update checks, Glossa tells you when a new version is out and opens its download page; drag the new Glossa onto Applications again, and your library is kept. Uninstall by moving Glossa to the Bin (and, if you want, deleting that folder).

## Coming from Xegesis

Glossa is the new name of Xegesis. Nothing of yours is lost when you install it:

- **Your library folder** `%APPDATA%\Xegesis` is renamed to `%APPDATA%\Glossa` the first time Glossa starts. It is one rename, never a copy; if it cannot be renamed (Xegesis is still running, say) Glossa keeps using the old folder and writes why to its log. A `Glossa` folder that already exists is never overwritten.
- **Books you added** stay in `Documents\Xegesis Library`; Glossa keeps using that folder if it is the only one there. Library folders marked by Xegesis, and exports and backups made by it, are still recognised.
- **Saved keys** (AI assistant, Bible sources) are read from the old keychain entry the first time they are needed and saved under the new one. Window and view settings are carried over too.
- **Environment variables** `XEGESIS_*` still work as a fallback for the matching `GLOSSA_*` name (for example `GLOSSA_LIBRARY`, `GLOSSA_KEYRING_SERVICE`, `GLOSSA_UPDATE_KEY`). The new name wins when both are set.
- **The setup** offers to remove an installed Xegesis (silently when it runs as an update). The command-line tool is now `glossa` (it was `xegesis`).

## Building

Needs Go (see `go.mod`). On Linux, also `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`. On macOS, `scripts/package-macos.sh`
builds the universal app and disk image (to build just the binary, set
`CGO_LDFLAGS="-framework UniformTypeIdentifiers"`).

```sh
go test ./...
go build ./cmd/glossa                                       # CLI
go build -tags desktop,production -o glossa-desktop ./app   # desktop app (add ,webkit2_41 on Linux)
```

Release builds (setup, Store package and macOS disk image) are made by `scripts/package-nsis.ps1`, `scripts/package-msix.ps1` and `scripts/package-macos.sh`; `docs/releasing.md` has the whole process. `go run ./cmd/glossa version` prints the version a build was stamped with.

## Using the CLI

Everything Glossa does is a command. The CLI, the desktop app and the JSON channel all run the same ones.

```sh
glossa commands                                    # list commands with their parameters
glossa run verse.parse '{"ref":"Rom 8:28-30"}'
glossa run note.create '{"body":"All things work together","ref":"Rom 8:28"}'
glossa run history.list '{"object_type":"notes","object_id":"<id>"}'
glossa serve                                       # one JSON request per line on stdin
```

The library lives in your user config folder (`Glossa/library.db`); set `GLOSSA_LIBRARY` or `-library` to use another.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) first. Glossa is designed clean-room from public sources: contributions must not use or copy any commercial Bible software.

## Licence

Licensed under either of

- Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE))
- MIT License ([LICENSE-MIT](LICENSE-MIT))

at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this work, as defined in the Apache-2.0 licence, shall be dual-licensed as above, without any additional terms or conditions.
