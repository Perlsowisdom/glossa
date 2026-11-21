package sync

import "errors"

// Errors callers can act on. Messages are in plain words because the interface shows them as they are.
var (
	ErrFolderMissing  = errors.New("the sync folder can't be found; check that it is still there and that your sync service is running")
	ErrWrongPassword  = errors.New("that password doesn't match this sync folder")
	ErrNotInitialised = errors.New("this folder has no Glossa sync data yet")
	ErrAlreadyExists  = errors.New("this folder already holds Glossa sync data")
	ErrCorrupt        = errors.New("a sync file is damaged, incomplete or has been changed")
	ErrMissingFile    = errors.New("a sync file is missing; it may still be downloading from your sync service")
	ErrNewerVersion   = errors.New("the sync folder was written by a newer version of Glossa; update Glossa to sync")
	ErrClockSkew      = errors.New("another device's clock is more than a day ahead; set its clock right, then sync again")
	ErrDiskFull       = errors.New("the disk is full")
	ErrForgotten      = errors.New("this device was removed from the sync folder; set up sync again to rejoin")
	ErrWeakPassword   = errors.New("the password is too short")
	ErrLocked         = errors.New("sync is locked; enter the password")
	ErrNotExist       = errors.New("not found in the sync folder")
	ErrExists         = errors.New("already exists in the sync folder")
)
