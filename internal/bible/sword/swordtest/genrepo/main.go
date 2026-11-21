// Command genrepo writes a tiny SWORD repository folder for the real-app checks: a catalogue
// (mods.d.tar.gz) and one public-domain Bible module, for a local server to serve as if it were a
// Get Bibles source. Usage: go run ./internal/bible/sword/swordtest/genrepo <folder>
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/bible/sword/swordtest"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genrepo <folder>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out string) error {
	const name = "KJVE2E"
	dir := filepath.Join(out, "modules", "texts", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	verses := map[swordtest.Key]string{
		swordtest.K(43, 3, 16): "For God so loved the world, that he gave his one and only Son.",
		swordtest.K(1, 1, 1):   "In the beginning God created the heavens and the earth.",
	}
	if err := swordtest.ZText(dir, verses); err != nil {
		return err
	}
	conf := swordtest.Conf(name, "zText", "Abbreviation=KJV\nAbout=A small King James text for tests.\n")
	conf = strings.Replace(conf, "Description="+name+" test module", "Description=King James Version", 1)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "mods.d/" + name + ".conf", Mode: 0o644, Size: int64(len(conf)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write([]byte(conf)); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "mods.d.tar.gz"), buf.Bytes(), 0o644)
}
