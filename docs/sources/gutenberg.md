# Project Gutenberg (gutenberg.org)

Written 27 November 2026 for T43.

## What could not be checked

On 27 November 2026 every gutenberg.org page the task asked to be read answered `503 Service Unavailable` with
`Retry-After: 21600` (six hours): the robot-access policy (`/policy/robot_access.html`), the mirroring page
(`/help/mirroring.html`), the permission page (`/policy/permission.html`) and the feeds folder
(`/cache/epub/feeds/`). They were asked for once each and not asked for again, because the answer itself says
"come back later". So the URLs and terms below come from the T43 brief and from how Gutenberg's files are
named, **not** from reading the policy pages that day. **Someone should read them and correct this page
before a release** (see "To check"). The app is built so that a change is one constant in
`internal/gutenberg/gutenberg.go`.

## What the app does

- Catalogue: `https://www.gutenberg.org/cache/epub/feeds/pg_catalog.csv.gz`, Gutenberg's own catalogue file
  (the brief's URL). One gzip file of CSV with the columns `Text#, Type, Issued, Title, Language, Authors,
  Subjects, LoCC, Bookshelves`. Glossa reads the columns by name, keeps only rows whose `Type` is `Text` (audio,
  music, images and other kinds are skipped), turns `Calvin, Jean, 1509-1564` into `Jean Calvin`, and keeps the
  book number, title, authors and the first language. It fetches the file at most once a week, and only when
  the user has turned Project Gutenberg on and opens From Logos or presses refresh.
- Book: `https://www.gutenberg.org/cache/epub/<n>/pg<n>-images-3.epub`, the EPUB 3 with images for book
  number `<n>` (the file name Gutenberg gives it). Only after the user presses Get free copy and confirms (or
  Get all free copies and confirms). The size comes from a `HEAD` request first.
- Automated access: one request at a time, at least five seconds between requests, the header
  `User-Agent: Glossa`, no cookies, no crawling, no request except for something the user asked for.
  The number of seconds and the `Glossa` header are the app's own choice (a conservative one); the policy page
  should confirm them or give a different figure.
- Gutenberg's Terms: Gutenberg texts are public domain in the United States. Glossa records each book as
  "Public domain (US)", with the credit "Project Gutenberg (gutenberg.org)" and the link
  `https://www.gutenberg.org/ebooks/<n>`. The book is added exactly as Gutenberg sent it, **with Gutenberg's
  header and licence text left in**: Gutenberg's licence lets a copy keep it, and removing it is allowed only if
  every mention of the Project Gutenberg name goes too, which Glossa will not do (it credits the source).
  Public domain outside the US varies by country; the licence label says "(US)" for that reason.

## Matching

Glossa compares the user's Logos titles and authors with the cached catalogue on this computer. It never sends
a title anywhere. See `docs/privacy.md` and `internal/logosimport/matcher.go`.

## To check (when the pages answer again)

1. `/policy/robot_access.html`: is the catalogue URL above still the recommended way to get the list; is there a
   stated delay between requests (the app uses five seconds); is a `User-Agent` of `Glossa` acceptable.
2. `/help/mirroring.html`: the catalogue feed names and the `pg<n>-images-3.epub` file name pattern.
3. `/policy/permission.html` and `/policy/license.html`: that keeping the header and licence text, with the
   credit, is what a personal copy needs.
