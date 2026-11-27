#!/usr/bin/env bash
# Checks a built Glossa .deb on Ubuntu or Debian (CI: ubuntu-22.04): its control fields and file
# list, that it installs (apt pulls in GTK and WebKitGTK), that the launcher file is valid, that the
# installed program starts under a virtual screen without crashing and carries the stamped version,
# and that the package removes cleanly.
#   scripts/check-linux.sh build/bin/Glossa-0.6.1-alpha-linux-amd64.deb 0.6.1-alpha
# The second argument is the version the package was built for (not the Debian form).
# Needs sudo (or root), xvfb and desktop-file-utils:  sudo apt-get install -y xvfb desktop-file-utils
# GLOSSA_CHECK_KEEP=1 leaves the package installed.
set -euo pipefail
deb="${1:?usage: check-linux.sh <deb> <version>}"
version="${2:?usage: check-linux.sh <deb> <version>}"
root="$(cd "$(dirname "$0")/.." && pwd)"
appid=io.github.perlsowisdom.glossa
fail() { echo "FAIL: $*" >&2; exit 1; }
[ -f "$deb" ] || fail "$deb is not there"
deb="$(cd "$(dirname "$deb")" && pwd)/$(basename "$deb")"   # apt wants a path it can tell from a package name
[ "$(id -u)" = 0 ] && sudo="" || sudo="sudo"
debver="$(cd "$root/tools" && go run ./versionmap deb "$version")"

# ---- control fields ----
field() { dpkg-deb -f "$deb" "$1"; }
[ "$(field Package)" = glossa ] || fail "package name is $(field Package)"
[ "$(field Version)" = "$debver" ] || fail "package version is $(field Version), want $debver"
[ "$(field Architecture)" = amd64 ] || fail "architecture is $(field Architecture)"
deps="$(field Depends)"
for d in libgtk-3-0 libwebkit2gtk-4.1-0; do
  case "$deps" in *"$d"*) ;; *) fail "Depends lacks $d: $deps" ;; esac
done
# A suffix (0.6.1-alpha) must sort before the release it leads to (0.6.1); a plain version must not.
core="${version%%[-+]*}"
case "$version" in
  *-*) dpkg --compare-versions "$debver" lt "$core" || fail "$debver does not sort before $core" ;;
  *) [ "$debver" = "$version" ] || fail "$version became $debver" ;;
esac
echo "OK: glossa $debver, amd64, depends on $deps"

# ---- file list ----
list="$(dpkg-deb -c "$deb")"
for f in ./usr/bin/glossa ./usr/share/applications/$appid.desktop ./usr/share/doc/glossa/copyright \
  ./usr/share/doc/glossa/LICENSE-MIT ./usr/share/doc/glossa/LICENSE-APACHE \
  ./usr/share/icons/hicolor/16x16/apps/$appid.png ./usr/share/icons/hicolor/48x48/apps/$appid.png \
  ./usr/share/icons/hicolor/256x256/apps/$appid.png ./usr/share/icons/hicolor/512x512/apps/$appid.png; do
  echo "$list" | awk '{print $6}' | grep -qxF "$f" || fail "the package has no $f"
done
echo "$list" | grep -E '^-rwxr-xr-x root/root .* \./usr/bin/glossa$' >/dev/null || fail "/usr/bin/glossa is not an executable owned by root"
if echo "$list" | awk '{print $2}' | grep -qv '^root/root$'; then fail "a file in the package is not owned by root"; fi
echo "OK: the package holds the program, the launcher file, the icons and the licences"

# ---- install ----
lib="$(mktemp -d "${TMPDIR:-/tmp}/glossa-lib.XXXXXX")"
trap '[ "${GLOSSA_CHECK_KEEP:-}" = 1 ] || $sudo apt-get remove -y glossa >/dev/null 2>&1 || true; rm -rf "$lib"' EXIT
$sudo apt-get install -y "$deb"
echo "OK: installed with its dependencies"
[ -x /usr/bin/glossa ] || fail "/usr/bin/glossa is not installed"
if ldd /usr/bin/glossa | grep -F "not found"; then fail "the program lacks a library (above)"; fi
command -v desktop-file-validate >/dev/null || fail "desktop-file-validate is missing (apt: desktop-file-utils)"
desktop-file-validate /usr/share/applications/$appid.desktop
echo "OK: the launcher file is valid"

# ---- start ----
# The window-less assistant mode reads standard input and stops when it ends.
GLOSSA_LIBRARY="$lib/library.db" timeout 60 /usr/bin/glossa mcp-assistant </dev/null >/dev/null 2>"$lib/mcp-err.txt" ||
  { cat "$lib/mcp-err.txt" >&2; fail "the program's assistant mode failed to start or stop"; }
echo "OK: the program starts and stops without a window"
# The real thing: the window opens on a virtual screen and stays up for a few seconds. timeout ends it
# with SIGTERM (exit 124); anything else means it stopped by itself, which is a crash.
command -v xvfb-run >/dev/null || fail "xvfb-run is missing (apt: xvfb)"
# Software drawing: a CI runner has no GPU.
export WEBKIT_DISABLE_COMPOSITING_MODE=1 WEBKIT_DISABLE_DMABUF_RENDERER=1
export XDG_CONFIG_HOME="$lib/config" HOME="$lib/home"
mkdir -p "$XDG_CONFIG_HOME" "$HOME"
set +e
xvfb-run -a timeout 8 /usr/bin/glossa >"$lib/out.txt" 2>&1
rc=$?
set -e
if [ "$rc" != 124 ]; then cat "$lib/out.txt" >&2; fail "the app exited with $rc within 8 seconds (it should keep running)"; fi
[ -f "$XDG_CONFIG_HOME/Glossa/library.db" ] || { cat "$lib/out.txt" >&2; fail "the app did not make its library in \$XDG_CONFIG_HOME/Glossa"; }
if ! grep -rqF "version=$version" "$XDG_CONFIG_HOME/Glossa" "$lib/out.txt"; then
  cat "$lib/out.txt" >&2
  fail "the app did not report version $version"
fi
echo "OK: the app opens, keeps running, keeps its library in XDG_CONFIG_HOME and reports version $version"

# ---- remove ----
if [ "${GLOSSA_CHECK_KEEP:-}" != 1 ]; then
  $sudo apt-get remove -y glossa >/dev/null
  [ ! -e /usr/bin/glossa ] || fail "removing the package left /usr/bin/glossa"
  [ ! -e /usr/share/applications/$appid.desktop ] || fail "removing the package left the launcher file"
  echo "OK: the package removes cleanly"
fi
