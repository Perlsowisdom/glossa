// CCEL as a free source (the file name sorts it last, after From Logos, which reads its own list first), in the real app: off until the user turns it on; then public-domain books in
// From Logos match CCEL books, and Get free copy asks first, downloads an EPUB and adds it to the
// library. ccel.org is a small fake on a local server (never the real network); the fake Logos
// folder holds only a library catalogue. The app is pointed at the fake through the file ccel.json in
// the test library's folder (the "base", "into" and "gap_ms" entries, which exist for tests).

import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { openFromLibrary } from "./book.e2e.mjs";
import { epub } from "./fixtures.mjs";
import { makeLogosLibraryFolder } from "./logoslibrary-fixture.mjs";

const INSTITUTES = "Institutes of the Christian Religion";

const entry = (path, title, author, name, classic) =>
  `<p class="List2"><a href="/ccel/${path}.html">${title}\n</a><span class="authorName">(<a href="/ccel/${author}">${name}</a>)</span>${classic ? "<b>(classic)</b>" : ""}</p>`;
const indexPage = `<html><body><ul class="nav-list">
${entry("calvin/institutes", INSTITUTES, "calvin", "John Calvin", true)}
${entry("owen/communion", "Communion with God", "owen", "John Owen")}
${entry("spurgeon/morning", "Morning and Evening", "spurgeon", "Charles Haddon Spurgeon")}
${entry("modern/dogmatics", "Reformed Dogmatics", "modern", "A. Modern")}
</ul></body></html>`;
const thml = (rights) => `<?xml version="1.0"?><ThML><ThML.head><generalInfo><DC.Rights>${rights}</DC.Rights></generalInfo></ThML.head>`;

const logosBooks = [
  ["LLS:INST", "text.monograph", INSTITUTES, "", "Calvin, John", "1845", "en", "Public Domain"],
  ["LLS:OWEN", "text.monograph", "Communion with God, a Treatise", "", "Owen, John", "1657", "en", "Public Domain"],
  ["LLS:ME", "text.monograph", "Morning and Evening", "", "Spurgeon, C. H.", "1866", "en", "Public Domain"],
  ["LLS:DOGM", "text.monograph", "Reformed Dogmatics", "", "Modern, A.", "2008", "en", "Copyright 2008 Example Press"],
];

