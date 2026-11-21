package library

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// AttachFile records a book file that sync put on this device, in the library folder dir, as a file
// of an item that already exists (the item came from another device). It adds dir to the library
// as a folder if it is not one yet. It returns the new file's ID. A deleted file record at the same
// place is reused.
func AttachFile(ctx context.Context, st *store.Store, itemID, dir, rel, format, sha string, size int64) (string, error) {
	folderID, err := addOrFind(ctx, &command.Env{Store: st}, dir)
	if err != nil {
		return "", err
	}
	id := store.NewID()
	var old string
	err = st.QueryRow(ctx, `SELECT id FROM files WHERE folder_id = ? AND rel_path = ?`, folderID, rel).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	err = st.Write(ctx, "sync.book", func(tx *store.Tx) error {
		if old == "" {
			return tx.Create("files", store.Row{"id": id, "item_id": itemID, "folder_id": folderID,
				"rel_path": rel, "format": format, "size_bytes": size, "sha256": sha})
		}
		id = old
		return tx.Update("files", store.ByID(old), store.Row{"item_id": itemID, "format": format,
			"size_bytes": size, "sha256": sha, "indexed_at": nil, "deleted_at": nil})
	})
	return id, err
}

// DefaultBooksDir is the folder added books are copied into when none is named.
func DefaultBooksDir() string { return booksDir() }
