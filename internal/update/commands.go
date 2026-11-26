//go:build !store

package update

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// settingsKey is a per-device setting (internal/store/remote.go): it is never synced.
const settingsKey = "update.settings"

// autoEvery is how long update.auto waits between checks.
const autoEvery = 20 * time.Hour

// maxNotes is the most of a release's notes that is kept.
const maxNotes = 2000

// Settings is stored in the settings table. Updates are on unless the person said no (ADR 0016).
type Settings struct {
	Enabled     bool   `json:"enabled" desc:"Check GitHub for new versions once a day. On unless you turn it off"`
	Asked       bool   `json:"asked" desc:"You chose on or off yourself (an old install that never answered counts as on)"`
	Channel     string `json:"channel,omitempty" desc:"stable (full releases) or preview (the newest release, pre-releases too). Empty until you choose"`
	LastChecked string `json:"last_checked,omitempty" desc:"When the last check finished (RFC 3339)"`
	Latest      string `json:"latest,omitempty" desc:"The newest version the last check found on this channel"`
	Notes       string `json:"notes,omitempty" desc:"What changed in that version, as its release says"`
	LastError   string `json:"last_error,omitempty" desc:"Why the last check failed; empty after a good one"`
	SkipUntil   string `json:"skip_until,omitempty" desc:"GitHub asked Glossa to wait; no check is made before this time (RFC 3339)"`
}

// Register adds the update commands with the real configuration.
func Register(r *command.Registry) { RegisterWith(r, DefaultConfig()) }

// RegisterWith adds the update commands with cfg (tests give it a local server).
func RegisterWith(r *command.Registry, cfg Config) {
	var mu sync.Mutex // one check at a time: the app's timer and the interface may ask together
	r.Add(
		command.Define("update.settings.get", "Show the update settings: whether Glossa checks for new versions (on unless you turn it off), the channel, when it last checked, and this version.", false,
			func(ctx context.Context, env *command.Env, _ struct{}) (SettingsInfo, error) {
				return settingsInfo(ctx, env, cfg)
			}),
		command.Define("update.settings.set", "Turn checking for updates on or off, or choose the channel (stable or preview). Nothing is checked or sent by this.", true,
			func(ctx context.Context, env *command.Env, p SetParams) (SettingsInfo, error) {
				return settingsSet(ctx, env, cfg, p)
			}),
		command.Define("update.check", "Look for a newer version now: one HTTPS GET of the release's latest.json (two on the preview channel). Needs updates turned on.", true,
			func(ctx context.Context, env *command.Env, _ struct{}) (CheckResult, error) {
				mu.Lock()
				defer mu.Unlock()
				return runCheck(ctx, env, cfg)
			}),
		command.Define("update.auto", "Check for a newer version if updates are on and the last check was a day ago. The app calls this at start-up and now and then; it does nothing when updates are off, and a failed check is recorded in the settings, not reported.", true,
			func(ctx context.Context, env *command.Env, _ struct{}) (CheckResult, error) {
				mu.Lock()
				defer mu.Unlock()
				return runAuto(ctx, env, cfg)
			}),
		command.Define("update.install", "Download the newer version, check its SHA-256 and signature, and run its installer; the app then restarts. Needs updates turned on.", false,
			func(ctx context.Context, env *command.Env, _ struct{}) (InstallResult, error) {
				return runInstall(ctx, env, cfg)
			}),
	)
}

type SettingsInfo struct {
	Settings
	Channel       string `json:"channel" desc:"The channel in use: stable or preview"`
	ChannelChosen bool   `json:"channel_chosen" desc:"You chose the channel; otherwise it follows this version (preview for a pre-release)"`
	Current       string `json:"current" desc:"This version"`
	Newer         bool   `json:"newer" desc:"The newest version found is newer than this one"`
	Key           bool   `json:"key_set" desc:"The build has a real update key; false means updates cannot verify yet"`
	Manual        bool   `json:"manual,omitempty" desc:"Updates are downloaded and installed by hand (macOS)"`
}

type SetParams struct {
	Enabled *bool  `json:"enabled,omitempty" desc:"true to check for updates, false to stop"`
	Channel string `json:"channel,omitempty" desc:"stable or preview. Switching never installs an older version"`
}

type CheckResult struct {
	Ran     bool   `json:"ran" desc:"A check was made"`
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty" desc:"The newest published version on the channel"`
	Newer   bool   `json:"newer" desc:"The newest version is newer than this one"`
	Notes   string `json:"notes,omitempty"`
	Manual  bool   `json:"manual,omitempty" desc:"This system cannot install updates itself: install downloads the new version in the browser instead"`
	Message string `json:"message,omitempty" desc:"Why no check was made, in plain words"`
}