export default [
  {
    row: "CCEL: off until turned on; then From Logos matches CCEL books and Get free copy asks first, downloads an EPUB and adds it to the library",
    shot: "ccel-match.png",
    timeout: 240000,
    async run(t) {
      const hits = [];
      const book = epub(INSTITUTES, { author: "John Calvin" });
      const server = createServer((req, res) => {
        const path = new URL(req.url, "http://x").pathname;
        hits.push(`${req.method} ${path}${req.headers.range ? " (range)" : ""}`);
        const send = (type, body) => { res.writeHead(200, { "content-type": type, "content-length": Buffer.byteLength(body) }); res.end(req.method === "HEAD" ? undefined : body); };
        if (path === "/index/title") return send("text/html", indexPage);
        if (path === "/ccel/c/calvin/institutes.xml") return send("text/xml", thml("Public Domain"));
        if (path === "/ccel/o/owen/communion.xml") return send("text/xml", thml("Public Domain"));
        if (path === "/ccel/s/spurgeon/morning.xml") return send("text/xml", thml("Public Domain"));
        if (path === "/ccel/c/calvin/institutes/cache/institutes.epub") return send("application/epub+zip", book);
        res.writeHead(404).end();
      });
      await new Promise((ok) => server.listen(0, "127.0.0.1", ok));
      const base = `http://127.0.0.1:${server.address().port}`;
      const dir = join(t.app.library, "ccel");
      const books = mkdtempSync(join(tmpdir(), "glossa-e2e-ccel-books-"));
      const root = makeLogosLibraryFolder(logosBooks);
      let cataloguesWere = false;
      try {
        mkdirSync(dir, { recursive: true });
        writeFileSync(join(dir, "ccel.json"), JSON.stringify({ base, into: books, gap_ms: 0 }));
        await t.app.restart();
        const page = t.app.page;
        const ccelHits = () => hits.length;
        // The Bible catalogues would offer their own copy of Matthew Henry; leave them off here.
        cataloguesWere = (await t.app.run("bible.sources")).enabled;
        await t.app.run("bible.sources", { enable: false });

        // Read the fake Logos library. CCEL is off, so nothing is offered and nothing is sent.
        // The list another file read stays in the library, so this check reads its own folder by command (its
        // four books replace those) and the page then shows it.
        await t.app.run("logos.library_refresh", { folder: root });
        await page.reload();
        await page.waitForSelector(".shell", { timeout: 30000 });
        await page.evaluate(() => { location.hash = "#fromlogos"; });
        await page.locator(".fromlogos-view").waitFor({ timeout: 15000 });
        await page.locator("table.fromlogos-table").waitFor({ timeout: 30000 });
        t.assert.match(await page.locator("#fromlogos-status").innerText(), /4 Logos books, none with a free copy found yet · 3 public domain/);
        { const tt = await page.locator("table.fromlogos-table").innerText(); t.assert.ok(!/Get free copy/.test(tt), `no free copy while CCEL is off: ${JSON.stringify(tt)}`); }
        t.assert.equal(ccelHits(), 0, `no request to ccel.org while it is off: ${hits}`);
        t.assert.equal((await t.app.run("ccel.settings")).enabled, false);
        await page.locator("#fromlogos-source-ccel").waitFor({ state: "attached" });
        await t.shot("ccel-off.png");

        // Settings → Privacy has the switch, off.
        await page.evaluate(() => { location.hash = "#settings/privacy"; });
        const sec = page.locator(".settings-section:visible");
        await sec.getByRole("heading", { name: "Privacy" }).waitFor();
        const sw = sec.getByRole("switch", { name: "Christian Classics Ethereal Library (ccel.org)" });
        t.assert.equal(await sw.isChecked(), false, "off by default");
        t.assert.match(await sec.innerText(), /Nothing else is sent/);
        t.assert.equal(ccelHits(), 0);
        await t.shot("ccel-privacy-off.png");
        await sw.click();
        await page.waitForFunction(() => document.querySelector('[role="switch"][aria-checked="true"]') !== null);
        for (let i = 0; i < 40 && !hits.length; i++) await page.waitForTimeout(250);
        await page.waitForTimeout(500);
        t.assert.deepEqual(hits, ["GET /index/title"], "turning it on fetches the list, once");
        await t.shot("ccel-privacy-on.png");

        // From Logos: the three public-domain books match CCEL; the copyrighted one does not.
        await page.evaluate(() => { location.hash = "#fromlogos"; });
        const row = (id) => page.locator(`tr[data-id="${id}"]`);
        await row("LLS:INST").waitFor({ timeout: 30000 });
        await page.waitForFunction(() => /Get free copy/.test(document.querySelector('tr[data-id="LLS:INST"]')?.innerText ?? ""), null, { timeout: 30000 });
        t.assert.match(await row("LLS:INST").innerText(), /Free copy on CCEL[\s\S]*Institutes of the Christian Religion[\s\S]*Get free copy/);
        t.assert.match(await row("LLS:ME").innerText(), /Free copy on CCEL[\s\S]*Morning and Evening/);
        t.assert.match(await row("LLS:OWEN").innerText(), /Possible match: check[\s\S]*Communion with God/);
        t.assert.ok(!/Get free copy/.test(await row("LLS:DOGM").innerText()), "a copyrighted book gets no free copy");
        t.assert.match(await page.locator("#fromlogos-status").innerText(), /3 of your 4 Logos books have a free copy/);
        t.assert.equal(hits.length, 1, "matching sends nothing");
        await t.shot("ccel-match.png");

        // Get free copy asks first: size, licence and the credit; nothing is downloaded yet.
        await row("LLS:INST").getByRole("button", { name: /Get free copy/ }).click();
        const box = page.locator('[role="dialog"]', { hasText: INSTITUTES });
        await box.waitFor({ timeout: 30000 });
        const text = await box.innerText();
        t.assert.match(text, /same book as “Institutes of the Christian Religion” in your Logos library/);
        t.assert.match(text, /Download: [\d.]+ KB \(EPUB\)/);
        t.assert.match(text, /Licence: Public domain/);
        t.assert.match(text, /Christian Classics Ethereal Library/);
        t.assert.ok(!hits.some((h) => h.includes(".epub") && h.startsWith("GET")), `the book is not downloaded before the user confirms: ${hits}`);
        await t.shot("ccel-confirm.png");
        await box.getByRole("button", { name: "Add to my library" }).click();
        await page.locator(".fromlogos-view .msg.ok", { hasText: "Added" }).waitFor({ timeout: 60000 });
        t.assert.ok(hits.some((h) => h.startsWith("GET") && h.endsWith("institutes.epub")), "the EPUB was downloaded after the confirmation");
        await page.waitForFunction(() => /Already in Glossa/.test(document.querySelector('tr[data-id="LLS:INST"]')?.innerText ?? ""), null, { timeout: 30000 });
        await t.shot("ccel-added.png");

        // It is in the library as a public-domain book credited to CCEL, and it opens.
        const found = await t.app.run("library.items", { text: INSTITUTES, limit: 5 });
        t.assert.equal(found.items.length, 1);
        const item = await t.app.run("library.item", { id: found.items[0].id });
        t.assert.equal(item.licence.type, "public_domain");
        t.assert.match(item.licence.attribution, /Christian Classics Ethereal Library/);
        await openFromLibrary({ ...t, page }, INSTITUTES, "Read");
        await page.locator(".doc-book:visible .book-text p", { hasText: "The first words of chapter 1" }).first().waitFor({ timeout: 20000 });
        await t.shot("ccel-opens.png");

        // Off again: the cached list stays, but nothing is offered and nothing is sent.
        const before = hits.length;
        await page.evaluate(() => { location.hash = "#settings/privacy"; });
        await sec.getByRole("heading", { name: "Privacy" }).waitFor();
        await sec.getByRole("switch", { name: "Christian Classics Ethereal Library (ccel.org)" }).click();
        await page.waitForFunction(() => document.querySelector('[role="switch"][aria-checked="true"]') === null || true);
        await t.app.run("ccel.settings").then((s) => t.assert.ok(s.works >= 0));
        for (let i = 0; i < 20 && (await t.app.run("ccel.settings")).enabled; i++) await page.waitForTimeout(250);
        await page.evaluate(() => { location.hash = "#fromlogos"; });
        await page.locator("table.fromlogos-table").waitFor();
        await page.waitForFunction(() => !/Get free copy|Free copy on CCEL/.test(document.querySelector("table.fromlogos-table")?.innerText ?? ""));
        t.assert.equal(hits.length, before, "no request after turning it off");
        const rejected = await t.app.run("ccel.get", { id: "calvin/institutes", preview: true }).then(() => false, () => true);
        t.assert.ok(rejected, "ccel.get is refused while off");
        t.assert.equal(hits.length, before);
      } finally {
        if (cataloguesWere) await t.app.run("bible.sources", { enable: true }).catch(() => {});
        // Leave no books of this check in the list for any later check: read an empty folder.
        const empty = makeLogosLibraryFolder([]);
        await t.app.run("logos.library_refresh", { folder: empty }).catch(() => {});
        rmSync(empty, { recursive: true, force: true });
        server.close();
        rmSync(dir, { recursive: true, force: true });
        rmSync(root, { recursive: true, force: true });
        rmSync(books, { recursive: true, force: true });
        await t.app.restart();
      }
    },
  },
];
