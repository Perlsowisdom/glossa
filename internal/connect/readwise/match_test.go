package readwise

import (
	"context"
	"fmt"
	"testing"
)

func TestMatching(t *testing.T) {
	g := newRig(t)
	g.addBook("mc", "Mere Christianity", "C. S. Lewis")
	g.addBook("pp", "The Pilgrim’s Progress", "John Bunyan")
	g.addBook("kg", "Knowing God", "J. I. Packer")
	g.addBook("rv", "Revelation: A Commentary", "Greg Beale")
	g.addBook("ck", "The Songs of Jesus", "Tim Keller;Kathy Keller")
	g.addBook("jm", "The Crucified God", "Jürgen Moltmann")
	g.addBook("nm", "Systematic Theology: An Introduction", "Wayne Grudem")
	m, err := LoadMatcher(context.Background(), g.env)
	if err != nil {
		t.Fatal(err)
	}
	if m.Books != 7 {
		t.Fatalf("books = %d", m.Books)
	}
	cases := []struct {
		title, author, want string
	}{
		// the same book, written differently
		{"Mere Christianity", "C. S. Lewis", "mc"},
		{"MERE CHRISTIANITY", "c.s. lewis", "mc"},
		{"Mere Christianity", "Lewis, C.S.", "mc"},
		{"  Mere   Christianity ", "C S Lewis", "mc"},
		{"Pilgrim's Progress", "John Bunyan", "pp"},
		{"The Pilgrims Progress", "Bunyan, John", "pp"},
		{"Knowing God: 20th Anniversary Edition", "J.I. Packer", "kg"},
		{"Revelation", "Greg Beale", "rv"},
		{"A Revelation: A Commentary", "Beale, Greg", "rv"},
		{"The Songs of Jesus", "Tim Keller and Kathy Keller", "ck"},
		{"Songs of Jesus", "Kathy Keller & Tim Keller", "ck"},
		{"Songs of Jesus", "Keller, Kathy; Keller, Tim", "ck"},
		{"Crucified God", "Jurgen Moltmann", "jm"},
		{"Systematic Theology", "Wayne Grudem", "nm"},
		// near misses that must not match
		{"Mere Christianity", "Clive Staples Lewis", ""},
		{"Mere Christianity", "Lewis", ""},
		{"Mere Christianity", "C. S. Lewis and Walter Hooper", ""},
		{"Mere Christianity", "", ""},
		{"Mere Christianity Study Guide", "C. S. Lewis", ""},
		{"Mere", "C. S. Lewis", ""},
		{"Mere Christianit", "C. S. Lewis", ""},
		{"Screwtape Letters", "C. S. Lewis", ""},
		{"Knowing God", "J. I. Packar", ""},
		{"Knowing God", "Packer", ""},
		{"Knowing God the Father", "J. I. Packer", ""},
		{"Revelation: The Book of Hope", "Greg Beale", ""},
		{"Systematic Theology: Volume 2", "Wayne Grudem", ""},
		{"The Songs of Jesus", "Tim Keller", ""},
		{"The Songs of Jesus", "Timothy Keller and Kathy Keller", ""},
		{"", "C. S. Lewis", ""},
		{"The", "C. S. Lewis", ""},
	}
	for _, c := range cases {
		got, amb := m.Match(c.title, c.author)
		if got != c.want || amb {
			t.Errorf("Match(%q, %q) = %q (ambiguous %v), want %q", c.title, c.author, got, amb, c.want)
		}
	}
}

func TestAuthorKeys(t *testing.T) {
	for in, want := range map[string]string{
		"Lewis, C. S.":                  "c s lewis",
		"C.S. Lewis":                    "c s lewis",
		"Tim Keller, Kathy Keller":      "kathy keller|tim keller",
		"Keller, Tim and Keller, Kathy": "kathy keller|tim keller",
		"A. B. Smith; Jones, Bob":       "a b smith|bob jones",
		"":                              "",
	} {
		if got := fmt.Sprint(joinKeys(authorKeys(in))); got != want {
			t.Errorf("authorKeys(%q) = %q, want %q", in, got, want)
		}
	}
}

func joinKeys(k []string) string {
	out := ""
	for i, s := range k {
		if i > 0 {
			out += "|"
		}
		out += s
	}
	return out
}