type InstallResult struct {
	Version  string `json:"version"`
	Launched bool   `json:"launched" desc:"The installer was started; the app must quit now"`
	OpenURL  string `json:"open_url,omitempty" desc:"Where to download the new version; the app opens it in the browser"`
}

func needStore(env *command.Env) error {
	if env.Store == nil {
		return command.Errorf("unavailable", "no library is open")
	}
	return nil
}

// load reads the settings. With no row, or a row from before the first-run question was removed that
// the person never answered, updates are on; an explicit "no" is kept (ADR 0016).
func load(ctx context.Context, s *store.Store) (Settings, error) {
	var raw string
	err := s.QueryRow(ctx, `SELECT value_json FROM settings WHERE key = ?`, settingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{Enabled: true}, nil
	}
	if err != nil {
		return Settings{}, err
	}
	var st Settings
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return Settings{Enabled: true}, nil
	}
	if !st.Asked {
		st.Enabled = true
	}
	return st, nil
}

func save(ctx context.Context, s *store.Store, st Settings) error {
	raw, _ := json.Marshal(st)
	return s.Write(ctx, "update.settings", func(tx *store.Tx) error {
		_, err := tx.Get("settings", store.Row{"key": settingsKey})
		if errors.Is(err, store.ErrNotFound) {
			return tx.Create("settings", store.Row{"key": settingsKey, "value_json": string(raw)})
		} else if err != nil {
			return err
		}
		return tx.Update("settings", store.Row{"key": settingsKey}, store.Row{"value_json": string(raw)})
	})
}

// channelOf is the channel in use: the one chosen, or the default for this version.
func channelOf(st Settings, current string) string {
	if st.Channel == Stable || st.Channel == Preview {
		return st.Channel
	}
	return DefaultChannel(current)
}

func settingsInfo(ctx context.Context, env *command.Env, cfg Config) (SettingsInfo, error) {
	if err := needStore(env); err != nil {
		return SettingsInfo{}, err
	}
	st, err := load(ctx, env.Store)
	return SettingsInfo{
		Settings: st, Channel: channelOf(st, cfg.Current), ChannelChosen: st.Channel != "", Current: cfg.Current,
		Newer: Newer(st.Latest, cfg.Current), Key: len(cfg.PubKey) > 0 && !cfg.Placeholder, Manual: cfg.Manual,
	}, err
}

func settingsSet(ctx context.Context, env *command.Env, cfg Config, p SetParams) (SettingsInfo, error) {
	if err := needStore(env); err != nil {
		return SettingsInfo{}, err
	}
	st, err := load(ctx, env.Store)
	if err != nil {
		return SettingsInfo{}, err
	}
	if p.Enabled != nil {
		st.Enabled, st.Asked = *p.Enabled, true
	}
	if p.Channel != "" {
		if p.Channel != Stable && p.Channel != Preview {
			return SettingsInfo{}, command.Errorf("invalid", "the channel is %q or %q", Stable, Preview)
		}
		if p.Channel != channelOf(st, cfg.Current) {
			// What the old channel found says nothing about the new one: forget it, so the next check starts afresh.
			st.Latest, st.Notes, st.LastChecked, st.LastError, st.SkipUntil = "", "", "", "", ""
		}
		st.Channel = p.Channel
	}
	if err := save(ctx, env.Store, st); err != nil {
		return SettingsInfo{}, err
	}
	return settingsInfo(ctx, env, cfg)
}

func requireOn(ctx context.Context, env *command.Env) (Settings, error) {
	if err := needStore(env); err != nil {
		return Settings{}, err
	}
	st, err := load(ctx, env.Store)
	if err != nil {
		return st, err
	}
	if !st.Enabled {
		return st, command.Errorf("unavailable", "updates are off; turn them on in Settings first")
	}
	return st, nil
}

func runCheck(ctx context.Context, env *command.Env, cfg Config) (CheckResult, error) {
	st, err := requireOn(ctx, env)
	if err != nil {
		return CheckResult{}, err
	}
	if wait, ok := waiting(st, cfg); ok {
		res := cachedResult(st, cfg)
		res.Message = "GitHub asked Glossa to wait before looking again. It will try again " + wait + "."
		return res, nil
	}
	res, err := check(ctx, env, cfg, st)
	if err != nil {
		return res, command.Errorf("failed", "could not check for updates: %v", err)
	}
	return res, nil
}

