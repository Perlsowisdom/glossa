package logosimport

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// netGUID writes a 16-byte .NET GUID (its first three groups little-endian) as 32 hex digits.
func netGUID(b []byte) string {
	if len(b) != 16 {
		return hex.EncodeToString(b)
	}
	o := []byte{b[3], b[2], b[1], b[0], b[5], b[4], b[7], b[6]}
	return hex.EncodeToString(append(o, b[8:]...))
}

// textGUID takes the GUID off the end of "Document:WordList:<guid>" and drops its dashes.
func textGUID(s string) string {
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}

func listName(title, fallback string) string {
	if name := strings.Join(strings.Fields(title), " "); name != "" {
		return name
	}
	return fallback
}

// readPassageLists makes saved lists of verses from passage lists.
func (s *Snapshot) readPassageLists(res *Result) error {
	if !s.Has("passages") {
		return nil
	}
	db, err := s.open("passages")
	if err != nil {
		return err
	}
	rows, err := db.Query(`SELECT Id, DocumentGuid, COALESCE(Title, ''), CompressedItems, COALESCE(ModifiedDate, ''), IsDeleted
		FROM PassageLists ORDER BY Id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var guid, items []byte
		var title, modified string
		var gone bool
		if err := rows.Scan(&id, &guid, &title, &items, &modified, &gone); err != nil {
			return err
		}
		if gone {
			continue
		}
		key := fmt.Sprintf("passage_list:%s", netGUID(guid))
		if len(guid) == 0 {
			key = fmt.Sprintf("passage_list:id%d", id)
		}
		name := listName(title, "Passage list")
		text, err := decodeBlob(items)
		var refs []struct {
			Ref string `json:"ref"`
		}
		if err == nil {
			err = json.Unmarshal([]byte(text), &refs)
		}
		if err != nil {
			res.skip(KindPassageList, key, name, "The list's passages could not be read")
			continue
		}
		data := &ListData{Kind: "verses", Name: name, Updated: millis(modified), Created: millis(modified)}
		for _, r := range refs {
			vr, err := ParseRef(r.Ref)
			if err != nil || !isBibleRef(r.Ref) {
				res.note("passages in lists that are not Bible passages were left out")
				continue
			}
			vr2 := vr
			data.Entries = append(data.Entries, ListEntry{Verses: &vr2})
		}
		res.Items = append(res.Items, Item{
			Kind: KindPassageList, Key: key, Changed: modified, Title: name, List: data,
			Sample: fmt.Sprintf("%d passages", len(data.Entries)),
		})
	}
	return rows.Err()
}

// lemmaLanguage maps the language part of a Logos lemma reference to a language code.
func lemmaLanguage(code string) string {
	switch code {
	case "el", "gr", "grc":
		return "grc"
	case "he", "hbo":
		return "hbo"
	case "arc", "aram":
		return "arc"
	}
	return code
}

// readWordLists makes saved lists of words from word lists.
func (s *Snapshot) readWordLists(res *Result) error {
	if !s.Has("words") {
		return nil
	}
	db, err := s.open("words")
	if err != nil {
		return err
	}
	type entry struct {
		word, gloss, lemma, lang string
	}
	byDoc := map[string][]entry{}
	rows, err := db.Query(`SELECT DocumentId, DisplayText, Gloss, COALESCE(LemmaText, ''), COALESCE(LemmaReference, '')
		FROM WordListEntries ORDER BY Id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var doc []byte
		var word, gloss, lemma, ref string
		if err := rows.Scan(&doc, &word, &gloss, &lemma, &ref); err != nil {
			rows.Close()
			return err
		}
		e := entry{word: strings.TrimSpace(word), gloss: strings.TrimSpace(gloss), lemma: strings.TrimSpace(lemma)}
		if parts := strings.SplitN(ref, ".", 4); len(parts) == 4 && parts[0] == "lemma" {
			e.lang = lemmaLanguage(parts[2])
			if e.lemma == "" {
				e.lemma = parts[3]
			}
		}
		byDoc[netGUID(doc)] = append(byDoc[netGUID(doc)], e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = db.Query(`SELECT DocumentId, Title, COALESCE(ModifiedDate, ''), COALESCE(CreatedDate, ''), IsDeleted
		FROM WordListDocuments ORDER BY Id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var doc, title, modified, created string
		var gone bool
		if err := rows.Scan(&doc, &title, &modified, &created, &gone); err != nil {
			return err
		}
		if gone {
			continue
		}
		guid := textGUID(doc)
		name := listName(title, "Word list")
		data := &ListData{Kind: "words", Name: name, Created: millis(created), Updated: millis(modified)}
		for _, e := range byDoc[guid] {
			word := e.word
			if word == "" {
				word = e.lemma
			}
			if word == "" {
				continue
			}
			data.Entries = append(data.Entries, ListEntry{Word: word, Lemma: e.lemma, Language: e.lang, Note: e.gloss})
		}
		res.Items = append(res.Items, Item{
			Kind: KindWordList, Key: "word_list:" + guid, Changed: modified, Title: name, List: data,
			Sample: fmt.Sprintf("%d words", len(data.Entries)),
		})
	}
	return rows.Err()
}

type prayerJSON struct {
	Title    string   `json:"title"`
	Notes    string   `json:"notes"`
	Answer   string   `json:"answer"`
	Tags     []string `json:"tags"`
	Schedule struct {
		Enabled    bool   `json:"isEnabled"`
		Recurrence string `json:"recurrence"`
		WeeklyDays string `json:"weeklyDays"`
	} `json:"schedule"`
}

// readPrayerLists makes a note, in the "Prayer lists" folder, of each prayer list.
func (s *Snapshot) readPrayerLists(res *Result) error {
	if !s.Has("prayers") {
		return nil
	}
	db, err := s.open("prayers")
	if err != nil {
		return err
	}
	last := map[string]string{} // prayer GUID -> when last prayed
	if rows, err := db.Query(`SELECT PrayerId, COALESCE(LastPrayed, '') FROM PrayerListStatus WHERE IsDeleted = 0`); err == nil {
		for rows.Next() {
			var id []byte
			var when string
			if rows.Scan(&id, &when) == nil && when != "" {
				last[netGUID(id)] = when
			}
		}
		rows.Close()
	}
	type prayer struct {
		id string
		p  prayerJSON
	}
	byDoc := map[string][]prayer{}
	rows, err := db.Query(`SELECT DocumentSyncId, PrayerId, Prayer FROM Prayers ORDER BY Id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var doc, id []byte
		var raw string
		if err := rows.Scan(&doc, &id, &raw); err != nil {
			rows.Close()
			return err
		}
		var p prayerJSON
		if json.Unmarshal([]byte(raw), &p) == nil {
			byDoc[netGUID(doc)] = append(byDoc[netGUID(doc)], prayer{netGUID(id), p})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = db.Query(`SELECT DocumentSyncId, COALESCE(Title, ''), COALESCE(ModifiedDate, ''), IsDeleted FROM PrayerListDocuments ORDER BY Id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var doc []byte
		var title, modified string
		var gone bool
		if err := rows.Scan(&doc, &title, &modified, &gone); err != nil {
			return err
		}
		if gone {
			continue
		}
		guid := netGUID(doc)
		name := listName(title, "Prayer list")
		var b strings.Builder
		var tags []string
		for _, p := range byDoc[guid] {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString("**" + listName(p.p.Title, "Prayer") + "**")
			if n := ToMarkdown(p.p.Notes); n != "" {
				b.WriteString("\n" + n)
			}
			if a := ToMarkdown(p.p.Answer); a != "" {
				b.WriteString("\n*Answer:* " + a)
			}
			if w := last[p.id]; w != "" {
				b.WriteString("\n*Last prayed:* " + w)
			}
			if p.p.Schedule.Enabled && p.p.Schedule.Recurrence != "" {
				b.WriteString("\n*Repeats:* " + strings.ToLower(p.p.Schedule.Recurrence))
			}
			for _, t := range p.p.Tags {
				tags = addTag(tags, t)
			}
		}
		if b.Len() == 0 {
			res.skip(KindPrayerList, "prayer_list:"+guid, name, "An empty prayer list")
			continue
		}
		res.addFolder(Folder{Key: "prayer-lists", Name: PrayerFolder})
		res.Items = append(res.Items, Item{
			Kind: KindPrayerList, Key: "prayer_list:" + guid, Changed: modified, Title: name,
			Sample: fmt.Sprintf("%d prayers", len(byDoc[guid])), Folder: "prayer-lists",
			Note: &NoteData{Title: name, Body: b.String(), Tags: addTag(tags, FromLogosTag), Created: millis(modified), Updated: millis(modified)},
		})
	}
	return rows.Err()
}

type sessionJSON struct {
	Date     string `json:"date"`
	Readings [][]struct {
		ResourceID string `json:"resourceId"`
		Reference  string `json:"reference"`
	} `json:"readings"`
}

// ReadingFolder is the folder reading plans that do not map go in, as notes.
const ReadingFolder = "Reading plans"

// readPlans makes a Glossa plan of each reading plan that is all Bible passages, and a note of
// each other one. What was read comes with it where Logos keeps it in a form we know.
func (s *Snapshot) readPlans(res *Result, cat *catalog) error {
	if !s.Has("plans") {
		return nil
	}
	db, err := s.open("plans")
	if err != nil {
		return err
	}
	progress := readPlanProgress(db)
	rows, err := db.Query(`SELECT DocumentId, COALESCE(Title, ''), COALESCE(Sessions, '[]'), COALESCE(ModifiedDate, ''), ScheduleId, IsDeleted
		FROM ReadingPlanDocuments ORDER BY Id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var doc, title, sessions, modified string
		var schedule []byte
		var gone bool
		if err := rows.Scan(&doc, &title, &sessions, &modified, &schedule, &gone); err != nil {
			return err
		}
		if gone {
			continue
		}
		key := "reading_plan:" + textGUID(doc)
		name := listName(title, "Reading plan")
		var days []sessionJSON
		if err := json.Unmarshal([]byte(sessions), &days); err != nil || len(days) == 0 {
			res.skip(KindReadingPlan, key, name, "The plan has no days Glossa could read")
			continue
		}
		done := progress[netGUID(schedule)]
		if plan, ok := biblePlan(name, days, done); ok {
			res.Items = append(res.Items, Item{
				Kind: KindReadingPlan, Key: key, Changed: modified, Title: name, Plan: plan,
				Sample: fmt.Sprintf("%d days, %d read", len(plan.Days), len(plan.Done)),
			})
			continue
		}
		body := planNote(days, done, cat)
		res.addFolder(Folder{Key: "reading-plans", Name: ReadingFolder})
		res.Items = append(res.Items, Item{
			Kind: KindReadingPlan, Key: key, Changed: modified, Title: name, Folder: "reading-plans",
			Sample: fmt.Sprintf("%d days, as a note: it reads in books, not Bible passages", len(days)),
			Note:   &NoteData{Title: name, Body: body, Tags: addTag(nil, FromLogosTag), Created: millis(modified), Updated: millis(modified)},
		})
		res.note("reading plans that read in books became notes")
	}
	return rows.Err()
}

// readPlanProgress reads which days were read, by plan. Logos keeps this as a list of dates or of
// day numbers; anything else is taken as nothing read.
func readPlanProgress(db *sql.DB) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	rows, err := db.Query(`SELECT SyncId, COALESCE(ReadSessions, '') FROM ReadingPlanStatus WHERE IsDeleted = 0`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if rows.Scan(&id, &raw) != nil {
			continue
		}
		var items []any
		if json.Unmarshal([]byte(raw), &items) != nil {
			continue
		}
		set := map[string]bool{}
		for _, it := range items {
			switch v := it.(type) {
			case string:
				set[v[:min(len(v), 10)]] = true
			case float64:
				set[fmt.Sprintf("#%d", int(v))] = true
			}
		}
		out[strings.ToLower(id)] = set
	}
	return out
}

// biblePlan makes a plan when every reading is a Bible passage.
func biblePlan(name string, days []sessionJSON, done map[string]bool) (*PlanData, bool) {
	p := &PlanData{Name: name, Start: days[0].Date[:min(len(days[0].Date), 10)]}
	for i, d := range days {
		var refs []string
		for _, group := range d.Readings {
			for _, r := range group {
				vr, err := ParseRef(r.Reference)
				if err != nil || !isBibleRef(r.Reference) {
					return nil, false
				}
				refs = append(refs, vr.String())
			}
		}
		if len(refs) == 0 {
			return nil, false
		}
		p.Days = append(p.Days, refs)
		p.Dates = append(p.Dates, d.Date[:min(len(d.Date), 10)])
		if done[d.Date[:min(len(d.Date), 10)]] || done[fmt.Sprintf("#%d", i)] {
			p.Done = append(p.Done, i+1)
		}
	}
	return p, true
}

// planNote lists a plan that reads in books: each day with what to read.
func planNote(days []sessionJSON, done map[string]bool, cat *catalog) string {
	var b strings.Builder
	b.WriteString("A reading plan from Logos. It reads in books, so it is kept as a list of days.\n")
	for i, d := range days {
		var parts []string
		for _, group := range d.Readings {
			for _, r := range group {
				parts = append(parts, readingText(r.ResourceID, r.Reference, cat))
			}
		}
		mark := ""
		if done[d.Date[:min(len(d.Date), 10)]] || done[fmt.Sprintf("#%d", i)] {
			mark = " (read)"
		}
		fmt.Fprintf(&b, "\n- %s: %s%s", d.Date[:min(len(d.Date), 10)], strings.Join(parts, "; "), mark)
	}
	return b.String()
}

func readingText(resource, ref string, cat *catalog) string {
	if isBibleRef(ref) {
		if vr, err := ParseRef(ref); err == nil {
			return vr.String()
		}
	}
	where := strings.TrimPrefix(ref, "page.")
	if strings.HasPrefix(ref, "page.") {
		where = "pages " + strings.ReplaceAll(where, "-", "–")
	}
	return fmt.Sprintf("%s, %s", cat.title(resource), where)
}
