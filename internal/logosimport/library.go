package logosimport

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/store"
)

// The list of books in the user's Logos library. It reads the catalogue's metadata only: title,
// author, type, resource ID and the rights fields. It never opens a book.

// bookLink opens a book in Logos. Logos registers the logosres: protocol on Windows when it is
// installed, so the link needs no web address and Glossa contacts nobody. The ID is written as
// Logos wrote it; only characters that cannot stand in a link are escaped.
func bookLink(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(":._-", r)) {
			b.WriteRune(r)
			continue
		}
		for _, c := range []byte(string(r)) {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return "logosres:" + b.String()
}

// LibraryBook is one book as the catalogue describes it.
type LibraryBook struct {
	ResourceID   string     `json:"resource_id" desc:"The book's ID in Logos"`
	Title        string     `json:"title"`
	Abbreviation string     `json:"abbreviation,omitempty"`
	Authors      string     `json:"authors,omitempty" desc:"Authors, separated by semicolons"`
	Type         string     `json:"type" desc:"Logos's resource type"`
	TypeLabel    string     `json:"type_label" desc:"The type in plain words"`
	Published    string     `json:"published,omitempty"`
	Edition      string     `json:"edition,omitempty"`
	Languages    string     `json:"languages,omitempty"`
	Rights       string     `json:"rights,omitempty" desc:"The catalogue's copyright field, word for word"`
	PublicDomain bool       `json:"public_domain" desc:"True when the rights field, or the date with no rights claim, clearly says so"`
	PDBasis      string     `json:"pd_basis,omitempty" enum:"rights,date" desc:"What the public-domain call rests on: the catalogue's rights field, or only a publication before 1900"`
	Link         string     `json:"link" desc:"Opens the book in Logos"`
	Free         *FreeMatch `json:"free,omitempty" desc:"A free copy Glossa can get, for a public-domain book"`
}

// FreeMatch is a free copy Glossa knows of for a public-domain book.
type FreeMatch struct {
	Source     string `json:"source" desc:"Pass to bible.get with item"`
	Item       string `json:"item"`
	Name       string `json:"name"`
	Kind       string `json:"kind" desc:"bible, commentary, dictionary or book"`
	Confidence string `json:"confidence" enum:"certain,likely" desc:"Certain when the titles are the same; likely when they are close or only the short name agrees"`
	Installed  bool   `json:"installed" desc:"Already in Glossa"`
}

var typeLabels = []struct{ prefix, label string }{
	{"text.monograph.bible", "Bible"},
	{"lbx.reverse-interlinear", "Bible (interlinear)"},
	{"text.monograph.commentary", "Commentary"},
	{"text.monograph.lexicon", "Lexicon"},
	{"text.monograph.encyclopedia", "Dictionary or encyclopedia"},
	{"text.monograph.dictionary", "Dictionary or encyclopedia"},
	{"text.monograph.sermon", "Sermons"},
	{"text.monograph.grammar", "Grammar"},
	{"text.monograph.systematic-theology", "Systematic theology"},
	{"text.monograph.church-history", "Church history"},
	{"text.monograph.biography", "Biography"},
	{"text.monograph.ancient-manuscript", "Ancient text"},
	{"text.monograph.study-guide", "Study guide"},
	{"text.monograph.courseware", "Course"},
	{"text.monograph.concordance", "Concordance"},
	{"text.monograph", "Book"},
	{"text.serial", "Journal or magazine"},
	{"text.manual", "Manual"},
	{"lbx.media", "Media"},
	{"lbx.interactive", "Interactive"},
	{"lbx.supplementaldata", "Data"},
	{"lbx", "Logos resource"},
}

func typeLabel(t string) string {
	t = strings.ToLower(t)
	for _, tl := range typeLabels {
		if strings.HasPrefix(t, tl.prefix) {
			return tl.label
		}
	}
	if t == "" {
		return "Other"
	}
	return "Other"
}

var yearRe = regexp.MustCompile(`\b(1[0-9]{3}|20[0-9]{2})\b`)

