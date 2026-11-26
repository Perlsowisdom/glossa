//go:build !store

package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultReleasesURL lists the newest releases, pre-releases included (GitHub's public API, no
// account or key). The Preview channel picks from it; the Stable channel never reads it.
const DefaultReleasesURL = "https://api.github.com/repos/Perlsowisdom/glossa/releases?per_page=10"

// The channels. Stable offers full releases only; Preview offers the newest release of any kind.
const (
	Stable  = "stable"
	Preview = "preview"
)

const maxReleases = 1 << 20

// Environment variables that point the updater at a server on this computer, so the interface tests
// never reach GitHub. They are ignored unless the address is on this computer (localhost, 127.x.x.x
// or ::1).
const (
	EndpointEnv    = "GLOSSA_UPDATE_ENDPOINT"
	ReleasesURLEnv = "GLOSSA_UPDATE_RELEASES_URL"
)

func (c Config) withTestServer() Config {
	for env, dst := range map[string]*string{EndpointEnv: &c.Endpoint, ReleasesURLEnv: &c.ReleasesURL} {
		v := os.Getenv(env)
		u, err := url.Parse(v)
		if v == "" || err != nil || u.Scheme != "http" || !loopback(u.Hostname()) {
			continue
		}
		*dst = v
		c.AllowHTTP = true
		c.DownloadPrefix = u.Scheme + "://" + u.Host + "/"
	}
	return c
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DefaultChannel is where an install that never chose is put: Preview while the running version is a
// pre-release (0.5.0-alpha), Stable otherwise.
func DefaultChannel(current string) string {
	v := parseVersion(current)
	if v.ok && v.pre != "" {
		return Preview
	}
	return Stable
}

// errNoRelease means GitHub answered but has no release with an update manifest: nothing to offer.
var errNoRelease = errors.New("no release has an update manifest")

// statusError is an answer other than 200 that is not a request to slow down.
type statusError struct {
	Host   string
	Status string
	Code   int
}

func (e *statusError) Error() string { return fmt.Sprintf("%s answered %s", e.Host, e.Status) }

// rateLimitError is GitHub asking to be left alone for a while.
type rateLimitError struct {
	Host  string
	Until time.Time
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("%s asked Glossa to wait until %s", e.Host, e.Until.Local().Format("15:04"))
}

// maxWait is the longest Glossa honours a request to wait.
const maxWait = 7 * 24 * time.Hour

// statusError turns a non-200 answer into the error that says what to do: wait (403 or 429 with a
// rate limit) or just fail.
func (c Config) statusError(host string, resp *http.Response) error {
	now := c.now()
	limited := resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusForbidden && (resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0")
	if !limited {
		return &statusError{Host: host, Status: resp.Status, Code: resp.StatusCode}
	}
	until := now.Add(autoEvery)
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && secs >= 0 {
			until = now.Add(time.Duration(secs) * time.Second)
		} else if t, err := http.ParseTime(ra); err == nil {
			until = t
		}
	} else if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
		until = time.Unix(reset, 0)
	}
	// Never sooner than the next daily check, never absurdly far.
	if min := now.Add(autoEvery); until.Before(min) {
		until = min
	}
	if max := now.Add(maxWait); until.After(max) {
		until = max
	}
	return &rateLimitError{Host: host, Until: until}
}

// Fetch reads the manifest of the newest release on a channel. It sends one GET for Stable and two
// for Preview (the release list, then the chosen release's latest.json), and nothing else.
func (c Config) Fetch(ctx context.Context, channel string) (Manifest, error) {
	var m Manifest
	var err error
	if channel == Preview {
		m, err = c.checkPreview(ctx)
	} else {
		m, err = c.Check(ctx)
	}
	var se *statusError
	if errors.As(err, &se) && se.Code == http.StatusNotFound {
		return Manifest{}, errNoRelease // nothing published with a manifest yet
	}
	return m, err
}

// ghRelease is the part of GitHub's release record that is used.
type ghRelease struct {
	Tag    string `json:"tag_name"`
	Draft  bool   `json:"draft"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (c Config) downloadPrefix() string {
	if c.DownloadPrefix != "" {
		return c.DownloadPrefix
	}
	return defaultDownloadPrefix
}

// pickRelease chooses the newest release that is not a draft and carries a latest.json: the highest
// version among them, whatever order GitHub listed them in. It returns that release's tag and the
// address of its manifest.
func pickRelease(list []ghRelease) (tag, manifestURL string) {
	for _, r := range list {
		if r.Draft || !parseVersion(r.Tag).ok {
			continue
		}
		for _, a := range r.Assets {
			if a.Name != "latest.json" {
				continue
			}
			if tag == "" || Newer(r.Tag, tag) {
				tag, manifestURL = r.Tag, a.URL
			}
		}
	}
	return tag, manifestURL
}

func (c Config) checkPreview(ctx context.Context) (Manifest, error) {
	body, err := c.get(ctx, c.ReleasesURL, maxReleases)
	if err != nil {
		return Manifest{}, err
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return Manifest{}, err
	}
	if len(data) > maxReleases {
		return Manifest{}, errors.New("the release list is too large")
	}
	var list []ghRelease
	if err := json.Unmarshal(data, &list); err != nil {
		return Manifest{}, fmt.Errorf("the release list is not valid: %w", err)
	}
	tag, addr := pickRelease(list)
	if tag == "" {
		return Manifest{}, errNoRelease
	}
	// The manifest must live under this project's releases, whatever the list says.
	if !strings.HasPrefix(addr, c.downloadPrefix()) {
		return Manifest{}, fmt.Errorf("refusing the manifest address %q: it is not under this project's releases", addr)
	}
	m, err := c.fetchManifest(ctx, addr)
	if err != nil {
		return Manifest{}, err
	}
	if strings.TrimPrefix(tag, "v") != m.Version {
		return Manifest{}, fmt.Errorf("the manifest of %s names version %s", tag, m.Version)
	}
	return m, nil
}
