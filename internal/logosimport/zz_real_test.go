package logosimport

import (
	"fmt"
	"os"
	"sort"
	"testing"
)

func TestZZReal(t *testing.T) {
	root := os.Getenv("ZZ_ROOT")
	if root == "" {
		t.Skip()
	}
	src, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := src.Snapshot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	res, err := snap.Read()
	if err != nil {
		t.Fatal(err)
	}
	c := map[Kind]int{}
	for _, it := range res.Items {
		c[it.Kind]++
	}
	fmt.Println("items", c, "folders", len(res.Folders))
	r := map[string]int{}
	for _, u := range res.Unmapped {
		r[string(u.Kind)+": "+u.Reason]++
	}
	var keys []string
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Println(r[k], k)
	}
	fmt.Println(res.Notes)
}