func cachedResult(st Settings, cfg Config) CheckResult {
	return CheckResult{Current: cfg.Current, Latest: st.Latest, Newer: Newer(st.Latest, cfg.Current), Notes: st.Notes, Manual: cfg.Manual}
}

// waiting says whether GitHub asked for a pause that is not over, and when the next try is.
func waiting(st Settings, cfg Config) (string, bool) {
	until, err := time.Parse(time.RFC3339, st.SkipUntil)
	if err != nil || !cfg.now().Before(until) {
		return "", false
	}
	return "after " + until.Local().Format("2 January, 15:04"), true
}

// check asks the channel's source for the newest release and records the answer: what it found, or why
// it could not. A request to wait is recorded as a pause and is not an error. The error returned is
// only for the caller that asked for the check; update.auto drops it (nobody is told).
func check(ctx context.Context, env *command.Env, cfg Config, st Settings) (CheckResult, error) {
	m, err := cfg.Fetch(ctx, channelOf(st, cfg.Current))
	var rl *rateLimitError
	switch {
	case errors.As(err, &rl):
		st.SkipUntil, st.LastError = rl.Until.UTC().Format(time.RFC3339), ""
		if serr := save(ctx, env.Store, st); serr != nil {
			return CheckResult{}, serr
		}
		res := cachedResult(st, cfg)
		res.Message = "GitHub asked Glossa to wait before looking again. It will try again later."
		return res, nil
	case errors.Is(err, errNoRelease):
		m = Manifest{} // nothing published yet: a good check that found nothing
	case err != nil:
		st.LastError = err.Error()
		if serr := save(ctx, env.Store, st); serr != nil {
			return CheckResult{}, serr
		}
		return CheckResult{Current: cfg.Current}, err
	}
	st.LastChecked = cfg.now().UTC().Format(time.RFC3339)
	st.Latest, st.LastError, st.SkipUntil, st.Notes = m.Version, "", "", m.Notes
	if len(st.Notes) > maxNotes {
		st.Notes = st.Notes[:maxNotes]
	}
	if err := save(ctx, env.Store, st); err != nil {
		return CheckResult{}, err
	}
	res := cachedResult(st, cfg)
	res.Ran = true
	return res, nil
}

func runAuto(ctx context.Context, env *command.Env, cfg Config) (CheckResult, error) {
	if err := needStore(env); err != nil {
		return CheckResult{}, err
	}
	st, err := load(ctx, env.Store)
	if err != nil || !st.Enabled {
		return CheckResult{Current: cfg.Current}, err
	}
	if t, err := time.Parse(time.RFC3339, st.LastChecked); err == nil && cfg.now().Sub(t) < autoEvery {
		return cachedResult(st, cfg), nil
	}
	if _, ok := waiting(st, cfg); ok {
		return cachedResult(st, cfg), nil
	}
	res, err := check(ctx, env, cfg, st)
	if err != nil {
		return cachedResult(st, cfg), nil // recorded in the settings; nothing intrusive
	}
	return res, nil
}

func runInstall(ctx context.Context, env *command.Env, cfg Config) (InstallResult, error) {
	st, err := requireOn(ctx, env)
	if err != nil {
		return InstallResult{}, err
	}
	m, err := cfg.Fetch(ctx, channelOf(st, cfg.Current))
	if err != nil && !errors.Is(err, errNoRelease) {
		return InstallResult{}, command.Errorf("failed", "could not check for updates: %v", err)
	}
	if err != nil || !Newer(m.Version, cfg.Current) {
		return InstallResult{}, command.Errorf("not_found", "Glossa %s is the newest version", cfg.Current)
	}
	if cfg.Manual || cfg.Placeholder {
		// No installer is run (the Mac cannot replace itself; a placeholder key cannot verify one): the
		// person gets the download or the release page and installs it themselves.
		return InstallResult{Version: m.Version, OpenURL: cfg.ManualURL(m)}, nil
	}
	file, err := cfg.Download(ctx, m)
	if err != nil {
		return InstallResult{}, command.Errorf("failed", "%v", err)
	}
	if cfg.Launch == nil {
		return InstallResult{}, command.Errorf("unavailable", "this build cannot start an installer")
	}
	if err := cfg.Launch(file); err != nil {
		return InstallResult{}, command.Errorf("failed", "could not start the installer: %v", err)
	}
	return InstallResult{Version: m.Version, Launched: true}, nil
}
