# CSL test suite (fetched, not committed)

The fixtures come from the official Citation Style Language test suite and the CSL locale files,
pinned to the commits below. They are **not committed**: the upstream test-suite repository carries
no licence file, so we do not redistribute it. `go generate ./internal/csl` fetches them into
`humans/` and `locales/` (both git-ignored); without them `TestCSLSuite` skips.

| Source | Commit | What is copied |
| --- | --- | --- |
| https://github.com/citation-style-language/test-suite | `6eefc5b07c6969ab8999e48542acbcc131cba864` (2026-04-08) | `processor-tests/humans/*.txt` into `humans/` |
| https://github.com/citation-style-language/locales | `21a175a1d84cc1792dd73f5a627de004fba5b207` | `locales-*.xml` and `locales.json` into `locales/` |

Licence: the upstream test-suite repository has no licence file at this commit, so the fixtures stay
out of this repository. The locale files are CC BY-SA 3.0 (each says so in its `<rights>` element).

`must-pass.txt` lists the fixtures that fail `go test` when they break (the features the bundled
styles use). Every other fixture only counts towards the pass rate that `TestCSLSuite` logs.

To fetch or refresh them (the only step that uses the network), set the commits in
`refresh_testdata.go` and run `go generate ./internal/csl`.
