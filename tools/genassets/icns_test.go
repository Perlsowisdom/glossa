package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildICNS(t *testing.T) {
	pngs := map[int][]byte{}
	for _, e := range icnsEntries {
		pngs[e.size] = bytes.Repeat([]byte{byte(e.size % 251)}, 10+e.size%7)
	}
	out := buildICNS(pngs)
	if string(out[:4]) != "icns" || int(binary.BigEndian.Uint32(out[4:8])) != len(out) {
		t.Fatalf("bad header: % x (file is %d bytes)", out[:8], len(out))
	}
	pos, seen := 8, 0
	for pos < len(out) {
		typ := string(out[pos : pos+4])
		n := int(binary.BigEndian.Uint32(out[pos+4 : pos+8]))
		if n < 8 || pos+n > len(out) {
			t.Fatalf("entry %s at %d has length %d", typ, pos, n)
		}
		want := icnsEntries[seen]
		if typ != want.typ || !bytes.Equal(out[pos+8:pos+n], pngs[want.size]) {
			t.Fatalf("entry %d is %s, want %s with its PNG", seen, typ, want.typ)
		}
		pos += n
		seen++
	}
	if seen != len(icnsEntries) {
		t.Fatalf("%d entries, want %d", seen, len(icnsEntries))
	}
}
