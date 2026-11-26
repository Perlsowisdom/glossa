//go:build !store

package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/update/updatesig"
)

// fakeGitHub stands in for github.com and api.github.com: the stable manifest, the release list and
// each release's latest.json. Nothing here leaves this computer.
type fakeGitHub struct {
	srv      *httptest.Server
	mu       sync.Mutex
	hits     map[string]int
	stable   *Manifest
	releases []fakeRelease
	limit    func(w http.ResponseWriter) bool // answers a request to slow down, when set
	listErr  int
	body     []byte
}

type fakeRelease struct {
	tag      string
	draft    bool
	manifest bool   // carries a latest.json
	badURL   string // the latest.json asset's address, when it should not be this server's
	version  string // what the manifest says, when it is not the tag's
}

func (f *fakeGitHub) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func (f *fakeGitHub) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, v := range f.hits {
		n += v
	}
	return n
}

func newFakeGitHub(t *testing.T, priv string) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{hits: map[string]int{}, body: []byte("installer")}
	sum, _ := updatesig.Sum(strings.NewReader(string(f.body)))
	manifest := func(version string) Manifest {
		sig, err := updatesig.Sign(priv, version, sum)
		if err != nil {
			t.Fatal(err)
		}
		return Manifest{Version: version, URL: f.srv.URL + "/dl/Glossa-" + version + "-setup.exe", SHA256: sum, Signature: sig, Notes: "Notes for " + version}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits[r.URL.Path]++
		limit := f.limit
		f.mu.Unlock()
		if limit != nil && limit(w) {
			return
		}
		switch {
		case r.URL.Path == "/stable/latest.json":
			if f.stable == nil {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(f.stable)
		case r.URL.Path == "/api/releases":
			if f.listErr != 0 {
				w.WriteHeader(f.listErr)
				return
			}
			var list []map[string]any
			for _, rel := range f.releases {
				assets := []map[string]string{{"name": "Glossa-setup.exe", "browser_download_url": f.srv.URL + "/dl/setup"}}
				if rel.manifest {
					addr := f.srv.URL + "/dl/" + rel.tag + "/latest.json"
					if rel.badURL != "" {
						addr = rel.badURL
					}
					assets = append(assets, map[string]string{"name": "latest.json", "browser_download_url": addr})
				}
				list = append(list, map[string]any{"tag_name": rel.tag, "draft": rel.draft, "prerelease": strings.Contains(rel.tag, "-"), "assets": assets})
			}
			json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(r.URL.Path, "/dl/") && strings.HasSuffix(r.URL.Path, "/latest.json"):
			tag := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/dl/"), "/latest.json")
			version := strings.TrimPrefix(tag, "v")
			for _, rel := range f.releases {
				if rel.tag == tag && rel.version != "" {
					version = rel.version
				}
			}
			json.NewEncoder(w).Encode(manifest(version))
		case strings.HasSuffix(r.URL.Path, "-setup.exe"):
			w.Write(f.body)
		default:
			http.NotFound(w, r)
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	m := manifest("0.4.1")
	f.stable = &m
	return f
}

func (f *fakeGitHub) config(t *testing.T, pubB64, current string, launched *[]string) Config {
	var pub []byte
	if pubB64 != "" {
		var err error
		if pub, err = updatesig.ParsePublicKey(pubB64); err != nil {
			t.Fatal(err)
		}
	}
	return Config{
		Endpoint: f.srv.URL + "/stable/latest.json", ReleasesURL: f.srv.URL + "/api/releases", DownloadPrefix: f.srv.URL + "/dl/",
		PubKey: pub, Current: current, HTTP: f.srv.Client(), AllowHTTP: true, TempDir: t.TempDir(),
		Launch: func(p string) error { *launched = append(*launched, p); return nil },
	}
}

// previewList: a draft, a release without a manifest, two pre-releases with one, and the full release.
var previewList = []fakeRelease{
	{tag: "v0.7.0-alpha.1", draft: true, manifest: true},
	{tag: "v0.6.0-beta.1", manifest: false},
	{tag: "v0.5.0-beta.2", manifest: true},
	{tag: "v0.5.0-beta.1", manifest: true},
	{tag: "v0.4.1", manifest: true},
}

type rig struct {
	f      *fakeGitHub
	reg    *command.Registry
	env    *command.Env
	cfg    Config
	now    *time.Time
	launch []string
}

func newRig(t *testing.T, current string) *rig {
	pubB64, priv, _ := updatesig.GenerateKey()
	r := &rig{f: newFakeGitHub(t, priv)}
	r.cfg = r.f.config(t, pubB64, current, &r.launch)
	now := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	r.now = &now
	r.cfg.Now = func() time.Time { return *r.now }
	r.reg = command.NewRegistry()
	RegisterWith(r.reg, r.cfg)
	r.env = openEnv(t)
	return r
}

func (r *rig) call(t *testing.T, id, params string) (any, error) {
	t.Helper()
	return run(t, r.reg, r.env, id, params)
}

func (r *rig) set(t *testing.T, params string) SettingsInfo {
	t.Helper()
	res, err := r.call(t, "update.settings.set", params)
	if err != nil {
		t.Fatal(err)
	}
	return res.(SettingsInfo)
}

func (r *rig) check(t *testing.T) CheckResult {
	t.Helper()
	res, err := r.call(t, "update.check", "")
	if err != nil {
		t.Fatal(err)
	}
	return res.(CheckResult)
}

func TestStableChannelReadsOnlyTheLatestManifest(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.f.releases = previewList
	if si := r.set(t, `{"channel":"stable"}`); si.Channel != Stable || !si.ChannelChosen {
		t.Fatalf("settings = %+v", si)
	}
	cr := r.check(t)
	if !cr.Ran || !cr.Newer || cr.Latest != "0.4.1" || cr.Notes != "Notes for 0.4.1" {
		t.Fatalf("check = %+v", cr)
	}
	if r.f.count("/api/releases") != 0 || r.f.total() != 1 {
		t.Fatalf("stable asked for more than latest.json: %v", r.f.hits)
	}
}

func TestPreviewPicksTheNewestUsableRelease(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.f.releases = previewList
	r.set(t, `{"channel":"preview"}`)
	cr := r.check(t)
	// 0.7.0-alpha.1 is a draft and 0.6.0-beta.1 has no latest.json: the newest usable one is 0.5.0-beta.2.
	if !cr.Ran || !cr.Newer || cr.Latest != "0.5.0-beta.2" {
		t.Fatalf("check = %+v", cr)
	}
	if r.f.count("/dl/v0.7.0-alpha.1/latest.json") != 0 || r.f.count("/dl/v0.6.0-beta.1/latest.json") != 0 {
		t.Fatalf("a draft or a release without a manifest was read: %v", r.f.hits)
	}
	if r.f.count("/stable/latest.json") != 0 || r.f.count("/api/releases") != 1 || r.f.count("/dl/v0.5.0-beta.2/latest.json") != 1 {
		t.Fatalf("requests = %v", r.f.hits)
	}
	// Listed in any order, the highest version still wins, and a full release beats its own pre-releases.
	r.f.releases = []fakeRelease{{tag: "v0.5.0-beta.1", manifest: true}, {tag: "v0.5.0", manifest: true}, {tag: "v0.5.0-rc.1", manifest: true}}
	*r.now = r.now.Add(21 * time.Hour)
	if res, _ := r.call(t, "update.auto", ""); res.(CheckResult).Latest != "0.5.0" {
		t.Fatalf("auto = %+v", res)
	}
}

func TestPreviewInstallsThroughTheSameChecks(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.f.releases = previewList
	r.set(t, `{"channel":"preview"}`)
	res, err := r.call(t, "update.install", "")
	if err != nil {
		t.Fatal(err)
	}
	if ir := res.(InstallResult); !ir.Launched || ir.Version != "0.5.0-beta.2" || len(r.launch) != 1 {
		t.Fatalf("install = %+v", ir)
	}
	// A manifest signed by another key is refused on the preview channel too.
	_, otherPriv, _ := updatesig.GenerateKey()
	other := newFakeGitHub(t, otherPriv)
	other.releases = previewList
	cfg := other.config(t, "", "0.4.0", &r.launch)
	cfg.PubKey = r.cfg.PubKey
	m, err := cfg.Fetch(context.Background(), Preview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Download(context.Background(), m); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("download = %v", err)
	}
}

func TestDefaultChannelFollowsTheVersion(t *testing.T) {
	for current, want := range map[string]string{"0.5.0-alpha": Preview, "1.0.0-beta.2": Preview, "0.5.0": Stable, "0.0.0-dev+abc": Preview, "junk": Stable} {
		if got := DefaultChannel(current); got != want {
			t.Errorf("DefaultChannel(%q) = %s, want %s", current, got, want)
		}
	}
	// Until the person chooses, a pre-release build is on Preview and offers the newest pre-release.
	r := newRig(t, "0.5.0-alpha")
	r.f.releases = []fakeRelease{{tag: "v0.5.0-alpha.2", manifest: true}, {tag: "v0.4.1", manifest: true}}
	res, _ := r.call(t, "update.settings.get", "")
	if si := res.(SettingsInfo); si.Channel != Preview || si.ChannelChosen {
		t.Fatalf("settings = %+v", si)
	}
	if cr := r.check(t); cr.Latest != "0.5.0-alpha.2" || !cr.Newer {
		t.Fatalf("check = %+v", cr)
	}
	// Choosing Stable sticks.
	if si := r.set(t, `{"channel":"stable"}`); si.Channel != Stable || !si.ChannelChosen {
		t.Fatalf("after choosing: %+v", si)
	}
}

func TestNothingNewerIsNeverOfferedAndNothingDowngrades(t *testing.T) {
	// Running a pre-release that is ahead of the newest full release: switching to Stable offers nothing.
	r := newRig(t, "0.5.0-beta.2")
	r.f.releases = previewList
	if cr := r.check(t); cr.Newer || cr.Latest != "0.5.0-beta.2" {
		t.Fatalf("preview, same version: %+v", cr)
	}
	r.set(t, `{"channel":"stable"}`)
	cr := r.check(t)
	if cr.Newer || cr.Latest != "0.4.1" {
		t.Fatalf("stable older than this: %+v", cr)
	}
	if si, _ := r.call(t, "update.settings.get", ""); si.(SettingsInfo).Newer {
		t.Fatalf("settings say newer: %+v", si)
	}
	if _, err := r.call(t, "update.install", ""); err == nil {
		t.Fatal("install went through for an older version")
	}
	if len(r.launch) != 0 {
		t.Fatal("an installer was started")
	}
}

func TestSwitchingChannelForgetsWhatTheOldOneFound(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.f.releases = previewList
	r.set(t, `{"channel":"preview"}`)
	r.check(t)
	si := r.set(t, `{"channel":"stable"}`)
	if si.Latest != "" || si.LastChecked != "" || si.Newer {
		t.Fatalf("after switching: %+v", si)
	}
	// So the very next automatic check runs, on the new channel, without waiting a day.
	before := r.f.count("/stable/latest.json")
	res, _ := r.call(t, "update.auto", "")
	if cr := res.(CheckResult); !cr.Ran || cr.Latest != "0.4.1" || r.f.count("/stable/latest.json") != before+1 {
		t.Fatalf("auto = %+v, hits %v", cr, r.f.hits)
	}
	// Choosing the channel already in use changes nothing.
	r.set(t, `{"channel":"stable"}`)
	if si, _ := r.call(t, "update.settings.get", ""); si.(SettingsInfo).Latest != "0.4.1" {
		t.Fatalf("same channel forgot the result: %+v", si)
	}
	if _, err := r.call(t, "update.settings.set", `{"channel":"nightly"}`); err == nil {
		t.Fatal("an unknown channel was accepted")
	}
}

func TestRateLimitSkipsUntilTheNextDailyCheck(t *testing.T) {
	for name, c := range map[string]struct {
		limit func(now time.Time) func(http.ResponseWriter) bool
	}{
		"403 with the limit used up": {func(now time.Time) func(http.ResponseWriter) bool {
			return func(w http.ResponseWriter) bool {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(30*time.Minute).Unix(), 10))
				w.WriteHeader(http.StatusForbidden)
				return true
			}
		}},
		"429 with Retry-After": {func(now time.Time) func(http.ResponseWriter) bool {
			return func(w http.ResponseWriter) bool {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				return true
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, "0.4.0")
			r.f.releases = previewList
			r.set(t, `{"channel":"preview"}`)
			r.f.limit = c.limit(*r.now)

			// The automatic check says nothing and does not fail.
			res, err := r.call(t, "update.auto", "")
			if err != nil || res.(CheckResult).Ran || res.(CheckResult).Newer {
				t.Fatalf("auto = %+v, %v", res, err)
			}
			si, _ := r.call(t, "update.settings.get", "")
			if info := si.(SettingsInfo); info.SkipUntil == "" || info.LastError != "" {
				t.Fatalf("settings after the limit: %+v", info)
			}
			hits := r.f.total()
			// Not tried again within the day, by the timer or by hand.
			*r.now = r.now.Add(5 * time.Hour)
			r.call(t, "update.auto", "")
			cr, err := r.call(t, "update.check", "")
			if err != nil || cr.(CheckResult).Ran || cr.(CheckResult).Message == "" {
				t.Fatalf("manual check while paused = %+v, %v", cr, err)
			}
			if r.f.total() != hits {
				t.Fatalf("asked again while paused: %v", r.f.hits)
			}
			// After the next daily check time it goes on, and a good answer clears the pause.
			r.f.limit = nil
			*r.now = r.now.Add(16 * time.Hour)
			res, err = r.call(t, "update.auto", "")
			if err != nil || !res.(CheckResult).Ran || res.(CheckResult).Latest != "0.5.0-beta.2" {
				t.Fatalf("auto after the pause = %+v, %v", res, err)
			}
			if si, _ := r.call(t, "update.settings.get", ""); si.(SettingsInfo).SkipUntil != "" {
				t.Fatalf("pause not cleared: %+v", si)
			}
		})
	}
}

