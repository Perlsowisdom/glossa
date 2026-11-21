#!/usr/bin/env bash
# Builds the macOS app and its disk image on a Mac (CI: macos-latest). Run from anywhere:
#   scripts/package-macos.sh 0.5.0-alpha            build/bin/Glossa-0.5.0-alpha-macos-universal.dmg
#   scripts/package-macos.sh --app-only 0.5.0-alpha only build/bin/Glossa.app (CI uses this on every push)
# The frontend must already be built (cd frontend; npm ci; npm run build).
#
# One universal binary (arm64 + x86_64, joined with lipo), in Glossa.app with the icon and
# Info.plist, ad-hoc signed (codesign -s -; Apple Silicon refuses unsigned code, but this is not an
# Apple Developer signature: first-open steps are in README.md), then a .dmg holding the app, a link
# to /Applications and the licences. Needs Xcode's command line tools (clang, lipo, codesign, hdiutil).
set -euo pipefail

app_only=0
if [ "${1:-}" = "--app-only" ]; then app_only=1; shift; fi
version="${1:-}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
if [ -z "$version" ]; then
  if git describe --tags --exact-match --match 'v*' >/dev/null 2>&1; then
    version="$(git describe --tags --exact-match --match 'v*' | sed 's/^v//')"
  else
    version="0.0.0-dev+$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  fi
fi
[ "$(uname -s)" = Darwin ] || { echo "package-macos.sh runs on macOS" >&2; exit 1; }
macver="$(cd tools && go run ./versionmap mac "$version")"
[ -f frontend/dist/index.html ] || { echo "frontend/dist is missing: build the frontend first" >&2; exit 1; }

out="$root/build/bin"
work="$(mktemp -d "${TMPDIR:-/tmp}/glossa-mac.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$out"

# Wails' own build settings for macOS (Wails CLI base.go): cgo on, the UniformTypeIdentifiers
# framework (macOS 11+), and a deployment target that matches LSMinimumSystemVersion.
export CGO_ENABLED=1
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=11.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -framework UniformTypeIdentifiers -mmacosx-version-min=11.0"
ld="-s -w -X github.com/Perlsowisdom/glossa/internal/command/core.Version=$version"
for arch in arm64 amd64; do
  echo "Building $arch"
  GOOS=darwin GOARCH=$arch go build -trimpath -tags desktop,production -ldflags "$ld" -o "$work/Glossa-$arch" ./app
done
lipo -create -output "$work/Glossa" "$work/Glossa-arm64" "$work/Glossa-amd64"
echo "Architectures: $(lipo -archs "$work/Glossa")"

app="$out/Glossa.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$work/Glossa" "$app/Contents/MacOS/Glossa"
chmod 755 "$app/Contents/MacOS/Glossa"
cp build/darwin/icon.icns "$app/Contents/Resources/icon.icns"
sed "s/{{VERSION}}/$macver/g" build/darwin/Info.plist.template > "$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist"
codesign --force --deep --sign - "$app"
codesign --verify --deep --strict "$app"
echo "Built $app version $version (bundle $macver), ad-hoc signed"
[ "$app_only" = 1 ] && exit 0

dmg="$out/Glossa-$version-macos-universal.dmg"
stage="$work/dmg"
mkdir -p "$stage"
cp -R "$app" "$stage/Glossa.app"
ln -s /Applications "$stage/Applications"
cp LICENSE-MIT LICENSE-APACHE "$stage/"
cat > "$stage/How to open Glossa.txt" <<'TXT'
Drag Glossa onto Applications.

Glossa is not signed by an Apple Developer account yet, so macOS asks before the first open:

1. In Applications, right-click (or Control-click) Glossa and choose Open, then Open again.
2. If there is no Open button: try to open Glossa once, then go to System Settings > Privacy & Security,
   scroll down to "Glossa was blocked" and choose Open Anyway.
3. Last resort, in Terminal:  xattr -dr com.apple.quarantine /Applications/Glossa.app

You only need to do this once. Glossa's source and checksums: https://github.com/Perlsowisdom/glossa
TXT
# hdiutil sometimes answers "Resource busy" on CI runners; a second try works.
for try in 1 2 3; do
  rm -f "$dmg"
  hdiutil create -volname "Glossa" -srcfolder "$stage" -ov -format UDZO -fs HFS+ "$dmg" && break
  [ "$try" = 3 ] && { echo "hdiutil create failed" >&2; exit 1; }
  sleep 5
done
hdiutil verify "$dmg"
echo "Built $dmg"
