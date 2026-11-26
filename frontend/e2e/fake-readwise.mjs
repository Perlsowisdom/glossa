// A fake Readwise for the real-app checks: the two endpoints of the Readwise API that Glossa uses,
// GET /api/v2/auth/ and GET /api/v2/export/ (updatedAfter, pageCursor, includeDeleted). The app is
// started with GLOSSA_READWISE_URL pointing here (see app.mjs; the app honours it only for an address on
// this computer), so a test never reaches readwise.io, whatever is on the network.
//
//   TOKEN:   the only token it accepts.
//   shelf:   Mere Christianity (matches the library book the check adds), two others and an article.
//   change(): changes a highlight the way the person would in Readwise (its updated_at moves to the future,
//            so an incremental import, which asks for everything after now, sees it).

import { createServer } from "node:http";

export const FAKE_TOKEN = "e2e-fake-readwise-token";
const FUTURE = "2099-01-01T00:00:00Z";

function shelf() {
  const hl = (id, text, loc, extra = {}) => ({ id, text, note: "", location: loc, location_type: "location", highlighted_at: "2026-02-01T00:00:00Z",
    updated_at: "2026-02-01T00:00:00Z", is_discard: false, is_deleted: false, readwise_url: `https://readwise.io/open/${id}`, ...extra });
  const book = (user_book_id, title, author, category, highlights, book_tags = []) => ({
    user_book_id, title, readable_title: title, author, category, source: category === "books" ? "kindle" : "web_clip", is_deleted: false,
    readwise_url: `https://readwise.io/bookreview/${user_book_id}`, source_url: null, document_note: "", book_tags: book_tags.map((name, i) => ({ id: i + 1, name })), highlights,
  });
  return [
    book(101, "Mere Christianity", "Lewis, C.S.", "books", [
      hl(1011, "Pride is the complete anti-god state of mind.", 110),
      hl(1012, "Humility is not thinking less of yourself, it is thinking of yourself less.", 240, { note: "Read this twice." })], ["apologetics"]),
    book(102, "The Pilgrim's Progress", "John Bunyan", "books", [hl(1021, "As I walked through the wilderness of this world.", 30)]),
    book(103, "Knowing God", "J. I. Packer", "books", [hl(1031, "What were we made for? To know God.", 15), hl(1032, "Thrown away.", 16, { is_discard: true })]),
    book(104, "A short note on prayer", "Some Writer", "articles", [hl(1041, "Pray without ceasing.", 0, { location_type: "order" })]),
  ];
}

export async function startFakeReadwise() {
  const state = { books: shelf(), requests: [], pageSize: 2 };
  const handler = (req, res) => {
    const url = new URL(req.url, "http://x");
    state.requests.push(`${req.method} ${url.pathname}${url.search}`);
    if (req.headers.authorization !== `Token ${FAKE_TOKEN}`) {
      res.writeHead(401, { "Content-Type": "application/json" });
      return res.end('{"detail":"Invalid token."}');
    }
    if (url.pathname === "/api/v2/auth/") { res.writeHead(204); return res.end(); }
    if (url.pathname !== "/api/v2/export/") { res.writeHead(404); return res.end(); }
    const after = url.searchParams.get("updatedAfter") ?? "";
    const deleted = url.searchParams.get("includeDeleted") === "true";
    const out = [];
    for (const b of state.books) {
      const highlights = b.highlights.filter((h) => (!after || h.updated_at > after) && (deleted || !h.is_deleted));
      if (highlights.length) out.push({ ...b, highlights });
    }
    const start = Number(url.searchParams.get("pageCursor") ?? 0);
    const end = start + state.pageSize;
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ count: out.length, nextPageCursor: end < out.length ? String(end) : null, results: out.slice(start, end) }));
  };
  const srv = createServer(handler);
  await new Promise((ok, fail) => { srv.on("error", fail); srv.listen(0, "127.0.0.1", ok); });
  const find = (id) => state.books.find((b) => b.user_book_id === id);
  return {
    url: `http://127.0.0.1:${srv.address().port}`,
    token: FAKE_TOKEN,
    requests: () => [...state.requests],
    forget: () => { state.requests.length = 0; },
    reset: () => { state.books = shelf(); state.requests.length = 0; },
    // change edits a highlight's text in Readwise.
    change(bookId, highlightId, patch) {
      const h = find(bookId).highlights.find((x) => x.id === highlightId);
      Object.assign(h, patch, { updated_at: FUTURE });
    },
    // add puts a new highlight in a book.
    add(bookId, id, text, location) {
      find(bookId).highlights.push({ id, text, note: "", location, location_type: "location", highlighted_at: FUTURE, updated_at: FUTURE,
        is_discard: false, is_deleted: false, readwise_url: `https://readwise.io/open/${id}` });
    },
    close: () => new Promise((done) => { srv.closeAllConnections?.(); srv.close(() => done()); }),
  };
}
