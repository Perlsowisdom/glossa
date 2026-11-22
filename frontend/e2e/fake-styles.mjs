// A fake styles repository for the real-app checks: the two places Glossa reads to download citation styles
// (the list of style names, and one raw style file). The app is started with GLOSSA_CSL_LIST_URL and
// GLOSSA_CSL_RAW_URL pointing here (see app.mjs), so a test never reaches GitHub.

import { createServer } from "node:http";

const TINY = `<?xml version="1.0" encoding="utf-8"?>
<style xmlns="http://purl.org/net/xbiblio/csl" class="note" version="1.0">
  <info>
    <title>Ibid notes</title>
    <id>http://www.zotero.org/styles/ibid-notes</id>
    <author><name>Test Author</name></author>
    <updated>2020-01-01T00:00:00+00:00</updated>
    <rights license="http://creativecommons.org/licenses/by-sa/3.0/">This work is licensed under a Creative Commons Attribution-ShareAlike 3.0 License</rights>
  </info>
  <citation>
    <layout suffix=".">
      <choose>
        <if position="ibid-with-locator"><group delimiter=", "><text term="ibid"/><text variable="locator"/></group></if>
        <else-if position="ibid"><text term="ibid"/></else-if>
        <else-if position="subsequent"><group delimiter=", "><names variable="author"><name form="short"/></names><text variable="title" form="short"/></group></else-if>
        <else><group delimiter=", "><names variable="author"/><text variable="title" font-style="italic"/></group></else>
      </choose>
    </layout>
  </citation>
</style>`;

const DEPENDENT = `<?xml version="1.0" encoding="utf-8"?>
<style xmlns="http://purl.org/net/xbiblio/csl" class="note" version="1.0">
  <info>
    <title>Ibid notes for a journal</title>
    <id>http://www.zotero.org/styles/ibid-notes-journal</id>
    <link href="http://www.zotero.org/styles/ibid-notes" rel="independent-parent"/>
    <rights license="http://creativecommons.org/licenses/by-sa/3.0/">CC BY-SA 3.0</rights>
  </info>
</style>`;

const FILES = { "ibid-notes.csl": TINY, "dependent/ibid-notes-journal.csl": DEPENDENT };

export async function startFakeStyles() {
  const requests = [];
  const srv = createServer((req, res) => {
    requests.push(`${req.method} ${req.url}`);
    const path = new URL(req.url, "http://x").pathname;
    const json = (body) => { res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify(body)); };
    if (path === "/tree/master") return json({ tree: [{ path: "ibid-notes.csl", type: "blob" }, { path: "apa.csl", type: "blob" }, { path: "dependent", type: "tree", sha: "DEP" }] });
    if (path === "/tree/DEP") return json({ tree: [{ path: "ibid-notes-journal.csl", type: "blob" }] });
    if (path.startsWith("/raw/") && FILES[path.slice(5)]) { res.writeHead(200, { "Content-Type": "text/plain" }); return res.end(FILES[path.slice(5)]); }
    res.writeHead(404);
    res.end();
  });
  await new Promise((ok, fail) => { srv.on("error", fail); srv.listen(0, "127.0.0.1", ok); });
  const base = `http://127.0.0.1:${srv.address().port}`;
  return {
    listUrl: `${base}/tree`,
    rawUrl: `${base}/raw`,
    requests: () => [...requests],
    forget: () => { requests.length = 0; },
    close: () => new Promise((done) => { srv.closeAllConnections?.(); srv.close(() => done()); }),
  };
}
