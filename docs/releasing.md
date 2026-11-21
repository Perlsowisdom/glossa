# Releasing Glossa

How a version gets to people: Windows and macOS, released together. The app was called Xegesis before version 0.4; releases from then on are Glossa. There are three channels:

| Channel | File | Who signs it | How people update |
| --- | --- | --- | --- |
| **Microsoft Store** | `Glossa-<version>-windows-x64.msix` (built with the `store` tag: no updater) | The Store, at certification | The Store |
| **Direct download** | `Glossa-<version>-windows-x64-setup.exe` (NSIS, not code-signed) | Our own Ed25519 signature in `latest.json` | The app's own update (off until the user turns it on) |
| **Direct download (macOS)** | `Glossa-<version>-macos-universal.dmg` (arm64 + Intel; ad-hoc signed only, not Apple-signed or notarised) | Our own Ed25519 signature in `latest.json` (`mac` entry) | A download the app opens in the browser (the app cannot replace itself); off until the user turns update checks on |

The version comes from the git tag. There is no version file to edit: a tag `v1.2.3` builds version
`1.2.3`, and a build with no tag says `0.0.0-dev+<commit>`. `glossa version` and Settings → About show it.

## Release from GitHub (the button)

No terminal needed. In the repository on github.com:

1. **Actions** -> **Release** -> **Run workflow**.
2. Choose the branch (the commit the release is made from; to use another, type a commit or branch in
   *commit*), then fill in:
   - **version**: `0.4.0-alpha`, or `1.2.3`, without the `v`. It must be `MAJOR.MINOR.PATCH` with an
     optional `-suffix`. The run stops at once if it is not, or if the tag `v<version>` already exists.
   - **kind**: `auto` (default: a pre-release when the version has a `-` suffix, otherwise a draft),
     `draft`, `prerelease` or `release` (published at once; refused for a version with a `-` suffix).
3. **Run workflow.** It builds, tests and packages exactly as the tag-triggered run does (Windows and macOS in parallel; if either fails there is no tag and no release), then creates the
   tag `v<version>` on that commit and the GitHub release of the chosen kind, with the setup, the MSIX, the DMG,
   `SHA256SUMS` and (when signed) `latest.json`. If the release cannot be created, the tag it just made
   is removed again. The workflow's own tag push does not start a second run.

A pre-release is never offered by the app's updater (GitHub's "latest" skips pre-releases), so
`0.4.0-alpha` is a safe way to try a build. A draft is visible only to people who can write to the
repository until you publish it.

While `internal/update/pubkey.txt` is still the placeholder (see below), the run **skips signing with a
warning**: the release is created without `latest.json`, so no app is offered it as an update. Installing
by hand works as usual.

The version reaches each packager in the form it needs (`tools/relver`, with tests): the app is stamped
`0.4.0-alpha`; the setup's file properties and the MSIX use `0.4.0.0` (four numbers, the suffix dropped).
Pushing a tag (`git tag v1.2.3 && git push origin v1.2.3`) still works and makes a draft, as in "Each
release" below.

## macOS notes

The `macos` job (macos-latest) runs the Go and frontend tests, then `scripts/package-macos.sh`: an arm64 and an
x86_64 build of the app (cgo, as Wails' own build does: `-mmacosx-version-min=11.0`, the
UniformTypeIdentifiers framework), joined with `lipo` into one `Glossa.app` (icon from `build/darwin/icon.icns`,
`Info.plist` from `build/darwin/Info.plist.template`, bundle id `io.github.perlsowisdom.glossa`, version from the tag with the
suffix dropped: 0.5.0-alpha is 0.5.0), `codesign --sign -` (ad-hoc; Apple Silicon will not run unsigned code), and a
`.dmg` made with `hdiutil`. `scripts/check-macos.sh` then mounts the DMG and checks its contents, both
architectures, the signature, the version and that the app's binary starts. CI runs the same two scripts on every push.

There is **no Apple Developer signature or notarisation** yet, so the first open needs the steps in the README
(right-click, Open; or System Settings, Privacy & Security, Open Anyway; `xattr -dr com.apple.quarantine` as the
last resort). Say so in the release notes. Moving to a Developer ID certificate later means adding
`codesign --options runtime` with the certificate, then `xcrun notarytool` and `stapler`, in `package-macos.sh`;
in-place updates (as on Windows) then become possible. The bundle id must stay the same.

