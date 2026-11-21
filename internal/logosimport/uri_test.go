package logosimport

import (
	"runtime"
	"testing"
)

func TestReadOnlyURI(t *testing.T) {
	q := "?_pragma=query_only(1)&_pragma=busy_timeout(3000)"
	cases := []struct{ path, want string }{
		{"/var/folders/x/notestool.db", "file:///var/folders/x/notestool.db"},
		{"/Users/a/Library/Application Support/Logos4/x.db", "file:///Users/a/Library/Application%20Support/Logos4/x.db"},
	}
	if runtime.GOOS == "windows" { // filepath.ToSlash rewrites backslashes only on Windows
		cases = append(cases, struct{ path, want string }{`C:\Users\a\AppData\Local\Temp\x.db`, "file:///C:/Users/a/AppData/Local/Temp/x.db"})
	}
	for _, c := range cases {
		if got := readOnlyURI(c.path); got != c.want+q {
			t.Errorf("readOnlyURI(%q) = %q, want %q", c.path, got, c.want+q)
		}
	}
}
