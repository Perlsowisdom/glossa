package logosimport

import "testing"

func TestHighlightMapping(t *testing.T) {
	root, docs, data := logosFolder(t)
	makeDB(t, data, fileCatalog, catalogSchema,
		`INSERT INTO Records VALUES ('LLS:ESV', NULL, 'English Standard Version', NULL, 'text.monograph.bible')`,
		`INSERT INTO Records VALUES ('LLS:COMM', NULL, 'A Commentary', NULL, 'text.monograph.commentary.bible')`,
	)
	hl := func(id, anchors, style string, extra string) string {
		return `INSERT INTO Notes (ExternalId, CreatedDate, ModifiedDate, Kind, AnchorsJson, NotebookExternalId, NoteStyleId` + extra + `)
			VALUES ('` + id + `', '2019-12-17T01:21:25Z', '2019-12-18T01:21:25Z', 1, '` + anchors + `', 'nb1', ` + style + `)`
	}
	makeDB(t, docs, fileNotes, notesSchema,
		`INSERT INTO NoteStyles VALUES (1, 'custom:YellowHighlighter'), (2, 'custom:RedUnderline'), (3, 'custom:Box'), (4, 'custom:Inductive\/Precept (2)/blood')`,
		`INSERT INTO Notebooks (ExternalId, Title) VALUES ('nb1', 'N')`,
		// whole verse by reference
		hl("h1", `[{"reference":{"raw":"bible+esv.64.3.16"}}]`, "1", ""),
		// part of verses in a Bible, by text range: Logos gives the verses
		hl("h2", `[{"textRange":{"resourceId":"LLS:ESV","version":"1","offset":5,"length":40}}]`, "2", ""),
		// in a commentary
		hl("h3", `[{"textRange":{"resourceId":"LLS:COMM","version":"1","offset":5,"length":40}}]`, "1", ""),
		// a Bible text range with no verses worked out
		hl("h4", `[{"textRange":{"resourceId":"LLS:ESV","version":"1","offset":5,"length":40}}]`, "3", ""),
		// a custom style Glossa has no colour for
		hl("h5", `[{"reference":{"raw":"bible.66.8.1"}}]`, "4", ""),
		// in the trash
		`INSERT INTO Notes (ExternalId, CreatedDate, Kind, AnchorsJson, NotebookExternalId, IsTrashed) VALUES ('h6', '2019-12-17T01:21:25Z', 1, '[{"reference":{"raw":"bible.66.8.2"}}]', 'nb1', 1)`,
	)
	// Logos worked out the verses for h2: two verses of John 3.
	makeFacets(t, docs, 2, "bible+esv.64.3.16", "bible+esv.64.3.17")

	src, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	res := readFixture(t, src)
	by := map[string]Item{}
	for _, it := range res.Items {
		by[it.Key] = it
	}
	h1 := by["highlight:h1"].Highlight
	if h1 == nil || h1.Style != "yellow" || h1.Range.Start != 43003016 || h1.Range.End != 43003016 || h1.OriginalStyle != "YellowHighlighter" {
		t.Errorf("h1 = %+v", h1)
	}
	if h1.Created == 0 || by["highlight:h1"].Changed != "2019-12-18T01:21:25Z" {
		t.Errorf("h1 dates: %d %q", h1.Created, by["highlight:h1"].Changed)
	}
	h2 := by["highlight:h2"].Highlight
	if h2 == nil || h2.Style != "pink-underline" || h2.Range.Start != 43003016 || h2.Range.End != 43003017 {
		t.Errorf("h2 = %+v", h2)
	}
	h5 := by["highlight:h5"].Highlight
	if h5 == nil || h5.Style != "yellow" || h5.OriginalStyle != `Inductive\/Precept (2)/blood` {
		t.Errorf("h5 = %+v", h5)
	}
	reasons := map[string]Unmapped{}
	for _, u := range res.Unmapped {
		reasons[u.Key] = u
	}
	if u := reasons["highlight:h3"]; u.Title != "A Commentary" || u.Reason != "A highlight in a book, not in a Bible" {
		t.Errorf("h3 = %+v", u)
	}
	if u := reasons["highlight:h4"]; u.Reason == "" {
		t.Errorf("h4 should be unmapped: %+v", u)
	}
	if u := reasons["highlight:h6"]; u.Reason != "In the Logos trash" {
		t.Errorf("h6 = %+v", u)
	}
	if res.Notes["highlights that covered part of a verse now cover the whole verse"] != 1 {
		t.Errorf("notes = %v", res.Notes)
	}
}