The DMG is signed with the same update key as the setup (a `mac` entry in `latest.json`: address, SHA-256 and
signature). The Mac app never installs it: it opens the DMG address (only when the signature verifies and the
address is under this repository's releases; otherwise the release page). To build on a Mac:
`cd frontend; npm ci; npm run build; cd ..; scripts/package-macos.sh 0.5.0-alpha`.

## Once: make the signing key

The direct download's updates are safe only because every update carries a signature that the app checks
against a public key built into it. The setup itself is not code-signed, so **this key is what protects
your users. Treat the private key like a password.**

1. Make the key pair, writing the private key **outside the repository**:

   ```powershell
   go run ./cmd/glossa-sign keygen -out $env:USERPROFILE\glossa-update-key.txt
   ```

   It prints the **public key** (one line of base64) and writes the **private key** to that file. It
   refuses to overwrite a file.
2. Put the public key in `internal/update/pubkey.txt`, replacing the placeholder line (and its comment
   saying PLACEHOLDER). Commit that file. It is public by design.
3. Give the private key to CI as a secret. In PowerShell, from anywhere:

   ```powershell
   Get-Content -Raw $env:USERPROFILE\glossa-update-key.txt | gh secret set GLOSSA_UPDATE_KEY --repo Perlsowisdom/glossa
   ```

   (Or in the GitHub web page: repository Settings → Secrets and variables → Actions → New repository
   secret, name `GLOSSA_UPDATE_KEY`, value the file's one line.)
4. **Keep a backup of the private key** in your password manager, then delete the file from disk. If it is
   lost you cannot sign updates any more (see Rotating).
5. **Never commit it, paste it in an issue or a chat, or print it in a workflow log.** The release workflow
   reads it from the secret and never prints it.

The workflow reads the secret `GLOSSA_UPDATE_KEY`, and falls back to `XEGESIS_UPDATE_KEY` while only the
old name is set. Rename it when convenient (the same `gh secret set` command with the new name). The same
goes for the repository variables `GLOSSA_MSIX_IDENTITY`, `GLOSSA_MSIX_PUBLISHER` and
`GLOSSA_MSIX_PUBLISHER_DISPLAY`, which fall back to their `XEGESIS_` names.

Until step 2 is done, the placeholder key is in the app. Nothing signed can verify against it, so a release made now is not signed and has no `latest.json` (no update is offered), and an install of anything is refused ("This build does not have its update key yet"). The
release workflow warns.

## Each release

1. **Check `main` is green** (CI: Go vet and tests on three systems, the frontend tests and build, the
   Store build). Run the real-app tests locally (`cd frontend; npm run e2e`): they need a desktop session,
   so CI cannot.
2. **Write the release notes** in plain words (what changed, what to know). They go in the GitHub release.
3. **Tag the commit and push the tag.** There is nothing to bump:

   ```powershell
   git switch main; git pull
   git tag v1.2.3
   git push origin v1.2.3
   ```

   Use `v1.2.3-beta.1` for a test version: it becomes a pre-release, which the updater never offers
   (GitHub's "latest" skips pre-releases).
4. **CI makes a draft.** The `Release` workflow (`.github/workflows/release.yml`) runs on the tag. It
   builds the frontend and the app, runs the Go and frontend tests, builds the setup and the Store package,
   installs and uninstalls the setup silently as a smoke test, computes the SHA-256, signs it with
   `GLOSSA_UPDATE_KEY` (or the old `XEGESIS_UPDATE_KEY` if that is all that is set), checks the signature against the public key in the repo, and creates a
   **draft** GitHub release holding:
   - `Glossa-1.2.3-windows-x64-setup.exe`
   - `Glossa-1.2.3-windows-x64.msix`
   - `Glossa-1.2.3-macos-universal.dmg`
   - `latest.json` (what the updater reads; missing if no key was available)
   - `SHA256SUMS`
5. **Check the draft** (Releases → the draft): all five files are there; download the setup on a clean
   Windows PC or VM, install it, open it, check Help → About shows `1.2.3`, then uninstall and confirm the
   library is kept (and removed only with "Also remove my library" ticked).
6. **Publish the release** (Edit → paste the notes → Publish release). Publishing is what makes the update
   live: `latest.json` at `releases/latest/download/` now points at it, and people who turned updates on
   are offered it within a day.
7. **Submit the Store package.** Download the `.msix` from the release and upload it in Partner Center
   as a new submission (steps in `docs/store/partner-center-checklist.md`). Upload it **unsigned**; the Store
   signs it. The Store takes days to certify and pushes the update itself.

To stop a bad release: unpublish or delete it (the updater then sees the previous release, which is not
newer, so nothing downgrades), and fix forward with the next patch version.

## Build on your own PC

Needs Go, Node, and for the installers NSIS (`makensis`) and the Windows SDK (`makeappx`, `signtool`).

```powershell
cd frontend; npm ci; npm run build; cd ..
scripts\build.ps1                       # build\bin\Glossa.exe (version from the tag, or 0.0.0-dev)
scripts\package-nsis.ps1                # the setup (downloads Microsoft's WebView2 bootstrapper to embed)
scripts\package-msix.ps1                # the unsigned MSIX for the Store (store build tag)
scripts\package-msix.ps1 -ManifestOnly  # only the manifest, no SDK needed
```

To try the MSIX on a test PC, sign a copy with a throwaway certificate (administrator PowerShell for the
install steps; see the header of the script):

```powershell
scripts\test-sign-msix.ps1 -Package build\bin\Glossa-1.2.3-windows-x64.msix
Import-Certificate build\bin\glossa-test.cer -CertStoreLocation Cert:\LocalMachine\TrustedPeople
Add-AppxPackage build\bin\Glossa-test-signed.msix
```

The test copy is never uploaded. To make the release files (checksums, signature, `latest.json`) by hand:
`scripts\release-files.ps1 -Version 1.2.3 -Setup <setup> -Msix <msix>` with `GLOSSA_UPDATE_KEY` set.

The Partner Center identity is read from `GLOSSA_MSIX_IDENTITY`, `GLOSSA_MSIX_PUBLISHER` and
`GLOSSA_MSIX_PUBLISHER_DISPLAY` (repository Variables in CI; see the checklist). Without them the manifest
carries `PLACEHOLDER` values.

## Signing by hand

```powershell
go run ./cmd/glossa-sign hash <setup.exe>                                  # SHA-256
go run ./cmd/glossa-sign sign -version 1.2.3 <setup.exe>                   # needs GLOSSA_UPDATE_KEY or -key <file>
go run ./cmd/glossa-sign verify -version 1.2.3 -pub internal\update\pubkey.txt <setup.exe> <signature>
```

The signature covers the setup's SHA-256 **and the version**, so an old installer cannot be passed off as a
newer release. `latest.json` looks like:

```json
{ "version": "1.2.3", "url": "https://github.com/Perlsowisdom/glossa/releases/download/v1.2.3/Glossa-1.2.3-windows-x64-setup.exe",
  "sha256": "...", "signature": "...", "notes": "",
  "mac": { "url": "https://github.com/Perlsowisdom/glossa/releases/download/v1.2.3/Glossa-1.2.3-macos-universal.dmg", "sha256": "...", "signature": "..." } }
```

## Rotating the key

Rotate if you suspect the private key leaked, or on a schedule you choose. An app only trusts the public key
it was built with, so the order matters:

1. Make the new key pair (as above, to a new file).
2. Release a version **signed with the old key** whose `internal/update/pubkey.txt` holds the **new**
   public key. People update to it with the old key's signature; from then on their app trusts the new key.
3. Put the new private key in the `GLOSSA_UPDATE_KEY` secret (`gh secret set`, as above). The next
   release is signed with the new key.
4. Delete the old private key everywhere.

If the old private key is **lost or leaked and an attacker could use it**, step 2 is not safe. Do this
instead: publish the new key on the project page, ship a new setup signed with the new key, and ask people
to install it by hand once; apps that still trust the old key will not accept the new signatures. Say so in
the release notes.

## What the Store build does differently

It is built with the `store` build tag: the updater, its commands, the Settings → Updates page and the
first-run question are not compiled in at all, and the Store build never contacts GitHub's releases. Tests:
`go test -tags store ./internal/update/...` (CI runs it).
