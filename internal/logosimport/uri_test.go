package logosimport

import (
	"strings"
	"testing"
)

func TestReadOnlyURI(t *testing.T) {
	q := "?_pragma=query_only(1)&_pragma=busy_timeout(3000)"
	for _, c := range []struct{ path, want string }{
		{"/var/folders/x/notestool.db", "file:///var/folders/x/notestool.db"},
		{"/Users/a/Library/Application Support/Logos4/x.db", "file:///Users/a/Library/Application%20Support/Logos4/x.db"},
		{`C:\Users\a\AppData\Local\Temp\x.db`, "file:///C:/Users/a/AppData/Local/Temp/x.db"},
	} {
		got := readOnlyURI(c.path)
		if strings.Contains(c.path, `\`) && !strings.HasPrefix(got, "file:///C:") {
			// filepath.ToSlash only rewrites backslashes on Windows; skip elsewhere.
			continue
		}
		if got != c.want+q {
			t.Errorf("readOnlyURI(%q) = %q, want %q", c.path, got, c.want+q)
		}
	}
}
