package problems

import (
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// osVersion is the Windows version, such as "Windows 10.0.26200".
func osVersion() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("Windows %d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

// WebView2Version reads the installed WebView2 runtime's version from the registry, or "".
func WebView2Version() string {
	const guid = `\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	for _, k := range []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node` + guid},
		{registry.LOCAL_MACHINE, `SOFTWARE` + guid},
		{registry.CURRENT_USER, `Software` + guid},
	} {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue("pv")
		key.Close()
		if err == nil && v != "" {
			return v
		}
	}
	return ""
}
