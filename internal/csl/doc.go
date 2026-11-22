// Package csl is Glossa's own Citation Style Language processor. It is written from the CSL
// 1.0.2 specification (https://docs.citationstyles.org/en/stable/specification.html) alone; no
// code from any other processor was read or used.
//
// Use it like this:
//
//	style, _ := csl.LoadStyle(styleFile)
//	loc, _ := csl.LoadLocale(localeFile)
//	p, _ := csl.New(style, csl.NewLocales(loc), items)
//	c, _ := p.Cite(csl.Cluster{Items: []csl.CiteItem{{ID: "smith2020", Locator: "12", Label: "page"}}})
//	fmt.Println(c.Output.Text)      // or .HTML, or .Runs for a word processor or PDF
//	for _, e := range p.Bibliography() { fmt.Println(e.Output.Text) }
//
// Clusters are given in document order. A note style gives each cluster a note number and the
// processor works out first, subsequent, ibid, ibid-with-locator and near-note positions.
package csl

//go:generate go run refresh_testdata.go
