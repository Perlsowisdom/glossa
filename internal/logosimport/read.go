package logosimport

import "sort"

// Read maps everything in the snapshot to what Glossa keeps. It writes nothing.
func (s *Snapshot) Read() (*Result, error) {
	res := &Result{}
	cat := s.catalog()
	if err := s.readNotes(res, cat); err != nil {
		return nil, err
	}
	if err := s.readClippings(res, cat); err != nil {
		return nil, err
	}
	if err := s.readPassageLists(res); err != nil {
		return nil, err
	}
	if err := s.readWordLists(res); err != nil {
		return nil, err
	}
	if err := s.readPrayerLists(res); err != nil {
		return nil, err
	}
	if err := s.readPlans(res, cat); err != nil {
		return nil, err
	}
	sort.SliceStable(res.Folders, func(i, j int) bool { return res.Folders[i].Name < res.Folders[j].Name })
	return res, nil
}