// classifyRights says whether the catalogue clearly marks a book public domain. The rights field
// decides: "Public Domain" alone is public domain; a note that also claims a copyright (a modern
// edition, a translation, a morphology layer) is not. With no rights claim at all, a publication
// date before 1900 counts too, marked as only the date.
func classifyRights(rights, published string) (pd bool, basis string) {
	r := strings.ToLower(strings.TrimSpace(rights))
	r = strings.Trim(r, " .;,")
	claims := strings.Contains(r, "copyright") || strings.Contains(r, "©") || strings.Contains(r, "(c)") ||
		strings.Contains(r, "all rights") || strings.Contains(r, "licen") || strings.Contains(r, "permission")
	switch {
	case (r == "public domain" || r == "public-domain" || r == "in the public domain") && !claims:
		return true, "rights"
	case r == "" && published != "":
		year := 0
		for _, m := range yearRe.FindAllString(published, -1) {
			var y int
			fmt.Sscanf(m, "%d", &y)
			if y > year {
				year = y
			}
		}
		if year > 0 && year < 1900 {
			return true, "date"
		}
	}
	return false, ""
}

// readBooks reads every book's metadata from the copied catalogue. It copes with a catalogue that
// lacks some columns.
func readBooks(db *sql.DB) ([]LibraryBook, error) {
	have := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM pragma_table_info('Records')`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		have[n] = true
	}
	rows.Close()
	if !have["ResourceId"] || !have["Title"] {
		return nil, fmt.Errorf("the Logos library catalogue has no book list")
	}
	col := func(names ...string) string {
		var parts []string
		for _, n := range names {
			if have[n] {
				parts = append(parts, "NULLIF("+n+", '')")
			}
		}
		if len(parts) == 0 {
			return "''"
		}
		return "COALESCE(" + strings.Join(parts, ", ") + ", '')"
	}
	q := fmt.Sprintf(`SELECT ResourceId, %s, %s, %s, %s, %s, %s, %s, %s FROM Records ORDER BY ResourceId`,
		col("UserTitle", "Title"), col("UserAbbreviatedTitle", "AbbreviatedTitle"), col("Authors"), col("Type"),
		col("PublicationDate"), col("Edition"), col("Languages"), col("Copyright"))
	rs, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	seen := map[string]bool{}
	out := []LibraryBook{}
	for rs.Next() {
		var b LibraryBook
		if err := rs.Scan(&b.ResourceID, &b.Title, &b.Abbreviation, &b.Authors, &b.Type, &b.Published, &b.Edition, &b.Languages, &b.Rights); err != nil {
			return nil, err
		}
		if b.ResourceID == "" || seen[b.ResourceID] {
			continue
		}
		seen[b.ResourceID] = true
		b.Authors = strings.Join(strings.FieldsFunc(b.Authors, func(r rune) bool { return r == '\t' }), "; ")
		b.Languages = strings.Join(strings.FieldsFunc(b.Languages, func(r rune) bool { return r == '\t' }), ",")
		b.PublicDomain, b.PDBasis = classifyRights(b.Rights, b.Published)
		out = append(out, b)
	}
	return out, rs.Err()
}

// RefreshParams are logos.library_refresh's parameters.
type RefreshParams struct {
	Folder string `json:"folder,omitempty" desc:"A folder holding Logos's Data and Documents folders; leave out to look where Logos keeps them"`
}

// RefreshResult says what a refresh changed.
type RefreshResult struct {
	Books        int    `json:"books" desc:"Books in the Logos library"`
	Added        int    `json:"added"`
	Changed      int    `json:"changed"`
	Removed      int    `json:"removed" desc:"No longer in the Logos library"`
	PublicDomain int    `json:"public_domain"`
	Source       string `json:"source"`
	RefreshedAt  int64  `json:"refreshed_at" desc:"Unix time in milliseconds"`
}

func (s *service) refreshLibrary(ctx context.Context, env *command.Env, p RefreshParams) (RefreshResult, error) {
	src, err := Find(p.Folder)
	if err != nil {
		return RefreshResult{}, command.Errorf("not_found", "no Logos data was found on this computer; choose the folder that holds Logos's Data and Documents folders")
	}
	snap, err := src.Snapshot(s.tmpDir)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("copy the Logos data: %w", err)
	}
	defer snap.Close()
	if !snap.Has("catalog") {
		return RefreshResult{}, command.Errorf("not_found", "Logos's library catalogue was not found")
	}
	db, err := snap.open("catalog")
	if err != nil {
		return RefreshResult{}, fmt.Errorf("read the Logos library: %w", err)
	}
	books, err := readBooks(db)
	if err != nil {
		return RefreshResult{}, command.Errorf("failed", "read the Logos library: %v", err)
	}
	old, err := loadStoredBooks(ctx, env)
	if err != nil {
		return RefreshResult{}, err
	}
	res := RefreshResult{Books: len(books), Source: src.Root, RefreshedAt: nowMS()}
	now := map[string]bool{}
	err = env.Store.Write(ctx, "logos.library_refresh", func(tx *store.Tx) error {
		for _, b := range books {
			now[b.ResourceID] = true
			if b.PublicDomain {
				res.PublicDomain++
			}
			row := bookRow(b)
			prev, ok := old[b.ResourceID]
			switch {
			case !ok:
				res.Added++
				if err := tx.Create("logos_books", row); err != nil {
					return err
				}
			case prev != b:
				res.Changed++
				delete(row, "resource_id")
				if err := tx.Update("logos_books", store.Row{"resource_id": b.ResourceID}, row); err != nil {
					return err
				}
			}
		}
		for id := range old {
			if !now[id] {
				res.Removed++
				if err := tx.Delete("logos_books", store.Row{"resource_id": id}); err != nil {
					return err
				}
			}
		}
		state := store.Row{"refreshed_at": res.RefreshedAt, "source": src.Root, "books": len(books)}
		if _, err := tx.Get("logos_library_state", store.Row{"id": "library"}); err != nil {
			state["id"] = "library"
			return tx.Create("logos_library_state", state)
		}
		return tx.Update("logos_library_state", store.Row{"id": "library"}, state)
	})
	return res, err
}

func bookRow(b LibraryBook) store.Row {
	pd := 0
	if b.PublicDomain {
		pd = 1
	}
	return store.Row{"resource_id": b.ResourceID, "title": b.Title, "abbreviation": b.Abbreviation, "authors": b.Authors,
		"type": b.Type, "published": b.Published, "edition": b.Edition, "languages": b.Languages, "rights": b.Rights,
		"public_domain": pd, "pd_basis": b.PDBasis}
}

// loadStoredBooks reads the list kept in the library.
func loadStoredBooks(ctx context.Context, env *command.Env) (map[string]LibraryBook, error) {
	list, err := storedBooks(ctx, env)
	if err != nil {
		return nil, err
	}
	out := make(map[string]LibraryBook, len(list))
	for _, b := range list {
		out[b.ResourceID] = b
	}
	return out, nil
}

func storedBooks(ctx context.Context, env *command.Env) ([]LibraryBook, error) {
	rows, err := env.Store.Query(ctx, `SELECT resource_id, title, abbreviation, authors, type, published, edition, languages,
		rights, public_domain, pd_basis FROM logos_books`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryBook
	for rows.Next() {
		var b LibraryBook
		var pd int
		if err := rows.Scan(&b.ResourceID, &b.Title, &b.Abbreviation, &b.Authors, &b.Type, &b.Published, &b.Edition,
			&b.Languages, &b.Rights, &pd, &b.PDBasis); err != nil {
			return nil, err
		}
		b.PublicDomain = pd == 1
		out = append(out, b)
	}
	return out, rows.Err()
}

// LibraryParams are logos.library's parameters.
type LibraryParams struct {
	Text         string `json:"text,omitempty" desc:"Words to find in the title, author, short name or ID"`
	Type         string `json:"type,omitempty" desc:"Only this kind of book, as a type_label such as Bible or Commentary"`
	Sort         string `json:"sort,omitempty" enum:"title,author,type,published,id" desc:"Order by (default title)"`
	Desc         bool   `json:"desc,omitempty" desc:"Reverse the order"`
	PublicDomain bool   `json:"public_domain,omitempty" desc:"Only public-domain books"`
	FreeCopy     bool   `json:"free_copy,omitempty" desc:"Only books with a free copy Glossa can get"`
	Limit        int    `json:"limit,omitempty" desc:"Most books to return (default 100)"`
	Offset       int    `json:"offset,omitempty"`
}

// TypeCount is the number of books of one kind.
type TypeCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// LibraryResult is a page of the list.
type LibraryResult struct {
	Books        []LibraryBook `json:"books"`
	Total        int           `json:"total" desc:"Books that match, before limit and offset"`
	Refreshed    bool          `json:"refreshed" desc:"False until the list has been read from Logos once"`
	RefreshedAt  int64         `json:"refreshed_at,omitempty"`
	Source       string        `json:"source,omitempty"`
	All          int           `json:"all" desc:"Books in the list"`
	PublicDomain int           `json:"public_domain" desc:"Public-domain books in the list"`
	Certain      int           `json:"certain" desc:"Public-domain books with a certain free copy"`
	Likely       int           `json:"likely" desc:"Public-domain books with a likely free copy"`
	Types        []TypeCount   `json:"types" desc:"Books of each kind, for a filter"`
}

// freeItems asks the Bible catalogues for the free texts Glossa could get. Without catalogues, or
// when they are off, there are none.
func (s *service) freeItems(ctx context.Context, env *command.Env) []FreeItem {
	var out []FreeItem
	for _, kind := range freeKinds {
		var res struct {
			Items []struct {
				Source       string `json:"source"`
				ID           string `json:"id"`
				Name         string `json:"name"`
				Abbreviation string `json:"abbreviation"`
				About        string `json:"about"`
				Language     string `json:"language"`
				Kind         string `json:"kind"`
				Installed    bool   `json:"installed"`
				Locked       bool   `json:"locked"`
				Supported    bool   `json:"supported"`
				Licence      struct {
					Type string `json:"type"`
				} `json:"licence"`
			} `json:"items"`
		}
		if err := s.call(ctx, env, "bible.available", map[string]any{"kind": kind, "supported": true, "limit": 100000}, &res); err != nil {
			return s.ccelItems(ctx, env)
		}
		for _, it := range res.Items {
			if it.Locked || !it.Supported || (it.Licence.Type != "public_domain" && it.Licence.Type != "creative_commons") {
				continue
			}
			out = append(out, FreeItem{Source: it.Source, ID: it.ID, Name: it.Name, Abbreviation: it.Abbreviation,
				About: it.About, Language: it.Language, Kind: it.Kind, Installed: it.Installed})
		}
	}
	return append(out, s.ccelItems(ctx, env)...)
}

// ccelItems are CCEL's public-domain books from its cached list. The list is read from the cache
// only, and is empty until the user has turned CCEL on.
func (s *service) ccelItems(ctx context.Context, env *command.Env) []FreeItem {
	var res struct {
		Items []struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Author    string `json:"author"`
			Kind      string `json:"kind"`
			Installed bool   `json:"installed"`
		} `json:"items"`
	}
	if err := s.call(ctx, env, "ccel.items", map[string]any{"limit": 100000}, &res); err != nil {
		return nil
	}
	out := make([]FreeItem, 0, len(res.Items))
	for _, it := range res.Items {
		out = append(out, FreeItem{Source: "ccel", ID: it.ID, Name: it.Title, About: it.Author, Kind: it.Kind, Installed: it.Installed})
	}
	return out
}

func (s *service) library(ctx context.Context, env *command.Env, p LibraryParams) (LibraryResult, error) {
	books, err := storedBooks(ctx, env)
	if err != nil {
		return LibraryResult{}, err
	}
	out := LibraryResult{Books: []LibraryBook{}, Types: []TypeCount{}, All: len(books)}
	err = env.Store.QueryRow(ctx, `SELECT refreshed_at, source FROM logos_library_state WHERE id = 'library'`).Scan(&out.RefreshedAt, &out.Source)
	if err == nil {
		out.Refreshed = true
	} else if err != sql.ErrNoRows {
		return LibraryResult{}, err
	}

	// Free copies for the public-domain books.
	var pdIdx []int
	var pdBooks []Book
	for i := range books {
		books[i].TypeLabel = typeLabel(books[i].Type)
		books[i].Link = bookLink(books[i].ResourceID)
		if books[i].PublicDomain {
			out.PublicDomain++
			pdIdx = append(pdIdx, i)
			pdBooks = append(pdBooks, Book{Title: books[i].Title, Abbreviation: books[i].Abbreviation, Authors: books[i].Authors,
				Type: books[i].Type, Languages: books[i].Languages})
		}
	}
	if len(pdBooks) > 0 {
		installed := map[string]bool{}
		items := s.freeItems(ctx, env)
		for _, it := range items {
			if it.Installed {
				installed[it.Source+"\x00"+it.ID] = true
			}
		}
		for k, m := range MatchAll(pdBooks, items) {
			b := &books[pdIdx[k]]
			b.Free = &FreeMatch{Source: m.Item.Source, Item: m.Item.ID, Name: m.Item.Name, Kind: m.Item.Kind,
				Confidence: m.Confidence, Installed: installed[m.Item.Source+"\x00"+m.Item.ID]}
			if m.Confidence == Certain {
				out.Certain++
			} else {
				out.Likely++
			}
		}
	}

	counts := map[string]int{}
	for _, b := range books {
		counts[b.TypeLabel]++
	}
	for label, n := range counts {
		out.Types = append(out.Types, TypeCount{Label: label, Count: n})
	}
	sort.Slice(out.Types, func(i, j int) bool {
		if out.Types[i].Count != out.Types[j].Count {
			return out.Types[i].Count > out.Types[j].Count
		}
		return out.Types[i].Label < out.Types[j].Label
	})

	// Filter, sort, page.
	words := strings.Fields(strings.ToLower(p.Text))
	var list []LibraryBook
	for _, b := range books {
		if p.Type != "" && b.TypeLabel != p.Type {
			continue
		}
		if p.PublicDomain && !b.PublicDomain {
			continue
		}
		if p.FreeCopy && b.Free == nil {
			continue
		}
		if len(words) > 0 {
			hay := strings.ToLower(b.Title + " " + b.Authors + " " + b.Abbreviation + " " + b.ResourceID)
			ok := true
			for _, w := range words {
				ok = ok && strings.Contains(hay, w)
			}
			if !ok {
				continue
			}
		}
		list = append(list, b)
	}
	key := func(b LibraryBook) string {
		switch p.Sort {
		case "author":
			return strings.ToLower(b.Authors)
		case "type":
			return strings.ToLower(b.TypeLabel)
		case "published":
			return b.Published
		case "id":
			return strings.ToLower(b.ResourceID)
		}
		return ""
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		ka, kb := key(a), key(b)
		if p.Sort == "author" || p.Sort == "published" {
			// Books with nothing to sort by go last, whichever way the order runs.
			if (ka == "") != (kb == "") {
				return ka != ""
			}
		}
		if ka != kb {
			if p.Desc {
				return ka > kb
			}
			return ka < kb
		}
		ta, tb := strings.ToLower(a.Title), strings.ToLower(b.Title)
		if p.Sort == "" || p.Sort == "title" {
			if p.Desc {
				return ta > tb
			}
			return ta < tb
		}
		return ta < tb
	})
	out.Total = len(list)
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Offset > len(list) {
		p.Offset = len(list)
	}
	end := min(len(list), p.Offset+p.Limit)
	out.Books = append(out.Books, list[p.Offset:end]...)
	return out, nil
}
