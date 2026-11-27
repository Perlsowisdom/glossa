//go:build !store

package update

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/update/updatesig"
)

// Where releases live. A download address from the manifest is only used when it is under
// defaultDownloadPrefix; anything else falls back to the release page.
const (
	defaultDownloadPrefix = "https://github.com/Perlsowisdom/glossa/releases/download/"
	releasePageFormat     = "https://github.com/Perlsowisdom/glossa/releases/tag/v"
)

// Asset is one more file of a release, signed like the setup: the signature covers its SHA-256
// and the release version.
type Asset struct {
	URL       string `json:"url" desc:"Where the file is"`
	SHA256    string `json:"sha256" desc:"The file's SHA-256, hex"`
	Signature string `json:"signature" desc:"Ed25519 signature (base64) over the version and SHA-256"`
}

// cleanAsset drops an entry (mac, linux) whose shape is wrong, so a bad one never stops the Windows
// update. It returns the entry to keep, or nil.
func cleanAsset(a *Asset, allowHTTP bool) *Asset {
	if a == nil {
		return nil
	}
	u, err := url.Parse(a.URL)
	raw, herr := hex.DecodeString(a.SHA256)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(allowHTTP && u.Scheme == "http")) ||
		herr != nil || len(raw) != sha256.Size || a.Signature == "" {
		return nil
	}
	a.SHA256 = strings.ToLower(a.SHA256)
	return a
}

// ManualURL is the address opened for a newer version where updates are manual: the platform's
// download (the disk image on macOS; the .deb on Linux, and only when this copy was installed from
// a package, since a copy unpacked from the .tar.gz needs the other file) when the manifest has an
// entry for it that is signed, verifies against the app's key and lives under the project's
// releases; otherwise that version's release page, which lists every file.
func (c Config) ManualURL(m Manifest) string {
	page := releasePageFormat + m.Version
	a := c.manualAsset(m)
	prefix := c.DownloadPrefix
	if prefix == "" {
		prefix = defaultDownloadPrefix
	}
	if a == nil || !strings.HasPrefix(a.URL, prefix) || len(c.PubKey) == 0 {
		return page
	}
	if updatesig.Verify(c.PubKey, m.Version, a.SHA256, a.Signature) != nil {
		return page
	}
	return a.URL
}

// manualAsset is the manifest entry for this system, or nil.
func (c Config) manualAsset(m Manifest) *Asset {
	switch c.os() {
	case "darwin":
		return m.Mac
	case "linux":
		if c.Packaged {
			return m.Linux
		}
	}
	return nil
}

func (c Config) os() string {
	if c.OS != "" {
		return c.OS
	}
	return runtime.GOOS
}

// Platform names the system for the interface's wording when updates are manual: "mac" or
// "linux"; empty where the app installs updates itself.
func (c Config) Platform() string {
	if !c.Manual {
		return ""
	}
	switch c.os() {
	case "darwin":
		return "mac"
	case "linux":
		return "linux"
	}
	return ""
}

// installedFromPackage reports whether the running program lives under /usr (the .deb puts it in
// /usr/bin), as opposed to a folder the person unpacked the .tar.gz into.
func installedFromPackage() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return strings.HasPrefix(exe, "/usr/")
}
