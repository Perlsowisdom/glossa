// A fake Zotero for the real-app checks: the local API of the Zotero app (/api/users/0/...) and the
// web API (/keys/<key>, /users/<id>/...). The app is started with GLOSSA_ZOTERO_LOCAL_URL and
// GLOSSA_ZOTERO_WEB_URL pointing here (see app.mjs), so a test never reaches the real Zotero on
// localhost:23119 or api.zotero.org, whatever is installed on the computer.
//
//   local: "ok" answers; "off" says 403, as Zotero does while "Allow other applications on this
//          computer to communicate with Zotero" is unticked.
//   KEY:   the only web key it accepts.

import { createServer } from "node:http";

export const FAKE_KEY = "e2e-fake-zotero-key";

const ITEMS = [
  { id: "http://zotero.org/users/local/e2e/items/AAAAAAAA", type: "book", title: "The Epistle to the Romans", author: [{ family: "Moo", given: "Douglas J." }], issued: { "date-parts": [[1996]] }, publisher: "Eerdmans" },
  { id: "http://zotero.org/users/local/e2e/items/BBBBBBBB", type: "book", title: "Galatians", author: [{ family: "Longenecker", given: "Richard N." }], issued: { "date-parts": [[1990]] } },
];

function handler(state, local) {
  return (req, res) => {
    state.requests.push(`${req.method} ${req.url}`);
    const json = (code, body) => { res.writeHead(code, { "Content-Type": "application/json", "Total-Results": String(ITEMS.length), "Last-Modified-Version": "7" }); res.end(JSON.stringify(body)); };
    const path = new URL(req.url, "http://x").pathname;
    if (local) {
      if (state.local === "off") { res.writeHead(403); return res.end(); }
      if (path.startsWith("/api/users/0/")) return json(200, path.endsWith("/collections") ? [] : { items: ITEMS });
    } else {
      if (path === `/keys/${FAKE_KEY}`) return json(200, { userID: 4242, username: "e2e" });
      if (path.startsWith("/keys/")) { res.writeHead(404); return res.end(); }
      if (path.startsWith("/users/4242/")) {
        if (req.headers["zotero-api-key"] !== FAKE_KEY) { res.writeHead(403); return res.end(); }
        return json(200, path.endsWith("/collections") ? [] : { items: ITEMS });
      }
    }
    res.writeHead(404);
    res.end();
  };
}

// startFakeZotero listens on two free ports on this computer. close() stops both.
export async function startFakeZotero() {
  const state = { local: "ok", requests: [] };
  const listen = (local) => new Promise((ok, fail) => {
    const srv = createServer(handler(state, local));
    srv.on("error", fail);
    srv.listen(0, "127.0.0.1", () => ok(srv));
  });
  const [localSrv, webSrv] = [await listen(true), await listen(false)];
  const url = (s) => `http://127.0.0.1:${s.address().port}`;
  return {
    localUrl: url(localSrv),
    webUrl: url(webSrv),
    key: FAKE_KEY,
    get mode() { return state.local; },
    set mode(m) { state.local = m; },
    // requests the fake has seen, as "GET /path"; for checking nothing was asked while a switch was off.
    requests: () => [...state.requests],
    forget: () => { state.requests.length = 0; },
    close: () => Promise.all([localSrv, webSrv].map((s) => new Promise((done) => { s.closeAllConnections?.(); s.close(() => done()); }))),
  };
}