func TestRetryAfterBeyondADayIsHonoured(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.set(t, `{"channel":"stable"}`)
	r.f.limit = func(w http.ResponseWriter) bool {
		w.Header().Set("Retry-After", strconv.Itoa(3*24*3600))
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	}
	r.call(t, "update.auto", "")
	r.f.limit = nil
	*r.now = r.now.Add(48 * time.Hour)
	hits := r.f.total()
	if res, _ := r.call(t, "update.auto", ""); res.(CheckResult).Ran || r.f.total() != hits {
		t.Fatal("asked again before the time GitHub gave")
	}
	*r.now = r.now.Add(25 * time.Hour)
	if res, _ := r.call(t, "update.auto", ""); !res.(CheckResult).Ran {
		t.Fatal("did not ask after the time GitHub gave")
	}
}

func TestAPlainForbiddenIsAFailureNotAPause(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.f.limit = func(w http.ResponseWriter) bool { w.WriteHeader(http.StatusForbidden); return true }
	if _, err := r.call(t, "update.check", ""); err == nil {
		t.Fatal("a 403 without rate-limit headers was not an error")
	}
	if si, _ := r.call(t, "update.settings.get", ""); si.(SettingsInfo).SkipUntil != "" || si.(SettingsInfo).LastError == "" {
		t.Fatalf("settings = %+v", si)
	}
}

