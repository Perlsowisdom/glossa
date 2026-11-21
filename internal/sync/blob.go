package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Blobs are large files (books, Bible modules) kept in <folder>/Glossa Sync/blobs/, content-addressed
// by the SHA-256 of the plain file, encrypted with the folder's key (ADR 0013).
//
// A blob is a run of chunk files, `<sha>.000000.c`, `<sha>.000001.c`, ..., each at most BlobChunk
// bytes of the plain file, compressed and encrypted on its own, then one manifest, `<sha>.m`, written
// last. The manifest is what makes the blob exist: a reader that finds no manifest sees nothing, so an
// upload cut short is never mistaken for a whole blob, and the next attempt finishes it. Memory stays
// at about one chunk whatever the file's size. The manifest lists the hash of every chunk, so a chunk
// that is missing, cut short, altered or swapped for another is refused, and the finished file is
// checked against the blob's name before it is put in place.

const (
	kindBlob         = 'B'
	kindBlobManifest = 'M'

	blobsDir         = "blobs"
	defaultBlobChunk = 4 << 20
	maxBlobChunk     = 64 << 20
)

var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func blobManifestName(sha string) string { return blobsDir + "/" + sha + ".m" }
func blobChunkName(sha string, i int) string {
	return fmt.Sprintf("%s/%s.%06d.c", blobsDir, sha, i)
}

// blobManifest is the plain text of a manifest file.
type blobManifest struct {
	Format    int      `json:"format"`
	SHA256    string   `json:"sha256"`
	Size      int64    `json:"size"`
	ChunkSize int      `json:"chunk_size"`
	Chunks    []string `json:"chunks"` // SHA-256 of each plain chunk
}

