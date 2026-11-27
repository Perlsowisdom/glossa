//go:build !windows

package problems

import (
	"os"
	"runtime"
	"strings"
)

// osVersion names the system: the distribution on Linux, the system name elsewhere.
func osVersion() string {
	if runtime.GOOS == "linux" {
		if b, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
					return strings.Trim(v, `"`)
				}
			}
		}
	}
	return runtime.GOOS
}

// WebView2Version is only known on Windows.
func WebView2Version() string { return "" }
