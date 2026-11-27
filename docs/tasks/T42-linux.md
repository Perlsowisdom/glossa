# T42: Linux package and release job

Brief for one agent (Sonnet), 27 November 2026. The installers task (T23) promised Windows, macOS and
Linux. Releases ship Windows and macOS only today, and the README says "a Linux build follows". Read
these first:
- `AGENTS.md`;
- `docs/releasing.md`;
- `.github/workflows/ci.yml` and `release.yml`;
- `scripts/package-macos.sh`, `check-macos.sh` and `release-files.ps1`;
- `internal/update` (especially `Manual` and the `mac` entry in `latest.json`).

Follow the macOS work as the pattern: it was the last platform added. Its commits start at `a2c1294`
(bundle and DMG) and `598a76a` (updater).

## Decisions (coordinator defaults; flag anything that argues against them)

- **Formats:**
  - **A `.deb`** for Debian, Ubuntu and their relatives. It declares its dependencies, at least
    `libgtk-3-0` and `libwebkit2gtk-4.1-0`, so `apt install ./Glossa-…deb` pulls them in.
  - **A `.tar.gz`** for everything else: the binary, a `.desktop` file, the icon and a short
    `README-linux.txt` that names the libraries needed.
  - **No AppImage or Flatpak for now.** Bundling WebKitGTK is large and fragile, and Flathub needs its
    own review. Record that choice and its reasons in `docs/releasing.md`.
- **Architecture:** x86-64 only for now. Note arm64 as later.
- **Build:**
  - Build on `ubuntu-22.04`. Its older glibc lets the binary run on more systems. Use the build tags
    `desktop,production,webkit2_41`.
  - Ubuntu 22.04 has WebKitGTK 4.1, so check that it installs there. If not, use `ubuntu-24.04` and
    say which systems that leaves out.
- **Install layout for the `.deb`:**
  - `/usr/bin/glossa` (or `/opt/glossa` with a link in `/usr/bin`);
  - `/usr/share/applications/io.github.perlsowisdom.glossa.desktop`;
  - icons under `/usr/share/icons/hicolor/<size>/apps/`;
  - copyright and licence files under `/usr/share/doc/glossa/`.

  Use the same app ID as macOS (`io.github.perlsowisdom.glossa`).
- **Updates:** like macOS, Linux can't replace itself safely, so it is `Manual`. Glossa says a new
  version is out and opens the download page.
  - Add a `linux` entry to `latest.json` with its signature, if the format has per-platform entries.
    If it doesn't, follow what macOS does.
  - The check still verifies signatures where it downloads anything.
- **Data folder:** check that `internal/appdir` uses `$XDG_DATA_HOME` (or `~/.local/share/Glossa`) and
  `$XDG_CONFIG_HOME` on Linux, and document it.
- **Claude Code and Codex:** check that `internal/assistant`'s PATH search covers `~/.local/bin`,
  `/usr/local/bin` and npm's global bin on Linux, as macOS does.

## What to build

1. **`scripts/package-linux.sh <version>`:**
   - Build the frontend if needed. CI uses the committed `frontend/dist`, as macOS does.
   - Build the binary with the version stamped, the same way the other scripts do.
   - Make the `.deb` (with `dpkg-deb --build --root-owner-group`) and the `.tar.gz`.
   - Name them `Glossa-<version>-linux-amd64.deb` and `Glossa-<version>-linux-amd64.tar.gz`.
   - Map the version for Debian. Check `tools/versionmap` for how macOS does it. A suffix such as
     `0.6.1-alpha` must sort before `0.6.1`, so use `~`, giving `0.6.1~alpha`.
2. **`scripts/check-linux.sh <deb> <version>`:**
   - Check the package's control fields and file list.
   - Install it in the runner with `sudo apt-get install -y ./…deb`.
   - Run `glossa version` or the CLI equivalent, and check the stamped version.
   - Start the desktop binary under `xvfb-run` for a few seconds and check that it doesn't crash on
     start.
   - Check the `.desktop` file with `desktop-file-validate`.
3. **CI (`ci.yml`):** on the existing `ubuntu-24.04` job, or a new `linux-package` job, build and
   check the package on every push, as macOS does.
4. **Release (`release.yml`):**
   - Add a `linux` job on a standard Ubuntu runner that builds, tests and uploads the two files as an
     artifact with `retention-days: 7`.
   - The `release` job needs `[prepare, windows, macos, linux]`.
   - Pass the Linux files to `release-files.ps1`, and include them in `SHA256SUMS` and, if relevant,
     `latest.json`.
   - If the Linux job fails, there is no tag and no release, as for the other two.
5. **The app:**
   - `Manual` is true on Linux.
   - Anything else the update notice and Settings → Updates need to say the right thing on Linux.
   - Plain words, as on macOS.
6. **Docs:**
   - `README.md`: an "Installing (Linux)" section covering the `.deb` and the `.tar.gz`, the needed
     libraries, where the library is kept, how updates work, and how to uninstall.
     Replace "a Linux build follows".
   - `docs/releasing.md`: Linux in the table, plus a "Linux notes" section.
   - `docs/privacy.md`, only if something changes.

## Tests

- **Go:** `latest.json` with a Linux entry, the Linux version mapping, `Manual` on Linux, and the XDG
  paths. Put Linux-only tests behind runtime checks or build tags, so they run on the Linux CI job.
- **Shell scripts:** run them under `bash -n`, and with `shellcheck` if it is available.
- **This PC is Windows.**
  - You can't run the package scripts here. Write them carefully, and use WSL if it is installed and
    already set up. Don't install WSL.
  - The real check is CI. You may NOT push.
  - Say clearly in your report which parts you could only check by reading, so the coordinator can
    run CI after merging.
- **Real-app e2e:** run only files your app changes touch, for example `updates.e2e.mjs` if the
  update wording changes. Run the full suite once at the end only if you changed frontend or app
  code.

## Rules

- **GitHub Actions stay free:**
  - standard GitHub-hosted runners only, such as `ubuntu-22.04`, `ubuntu-24.04` or `ubuntu-latest`;
  - no larger runners;
  - artifacts kept 7 days;
  - no new caches beyond what `setup-go` and `setup-node` already do;
  - no schedules.
- Never create, read or handle the private signing key. It is the `GLOSSA_UPDATE_KEY` secret, used
  only by the release workflow.
- Permissive licences only. Any packaging tool used in CI must be a standard Ubuntu package or come
  from the distribution; download no third-party binaries.
- No real network in Go or e2e tests. Never use the user's real library.
- Run `npm ci` in `frontend/` if you touch the frontend; no junctions.
- Run long commands with timeouts, and close every process you start, including `glossa-e2e.exe` and
  e2e node processes.
- Never touch `%LOCALAPPDATA%\Temp\glossa-check`, `xegesis-check` or `%LOCALAPPDATA%\Programs\Glossa`.
- Don't commit `frontend/dist`. Don't push. Never `git stash`. Never switch branches.
- The git author email is `52300301+Perlsowisdom@users.noreply.github.com`. End commit messages with
  a blank line, then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Commit after each step.
- Use `scripts\build.ps1` for any Windows app build.