func (m blobManifest) valid(sha string) bool {
	if m.Format != formatVersion || m.SHA256 != sha || m.Size < 0 || m.ChunkSize < 1 || m.ChunkSize > maxBlobChunk {
		return false
	}
	want := (m.Size + int64(m.ChunkSize) - 1) / int64(m.ChunkSize)
	if int64(len(m.Chunks)) != want {
		return false
	}
	for _, c := range m.Chunks {
		if !shaRE.MatchString(c) {
			return false
		}
	}
	return true
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hashFile hashes a file in a stream.
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// blobExists tells whether a whole blob (its manifest) is in the folder.
func (e *Engine) blobExists(ctx context.Context, sha string) (bool, error) {
	_, err := e.t.Read(ctx, blobManifestName(sha))
	if errors.Is(err, ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// errChanged is returned when a file is not what its hash says: it was edited or replaced since.
var errChanged = errors.New("the file changed while it was being read")

// putBlob uploads a file as a blob unless the folder already has it. The file must hash to sha. It
// reports whether anything was written; advance is told how many plain bytes were handled.
func (e *Engine) putBlob(ctx context.Context, sha, path string, advance func(int64)) (bool, error) {
	if ok, err := e.blobExists(ctx, sha); err != nil || ok {
		return false, err
	}
	got, size, err := hashFile(path)
	if err != nil {
		return false, err
	}
	if got != sha {
		return false, errChanged
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	chunk := e.blobChunk()
	man := blobManifest{Format: formatVersion, SHA256: sha, Size: size, ChunkSize: chunk}
	buf := make([]byte, chunk)
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		n, err := io.ReadFull(f, buf)
		if n == 0 && (err == io.EOF || err == io.ErrUnexpectedEOF) {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			return false, err
		}
		plain := buf[:n]
		man.Chunks = append(man.Chunks, sum(plain))
		if err := e.putChunk(ctx, sha, i, plain, man.Chunks[i]); err != nil {
			return false, err
		}
		advance(int64(n))
		if err == io.ErrUnexpectedEOF {
			break
		}
	}
	if !man.valid(sha) {
		return false, errChanged
	}
	raw, err := json.Marshal(man)
	if err != nil {
		return false, err
	}
	data, err := seal(e.key, kindBlobManifest, sha, 0, raw)
	if err != nil {
		return false, err
	}
	// The manifest goes last: until it is there the blob does not exist.
	if err := e.t.Create(ctx, blobManifestName(sha), data); err != nil && !errors.Is(err, ErrExists) {
		return false, err
	}
	return true, nil
}

// putChunk writes one chunk. A chunk left by an earlier, unfinished upload is kept if it is right
// and replaced if it is not.
func (e *Engine) putChunk(ctx context.Context, sha string, i int, plain []byte, plainSum string) error {
	name := blobChunkName(sha, i)
	if old, err := e.t.Read(ctx, name); err == nil {
		if p, err := open(e.key, old, kindBlob, sha, uint64(i)); err == nil && sum(p) == plainSum {
			return nil
		}
		if err := e.t.Remove(ctx, name); err != nil {
			return err
		}
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}
	data, err := seal(e.key, kindBlob, sha, uint64(i), plain)
	if err != nil {
		return err
	}
	if err := e.t.Create(ctx, name, data); err != nil && !errors.Is(err, ErrExists) {
		return err
	}
	return nil
}

// getBlob downloads a blob into a temporary file in dir and returns its path. The file has been
// checked against the manifest chunk by chunk and against the blob's name as a whole; if anything
// is wrong the temporary file is deleted and nothing is left behind. The caller moves it into place.
func (e *Engine) getBlob(ctx context.Context, sha, dir string, advance func(int64)) (string, error) {
	raw, err := e.t.Read(ctx, blobManifestName(sha))
	if errors.Is(err, ErrNotExist) {
		return "", fmt.Errorf("%w: blob %s", ErrMissingFile, sha[:12])
	}
	if err != nil {
		return "", err
	}
	plain, err := open(e.key, raw, kindBlobManifest, sha, 0)
	if err != nil {
		return "", fmt.Errorf("%w: blob %s", err, sha[:12])
	}
	var man blobManifest
	if err := json.Unmarshal(plain, &man); err != nil || !man.valid(sha) {
		return "", fmt.Errorf("%w: blob %s", ErrCorrupt, sha[:12])
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", wrapDisk(err)
	}
	tmp, err := os.CreateTemp(dir, ".glossa-download-*")
	if err != nil {
		return "", wrapDisk(err)
	}
	fail := func(err error) (string, error) {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	h := sha256.New()
	var total int64
	for i, want := range man.Chunks {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		data, err := e.t.Read(ctx, blobChunkName(sha, i))
		if errors.Is(err, ErrNotExist) {
			return fail(fmt.Errorf("%w: blob %s, part %d", ErrMissingFile, sha[:12], i+1))
		}
		if err != nil {
			return fail(err)
		}
		p, err := open(e.key, data, kindBlob, sha, uint64(i))
		if err != nil || len(p) > man.ChunkSize || sum(p) != want {
			return fail(fmt.Errorf("%w: blob %s, part %d", ErrCorrupt, sha[:12], i+1))
		}
		if _, err := tmp.Write(p); err != nil {
			return fail(wrapDisk(err))
		}
		h.Write(p)
		total += int64(len(p))
		advance(int64(len(p)))
	}
	if total != man.Size || hex.EncodeToString(h.Sum(nil)) != sha {
		return fail(fmt.Errorf("%w: blob %s", ErrCorrupt, sha[:12]))
	}
	if err := tmp.Sync(); err != nil {
		return fail(wrapDisk(err))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", wrapDisk(err)
	}
	return tmp.Name(), nil
}

// removeBlob deletes a blob: the manifest first, so it stops existing at once, then its chunks.
func (e *Engine) removeBlob(ctx context.Context, sha string) error {
	if err := e.t.Remove(ctx, blobManifestName(sha)); err != nil {
		return err
	}
	es, err := e.t.List(ctx, blobsDir)
	if err != nil {
		return err
	}
	for _, en := range es {
		if !en.Dir && strings.HasPrefix(en.Name, sha+".") {
			if err := e.t.Remove(ctx, blobsDir+"/"+en.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// blobsInFolder lists the hashes of the whole blobs in the folder.
func (e *Engine) blobsInFolder(ctx context.Context) (map[string]int64, error) {
	es, err := e.t.List(ctx, blobsDir)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	sizes := map[string]int64{}
	for _, en := range es {
		if en.Dir {
			continue
		}
		if sha, ok := strings.CutSuffix(en.Name, ".m"); ok && shaRE.MatchString(sha) {
			out[sha] = 0
		} else if len(en.Name) > 64 && shaRE.MatchString(en.Name[:64]) {
			sizes[en.Name[:64]] += en.Size
		}
	}
	for sha := range out {
		out[sha] = sizes[sha]
	}
	return out, nil
}

// uniqueName returns a file name in dir that is not taken, adding " (2)" and so on.
func uniqueName(dir, name string) string {
	name = filepath.Base(strings.NewReplacer("\\", "_", "/", "_").Replace(name))
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		name = "book" + name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		if _, err := os.Lstat(filepath.Join(dir, n)); os.IsNotExist(err) {
			return n
		}
	}
}
