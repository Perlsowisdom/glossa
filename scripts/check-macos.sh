#!/usr/bin/env bash
# Checks a built Glossa disk image on a Mac: it mounts, holds the app (both architectures, valid
# ad-hoc signature, version in Info.plist), the Applications link and the licences, and the
# app's binary starts (in its window-less assistant mode) and exits when its input ends.
#   scripts/check-macos.sh build/bin/Glossa-0.5.0-alpha-macos-universal.dmg 0.5.0
# The second argument is the three-number bundle version.
set -euo pipefail
dmg="${1:?usage: check-macos.sh <dmg> <bundle version>}"
want="${2:?usage: check-macos.sh <dmg> <bundle version>}"
mnt="$(mktemp -d "${TMPDIR:-/tmp}/glossa-dmg.XXXXXX")"
lib="$(mktemp -d "${TMPDIR:-/tmp}/glossa-lib.XXXXXX")"
cleanup() { hdiutil detach "$mnt" -force >/dev/null 2>&1 || true; rm -rf "$mnt" "$lib"; }
trap cleanup EXIT
hdiutil attach "$dmg" -readonly -nobrowse -noverify -mountpoint "$mnt" >/dev/null
for f in Glossa.app/Contents/MacOS/Glossa Glossa.app/Contents/Resources/icon.icns Glossa.app/Contents/Info.plist LICENSE-MIT LICENSE-APACHE "How to open Glossa.txt"; do
  [ -e "$mnt/$f" ] || { echo "the disk image has no $f" >&2; exit 1; }
done
[ -L "$mnt/Applications" ] || { echo "the disk image has no Applications link" >&2; exit 1; }
bin="$mnt/Glossa.app/Contents/MacOS/Glossa"
archs="$(lipo -archs "$bin")"
case "$archs" in *arm64*x86_64* | *x86_64*arm64*) ;; *) echo "not a universal binary: $archs" >&2; exit 1 ;; esac
codesign --verify --deep --strict "$mnt/Glossa.app"
got="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$mnt/Glossa.app/Contents/Info.plist")"
[ "$got" = "$want" ] || { echo "bundle version is $got, want $want" >&2; exit 1; }
id="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$mnt/Glossa.app/Contents/Info.plist")"
min="$(/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' "$mnt/Glossa.app/Contents/Info.plist")"
echo "OK: $archs, bundle $id $got, needs macOS $min"
# Start the binary without a window: the assistant's tool server reads standard input and stops when
# it ends. A watchdog stops it if it hangs. The library is a temporary one.
GLOSSA_LIBRARY="$lib/library.db" "$bin" mcp-assistant </dev/null >/dev/null 2>"$lib/err.txt" &
pid=$!
( sleep 60; kill -9 "$pid" 2>/dev/null ) &
dog=$!
if wait "$pid"; then rc=0; else rc=$?; fi
kill "$dog" 2>/dev/null || true
if [ "$rc" != 0 ]; then echo "the app binary exited with $rc:" >&2; cat "$lib/err.txt" >&2; exit 1; fi
echo "OK: the app binary starts and stops"
