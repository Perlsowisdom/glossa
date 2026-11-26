package sync

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// KDFParams are the Argon2id cost settings. They are stored in the folder next to the salt, so a
// later version can raise them.
type KDFParams struct {
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
}

// DefaultKDF is what Glossa uses: 3 passes over 128 MiB on 4 lanes, about a second on a current laptop.
var DefaultKDF = KDFParams{Time: 3, MemoryKiB: 128 * 1024, Threads: 4}

// Limits on parameters read from a folder, so a tampered key file cannot make Glossa use no
// memory at all or all of it.
const (
	minKDFMemoryKiB = 8 * 1024
	maxKDFMemoryKiB = 1024 * 1024
	maxKDFTime      = 20
	maxKDFThreads   = 16
)

func (p KDFParams) valid() error {
	if p.Time < 1 || p.Time > maxKDFTime || p.Threads < 1 || p.Threads > maxKDFThreads ||
		p.MemoryKiB < minKDFMemoryKiB || p.MemoryKiB > maxKDFMemoryKiB {
		return fmt.Errorf("%w: the key settings are out of range", ErrCorrupt)
	}
	return nil
}

// MinPasswordLength is the shortest password accepted, in characters.
const MinPasswordLength = 10

// CheckPassword refuses a password that is too short.
func CheckPassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return fmt.Errorf("%w: use at least %d characters", ErrWeakPassword, MinPasswordLength)
	}
	return nil
}

// Strength rates a password from 0 (too short) to 4 (strong) with a hint for the person choosing it.
// It is a rough guide, not a guarantee: length matters most.
func Strength(password string) (int, string) {
	n := utf8.RuneCountInString(password)
	if n < MinPasswordLength {
		return 0, fmt.Sprintf("Too short: use at least %d characters.", MinPasswordLength)
	}
	var lower, upper, digit, other bool
	distinct := map[rune]bool{}
	for _, r := range password {
		distinct[r] = true
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	classes := 0
	for _, b := range []bool{lower, upper, digit, other} {
		if b {
			classes++
		}
	}
	score := 1
	switch {
	case n >= 20 || (n >= 16 && classes >= 3):
		score = 4
	case n >= 14 || (n >= 12 && classes >= 3):
		score = 3
	case n >= 12 || classes >= 3:
		score = 2
	}
	if len(distinct) < n/3 {
		score = min(score, 1) // mostly repeated characters
	}
	switch score {
	case 1:
		return 1, "Weak: a longer password, or a few random words, is much stronger."
	case 2:
		return 2, "Fair: longer is better. A phrase of several words works well."
	case 3:
		return 3, "Good."
	}
	return 4, "Strong."
}

// File kinds, part of every file's header and authenticated with it.
const (
	kindChange   = 'C'
	kindSnapshot = 'S'
	kindKeyCheck = 'K'
	kindJoin     = 'J' // a device's "I have joined" marker
)

const (
	formatVersion = 1
	headerLen     = 6 // "GLSY", version, kind
	maxPlaintext  = 1 << 30
)

func deriveKey(password string, salt []byte, p KDFParams) []byte {
	return argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, chacha20poly1305.KeySize)
}

// seal compresses and encrypts plaintext. The header, the device and the sequence number are
// authenticated, so a file cannot be renamed, moved to another device's folder or given another
// kind without being refused.
func seal(key []byte, kind byte, device string, seq uint64, plaintext []byte) ([]byte, error) {
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	if _, err := zw.Write(plaintext); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	hdr := append([]byte("GLSY"), formatVersion, kind)
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(append([]byte{}, hdr...), nonce...)
	return aead.Seal(out, nonce, z.Bytes(), aad(hdr, device, seq)), nil
}

// open reverses seal. Any failure, from a short file to a changed byte, is ErrCorrupt.
func open(key, data []byte, kind byte, device string, seq uint64) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(data) < headerLen+aead.NonceSize()+aead.Overhead() {
		return nil, fmt.Errorf("%w: it is too short", ErrCorrupt)
	}
	hdr := data[:headerLen]
	if string(hdr[:4]) != "GLSY" || hdr[5] != kind {
		return nil, fmt.Errorf("%w: it is not a Glossa sync file", ErrCorrupt)
	}
	if hdr[4] != formatVersion {
		if hdr[4] > formatVersion {
			return nil, ErrNewerVersion
		}
		return nil, fmt.Errorf("%w: unknown format", ErrCorrupt)
	}
	nonce := data[headerLen : headerLen+aead.NonceSize()]
	z, err := aead.Open(nil, nonce, data[headerLen+aead.NonceSize():], aad(hdr, device, seq))
	if err != nil {
		return nil, ErrCorrupt
	}
	zr, err := gzip.NewReader(bytes.NewReader(z))
	if err != nil {
		return nil, ErrCorrupt
	}
	out, err := io.ReadAll(io.LimitReader(zr, maxPlaintext+1))
	if err != nil || len(out) > maxPlaintext {
		return nil, ErrCorrupt
	}
	return out, nil
}

func aad(hdr []byte, device string, seq uint64) []byte {
	out := append([]byte{}, hdr...)
	out = append(out, device...)
	out = append(out, 0)
	return binary.BigEndian.AppendUint64(out, seq)
}

// keyDoc is key.json, the one unencrypted file: the salt and cost settings the key is derived with,
// and a small encrypted value that tells a right password from a wrong one.
type keyDoc struct {
	Format  int       `json:"format"`
	KDF     string    `json:"kdf"`
	Params  KDFParams `json:"params"`
	Salt    []byte    `json:"salt"`
	Created int64     `json:"created"`
	Check   []byte    `json:"check"`
}

const keyCheckText = "glossa sync: password ok"

func newKeyDoc(password string, p KDFParams, created int64) (keyDoc, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return keyDoc{}, err
	}
	key := deriveKey(password, salt, p)
	check, err := seal(key, kindKeyCheck, "", 0, []byte(keyCheckText))
	if err != nil {
		return keyDoc{}, err
	}
	return keyDoc{Format: formatVersion, KDF: "argon2id", Params: p, Salt: salt, Created: created, Check: check}, nil
}

// keyFrom derives the key from a password and checks it against the folder's key file.
func keyFrom(raw []byte, password string) ([]byte, error) {
	var d keyDoc
	if err := json.Unmarshal(raw, &d); err != nil || d.KDF != "argon2id" || len(d.Salt) < 8 {
		return nil, fmt.Errorf("%w: the key file is not readable", ErrCorrupt)
	}
	if d.Format > formatVersion {
		return nil, ErrNewerVersion
	}
	if err := d.Params.valid(); err != nil {
		return nil, err
	}
	key := deriveKey(password, d.Salt, d.Params)
	text, err := open(key, d.Check, kindKeyCheck, "", 0)
	if err != nil || string(text) != keyCheckText {
		return nil, ErrWrongPassword
	}
	return key, nil
}
