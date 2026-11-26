package syncutil

import "testing"

func TestDecide(t *testing.T) {
	cases := []struct {
		name                     string
		gNow, gThen, oNow, oThen string
		same                     bool
		want                     Action
	}{
		{"nothing changed", "a", "a", "b", "b", false, Nothing},
		{"only the other side", "a", "a", "b2", "b", false, KeepOther},
		{"only Glossa", "a2", "a", "b", "b", false, KeepOwn},
		{"both", "a2", "a", "b2", "b", false, Ask},
		{"both, but now equal", "x", "a", "x", "b", true, Nothing},
		{"no baseline, different", "a", "", "b", "", false, Ask},
		{"no baseline, equal", "a", "", "a", "", true, Nothing},
		{"no baseline for the file only", "a", "a", "b", "", false, KeepOther},
	}
	for _, c := range cases {
		if got := Decide(c.gNow, c.gThen, c.oNow, c.oThen, c.same); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFromDifference(t *testing.T) {
	a, b := FromDifference("one\ntwo\nthree", "one\ntwo\nTHREE")
	if a != "… three" || b != "… THREE" {
		t.Errorf("%q %q", a, b)
	}
	if Hash("a", "b") == Hash("ab", "") {
		t.Error("parts run into each other")
	}
}
