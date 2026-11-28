// A fake Any for the real-app checks: the part of the Any app's own API that Glossa uses (/v1/spaces,
// types, objects/query, search, objects, the Markdown of a page and /v1/health for the account id), shaped
// from the description the Any app
// publishes about itself (Any API 1.0). The app is started with GLOSSA_ANY_CANDIDATES pointing here
// (see app.mjs), so a test never reaches the real Any app on port 31310 or 31009, whatever is running
// on the computer.
//
//   account: the id of the account open in it (GET /v1/health); change it to be a different Any.
//   edit(): changes a page the way the person would inside Any.
//   refuse: when true it answers 401 to everything, like an Any that refuses Glossa.
//   bin(): puts a page in the bin.
//   pause()/resume(): Any quits and starts again on the same port.
//   forbidden(): any request Glossa must never make (DELETE, /auth, /shutdown).

import { createServer } from "node:http";

const PAGE_TYPE = "bafy-page-type";

export async function startFakeAny() {
  const state = { objs: new Map(), requests: [], rev: 0, next: 0, refuse: false, account: "e2e-account-1", conflictNext: false };
  const touch = (o) => { o.rev = ++state.rev; o.version += 1; };
  const add = (space, name, markdown) => {
    const o = { id: `obj-${++state.next}`, space, name, markdown, rev: 0, version: 0, binned: false };
    touch(o);
    state.objs.set(o.id, o);
    return o;
  };
  const record = (o) => ({ id: o.id, any: { name: o.name, type: PAGE_TYPE, collections: o.binned ? ["bin"] : [] }, modifiedAt: 1700000000000 + o.rev });

  const handler = (req, res) => {
    const url = new URL(req.url, "http://x");
    const path = url.pathname;
    state.requests.push(`${req.method} ${path}`);
    let raw = "";
    req.on("data", (c) => { raw += c; });
    req.on("end", () => {
      const send = (code, body) => { res.writeHead(code, { "Content-Type": "application/json" }); res.end(JSON.stringify(body)); };
      const err = (code, ecode, message) => send(code, { error: { code: ecode, message } });
      const body = raw ? JSON.parse(raw) : {};
      if (path.startsWith("/v1/auth")) return err(405, "method.not_allowed", "Glossa must never call this");
      if (state.refuse) return err(401, "auth.required", "unauthorized");
      if (path === "/v1/health") return send(200, { status: "ok", version: "1.0", account: state.account });
      const seg = path.split("/").filter(Boolean); // v1 spaces {space} ...
      const inSpace = (list) => list.filter((o) => o.space === seg[2]);
      if (path === "/v1/spaces" && req.method === "GET") {
        return send(200, { spaces: [
          { id: "space-study", name: "Study", type: "space", spaceType: "personal", status: "active" },
          { id: "space-church", name: "Church", type: "space", spaceType: "shared", status: "active" }] });
      }
      if (seg[3] === "types" && seg.length === 4 && req.method === "GET") {
        return send(200, { types: [{ id: "bafy-note-type", name: "Note", xKey: "note", builtIn: true }, { id: PAGE_TYPE, name: "Page", xKey: "page", builtIn: true }] });
      }
      if (seg[3] === "objects" && seg[4] === "query" && req.method === "POST") {
        const list = inSpace([...state.objs.values()]).sort((a, b) => b.rev - a.rev);
        const off = body.offset ?? 0;
        const lim = body.limit || 100;
        return send(200, { records: list.slice(off, off + lim).map(record), total: list.length, hasNext: off + lim < list.length });
      }
      if (seg[3] === "search" && req.method === "POST") {
        if (!body.query) return err(400, "request.missing_field", "query is required");
        const q = body.query.toLowerCase();
        const list = inSpace([...state.objs.values()]).filter((o) => `${o.name} ${o.markdown}`.toLowerCase().includes(q));
        return send(200, { hits: list.map((o) => ({ objectId: o.id, dataset: "editor_blocks", data: o.markdown.slice(0, 80), score: 1 })), mode: "hybrid" });
      }
      if (seg[3] === "objects" && seg.length === 4 && req.method === "POST") {
        const name = body.initialProperties?.any?.name;
        if (body.type !== PAGE_TYPE || !name) return err(400, "request.invalid_field", "type and initialProperties.any.name are required");
        return send(201, { objectId: add(seg[2], name, "").id });
      }
      if (seg[3] === "objects" && seg[4]) {
        const o = state.objs.get(seg[4]);
        if (!o || o.space !== seg[2]) return err(404, "object.not_found", "no such object");
        if (seg.length === 5 && req.method === "GET") return send(200, { objectId: o.id, record: record(o) });
        if (seg[5] === "editor" && seg[6] === "editor_blocks" && seg[7] === "markdown") {
          if (req.method === "GET") return send(200, { content: o.markdown, version: `v${o.version}` });
          if (req.method === "PUT") {
            if (state.conflictNext || (body.ifVersion && body.ifVersion !== `v${o.version}`)) {
              state.conflictNext = false;
              return err(409, "markdown.conflict", "the page changed");
            }
            o.markdown = body.content ?? "";
            touch(o);
            return send(200, { version: `v${o.version}`, unchanged: 0 });
          }
        }
      }
      err(404, "route.not_found", `no route ${path}`);
    });
  };

  const srv = createServer(handler);
  await new Promise((ok, fail) => { srv.on("error", fail); srv.listen(0, "127.0.0.1", ok); });
  const port = srv.address().port;
  return {
    url: `http://127.0.0.1:${port}`,
    port,
    // seed puts a page in a space and returns its id.
    seed: (space, name, markdown) => add(space, name, markdown).id,
    // edit changes a page as the person would inside Any.
    edit: (id, name, markdown) => { const o = state.objs.get(id); o.name = name; o.markdown = markdown; touch(o); },
    bin: (id) => { state.objs.get(id).binned = true; },
    pages: () => [...state.objs.values()].map((o) => ({ ...o })),
    set refuse(v) { state.refuse = !!v; },
    get account() { return state.account; },
    set account(v) { state.account = v; },
    set conflictNext(v) { state.conflictNext = !!v; },
    pause: () => new Promise((done) => { srv.closeAllConnections?.(); srv.close(() => done()); }),
    resume: () => new Promise((ok, fail) => { srv.once("error", fail); srv.listen(port, "127.0.0.1", ok); }),
    requests: () => [...state.requests],
    forbidden: () => state.requests.filter((r) => r.startsWith("DELETE ") || r.includes("/shutdown") || /^[A-Z]+ \/v1\/auth/.test(r)),
    forget: () => { state.requests.length = 0; },
    reset: () => { state.objs.clear(); state.requests.length = 0; state.refuse = false; state.account = "e2e-account-1"; state.conflictNext = false; },
    close: () => new Promise((done) => { srv.closeAllConnections?.(); srv.close(() => done()); }),
  };
}
