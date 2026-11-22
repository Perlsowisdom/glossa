package csljson

import (
	"encoding/json"
	"testing"
)

func TestParseWrappedAndBare(t *testing.T) {
	in := `{"items":[{"id":"1/ABC","type":"book","title":"Romans","author":[{"family":"Moo","given":"Douglas J."}],
	  "issued":{"date-parts":[["1996"]]},"volume":3,"edition":"2","number-of-volumes":4},{"id":"","type":"book"}]}`
	items, err := Parse([]byte(in))
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	it := items[0]
	if it.Year() != 1996 || it.Volume != "3" || it.Edition != "2" || it.NumberOfVolumes != "4" {
		t.Errorf("%+v", it)
	}
	if got := it.Creators()[0].Display(); got != "Moo, Douglas J." {
		t.Errorf("display %q", got)
	}
	bare, err := Parse([]byte(`[{"id":"x","type":"webpage"}]`))
	if err != nil || len(bare) != 1 {
		t.Errorf("bare: %v %v", bare, err)
	}
}

func TestDates(t *testing.T) {
	var d Date
	if err := json.Unmarshal([]byte(`{"date-parts":[[2020,5,3],[2020,6]]}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.Year != 2020 || d.Month != 5 || d.Day != 3 || d.End == nil || d.End.Month != 6 {
		t.Errorf("%+v", d)
	}
	b, _ := json.Marshal(d)
	var back Date
	if err := json.Unmarshal(b, &back); err != nil || back.Day != 3 || back.End.Month != 6 {
		t.Errorf("round trip %s %+v %v", b, back, err)
	}
	var lit Date
	json.Unmarshal([]byte(`{"literal":"Spring 1999"}`), &lit)
	if lit.Literal != "Spring 1999" || lit.Year != 0 || lit.IsZero() {
		t.Errorf("%+v", lit)
	}
}

func TestOrganisationName(t *testing.T) {
	if (Name{Literal: "Zondervan"}).Display() != "Zondervan" {
		t.Error("literal")
	}
}
