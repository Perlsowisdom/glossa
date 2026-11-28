// T43, more free sources for your Logos books, in the real app (the file name sorts it after zccel).
// From Logos matches each book to the best free copy: a Bible module (a fake SWORD repository), CCEL
// and Project Gutenberg (two small fake servers on this machine, never the real network). Checks the
// summary line, the filters, the "Look for free copies on" switches, a Gutenberg Get free copy that
// adds a book, an Install for the module, the Internet Archive link (caught, nothing opened), Get all
// free copies, and that with Gutenberg off no request reaches the fake server. The fake Logos folder
// holds only a library catalogue. The app is pointed at the fakes through ccel.json and gutenberg.json
// in the test library's folders (the "base", "into" and "gap_ms" entries, which exist for tests).

import { execFileSync } from "node:child_process";
import { copyFileSync, createReadStream, existsSync, mkdirSync, mkdtempSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { gzipSync } from "node:zlib";
import { epub } from "./fixtures.mjs";
import { makeLogosLibraryFolder } from "./logoslibrary-fixture.mjs";

const INSTITUTES = "The Bruised Reed";
const PILGRIM = "The Holy War";

const logosBooks = [
  ["LLS:KJV", "text.monograph.bible", "King James Version", "KJV", "", "1900", "en", "Public Domain"],
  ["LLS:PILGRIM", "text.monograph", PILGRIM, "", "Bunyan, John", "1678", "en", "Public Domain"],
  ["LLS:INST", "text.monograph", INSTITUTES, "", "Sibbes, Richard", "1630", "en", "Public Domain"],
  ["LLS:MERE", "text.monograph", "The Weight of Glory Revisited", "", "Example, Anna", "1999", "en", "Copyright 1999 Example Press"],
];

const catalogue = `Text#,Type,Issued,Title,Language,Authors,Subjects,LoCC,Bookshelves
45001,Text,2014-01-02,${INSTITUTES},en,"Sibbes, Richard, 1577-1635",Theology,BX,
45002,Text,2010-05-06,The Holy War,en,"Bunyan, John, 1628-1688",Allegories,PR,
45003,Sound,2012-03-03,Holy War audio,en,"Bunyan, John, 1628-1688",,,
`;
const ccelPage = `<html><body><ul class="nav-list">
<p class="List2"><a href="/ccel/sibbes/bruised.html">${INSTITUTES}\n</a><span class="authorName">(<a href="/ccel/sibbes">Richard Sibbes</a>)</span><b>(classic)</b></p>
</ul></body></html>`;
const thml = `<?xml version="1.0"?><ThML><ThML.head><generalInfo><DC.Rights>Public Domain</DC.Rights></generalInfo></ThML.head>`;

export default [
  {
    row: "More free sources: From Logos finds free copies on a Bible module, CCEL and Project Gutenberg, with filters, a summary, bulk download and an Internet Archive link; Gutenberg sends nothing while off",
    shot: "t43-sources.png",
    timeout: 360000,
    async run(t) {
      const gHits = [];
      const cHits = [];
      const pilgrim = epub(PILGRIM, { author: "John Bunyan" });
      const inst = epub(INSTITUTES, { author: "Richard Sibbes" });
      const send = (req, res, type, body) => { res.writeHead(200, { "content-type": type, "content-length": Buffer.byteLength(body) }); res.end(req.method === "HEAD" ? undefined : body); };
      const gServer = createServer((req, res) => {
        const path = new URL(req.url, "http://x").pathname;
        gHits.push(`${req.method} ${path} ${req.headers["user-agent"]}`);
        if (path === "/cache/epub/feeds/pg_catalog.csv.gz") return send(req, res, "application/gzip", gzipSync(catalogue));
        if (path === "/cache/epub/45002/pg45002-images-3.epub") return send(req, res, "application/epub+zip", pilgrim);
        res.writeHead(404).end();
      });
      const cServer = createServer((req, res) => {
        const path = new URL(req.url, "http://x").pathname;
        cHits.push(`${req.method} ${path}`);
        if (path === "/index/title") return send(req, res, "text/html", ccelPage);
        if (path === "/ccel/s/sibbes/bruised.xml") return send(req, res, "text/xml", thml);
        if (path === "/ccel/s/sibbes/bruised/cache/bruised.epub") return send(req, res, "application/epub+zip", inst);
        res.writeHead(404).end();
      });
      await Promise.all([gServer, cServer].map((s) => new Promise((ok) => s.listen(0, "127.0.0.1", ok))));
      const gBase = `http://127.0.0.1:${gServer.address().port}`;
      const cBase = `http://127.0.0.1:${cServer.address().port}`;

      // A small fake SWORD repository gives the Bible-module copy (as the fromlogos check does).
      const repo = mkdtempSync(join(tmpdir(), "glossa-e2e-repo-"));
      execFileSync("go", ["run", "./internal/bible/sword/swordtest/genrepo", repo], { cwd: resolve(import.meta.dirname, "../.."), stdio: "pipe", timeout: 120000 });
      const mHits = [];
      const mServer = createServer((req, res) => {
        const file = join(repo, decodeURIComponent(new URL(req.url, "http://x").pathname));
        mHits.push(`${req.method} ${req.url}`);
        if (!file.startsWith(repo) || !existsSync(file) || !statSync(file).isFile()) { res.writeHead(404).end(); return; }
        res.writeHead(200, { "content-length": statSync(file).size });
        if (req.method === "HEAD") res.end(); else createReadStream(file).pipe(res);
      });
      await new Promise((ok) => mServer.listen(0, "127.0.0.1", ok));
      const mUrl = `http://127.0.0.1:${mServer.address().port}`;

      const gDir = join(t.app.library, "gutenberg");
      const cDir = join(t.app.library, "ccel");
      const cat = join(t.app.library, "catalogues");
      const kept = `${cat}-kept`;
      const books = mkdtempSync(join(tmpdir(), "glossa-e2e-t43-books-"));
      const root = makeLogosLibraryFolder(logosBooks);
      try {
        mkdirSync(gDir, { recursive: true });
        mkdirSync(cDir, { recursive: true });
        writeFileSync(join(gDir, "gutenberg.json"), JSON.stringify({ base: gBase, into: books, gap_ms: 0 }));
        writeFileSync(join(cDir, "ccel.json"), JSON.stringify({ base: cBase, into: books, gap_ms: 0 }));
        if (existsSync(cat)) renameSync(cat, kept);
        mkdirSync(cat, { recursive: true });
        copyFileSync(join(repo, "mods.d.tar.gz"), join(cat, "crosswire.mods.d.tar.gz"));
        writeFileSync(join(cat, "state.json"), JSON.stringify({ enabled: true, sources: [{ id: "crosswire", name: "CrossWire", kind: "sword", url: mUrl, items: 1 }] }));
        await t.app.restart();
        const page = t.app.page;
        await t.app.resize(1536, 816);
        await t.app.run("logos.library_refresh", { folder: root });
        await page.reload();
        await page.waitForSelector(".shell", { timeout: 30000 });

        // Both internet sources start off; only the module (from the cached Bible catalogue) is found.
        await page.evaluate(() => { location.hash = "#fromlogos"; });
        await page.locator("table.fromlogos-table").waitFor({ timeout: 30000 });
        const row = (id) => page.locator(`tr[data-id="${id}"]`);
        const status = page.locator("#fromlogos-status");
        await page.waitForFunction(() => /1 of your 4 Logos books have a free copy/.test(document.querySelector("#fromlogos-status")?.innerText ?? ""), null, { timeout: 30000 });
        t.assert.match(await row("LLS:KJV").innerText(), /Free as a Bible module[\s\S]*King James Version[\s\S]*Install/);
        t.assert.match(await row("LLS:PILGRIM").innerText(), /Not free[\s\S]*Search Internet Archive/);
        t.assert.equal(await page.locator("#fromlogos-source-gutenberg").isChecked(), false);
        t.assert.equal(await page.locator("#fromlogos-source-ccel").isChecked(), false);
        t.assert.equal(gHits.length + cHits.length, 0, `nothing sent while the switches are off: ${gHits} ${cHits}`);
        t.assert.equal((await t.app.run("gutenberg.settings")).enabled, false);
        await t.shot("t43-off.png");

        // Turning Gutenberg on asks first, says what it sends, and then fetches the catalogue once.
        await page.locator('label.chip-toggle:has(#fromlogos-source-gutenberg)').click();
        const ask = page.locator('[role="dialog"]', { hasText: "Use Project Gutenberg?" });
        await ask.waitFor();
        t.assert.match(await ask.innerText(), /gutenberg\.org for its catalogue file[\s\S]*nothing about your Logos library/);
        t.assert.equal(gHits.length, 0, "nothing is sent before the user agrees");
        await t.shot("t43-ask.png");
        await ask.getByRole("button", { name: "Turn on" }).click();
        await page.waitForFunction(() => /Holy War/.test(document.querySelector('tr[data-id="LLS:PILGRIM"]')?.innerText ?? "") && /Free copy on Project Gutenberg/.test(document.querySelector('tr[data-id="LLS:PILGRIM"]')?.innerText ?? ""), null, { timeout: 30000 });
        t.assert.deepEqual(gHits, ["GET /cache/epub/feeds/pg_catalog.csv.gz Glossa"], "the catalogue is fetched once, as Glossa");
        t.assert.match(await row("LLS:PILGRIM").innerText(), /Free copy on Project Gutenberg[\s\S]*Get free copy/);
        t.assert.equal(await page.locator("#fromlogos-source-gutenberg").isChecked(), true);
        t.assert.match(await status.innerText(), /3 of your 4 Logos books have a free copy/);

        // CCEL too: it is preferred where both have the same book, and the other copy is listed.
        await page.locator('label.chip-toggle:has(#fromlogos-source-ccel)').click();
        await page.locator('[role="dialog"]', { hasText: "Use the Christian Classics Ethereal Library?" }).getByRole("button", { name: "Turn on" }).click();
        await page.waitForFunction(() => /Free copy on CCEL/.test(document.querySelector('tr[data-id="LLS:INST"]')?.innerText ?? ""), null, { timeout: 30000 });
        t.assert.match(await row("LLS:INST").innerText(), /Free copy on CCEL[\s\S]*Other free copies: Project Gutenberg/);
        t.assert.match(await status.innerText(), /3 of your 4 Logos books have a free copy/);
        t.assert.match(await row("LLS:MERE").innerText(), /Not free/, "a modern copyrighted book gets no free copy");
        t.assert.equal(gHits.length, 1);
        await t.shot("t43-sources.png");

        // Filters.
        const state = page.locator("#fromlogos-state");
        const rows = (n) => page.waitForFunction((n) => document.querySelectorAll("table.fromlogos-table tbody tr").length === n, n);
        await state.getByText("Not free", { exact: true }).click();
        await rows(1);
        t.assert.match(await page.locator("table.fromlogos-table").innerText(), /The Weight of Glory Revisited/);
        await state.getByText("Already in Glossa", { exact: true }).click();
        await page.locator(".fromlogos-view .empty", { hasText: "No books match" }).waitFor();
        await state.getByText("Free copy found", { exact: true }).click();
        await rows(3);
        await state.getByText("All", { exact: true }).click();
        await rows(4);

        // The Internet Archive link opens the browser (caught here) and sends nothing from Glossa.
        await page.evaluate(() => { window.__opened = []; window.runtime.BrowserOpenURL = (u) => { window.__opened.push(u); }; });
        const before = gHits.length + cHits.length + mHits.length;
        await row("LLS:MERE").getByRole("button", { name: /Search Internet Archive/ }).click();
        t.assert.deepEqual(await page.evaluate(() => window.__opened), ["https://archive.org/search?query=The+Weight+of+Glory+Revisited+Example%2C+Anna"]);
        t.assert.equal(gHits.length + cHits.length + mHits.length, before, "the link sends nothing from the app");
        await t.shot("t43-archive.png");

        // Get free copy from Gutenberg: size and licence first, the book only after the user agrees.
        await row("LLS:PILGRIM").getByRole("button", { name: /Get free copy/ }).click();
        const box = page.locator('[role="dialog"]', { hasText: "Get The Holy War" });
        await box.waitFor({ timeout: 30000 });
        const text = await box.innerText();
        t.assert.match(text, /Download: [\d.]+ KB \(EPUB\) from Project Gutenberg/);
        t.assert.match(text, /Licence: Public domain \(US\)[\s\S]*Project Gutenberg \(gutenberg\.org\)/);
        t.assert.ok(!gHits.some((h) => h.startsWith("GET") && h.includes(".epub")), `not downloaded before the user confirms: ${gHits}`);
        await t.shot("t43-confirm.png");
        await box.getByRole("button", { name: "Add to my library" }).click();
        await page.locator(".fromlogos-view .msg.ok", { hasText: "Added" }).waitFor({ timeout: 60000 });
        t.assert.ok(gHits.some((h) => h.startsWith("GET /cache/epub/45002/pg45002-images-3.epub")), "the EPUB was downloaded after the confirmation");
        t.assert.ok(gHits.every((h) => h.endsWith(" Glossa")), "every request says it is Glossa");
        await page.waitForFunction(() => /Already in Glossa/.test(document.querySelector('tr[data-id="LLS:PILGRIM"]')?.innerText ?? ""), null, { timeout: 30000 });
        const found = await t.app.run("library.items", { text: "Holy War", limit: 5 });
        t.assert.equal(found.items.length, 1);
        const item = await t.app.run("library.item", { id: found.items[0].id });
        t.assert.equal(item.licence.type, "public_domain");
        t.assert.match(item.licence.attribution, /Project Gutenberg \(gutenberg\.org\)/);
        await t.shot("t43-added.png");

        // Install for the module match.
        await row("LLS:KJV").getByRole("button", { name: /Install/ }).click();
        const mbox = page.locator('[role="dialog"]', { hasText: "King James Version" });
        await mbox.waitFor({ timeout: 30000 });
        await mbox.getByRole("button", { name: "Install", exact: true }).click();
        await page.locator(".fromlogos-view .msg.ok", { hasText: "Installed King James Version" }).waitFor({ timeout: 60000 });
        t.assert.match(await row("LLS:KJV").innerText(), /Already in Glossa/);

        // Get all free copies: lists what will be downloaded, asks once, then gets them one at a time.
        await page.locator("#fromlogos-bulk-start").click();
        const bulk = page.locator('[role="dialog"]', { hasText: "Get all free copies?" });
        await bulk.waitFor({ timeout: 30000 });
        t.assert.match(await bulk.innerText(), /1 book: 1 from CCEL/);
        t.assert.ok(!cHits.some((h) => h.endsWith(".epub")), "nothing is downloaded until the user agrees");
        await t.shot("t43-bulk.png");
        await bulk.getByRole("button", { name: /Get 1 book/ }).click();
        await page.locator(".fromlogos-view .msg.ok", { hasText: "Added 1 of 1 book" }).waitFor({ timeout: 90000 });
        await page.waitForFunction(() => /Already in Glossa/.test(document.querySelector('tr[data-id="LLS:INST"]')?.innerText ?? ""), null, { timeout: 30000 });
        t.assert.match(await status.innerText(), /3 of your 4 Logos books have a free copy · 3 already in Glossa/);
        await t.shot("t43-done.png");

        // Settings → Privacy shows both switches; turning Gutenberg off stops every request to it.
        await page.evaluate(() => { location.hash = "#settings/privacy"; });
        const sec = page.locator(".settings-section:visible");
        await sec.getByRole("heading", { name: "Privacy" }).waitFor();
        const sw = sec.getByRole("switch", { name: "Project Gutenberg (gutenberg.org)" });
        t.assert.equal(await sw.isChecked(), true);
        t.assert.match(await sec.innerText(), /asks gutenberg\.org for its catalogue file/);
        await t.shot("t43-privacy.png");
        await sw.click();
        for (let i = 0; i < 20 && (await t.app.run("gutenberg.settings")).enabled; i++) await page.waitForTimeout(250);
        const gOff = gHits.length;
        await page.evaluate(() => { location.hash = "#fromlogos"; });
        await page.locator("table.fromlogos-table").waitFor();
        await page.reload();
        await page.waitForSelector(".shell", { timeout: 30000 });
        await page.evaluate(() => { location.hash = "#fromlogos"; });
        await page.locator("table.fromlogos-table").waitFor({ timeout: 30000 });
        await page.waitForFunction(() => document.querySelector("#fromlogos-source-gutenberg")?.checked === false);
        t.assert.equal(gHits.length, gOff, "no request to Project Gutenberg after turning it off");
        const refused = await t.app.run("gutenberg.get", { id: "45002", preview: true }).then(() => false, () => true);
        t.assert.ok(refused, "gutenberg.get is refused while off");
        t.assert.equal(gHits.length, gOff);
        await t.shot("t43-gutenberg-off.png");
      } finally {
        await t.app.run("gutenberg.settings", { enable: false }).catch(() => {});
        await t.app.run("ccel.settings", { enable: false }).catch(() => {});
        for (const m of await t.app.run("bible.modules", { kind: "all" }).catch(() => [])) {
          if (m.name === "King James Version") await t.app.run("bible.remove", { module: m.id }).catch(() => {});
        }
        const empty = makeLogosLibraryFolder([]);
        await t.app.run("logos.library_refresh", { folder: empty }).catch(() => {});
        rmSync(empty, { recursive: true, force: true });
        rmSync(cat, { recursive: true, force: true });
        if (existsSync(kept)) renameSync(kept, cat);
        for (const s of [gServer, cServer, mServer]) s.close();
        rmSync(gDir, { recursive: true, force: true });
        rmSync(cDir, { recursive: true, force: true });
        rmSync(repo, { recursive: true, force: true });
        rmSync(root, { recursive: true, force: true });
        rmSync(books, { recursive: true, force: true });
        await t.app.restart();
      }
    },
  },
];
