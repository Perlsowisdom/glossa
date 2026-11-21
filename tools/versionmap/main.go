// Command versionmap prints a release version in the form a packager needs (see package relver):
//
//	go run ./versionmap validate 0.4.0-alpha
//	go run ./versionmap nsis 0.4.0-alpha     0.4.0.0
//	go run ./versionmap msix 0.4.0-alpha     0.4.0.0
//	go run ./versionmap mac 0.4.0-alpha      0.4.0
package main

import (
	"fmt"
	"os"

	"github.com/Perlsowisdom/glossa/tools/relver"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: versionmap validate|nsis|msix|mac <version>")
		os.Exit(2)
	}
	var out string
	var err error
	switch os.Args[1] {
	case "validate":
		err = relver.Validate(os.Args[2])
		out = os.Args[2]
	case "nsis":
		out, err = relver.FileVersion(os.Args[2])
	case "mac":
		out, err = relver.Mac(os.Args[2])
	case "msix":
		out, err = relver.MSIX(os.Args[2])
	default:
		err = fmt.Errorf("unknown form %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(out)
}
