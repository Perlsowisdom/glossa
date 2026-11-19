// Command winres writes app/rsrc_windows_amd64.syso: the app icon, a manifest (per-monitor DPI,
// current common controls) and the version info that Windows shows in the file's Properties. The Go
// linker picks the .syso file up by its name. Run before building the app; the file is not committed.
//
//	go run ./winres <repo root> <version>
package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/Perlsowisdom/glossa/tools/relver"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: winres <repo root> <version>")
	}
	root, ver := os.Args[1], os.Args[2]
	data, err := os.ReadFile(filepath.Join(root, "build", "windows", "icon.ico"))
	if err != nil {
		log.Fatal(err)
	}
	icon, err := winres.LoadICO(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.Name("APP"), icon); err != nil {
		log.Fatal(err)
	}
	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: "Glossa.App", Version: [4]uint16{1, 0, 0, 0}},
		Description:         "Glossa Bible study",
		Compatibility:       winres.Win7AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UIAccess:            false,
		UseCommonControlsV6: true,
	})
	vi := version.Info{}
	fv, err := relver.FileVersion(ver)
	if err != nil {
		log.Fatal(err)
	}
	vi.SetFileVersion(fv)
	vi.SetProductVersion(fv)
	const lang = 0x409
	vi.Set(lang, version.ProductName, "Glossa")
	vi.Set(lang, version.FileDescription, "Glossa Bible study")
	vi.Set(lang, version.LegalCopyright, "MIT OR Apache-2.0")
	vi.Set(lang, version.OriginalFilename, "Glossa.exe")
	vi.Set(lang, version.ProductVersion, ver)
	rs.SetVersionInfo(vi)
	out, err := os.Create(filepath.Join(root, "app", "rsrc_windows_amd64.syso"))
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
		log.Fatal(err)
	}
}
