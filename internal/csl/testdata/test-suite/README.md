# CSL test suite (pinned copy)

These files are a pinned copy of the official Citation Style Language test suite and the CSL
locale files the fixtures need. They are test data only: nothing here is compiled into Glossa.

| Source | Commit | What is copied |
| --- | --- | --- |
| https://github.com/citation-style-language/test-suite | `6eefc5b07c6969ab8999e48542acbcc131cba864` (2026-04-08) | `processor-tests/humans/*.txt` into `humans/` |
| https://github.com/citation-style-language/locales | `21a175a1d84cc1792dd73f5a627de004fba5b207` | `locales-*.xml` and `locales.json` into `locales/` |

Licence: the test suite is MIT licensed (see `LICENSE`). The upstream repository carries no licence
file at this commit; the MIT terms are those the CSL project states for it. The locale files are
licensed Creative Commons Attribution-ShareAlike 3.0 (each file says so in its `<rights>` element).

`must-pass.txt` lists the fixtures that fail `go test` when they break (the features the bundled
styles use). Every other fixture only counts towards the pass rate that `TestCSLSuite` logs.

To refresh the copy (the only step that uses the network), change the commits in
`refresh_testdata.go` and run `go generate ./internal/csl`.
