package logosimport

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
)

// decodeBlob reads the text Logos packs into some columns: a marker byte, a 4-byte length, then
// the text itself, either as it is (marker 2) or compressed.
func decodeBlob(b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	if len(b) < 5 {
		return "", fmt.Errorf("a short packed value")
	}
	body := b[5:]
	if b[0] == 2 {
		return string(body), nil
	}
	readers := []func() (io.Reader, error){
		func() (io.Reader, error) { return gzip.NewReader(bytes.NewReader(body)) },
		func() (io.Reader, error) { return flate.NewReader(bytes.NewReader(body)), nil },
		func() (io.Reader, error) { return zlib.NewReader(bytes.NewReader(body)) },
	}
	for _, open := range readers {
		r, err := open()
		if err != nil {
			continue
		}
		if out, err := io.ReadAll(io.LimitReader(r, 64<<20)); err == nil {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("an unknown packing")
}
