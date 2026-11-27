#!/usr/bin/env bash
# Builds the Linux packages on an x86-64 Ubuntu or Debian machine (CI: ubuntu-22.04). Run from anywhere:
#   scripts/package-linux.sh 0.6.1-alpha
#     build/bin/Glossa-0.6.1-alpha-linux-amd64.deb
#     build/bin/Glossa-0.6.1-alpha-linux-amd64.tar.gz
# With no version it uses the git tag, or 0.0.0-dev+<commit>.
# The frontend must already be built (cd frontend; npm ci; npm run build); CI uses the committed build.
#
# The binary is built with cgo against GTK 3 and WebKitGTK 4.1 (apt: libgtk-3-dev libwebkit2gtk-4.1-dev,
# build tags desktop,production,webkit2_41). Building on Ubuntu 22.04 keeps the glibc requirement low
# (2.35), so the program also runs on older systems. The .deb needs GTK and WebKitGTK from the system
# (apt installs them); the .tar.gz holds the same files for other distributions.
#
# GLOSSA_LINUX_BINARY=<file> uses an already built program instead of building one (to try the
# packaging without the build tools; the release never sets it).
set -euo pipefail

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
[ "$(uname -s)" = Linux ] || { echo "package-linux.sh runs on Linux" >&2; exit 1; }
case "$(uname -m)" in x86_64 | amd64) ;; *) echo "only x86-64 is built for now (this is $(uname -m))" >&2; exit 1 ;; esac
for tool in dpkg-deb tar gzip install; do
  command -v "$tool" >/dev/null || { echo "$tool is needed" >&2; exit 1; }
done
debver="$(cd tools && go run ./versionmap deb "$version")"
[ -f frontend/dist/index.html ] || { echo "frontend/dist is missing: build the frontend first" >&2; exit 1; }

appid=io.github.perlsowisdom.glossa
out="$root/build/bin"
work="$(mktemp -d "${TMPDIR:-/tmp}/glossa-linux.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$out"

bin="$work/glossa"
if [ -n "${GLOSSA_LINUX_BINARY:-}" ]; then
  cp "$GLOSSA_LINUX_BINARY" "$bin"
else
  if ! { command -v pkg-config >/dev/null && pkg-config --exists gtk+-3.0 webkit2gtk-4.1; }; then
    echo "GTK 3 and WebKitGTK 4.1 are needed: sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev" >&2; exit 1;
  fi
  echo "Building glossa $version"
  ld="-s -w -X github.com/Perlsowisdom/glossa/internal/command/core.Version=$version"
  CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -tags desktop,production,webkit2_41 -ldflags "$ld" -o "$bin" ./app
fi
chmod 755 "$bin"

# Files get the time of the commit (or now), so two builds of one commit hold the same dates.
stamp="$(git log -1 --format=%ct 2>/dev/null || date +%s)"
touch_all() { find "$1" -exec touch -h -d "@$stamp" {} +; }

icon_sizes="16 24 32 48 64 128 256 512"
icon() { echo "$root/build/icon/png/glossa-icon-$1.png"; }
for s in $icon_sizes; do [ -f "$(icon "$s")" ] || { echo "missing icon $(icon "$s")" >&2; exit 1; }; done

# ---- .deb ----
deb="$out/Glossa-$version-linux-amd64.deb"
pkg="$work/deb"
install -d -m 755 "$pkg/DEBIAN" "$pkg/usr/bin" "$pkg/usr/share/applications" "$pkg/usr/share/doc/glossa"
install -m 755 "$bin" "$pkg/usr/bin/glossa"
install -m 644 "$root/build/linux/$appid.desktop" "$pkg/usr/share/applications/$appid.desktop"
for s in $icon_sizes; do
  install -d -m 755 "$pkg/usr/share/icons/hicolor/${s}x${s}/apps"
  install -m 644 "$(icon "$s")" "$pkg/usr/share/icons/hicolor/${s}x${s}/apps/$appid.png"
done
install -m 644 "$root/build/linux/copyright" "$pkg/usr/share/doc/glossa/copyright"
install -m 644 "$root/LICENSE-MIT" "$root/LICENSE-APACHE" "$pkg/usr/share/doc/glossa/"
# The icon cache and the application menu refresh themselves when the package is installed or removed.
for s in postinst postrm; do
  cat > "$pkg/DEBIAN/$s" <<'SH'
#!/bin/sh
set -e
if [ "$1" = configure ] || [ "$1" = remove ] || [ "$1" = purge ]; then
  if command -v gtk-update-icon-cache >/dev/null 2>&1; then gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true; fi
  if command -v update-desktop-database >/dev/null 2>&1; then update-desktop-database -q /usr/share/applications || true; fi
fi
exit 0
SH
  chmod 755 "$pkg/DEBIAN/$s"
done
size_kb="$(du -sk --exclude=DEBIAN "$pkg" | cut -f1)"
cat > "$pkg/DEBIAN/control" <<CONTROL
Package: glossa
Version: $debver
Architecture: amd64
Maintainer: Glossa project <52300301+Perlsowisdom@users.noreply.github.com>
Installed-Size: $size_kb
Depends: libc6 (>= 2.35), libgtk-3-0 | libgtk-3-0t64, libwebkit2gtk-4.1-0
Recommends: xdg-utils
Section: utils
Priority: optional
Homepage: https://github.com/Perlsowisdom/glossa
Description: Bible study app that works offline
 Glossa is a free, open-source Bible study app. It is fast, works fully
 offline, keeps your library in open formats, and can be used by people,
 scripts and AI assistants alike.
CONTROL
touch_all "$pkg"
rm -f "$deb"
dpkg-deb --root-owner-group -Zxz --build "$pkg" "$deb" >/dev/null
echo "Built $deb (package version $debver)"

# ---- .tar.gz ----
name="Glossa-$version-linux-amd64"
tarball="$out/$name.tar.gz"
stage="$work/tar/$name"
install -d -m 755 "$stage"
install -m 755 "$bin" "$stage/glossa"
install -m 644 "$root/build/linux/$appid.desktop" "$stage/$appid.desktop"
install -m 644 "$(icon 512)" "$stage/glossa.png"
install -m 644 "$root/build/linux/README-linux.txt" "$root/LICENSE-MIT" "$root/LICENSE-APACHE" "$stage/"
touch_all "$work/tar"
rm -f "$tarball"
tar -C "$work/tar" --sort=name --owner=0 --group=0 --numeric-owner -cf - "$name" | gzip -9n > "$tarball"
echo "Built $tarball"
