//go:build !store

package update

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
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

// cleanMac drops a mac entry whose shape is wrong, so a bad one never stops the Windows update.
func cleanMac(m *Manifest, allowHTTP bool) {
	a := m.Mac
	if a == nil {
		return
	}
	u, err := url.Parse(a.URL)
	raw, herr := hex.DecodeString(a.SHA256)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(allowHTTP && u.Scheme == "http")) ||
		herr != nil || len(raw) != sha256.Size || a.Signature == "" {
		m.Mac = nil
		return
	}
	a.SHA256 = strings.ToLower(a.SHA256)
}

// ManualURL is the address opened for a newer version where updates are manual: the disk image
// when the manifest has a macOS entry that is signed, verifies against the app's key and lives
// under the project's releases; otherwise that version's release page.
func (c Config) ManualURL(m Manifest) string {
	page := releasePageFormat + m.Version
	a := m.Mac
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
