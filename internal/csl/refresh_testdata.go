//go:build ignore

// This program refreshes the pinned copies of upstream CSL data used by the tests and bundled
// with Glossa. It is the only place that uses the network; go test never does.
//
//	go generate ./internal/csl        (or: go run refresh_testdata.go)
//
// The commits below are the pins. Change them to move to newer upstream data, then run it.
package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	testSuiteCommit = "6eefc5b07c6969ab8999e48542acbcc131cba864"
	localesCommit   = "21a175a1d84cc1792dd73f5a627de004fba5b207"
	stylesCommit    = "15ea5749688396c9352b01b7641bcc5fe1b77da5"
)

var bundledStyles = []string{
	"society-of-biblical-literature-fullnote-bibliography.csl",
	"chicago-notes-bibliography.csl",
	"apa.csl",
}

func main() {
	check(fetch("citation-style-language/test-suite", testSuiteCommit, func(name string) (string, bool) {
		if d, f := path.Split(name); d == "processor-tests/humans/" && strings.HasSuffix(f, ".txt") {
			return filepath.Join("testdata", "test-suite", "humans", f), true
		}
		return "", false
	}))
	check(fetch("citation-style-language/locales", localesCommit, func(name string) (string, bool) {
		if strings.HasPrefix(name, "locales-") && strings.HasSuffix(name, ".xml") || name == "locales.json" {
			return filepath.Join("testdata", "test-suite", "locales", name), true
		}
		return "", false
	}))
	check(fetch("citation-style-language/locales", localesCommit, func(name string) (string, bool) {
		if name == "locales-en-US.xml" || name == "locales-en-GB.xml" {
			return filepath.Join("styles", name), true
		}
		return "", false
	}))
	check(fetch("citation-style-language/styles", stylesCommit, func(name string) (string, bool) {
		for _, s := range bundledStyles {
			if name == s {
				return filepath.Join("styles", name), true
			}
		}
		return "", false
	}))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "refresh:", err)
		os.Exit(1)
	}
}

// fetch downloads a repository archive at a commit and writes the files pick selects. pick gets
// the path inside the archive (without the top folder) and returns where to write it.
func fetch(repo, commit string, pick func(string) (string, bool)) error {
	resp, err := http.Get("https://codeload.github.com/" + repo + "/tar.gz/" + commit)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", repo, resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		i := strings.IndexByte(h.Name, '/')
		if h.Typeflag != tar.TypeReg || i < 0 {
			continue
		}
		dst, ok := pick(h.Name[i+1:])
		if !ok {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
		n++
	}
	fmt.Printf("%s@%.7s: %d files\n", repo, commit, n)
	return nil
}
