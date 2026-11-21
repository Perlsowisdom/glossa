package main

import (
	"bytes"
	"encoding/binary"
)

// icnsEntry is one picture in an .icns file: a four-letter type and PNG data.
type icnsEntry struct {
	typ  string
	size int // pixels, one side, of the PNG
}

// icnsEntries lists the PNG-based types macOS reads (macOS 10.7 and later). The "@2x" types hold
// the pixels of the next size up, so each size in build/icon/png feeds two entries.
var icnsEntries = []icnsEntry{
	{"icp4", 16}, {"icp5", 32}, {"icp6", 64},
	{"ic11", 32}, {"ic12", 64}, // 16@2x, 32@2x
	{"ic07", 128}, {"ic13", 256}, // 128, 128@2x
	{"ic08", 256}, {"ic14", 512}, // 256, 256@2x
	{"ic09", 512}, {"ic10", 1024}, // 512, 512@2x
}

// buildICNS writes an .icns file: the "icns" magic, the total length, then each entry as its type,
// its length (including these 8 bytes) and the PNG. pngs maps a size to its PNG data.
func buildICNS(pngs map[int][]byte) []byte {
	var body bytes.Buffer
	for _, e := range icnsEntries {
		data := pngs[e.size]
		body.WriteString(e.typ)
		binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}