func TestAFailedAutomaticCheckIsQuietAndRecorded(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.f.releases = previewList
	r.set(t, `{"channel":"preview"}`)
	r.f.listErr = http.StatusInternalServerError
	res, err := r.call(t, "update.auto", "")
	if err != nil || res.(CheckResult).Ran || res.(CheckResult).Newer {
		t.Fatalf("auto = %+v, %v", res, err)
	}
	si, _ := r.call(t, "update.settings.get", "")
	if e := si.(SettingsInfo).LastError; !strings.Contains(e, "500") {
		t.Fatalf("last error = %q", e)
	}
	// Pressing the button does report it.
	if _, err := r.call(t, "update.check", ""); err == nil {
		t.Fatal("a manual check hid its failure")
	}
	// A later good check clears it.
	r.f.listErr = 0
	r.check(t)
	if si, _ := r.call(t, "update.settings.get", ""); si.(SettingsInfo).LastError != "" {
		t.Fatalf("error kept after a good check: %+v", si)
	}
}

func TestManifestAddressMustBeUnderThisProjectsReleases(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.set(t, `{"channel":"preview"}`)
	var foreign int
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { foreign++ }))
	defer evil.Close()
	r.f.releases = []fakeRelease{{tag: "v0.5.0-beta.2", manifest: true, badURL: evil.URL + "/latest.json"}}
	if _, err := r.call(t, "update.check", ""); err == nil || !strings.Contains(err.Error(), "not under this project") {
		t.Fatalf("check = %v", err)
	}
	if foreign != 0 {
		t.Fatal("a manifest was fetched from another host")
	}
	if si, _ := r.call(t, "update.settings.get", ""); si.(SettingsInfo).Latest != "" {
		t.Fatalf("settings = %+v", si)
	}
	// The real prefix is GitHub's; without the test override a local address is refused as well.
	cfg := r.cfg
	cfg.DownloadPrefix = ""
	if _, err := cfg.Fetch(context.Background(), Preview); err == nil {
		t.Fatal("a local address passed the default prefix")
	}
	// A manifest that names another version than its release is refused.
	r.f.releases = []fakeRelease{{tag: "v0.5.0-beta.2", manifest: true, version: "9.9.9"}}
	if _, err := r.call(t, "update.check", ""); err == nil || !strings.Contains(err.Error(), "names version") {
		t.Fatalf("check = %v", err)
	}
}

