package cite_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.uber.org/goleak"

	"github.com/Perlsowisdom/glossa/internal/command"
	"github.com/Perlsowisdom/glossa/internal/command/all"
	"github.com/Perlsowisdom/glossa/internal/integrations/secrets"
	"github.com/Perlsowisdom/glossa/internal/library"
	"github.com/Perlsowisdom/glossa/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const osisKJV = `<?xml version="1.0" encoding="UTF-8"?>
<osis xmlns="http://www.bibletechnologies.net/2003/OSIS/namespace">
<osisText osisIDWork="KJVtest" xml:lang="en">
<header><work osisWork="KJVtest"><title>King James Version (test)</title></work></header>
<div type="book" osisID="John"><chapter osisID="John.3">
<verse osisID="John.3.16">For God so loved the world.</verse>
</chapter></div>
</osisText></osis>`

const zoteroArticle = `{"id":"zotero:ABCD2345","type":"article-journal","title":"Justification and Works","container-title":"Journal of Pauline Studies","volume":"12","page":"45-67","author":[{"family":"Wright","given":"N. T."}],"issued":{"date-parts":[[2009]]}}`

type rig struct {
	t    *testing.T
	data string
	reg  *command.Registry
	env  *command.Env
	book string // the library book's key
}

func setup(t *testing.T) *rig {
	t.Helper()
	data := t.TempDir()
	st, err := store.Open(filepath.Join(data, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g := &rig{t: t, data: data, reg: all.Registry(data, secrets.NewMemory()), env: &command.Env{Store: st}}
	t.Cleanup(g.reg.Close)

	kjv := filepath.Join(data, "kjv.xml")
	os.WriteFile(kjv, []byte(osisKJV), 0o644)
	g.must("bible.install", map[string]any{"path": kjv, "id": "kjv", "abbreviation": "KJV", "licence_type": "public_domain"}, nil)

	books := filepath.Join(data, "books")
	os.MkdirAll(books, 0o755)
	os.WriteFile(filepath.Join(books, "romans.md"), []byte("# Romans\n\nText.\n"), 0o644)
	var f library.Folder
	g.must("library.add_folder", map[string]any{"path": books}, &f)
	g.must("library.scan", map[string]any{"folder_id": f.ID}, nil)
	var items struct {
		Items []library.ItemSummary `json:"items"`
	}
	g.must("library.items", map[string]any{}, &items)
	id := items.Items[0].ID
	g.must("library.update_item", map[string]any{"id": id, "title": "The Epistle to the Romans", "year": 1996, "publisher": "Eerdmans",
		"authors": []string{"Douglas J. Moo"}}, nil)
	g.book = "lib:" + id

	err = st.Write(context.Background(), "test", func(tx *store.Tx) error {
		return tx.Create("cite_items", store.Row{"id": "zotero:ABCD2345", "connection": "zotero", "item_key": "ABCD2345",
			"csl_json": zoteroArticle, "title": "Justification and Works", "cached_at": 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func (g *rig) run(id string, params, out any) error {
	g.t.Helper()
	raw, _ := json.Marshal(params)
	res, err := g.reg.Run(context.Background(), g.env, id, raw)
	if err != nil {
		return err
	}
	if out != nil {
		reflect.ValueOf(out).Elem().SetZero()
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, out); err != nil {
			g.t.Fatal(err)
		}
	}
	return nil
}

func (g *rig) must(id string, params, out any) {
	g.t.Helper()
	if err := g.run(id, params, out); err != nil {
		g.t.Fatalf("%s: %v", id, err)
	}
}
