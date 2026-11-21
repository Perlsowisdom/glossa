# 0012: CCEL as an opt-in source of free books

Status: Accepted (approved by the user, 21 November 2026: "Yes, add CCEL"). Extends 0009.

## Decision

- Glossa may fetch public-domain books from the Christian Classics Ethereal Library (ccel.org). This adds
  CCEL to the features ADR 0009 allows to use the network.
- It is off until the user turns it on in Settings → Privacy. While off, Glossa sends nothing to ccel.org.
- Requests: its list of books (one page, at most once a week), and for a book the user chose, its rights
  note and EPUB. `docs/privacy.md` lists them; `docs/sources/ccel.md` records CCEL's terms. Requests are at
  least ten seconds apart (CCEL's robots.txt `Crawl-delay`), carry only `User-Agent: Glossa`, and never
  contain anything from the user's library.
- Only books whose rights note says Public Domain are downloaded; they are added with that licence and a
  credit to CCEL.

## Consequences

- The Logos matcher uses CCEL's cached list; it never causes a request.
