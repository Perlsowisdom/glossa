package logosimport

import "sort"

// Read maps everything in the snapshot to what Glossa keeps. It writes nothing.
func (s *Snapshot) Read() (*Result, error) {
	res := &Result{}
	cat := s.catalog()
	if err := s.readNotes(res, cat); err != nil {
		return nil, err
	}
	sort.SliceStable(res.Folders, func(i, j int) bool { return res.Folders[i].Name < res.Folders[j].Name })
	return res, nil
}
