# CCEL (Christian Classics Ethereal Library, ccel.org)

Researched 21 November 2026 with a handful of requests (User-Agent `Glossa`, 10 s apart).

## Terms

- Page: <https://ccel.org/about/copyright.html>. Most editions are based on books in the US public
  domain. The site and its added material (introductions, covers) are copyrighted. "Personal,
  educational and non-profit use" is allowed; republishing or commercial use needs permission. A few
  books are used by permission of another publisher and say so on their own page. Some US public-domain
  books may be protected elsewhere. No credit line is required; the page says nothing about automated
  access.
- `https://ccel.org/robots.txt`: `User-agent: *`, `Crawl-delay: 10`, nothing disallowed. Only
  `meta-externalagent` is blocked. Glossa waits at least 10 s between requests to ccel.org.
- Conclusion: downloading a work for the user's own library is allowed. Glossa only downloads on the
  user's action, keeps a personal copy, never redistributes, and shows a credit line to CCEL anyway.

## Formats

Per work: EPUB (`/ccel/<a>/<author>/<work>/cache/<work>.epub`, where `<a>` is the author id's first
letter), PDF, plain text (`.../cache/<work>.txt`) and ThML (`/ccel/<a>/<author>/<work>.xml`). Glossa
uses EPUB.

## Catalogue

No OPDS feed, sitemap or bulk export exists (`/opds`, `/sitemap.xml` are 404). `/index/title` is one HTML
page (about 550 KB, 2,700 works) listing every work as `/ccel/<author>/<work>.html`, title and author
name, with `(classic)` marks. Glossa fetches that one page at most once a week. The rights are not in
that list: each work's ThML header carries `<DC.Rights>Public Domain</DC.Rights>`. Glossa reads the first
few KB of the ThML (a range request) when the user clicks Get free copy, and offers only works whose
rights are "Public Domain".
