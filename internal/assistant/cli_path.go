package assistant

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// A desktop app started from Finder (macOS) or a launcher (Linux) gets a short PATH
// (/usr/bin:/bin:...), without the folders where Claude Code and Codex usually live. extraToolDirs
// lists those folders; findTool looks there after PATH, and toolEnv adds them to the PATH a tool is
// started with (these tools are often scripts that need `node` from the same folders).
// Windows has no such gap, so it has no extra folders. Tests replace this.
var extraToolDirs = defaultToolDirs

func defaultToolDirs() []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	home, _ := os.UserHomeDir()
	var dirs []string
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".claude", "local"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, "bin"),
		)
		// nvm keeps one folder per Node version; newest name first.
		if vs, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); len(vs) > 0 {
			sort.Sort(sort.Reverse(sort.StringSlice(vs)))
			dirs = append(dirs, vs...)
		}
	}
	return append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
}

// findTool is exec.LookPath, then the extra folders.
func findTool(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}
	for _, d := range extraToolDirs() {
		p := filepath.Join(d, name)
		if st, serr := os.Stat(p); serr == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", err
}

// toolEnv is the environment for a tool: the app's own, with the extra folders (and the tool's
// own folder) added to the end of PATH when they are missing.
func toolEnv(toolPath string) []string {
	env := os.Environ()
	extra := extraToolDirs()
	if len(extra) == 0 {
		return env
	}
	have := map[string]bool{}
	var pathIdx = -1
	for i, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == "PATH" {
			pathIdx = i
			for _, d := range filepath.SplitList(v) {
				have[d] = true
			}
		}
	}
	add := append([]string{filepath.Dir(toolPath)}, extra...)
	var missing []string
	for _, d := range add {
		if !have[d] {
			have[d] = true
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return env
	}
	if pathIdx < 0 {
		return append(env, "PATH="+strings.Join(missing, string(os.PathListSeparator)))
	}
	env[pathIdx] += string(os.PathListSeparator) + strings.Join(missing, string(os.PathListSeparator))
	return env
}
