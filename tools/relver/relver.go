// Package relver maps a release version such as 0.4.0-alpha to the forms the packagers need.
//
//	go run ./versionmap validate <version>   exit 0 when it is MAJOR.MINOR.PATCH with an optional -suffix
//	go run ./versionmap nsis <version>       four-part number for the setup's file properties: 0.4.0.0
//	go run ./versionmap msix <version>       four-part number for the Store package: 0.4.0.0
//	go run ./versionmap mac <version>        three-part number for the macOS app bundle: 0.4.0
//	go run ./versionmap deb <version>        Debian package version: 0.4.0~alpha (sorts before 0.4.0)
package relver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// semver is MAJOR.MINOR.PATCH, an optional -pre.release suffix and an optional +build suffix.
// Numbers have no leading zeros. (The build suffix is for dev builds: 0.0.0-dev+abc1234.)
var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// Validate reports whether v is a version the release workflow accepts. A leading "v" is not part
// of the version.
func Validate(v string) error {
	if !semver.MatchString(v) {
		return fmt.Errorf("%q is not a version like 1.2.3 or 1.2.3-beta.1", v)
	}
	return nil
}

// parts returns the three leading numbers; the suffix is ignored, and numbers are capped at the
// 16-bit range that file versions and package versions allow.
func parts(v string) ([3]int, error) {
	var n [3]int
	if err := Validate(v); err != nil {
		return n, err
	}
	core := strings.FieldsFunc(v, func(r rune) bool { return r == '-' || r == '+' })[0]
	for i, s := range strings.Split(core, ".") {
		x, err := strconv.Atoi(s)
		if err != nil || x > 65535 {
			return n, fmt.Errorf("%q: %s is too large for a file version (at most 65535)", v, s)
		}
		n[i] = x
	}
	return n, nil
}

// FileVersion is the four-part number for version resources and the installer's VIProductVersion:
// 0.4.0-alpha gives 0.4.0.0.
func FileVersion(v string) (string, error) {
	n, err := parts(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d.0", n[0], n[1], n[2]), nil
}

// MSIX is the four-part package version. The Store reserves the fourth part for itself (it must be
// 0) and does not allow 0.0.0.0, so a version of 0.0.0 (a development build) becomes 0.0.1.0.
func MSIX(v string) (string, error) {
	n, err := parts(v)
	if err != nil {
		return "", err
	}
	if n[0]+n[1]+n[2] == 0 {
		n[2] = 1
	}
	return fmt.Sprintf("%d.%d.%d.0", n[0], n[1], n[2]), nil
}

// Mac is the version for a macOS app bundle (CFBundleShortVersionString and CFBundleVersion): three
// numbers, the suffix dropped. 0.5.0-alpha gives 0.5.0. A development build's 0.0.0 stays 0.0.0.
func Mac(v string) (string, error) {
	n, err := parts(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2]), nil
}

// Deb is the version for a Debian package's control file. A suffix sorts before the release it leads
// to, so 0.6.1-alpha becomes 0.6.1~alpha (the tilde sorts before everything, even the empty string):
// dpkg and apt treat 0.6.1~alpha as older than 0.6.1. Hyphens inside the suffix would read as the
// start of a Debian revision, so they become dots; "+build" metadata is kept, as Debian allows it.
func Deb(v string) (string, error) {
	n, err := parts(v)
	if err != nil {
		return "", err
	}
	rest := v[len(fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2])):]
	pre, build, _ := strings.Cut(rest, "+")
	out := fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2])
	if pre != "" {
		out += "~" + strings.ReplaceAll(strings.TrimPrefix(pre, "-"), "-", ".")
	}
	if build != "" {
		out += "+" + strings.ReplaceAll(build, "-", ".")
	}
	return out, nil
}
