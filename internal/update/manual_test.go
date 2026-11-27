//go:build !store

package update

import (
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/update/updatesig"
)

func TestManualUpdatesOpenADownloadAndInstallNothing(t *testing.T) {
	pubB64, priv, _ := updatesig.GenerateKey()
	_, otherPriv, _ := updatesig.GenerateKey()
	dmg := strings.Repeat("cd", 32)
	sig := func(p string) string { s, _ := updatesig.Sign(p, "1.1.0", dmg); return s }
	const good = "https://github.com/Perlsowisdom/glossa/releases/download/v1.1.0/Glossa-1.1.0-macos-universal.dmg"
	page := "https://github.com/Perlsowisdom/glossa/releases/tag/v1.1.0"
	for name, c := range map[string]struct {
		mac  *Asset
		want string
	}{
		"signed dmg":        {&Asset{URL: good, SHA256: dmg, Signature: sig(priv)}, good},
		"no mac entry":      {nil, page},
		"signed by another": {&Asset{URL: good, SHA256: dmg, Signature: sig(otherPriv)}, page},
		"foreign address":   {&Asset{URL: "https://evil.example/x.dmg", SHA256: dmg, Signature: sig(priv)}, page},
		"bad hash shape":    {&Asset{URL: good, SHA256: "12", Signature: sig(priv)}, page},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRelease(t, "1.1.0", priv, []byte("installer"), func(m *Manifest) { m.Mac = c.mac })
			var launched []string
			cfg := testConfig(t, r, pubB64, &launched)
			cfg.Manual = true
			cfg.OS = "darwin"
			reg := command.NewRegistry()
			RegisterWith(reg, cfg)
			env := openEnv(t)
			if _, err := run(t, reg, env, "update.settings.set", `{"enabled":true}`); err != nil {
				t.Fatal(err)
			}
			res, err := run(t, reg, env, "update.check", "")
			if err != nil || !res.(CheckResult).Newer || !res.(CheckResult).Manual {
				t.Fatalf("check = %+v, %v", res, err)
			}
			res, err = run(t, reg, env, "update.install", "")
			if err != nil {
				t.Fatal(err)
			}
			ir := res.(InstallResult)
			if ir.Launched || ir.OpenURL != c.want || ir.Version != "1.1.0" {
				t.Fatalf("install = %+v, want url %s", ir, c.want)
			}
			if len(launched) != 0 {
				t.Fatal("an installer was launched")
			}
			if got := r.hits.Load(); got != 2 { // two manifest fetches, no download
				t.Fatalf("server hit %d times, want 2 (no download)", got)
			}
		})
	}
}

func TestManualInstallStillNeedsUpdatesOnAndNewer(t *testing.T) {
	pubB64, priv, _ := updatesig.GenerateKey()
	r := newRelease(t, "1.0.0", priv, []byte("x"), nil)
	var launched []string
	cfg := testConfig(t, r, pubB64, &launched)
	cfg.Manual = true
	reg := command.NewRegistry()
	RegisterWith(reg, cfg)
	env := openEnv(t)
	if _, err := run(t, reg, env, "update.install", ""); err == nil {
		t.Fatal("manual install ran while updates were off")
	}
	run(t, reg, env, "update.settings.set", `{"enabled":true}`)
	if _, err := run(t, reg, env, "update.install", ""); err == nil {
		t.Fatal("manual install offered a version that is not newer")
	}
}

func TestParseManifestDropsBadMacEntryKeepsRest(t *testing.T) {
	h := strings.Repeat("ab", 32)
	m, err := ParseManifest([]byte(`{"version":"1.2.3","url":"https://a/b","sha256":"`+h+`","signature":"c2ln","mac":{"url":"http://a/b","sha256":"zz","signature":""}}`), false)
	if err != nil || m.Mac != nil {
		t.Fatalf("%+v %v", m, err)
	}
	m, err = ParseManifest([]byte(`{"version":"1.2.3","url":"https://a/b","sha256":"`+h+`","signature":"c2ln","mac":{"url":"https://a/c.dmg","sha256":"`+strings.ToUpper(h)+`","signature":"c2ln"}}`), false)
	if err != nil || m.Mac == nil || m.Mac.SHA256 != h {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestLinuxManualURLOffersTheDebOnlyToAPackageInstall(t *testing.T) {
	pubB64, priv, _ := updatesig.GenerateKey()
	pub, _ := updatesig.ParsePublicKey(pubB64)
	deb := strings.Repeat("ef", 32)
	s, _ := updatesig.Sign(priv, "1.1.0", deb)
	const good = "https://github.com/Perlsowisdom/glossa/releases/download/v1.1.0/Glossa-1.1.0-linux-amd64.deb"
	page := "https://github.com/Perlsowisdom/glossa/releases/tag/v1.1.0"
	m := Manifest{Version: "1.1.0", Linux: &Asset{URL: good, SHA256: deb, Signature: s}, Mac: &Asset{URL: "https://github.com/Perlsowisdom/glossa/releases/download/v1.1.0/x.dmg", SHA256: deb, Signature: s}}
	for name, c := range map[string]struct {
		cfg  Config
		want string
	}{
		"installed from the package":     {Config{OS: "linux", Packaged: true, Manual: true}, good},
		"unpacked from the tar.gz":       {Config{OS: "linux", Manual: true}, page},
		"linux entry is not for the mac": {Config{OS: "darwin", Manual: true}, m.Mac.URL},
		"no linux entry":                 {Config{OS: "linux", Packaged: true, Manual: true}, page},
	} {
		t.Run(name, func(t *testing.T) {
			c.cfg.PubKey = pub
			mm := m
			switch name {
			case "no linux entry":
				mm.Linux = nil
			}
			if got := c.cfg.ManualURL(mm); got != c.want {
				t.Fatalf("ManualURL = %s, want %s", got, c.want)
			}
		})
	}
}

func TestPlatformNamesManualSystems(t *testing.T) {
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{OS: "darwin", Manual: true}, "mac"},
		{Config{OS: "linux", Manual: true}, "linux"},
		{Config{OS: "windows"}, ""},
		{Config{OS: "linux"}, ""},
	} {
		if got := c.cfg.Platform(); got != c.want {
			t.Errorf("%+v: Platform = %q, want %q", c.cfg, got, c.want)
		}
	}
}

func TestParseManifestKeepsGoodLinuxEntryDropsBadOne(t *testing.T) {
	h := strings.Repeat("ab", 32)
	base := `{"version":"1.2.3","url":"https://a/b","sha256":"` + h + `","signature":"c2ln","linux":`
	m, err := ParseManifest([]byte(base+`{"url":"https://a/c.deb","sha256":"`+strings.ToUpper(h)+`","signature":"c2ln"}}`), false)
	if err != nil || m.Linux == nil || m.Linux.SHA256 != h {
		t.Fatalf("%+v %v", m, err)
	}
	m, err = ParseManifest([]byte(base+`{"url":"http://a/c.deb","sha256":"zz","signature":""}}`), false)
	if err != nil || m.Linux != nil || m.URL == "" {
		t.Fatalf("%+v %v", m, err)
	}
}
