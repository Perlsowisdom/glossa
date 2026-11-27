package problems

import "testing"

var winRoots = Roots{
	Home:    `C:\Users\Jane Doe`,
	Data:    `C:\Users\Jane Doe\AppData\Roaming\Glossa`,
	Library: `C:\Users\Jane Doe\AppData\Roaming\Glossa\library.db`,
}

var macRoots = Roots{
	Home:    "/Users/jane",
	Data:    "/Users/jane/Library/Application Support/Glossa",
	Library: "/Users/jane/Library/Application Support/Glossa/library.db",
}

func TestStrip(t *testing.T) {
	tests := []struct {
		name  string
		roots Roots
		in    string
		want  string
	}{
		{"windows library file", winRoots, `open C:\Users\Jane Doe\AppData\Roaming\Glossa\library.db: locked`, `open <library>: locked`},
		{"windows data folder", winRoots, `wrote C:\Users\Jane Doe\AppData\Roaming\Glossa\logs\glossa.log`, `wrote <data>`},
		{"windows home with spaces and a book title", winRoots, `read C:\Users\Jane Doe\Documents\Books\My Study Bible.epub failed`, `read <home> failed`},
		{"windows home in other case", winRoots, `c:\users\jane doe\Desktop\x.txt`, `<home>`},
		{"windows home with forward slashes", winRoots, `C:/Users/Jane Doe/Documents/x.pdf`, `<home>`},
		{"windows long path prefix", winRoots, `\\?\C:\Users\Jane Doe\Documents\x.pdf`, `<home>`},
		{"windows other drive", winRoots, `scan D:\Church Library\Commentaries\Romans.epub now`, `scan <path> now`},
		{"windows other long path", winRoots, `\\?\D:\Books\Romans.epub`, `<path>`},
		{"windows program files", winRoots, `C:\Program Files\Glossa\Glossa.exe`, `<path>`},
		{"windows UNC share", winRoots, `\\nas01\books\Greek\Romans.epub`, `<path>`},
		{"windows long UNC", winRoots, `\\?\UNC\nas01\books\Romans.epub`, `<path>`},
		{"windows path in json", winRoots, `{"path":"D:\\Books\\Romans.epub"}`, `{"path":"<path>"}`},
		{"windows path then colon", winRoots, `open D:\Books\a.epub: access denied`, `open <path>: access denied`},
		{"mac library", macRoots, `open /Users/jane/Library/Application Support/Glossa/library.db failed`, `open <library> failed`},
		{"mac data", macRoots, `/Users/jane/Library/Application Support/Glossa/logs/glossa.log`, `<data>`},
		{"mac home", macRoots, `/Users/jane/Documents/Books/Romans.epub`, `<home>`},
		{"mac other user", macRoots, `/Users/bob/Documents/x.epub`, `<path>`},
		{"mac volume", macRoots, `scan /Volumes/Church/Books/Romans.epub`, `scan <path>`},
		{"home with a longer neighbour", macRoots, `/Users/janet/x`, `<path>`},
		{"linux path", Roots{Home: "/home/jane"}, `open /home/jane/books/a.epub and /usr/lib/x`, `open <home> and <path>`},
		{"unix path in parentheses", Roots{}, `(/var/tmp/glossa/x.db)`, `(<path>)`},
		{"tilde", Roots{}, `~/Documents/Books/a.epub`, `<home>`},
		{"go stack frame", Roots{}, "\tC:/Users/dev/glossa/internal/x.go:12 +0x1a", "\t<path>:12 +0x1a"},
		{"unix root-level single name kept", Roots{}, `GET /api and and/or 1/2`, `GET /api and and/or 1/2`},
		{"single unix folder under a known root", Roots{}, `in /tmp now`, `in <path> now`},

		{"email", Roots{}, `sent to jane.doe+bible@example.co.uk today`, `sent to <email> today`},
		{"url keeps host and path", Roots{}, `GET https://api.github.com/repos/Perlsowisdom/glossa/releases`, `GET https://api.github.com/repos/Perlsowisdom/glossa/releases`},
		{"url drops query", Roots{}, `GET https://example.org/search?q=Romans+8&key=abc123`, `GET https://example.org/search?<removed>`},
		{"url drops fragment", Roots{}, `https://example.org/a#frag`, `https://example.org/a`},
		{"url drops user and password", Roots{}, `https://jane:pw@example.org/x`, `https://example.org/x`},
		{"file url", winRoots, `file:///C:/Users/Jane Doe/x.pdf`, `<home>`},
		{"file url elsewhere", winRoots, `file:///D:/Books/x.pdf`, `<path>`},
		{"url path is not a file path", Roots{}, `https://example.org/Users/jane/x`, `https://example.org/Users/jane/x`},
		{"api key assignment", Roots{}, `apikey=abcd1234 failed`, `apikey=<secret> failed`},
		{"token in json", Roots{}, `{"token": "xyz.abc"}`, `{"token": <secret>}`},
		{"password colon", Roots{}, `password: hunter2`, `password: <secret>`},
		{"bearer", Roots{}, `Authorization: Bearer abc.DEF-123_xyz`, `Authorization: Bearer <secret>`},
		{"long hex", Roots{}, `sha 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08 ok`, `sha <secret> ok`},
		{"long base64", Roots{}, `blob QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5 end`, `blob <secret> end`},
		{"uuid", Roots{}, `device 123e4567-e89b-12d3-a456-426614174000`, `device <secret>`},
		{"long word without digits kept", Roots{}, `github.com/Perlsowisdom/glossa/internal/problems.(*Service).Guard`, `github.com/Perlsowisdom/glossa/internal/problems.(*Service).Guard`},
		{"plain text unchanged", winRoots, `command search.run failed (code failed) after 120 ms`, `command search.run failed (code failed) after 120 ms`},
		{"idempotent", winRoots, `<home> and <data> and <library> and <path> and <email>`, `<home> and <data> and <library> and <path> and <email>`},
		{"empty roots", Roots{}, `C:\x\y`, `<path>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Strip(tt.in, tt.roots)
			if got != tt.want {
				t.Errorf("Strip(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
			if again := Strip(got, tt.roots); again != got {
				t.Errorf("not idempotent: %q then %q", got, again)
			}
		})
	}
}

func TestStripNeverLeavesPrivateContent(t *testing.T) {
	in := `error reading C:\Users\Jane Doe\Documents\Sermons\Grace and Truth.docx for jane@example.org
at \\?\D:\Church Library\Romans Commentary.epub (token=ABCDEF)
see https://example.org/x?note=My+private+note`
	out := Strip(in, winRoots)
	for _, secret := range []string{"Jane", "Grace and Truth", "Church Library", "Romans Commentary", "example.org/x?note", "ABCDEF", "private note"} {
		if contains(out, secret) {
			t.Errorf("%q survived in %q", secret, out)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRedactQuoted(t *testing.T) {
	for in, want := range map[string]string{
		`no book "Genesis Commentary" found`: `no book "…" found`,
		`nothing quoted`:                     `nothing quoted`,
		`it's fine, isn't it`:                `it's fine, isn't it`,
	} {
		if got := redactQuoted(in); got != want {
			t.Errorf("redactQuoted(%q) = %q, want %q", in, got, want)
		}
	}
}
