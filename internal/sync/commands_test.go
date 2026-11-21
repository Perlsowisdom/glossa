package sync

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// app is one simulated installation driven only through commands.
type app struct {
	t    *testing.T
	reg  *command.Registry
	env  *command.Env
	keys *secrets.Memory
	d    *dev
}

func newApp(t *testing.T, name string) *app {
	t.Helper()
	d := newDev(t, name)
	keys := secrets.NewMemory()
	reg := command.NewRegistry()
	Register(reg, keys)
	t.Cleanup(reg.Close)
	return &app{t: t, reg: reg, env: &command.Env{Store: d.st}, keys: keys, d: d}
}

func (a *app) run(id string, params any, out any) error {
	a.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := a.reg.Run(context.Background(), a.env, id, raw)
	if err != nil {
		return err
	}
	if out != nil {
		b, _ := json.Marshal(res)
		return json.Unmarshal(b, out)
	}
	return nil
}

func code(err error) string {
	if ce, ok := err.(*command.Error); ok {
		return ce.Code
	}
	return ""
}

func TestCommandsEndToEnd(t *testing.T) {
	setupKDF = fastKDF
	defer func() { setupKDF = DefaultKDF }()
	folder := folderOf(t)
	a, b := newApp(t, "Laptop"), newApp(t, "Desktop")

	var s Status
	if err := a.run("sync.status", nil, &s); err != nil || s.Configured {
		t.Fatalf("fresh status = %+v, %v", s, err)
	}
	if err := a.run("sync.setup", SetupParams{Folder: folder, Password: "short"}, nil); code(err) != "weak_password" {
		t.Fatalf("short password: %v", err)
	}
	if err := a.run("sync.setup", SetupParams{Folder: filepath.Join(folder, "nope"), Password: password}, nil); code(err) != "folder_missing" {
		t.Fatalf("missing folder: %v", err)
	}
	if err := a.run("sync.now", nil, nil); code(err) != "not_configured" {
		t.Fatalf("sync before setup: %v", err)
	}

	a.d.mkNote("N1", "from the laptop")
	var pv Preview
	if err := a.run("sync.preview", PreviewParams{Folder: folder, Password: password}, &pv); err != nil {
		t.Fatal(err)
	}
	if pv.FolderHasData || pv.ThisDevice["notes"] != 1 || pv.ToFolder == 0 {
		t.Fatalf("preview of a new folder = %+v", pv)
	}
	if err := a.run("sync.setup", SetupParams{Folder: folder, Password: password, DeviceName: "Study laptop", Remember: true}, &s); err != nil {
		t.Fatal(err)
	}
	if !s.Configured || !s.Unlocked || !s.PasswordSaved || s.DeviceName != "Study laptop" || s.PendingChanges == 0 {
		t.Fatalf("status after setup = %+v", s)
	}
	var res Result
	if err := a.run("sync.now", nil, &res); err != nil || res.Pushed == 0 {
		t.Fatalf("sync.now = %+v, %v", res, err)
	}

	// B: wrong password first, then the right one.
	err := b.run("sync.setup", SetupParams{Folder: folder, Password: "not the right one"}, nil)
	if code(err) != "wrong_password" {
		t.Fatalf("wrong password: %v", err)
	}
	if strings.Contains(err.Error(), "not the right one") {
		t.Fatal("password in error")
	}
	if err := b.run("sync.setup", SetupParams{Folder: folder, Password: password, DeviceName: "Desk"}, &s); err != nil {
		t.Fatal(err)
	}
	if s.PasswordSaved {
		t.Fatal("password saved without being asked")
	}
	b.d.mkNote("N2", "from the desk")
	if err := b.run("sync.preview", PreviewParams{}, &pv); err != nil {
		t.Fatal(err)
	}
	if !pv.FolderHasData || pv.ToThisDevice.Created != 1 || pv.AfterMerge["notes"] != 2 || pv.Devices[0] != "Study laptop" {
		t.Fatalf("B preview = %+v", pv)
	}
	if _, _, ok := b.d.note("N1"); ok {
		t.Fatal("preview merged")
	}
	if err := b.run("sync.now", nil, &res); err != nil || res.Pulled.Created != 1 {
		t.Fatalf("B sync = %+v, %v", res, err)
	}
	if err := a.run("sync.now", nil, &res); err != nil {
		t.Fatal(err)
	}
	sameLibraries(t, a.d, b.d)

	var u Usage
	if err := a.run("sync.devices", nil, &u); err != nil || len(u.Devices) != 2 || u.Bytes == 0 {
		t.Fatalf("devices = %+v, %v", u, err)
	}
	if err := a.run("sync.status", nil, &s); err != nil || s.LastSync == 0 || s.PendingChanges != 0 || len(s.Devices) != 2 {
		t.Fatalf("status = %+v, %v", s, err)
	}

	// B restarts: the password is gone from memory, so it must be entered again.
	b2 := &app{t: t, reg: command.NewRegistry(), env: b.env, keys: b.keys, d: b.d}
	Register(b2.reg, b2.keys)
	t.Cleanup(b2.reg.Close)
	if err := b2.run("sync.now", nil, nil); code(err) != "locked" {
		t.Fatalf("after restart: %v", err)
	}
	if err := b2.run("sync.unlock", UnlockParams{Password: "wrong wrong wrong"}, nil); code(err) != "wrong_password" {
		t.Fatalf("unlock: %v", err)
	}
	if err := b2.run("sync.unlock", UnlockParams{Password: password}, nil); err != nil {
		t.Fatal(err)
	}
	if err := b2.run("sync.now", nil, nil); err != nil {
		t.Fatal(err)
	}
	// A restarts too, but remembered its password.
	a2 := &app{t: t, reg: command.NewRegistry(), env: a.env, keys: a.keys, d: a.d}
	Register(a2.reg, a2.keys)
	t.Cleanup(a2.reg.Close)
	if err := a2.run("sync.now", nil, nil); err != nil {
		t.Fatalf("remembered password: %v", err)
	}

	// A text conflict shows through commands.
	a.d.at(50000).editNote("N1", "laptop edit")
	b.d.at(60000).editNote("N1", "desk edit")
	a.run("sync.now", nil, nil)
	b.run("sync.now", nil, nil)
	a.run("sync.now", nil, nil)
	var cs []store.SyncConflict
	if err := a.run("sync.conflicts", nil, &cs); err != nil || len(cs) != 1 || cs[0].Note != "Changed on Study laptop too" {
		t.Fatalf("conflicts = %+v, %v", cs, err)
	}
	if err := a.run("sync.conflicts.dismiss", DismissParams{ID: cs[0].ID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := a.run("sync.conflicts", nil, &cs); err != nil || len(cs) != 0 {
		t.Fatalf("after dismiss = %+v, %v", cs, err)
	}

	// Forget B, then stop A.
	if err := a.run("sync.forget_device", ForgetParams{DeviceID: b.d.st.Device()}, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.run("sync.now", nil, nil); code(err) != "forgotten" {
		t.Fatalf("forgotten device: %v", err)
	}
	if err := a.run("sync.stop", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.keys.Get(secretName); err == nil {
		t.Fatal("stop left the saved password")
	}
	if err := a.run("sync.status", nil, &s); err != nil || s.Configured {
		t.Fatalf("after stop = %+v", s)
	}
}

func TestCommandsAreDescribedAndNeverShowThePassword(t *testing.T) {
	reg := command.NewRegistry()
	Register(reg, secrets.NewMemory())
	n := 0
	for _, d := range reg.List() {
		if !strings.HasPrefix(d.ID, "sync.") {
			continue
		}
		n++
		if d.Summary == "" {
			t.Errorf("%s has no summary", d.ID)
		}
	}
	if n < 10 {
		t.Fatalf("only %d sync commands", n)
	}
	// Status carries no password field at all.
	if strings.Contains(strings.ToLower(jsonOf(Status{})), "password\"") {
		// "password_saved" is a flag, not a value.
		if strings.Contains(jsonOf(Status{}), `"password":`) {
			t.Fatal("status has a password field")
		}
	}
}
