// A fake Any for the real-app checks: the part of the local API of the Any app that Glossa uses
// (/v1/auth/challenges, /v1/auth/api_keys, /v1/spaces, search, objects, properties), version
// 2025-11-08. The app is started with GLOSSA_ANY_URL pointing here (see app.mjs), so a test never
// reaches the real Any app on port 31009, whatever is running on the computer.
//
//   CODE: the only 4-digit code it accepts.
//   edit(): changes a page the way the person would inside Any.

import { createServer } from "node:http";

export const FAKE_CODE = "4321";
const KEY = "e2e-fake-any-key";
const VERSION = "2025-11-08";

export async function startFakeAny() {
  const state = { objs: new Map(), props: new Set(), requests: [], rev: 0, next: 0, running: true, versionOk: true };
  const stamp = (n) => `2026-01-01T00:${String(Math.floor(n / 60)).padStart(2, "0")}:${String(n % 60).padStart(2, "0")}Z`;
  const wire = (o, body) => ({
    id: o.id, space_id: o.space, name: o.name, archived: false, type: { key: "page" }, snippet: o.markdown.slice(0, 80),
    properties: [{ key: "last_modified_date", date: stamp(o.rev) }, ...Object.entries(o.props).map(([key, text]) => ({ key, text }))],
    ...(body ? { markdown: o.markdown } : {}),
  });
  const touch = (o) => { o.rev = ++state.rev; };
  const add = (space, name, markdown) => {
    const o = { id: `obj-${++state.next}`, space, name, markdown, props: {}, rev: 0 };
    touch(o);
    state.objs.set(o.id, o);
    return o;
  };
  const apply = (o, body, field) => {
    if (typeof body.name === "string") o.name = body.name;
    if (typeof body[field] === "string") o.markdown = body[field];
    for (const p of body.properties ?? []) o.props[p.key] = p.text;
    touch(o);
  };

  const handler = (req, res) => {
    const url = new URL(req.url, "http://x");
    const path = url.pathname;
    state.requests.push(`${req.method} ${path}`);
    let raw = "";
    req.on("data", (c) => { raw += c; });
    req.on("end", () => {
      const send = (code, body) => { res.writeHead(code, { "Content-Type": "application/json" }); res.end(JSON.stringify(body)); };
      const err = (code, message) => send(code, { object: "error", status: code, message });
      const page = (items) => {
        const off = Number(url.searchParams.get("offset") ?? 0);
        const lim = Math.min(Number(url.searchParams.get("limit") ?? 100), 100);
        send(200, { data: items.slice(off, off + lim), pagination: { total: items.length, offset: off, limit: lim, has_more: off + lim < items.length } });
      };
      if (req.headers["anytype-version"] !== VERSION) { state.versionOk = false; return err(400, "wrong Anytype-Version"); }
      const body = raw ? JSON.parse(raw) : {};
      if (path === "/v1/auth/challenges" && req.method === "POST") return send(201, { challenge_id: "e2e-challenge" });
      if (path === "/v1/auth/api_keys" && req.method === "POST") {
        return body.challenge_id === "e2e-challenge" && body.code === FAKE_CODE ? send(201, { api_key: KEY }) : err(400, "wrong code");
      }
      if (req.headers.authorization !== `Bearer ${KEY}`) return err(401, "unauthorized");
      const seg = path.split("/");
      if (path === "/v1/spaces") return page([{ id: "space-study", name: "Study" }, { id: "space-church", name: "Church" }]);
      if (path.endsWith("/properties") && req.method === "GET") return page([...state.props].filter((k) => k.startsWith(`${seg[3]}/`)).map((k) => ({ key: k.split("/")[1] })));
      if (path.endsWith("/properties") && req.method === "POST") { state.props.add(`${seg[3]}/${body.key}`); return send(201, { property: { key: body.key } }); }
      if (path.endsWith("/search") && req.method === "POST") {
        const q = (body.query ?? "").toLowerCase();
        const list = [...state.objs.values()].filter((o) => o.space === seg[3] && (!q || `${o.name} ${o.markdown}`.toLowerCase().includes(q))).sort((a, b) => b.rev - a.rev);
        return page(list.map((o) => wire(o, false)));
      }
      if (path.endsWith("/objects") && req.method === "POST") {
        const o = add(seg[3], "", "");
        apply(o, body, "body");
        return send(201, { object: wire(o, true) });
      }
      if (seg[4] === "objects" && seg[5]) {
        const o = state.objs.get(seg[5]);
        if (!o || o.space !== seg[3]) return err(404, "not found");
        if (req.method === "PATCH") apply(o, body, "markdown");
        return send(200, { object: wire(o, true) });
      }
      err(404, `no route ${path}`);
    });
  };

  const srv = createServer(handler);
  await new Promise((ok, fail) => { srv.on("error", fail); srv.listen(0, "127.0.0.1", ok); });
  return {
    url: `http://127.0.0.1:${srv.address().port}`,
    code: FAKE_CODE,
    key: KEY,
    // seed puts a page in a space and returns its id.
    seed: (space, name, markdown) => add(space, name, markdown).id,
    // edit changes a page as the person would inside Any.
    edit: (id, name, markdown) => { const o = state.objs.get(id); o.name = name; o.markdown = markdown; touch(o); },
    pages: () => [...state.objs.values()].map((o) => ({ ...o })),
    get versionOk() { return state.versionOk; },
    requests: () => [...state.requests],
    forget: () => { state.requests.length = 0; },
    reset: () => { state.objs.clear(); state.props.clear(); state.requests.length = 0; state.versionOk = true; },
    close: () => new Promise((done) => { srv.closeAllConnections?.(); srv.close(() => done()); }),
  };
}
