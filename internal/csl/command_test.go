package csl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/Perlsowisdom/glossa/internal/command"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func cmdRegistry(t *testing.T) (*command.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	r := command.NewRegistry()
	Register(r, dir)
	return r, dir
}

func run[T any](t *testing.T, r *command.Registry, id, params string) (T, error) {
	t.Helper()
	res, err := r.Run(context.Background(), &command.Env{}, id, json.RawMessage(params))
	if err != nil {
		var zero T
		return zero, err
	}
	return res.(T), nil
}

func TestStylesCommand(t *testing.T) {
	r, dir := cmdRegistry(t)
	res, err := run[StylesResult](t, r, "csl.styles", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Styles) != 3 {
		t.Fatalf("want the 3 bundled styles, got %+v", res.Styles)
	}
	ids := map[string]string{}
	for _, s := range res.Styles {
		ids[s.ID] = s.Class
	}
	if ids["apa"] != "in-text" || ids["chicago-notes-bibliography"] != "note" {
		t.Errorf("classes wrong: %v", ids)
	}
	// An added style appears, and a dependent one resolves its bundled parent.
	dep := `<style xmlns="http://purl.org/net/xbiblio/csl" class="in-text" version="1.0"><info><title>My APA</title><id>x</id>` +
		`<link href="http://www.zotero.org/styles/apa" rel="independent-parent"/></info></style>`
	if err := os.WriteFile(filepath.Join(dir, "my-apa.csl"), []byte(dep), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = run[StylesResult](t, r, "csl.styles", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range res.Styles {
		if s.ID == "my-apa" && s.Source == "added" && s.Title == "My APA" {
			found = true
		}
	}
	if !found {
		t.Errorf("added style missing: %+v", res.Styles)
	}
	out, err := run[RenderResult](t, r, "csl.render", `{"style":"my-apa","items":[{"id":"a","type":"book","title":"T","author":[{"family":"Roe","given":"Jane"}],"issued":{"date-parts":[[2001]]}}],"clusters":[{"cites":[{"item":"a"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if out.Citations[0].Output != "(Roe, 2001)" {
		t.Errorf("dependent style rendered %q", out.Citations[0].Output)
	}
}

func TestRenderCommand(t *testing.T) {
	r, _ := cmdRegistry(t)
	params := map[string]any{
		"style":  "chicago-notes-bibliography",
		"format": "html",
		"items":  json.RawMessage(goldenItems),
		"clusters": []map[string]any{
			{"cites": []map[string]any{{"item": "book", "locator": "12"}}},
			{"cites": []map[string]any{{"item": "book", "locator": "13"}}},
		},
	}
	b, _ := json.Marshal(params)
	res, err := run[RenderResult](t, r, "csl.render", string(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Citations) != 2 || res.Citations[0].NoteNumber != 1 || res.Citations[1].NoteNumber != 2 {
		t.Fatalf("citations: %+v", res.Citations)
	}
	if !strings.Contains(res.Citations[0].Output, "<i>Genesis 1–15</i>") {
		t.Errorf("html missing italics: %q", res.Citations[0].Output)
	}
	if res.Citations[1].Positions[0] != "ibid-with-locator" || res.Citations[1].Output != "Wenham, <i>Genesis 1–15</i>, 13." {
		t.Errorf("second cite: %+v", res.Citations[1])
	}
	if len(res.Bibliography) != 1 || res.Bibliography[0].ID != "book" {
		t.Errorf("bibliography should hold only the cited item: %+v", res.Bibliography)
	}
	// Bibliography only: every item, plain text.
	b, _ = json.Marshal(map[string]any{"style": "apa", "items": json.RawMessage(goldenItems)})
	res, err = run[RenderResult](t, r, "csl.render", string(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Citations) != 0 || len(res.Bibliography) != 8 {
		t.Errorf("want 8 entries and no citations, got %d and %d", len(res.Bibliography), len(res.Citations))
	}
}

func TestRenderCommandErrors(t *testing.T) {
	r, _ := cmdRegistry(t)
	for name, params := range map[string]string{
		"unknown style": `{"style":"nope","items":[{"id":"a","type":"book"}]}`,
		"bad path":      `{"style":"../etc/passwd","items":[{"id":"a","type":"book"}]}`,
		"unknown item":  `{"style":"apa","items":[{"id":"a","type":"book"}],"clusters":[{"cites":[{"item":"zz"}]}]}`,
		"no style":      `{"items":[{"id":"a","type":"book"}]}`,
	} {
		if _, err := run[RenderResult](t, r, "csl.render", params); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
