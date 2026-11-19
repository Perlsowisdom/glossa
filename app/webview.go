package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/appdir"
)

// migrateWebViewData carries WebView2's user data (it holds the interface's saved settings) over
// from the Xegesis days. WebView2 keeps it in %APPDATA%\<exe name>, so Glossa.exe would start with
// an empty one; this moves %APPDATA%\Xegesis.exe to %APPDATA%\Glossa.exe before the window opens
// (see appdir.Migrate). Other executable names (development builds) are left alone.
func migrateWebViewData() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := os.Executable()
	appData := os.Getenv("AppData")
	if err != nil || appData == "" || !strings.EqualFold(filepath.Base(exe), appdir.Name+".exe") {
		return
	}
	appdir.Migrate(appData, appdir.Name+".exe", appdir.OldName+".exe", nil, log.Printf)
}
