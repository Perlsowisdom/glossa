package logosimport

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var tagSplit = regexp.MustCompile(`[\t,;]+`)

// readClippings reads clipping folders and the clippings in them. Each clipping becomes a note.
func (s *Snapshot) readClippings(res *Result, cat *catalog) error {
	if !s.Has("clippings") {
		return nil
	}
	db, err := s.open("clippings")
	if err != nil {
		return err
	}
	type folder struct {
		key  string
		name string
		gone bool
	}
	folders := map[int64]folder{}
	rows, err := db.Query(`SELECT RowId, DocumentId, Title, IsDeleted FROM ClippingsDocuments ORDER BY RowId`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var doc []byte
		var title string
		var gone bool
		if err := rows.Scan(&id, &doc, &title, &gone); err != nil {
			rows.Close()
			return err
		}
		name := strings.Join(strings.Fields(title), " ")
		if name == "" {
			name = "Clippings"
		}
		folders[id] = folder{key: "clippings:" + hex.EncodeToString(doc), name: name, gone: gone}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = db.Query(`SELECT ItemId, Revision, DocumentRowId, ResourceId, CreatedDate, Title, Content, Notes, COALESCE(Tags, '')
		FROM Clippings ORDER BY DocumentRowId, Rank, RowId`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item, rev, title, content, notes []byte
		var docRow int64
		var resource, created, tags string
		if err := rows.Scan(&item, &rev, &docRow, &resource, &created, &title, &content, &notes, &tags); err != nil {
			return err
		}
		key := "clipping:" + hex.EncodeToString(item)
		f, ok := folders[docRow]
		if !ok || f.gone {
			res.skip(KindClipping, key, cat.title(resource), "In a clippings folder that was deleted")
			continue
		}
		body, err1 := decodeBlob(content)
		titleXML, _ := decodeBlob(title)
		notesXML, _ := decodeBlob(notes)
		if err1 != nil {
			res.skip(KindClipping, key, cat.title(resource), "The clipping's text could not be read")
			continue
		}
		md := ToMarkdown(body)
		if md == "" {
			res.skip(KindClipping, key, cat.title(resource), "An empty clipping")
			continue
		}
		name := PlainText(titleXML)
		if name == "" {
			name = firstWords(PlainText(body), 60)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "**Logos:** *%s* — [Open in Logos](%s)\n\n%s", cat.title(resource), resourceLink(resource), md)
		if own := ToMarkdown(notesXML); own != "" {
			b.WriteString("\n\n**My note:** " + own)
		}
		data := &NoteData{Title: name, Body: b.String(), Created: millis(created), Updated: millis(created)}
		data.Tags = addTag(nil, FromLogosTag)
		for _, t := range tagSplit.Split(tags, -1) {
			data.Tags = addTag(data.Tags, t)
		}
		res.addFolder(Folder{Key: f.key, Name: f.name})
		res.Items = append(res.Items, Item{
			Kind: KindClipping, Key: key, Changed: hex.EncodeToString(rev), Title: name,
			Sample: firstWords(PlainText(body), 120), Folder: f.key, Note: data,
		})
	}
	return rows.Err()
}
