package relver

import "testing"

func TestMapping(t *testing.T) {
	for _, c := range []struct{ in, file, msix string }{
		{"1.2.3", "1.2.3.0", "1.2.3.0"},
		{"0.4.0-alpha", "0.4.0.0", "0.4.0.0"},
		{"0.4.0-rc.1", "0.4.0.0", "0.4.0.0"},
		{"10.20.30-beta.2+build.5", "10.20.30.0", "10.20.30.0"},
		{"0.0.0-dev+abc1234", "0.0.0.0", "0.0.1.0"},
		{"1.0.0-0.3.7", "1.0.0.0", "1.0.0.0"},
	} {
		if got, err := FileVersion(c.in); err != nil || got != c.file {
			t.Errorf("FileVersion(%q) = %q, %v; want %q", c.in, got, err, c.file)
		}
		if got, err := MSIX(c.in); err != nil || got != c.msix {
			t.Errorf("MSIX(%q) = %q, %v; want %q", c.in, got, err, c.msix)
		}
	}
}

func TestInvalid(t *testing.T) {
	for _, v := range []string{"", "1", "1.2", "1.2.3.4", "v1.2.3", "01.2.3", "1.2.3-", "1.2.3-a..b", "1.2.3 beta", "1.2.x", "1.2.3-alpha_1", "70000.0.0"} {
		if _, err := FileVersion(v); err == nil {
			t.Errorf("FileVersion(%q) accepted", v)
		}
		if _, err := MSIX(v); err == nil {
			t.Errorf("MSIX(%q) accepted", v)
		}
	}
	if Validate("0.4.0-alpha") != nil || Validate("1.0.0") != nil {
		t.Error("valid versions rejected")
	}
}

func TestMac(t *testing.T) {
	for in, want := range map[string]string{"0.5.0-alpha": "0.5.0", "1.2.3": "1.2.3", "0.0.0-dev+abc1234": "0.0.0", "10.20.30-beta.1": "10.20.30"} {
		if got, err := Mac(in); err != nil || got != want {
			t.Errorf("Mac(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Mac("1.2"); err == nil {
		t.Error("Mac accepted 1.2")
	}
}

func TestDeb(t *testing.T) {
	for in, want := range map[string]string{
		"0.6.1":                   "0.6.1",
		"0.6.1-alpha":             "0.6.1~alpha",
		"0.4.0-rc.1":              "0.4.0~rc.1",
		"1.0.0-beta-2":            "1.0.0~beta.2",
		"0.0.0-dev+abc1234":       "0.0.0~dev+abc1234",
		"10.20.30-beta.2+build.5": "10.20.30~beta.2+build.5",
		"0.0.0+abc-def":           "0.0.0+abc.def",
	} {
		if got, err := Deb(in); err != nil || got != want {
			t.Errorf("Deb(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Deb("1.2"); err == nil {
		t.Error("Deb accepted 1.2")
	}
}