func TestNothingPublishedOffersNothingWithoutAnError(t *testing.T) {
	r := newRig(t, "0.4.0")
	r.f.stable = nil
	r.f.releases = []fakeRelease{{tag: "v0.5.0-beta.1"}, {tag: "v0.4.0", draft: true, manifest: true}}
	for _, ch := range []string{Stable, Preview} {
		r.set(t, fmt.Sprintf(`{"channel":%q}`, ch))
		cr, err := r.call(t, "update.check", "")
		if err != nil || !cr.(CheckResult).Ran || cr.(CheckResult).Newer || cr.(CheckResult).Latest != "" {
			t.Fatalf("%s: check = %+v, %v", ch, cr, err)
		}
		if si, _ := r.call(t, "update.settings.get", ""); si.(SettingsInfo).LastError != "" {
			t.Fatalf("%s: error recorded: %+v", ch, si)
		}
	}
}

func TestPickRelease(t *testing.T) {
	rel := func(tag string, draft bool, assetName, addr string) ghRelease {
		r := ghRelease{Tag: tag, Draft: draft}
		r.Assets = append(r.Assets, struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}{assetName, addr})
		return r
	}
	list := []ghRelease{
		rel("v2.0.0", true, "latest.json", "draft"),
		rel("not-a-version", false, "latest.json", "junk"),
		rel("v1.2.0-beta.1", false, "latest.json", "b1"),
		rel("v1.2.0-beta.3", false, "setup.exe", "no manifest"),
		rel("v1.2.0-beta.2", false, "latest.json", "b2"),
	}
	if tag, addr := pickRelease(list); tag != "v1.2.0-beta.2" || addr != "b2" {
		t.Fatalf("picked %s %s", tag, addr)
	}
	if tag, _ := pickRelease(nil); tag != "" {
		t.Fatalf("picked %s from nothing", tag)
	}
}

func TestTestServersAreOnlyHonouredOnThisComputer(t *testing.T) {
	t.Setenv(EndpointEnv, "http://127.0.0.1:9/latest.json")
	t.Setenv(ReleasesURLEnv, "https://example.com/releases")
	c := DefaultConfig()
	if c.Endpoint != "http://127.0.0.1:9/latest.json" || !c.AllowHTTP || c.DownloadPrefix != "http://127.0.0.1:9/" {
		t.Fatalf("local override not used: %+v", c)
	}
	if c.ReleasesURL != DefaultReleasesURL {
		t.Fatalf("a remote address replaced the release list: %s", c.ReleasesURL)
	}
	t.Setenv(EndpointEnv, "http://evil.example/latest.json")
	t.Setenv(ReleasesURLEnv, "")
	if c := DefaultConfig(); c.Endpoint != DefaultEndpoint || c.AllowHTTP {
		t.Fatalf("a remote http address was honoured: %+v", c)
	}
}
