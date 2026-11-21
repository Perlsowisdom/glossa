package store

import (
	"context"
	"database/sql"
	"errors"
)

// SyncFile is this device's record of a shared file it holds (sync_files; never logged or synced).
type SyncFile struct {
	FileID      string // shared_files.id
	SHA256      string
	Size        int64
	MTime       int64
	Origin      string // "local" or "synced"
	LocalFileID string // books: this device's files row
}

// SyncFile reads this device's record for a shared file.
func (s *Store) SyncFile(ctx context.Context, fileID string) (SyncFile, bool, error) {
	var f SyncFile
	var local sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT file_id, sha256, size_bytes, mtime, origin, local_file_id FROM sync_files
		WHERE file_id = ?`, fileID).Scan(&f.FileID, &f.SHA256, &f.Size, &f.MTime, &f.Origin, &local)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncFile{}, false, nil
	}
	f.LocalFileID = local.String
	return f, err == nil, err
}

// SyncFiles lists every record.
func (s *Store) SyncFiles(ctx context.Context) ([]SyncFile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT file_id, sha256, size_bytes, mtime, origin, local_file_id FROM sync_files ORDER BY file_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncFile
	for rows.Next() {
		var f SyncFile
		var local sql.NullString
		if err := rows.Scan(&f.FileID, &f.SHA256, &f.Size, &f.MTime, &f.Origin, &local); err != nil {
			return nil, err
		}
		f.LocalFileID = local.String
		out = append(out, f)
	}
	return out, rows.Err()
}

// PutSyncFile writes or replaces a record.
func (s *Store) PutSyncFile(ctx context.Context, f SyncFile) error {
	var local any
	if f.LocalFileID != "" {
		local = f.LocalFileID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_files (file_id, sha256, size_bytes, mtime, origin, local_file_id)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(file_id) DO UPDATE SET sha256 = excluded.sha256,
		size_bytes = excluded.size_bytes, mtime = excluded.mtime, origin = excluded.origin, local_file_id = excluded.local_file_id`,
		f.FileID, f.SHA256, f.Size, f.MTime, f.Origin, local)
	return err
}

// DeleteSyncFile forgets a record.
func (s *Store) DeleteSyncFile(ctx context.Context, fileID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sync_files WHERE file_id = ?`, fileID)
	return err
}
