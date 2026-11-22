# Bundled citation styles and locales

This folder holds data, not code. It is embedded into Glossa with `go:embed` (see `bundled.go`)
and is kept apart from the Go code because its licence differs.

## Licence

Every file here is licensed under the Creative Commons Attribution-ShareAlike 3.0 Unported
licence (CC BY-SA 3.0); each file says so in its `<rights>` element. The full legal text is in
`LICENSE-CC-BY-SA-3.0.txt`. The files are unmodified copies of the upstream files. If you change
one, the changed file must stay under CC BY-SA 3.0 and keep its attribution.

The Go code of Glossa (MIT OR Apache-2.0) does not link these files into a combined work: it
reads them as data, the same way it reads any style a user downloads.

## Attribution

The styles and locales are from the Citation Style Language project,
https://citationstyles.org, maintained at
https://github.com/citation-style-language/styles and
https://github.com/citation-style-language/locales.

| File | Upstream commit | Authors named in the file |
| --- | --- | --- |
| `society-of-biblical-literature-fullnote-bibliography.csl` (SBL Handbook of Style, 2nd ed., notes) | styles `15ea5749688396c9352b01b7641bcc5fe1b77da5` | Julian Onions, Simon Kornblith, Elena Razlogova, Sebastian Karcher, Tyler Mykkanen, J. David Stark, Andrew Dunning, Patrick O'Brien, Ramón Marrero Cedeño |
| `chicago-notes-bibliography.csl` (Chicago Manual of Style 18th ed., notes and bibliography) | styles `15ea5749688396c9352b01b7641bcc5fe1b77da5` | Andrew Dunning |
| `apa.csl` (APA Style 7th ed.) | styles `15ea5749688396c9352b01b7641bcc5fe1b77da5` | Brenton M. Wiernik, Andrew Dunning, Jay Pfaffman |
| `locales-en-US.xml` | locales `21a175a1d84cc1792dd73f5a627de004fba5b207` | Andrew Dunning, Sebastian Karcher, Rintze M. Zelle, Denis Meier, Brenton M. Wiernik |
| `locales-en-GB.xml` | locales `21a175a1d84cc1792dd73f5a627de004fba5b207` | see the file |

To refresh these copies (the only step that uses the network), change the commits in
`../refresh_testdata.go` and run `go generate ./internal/csl`.
