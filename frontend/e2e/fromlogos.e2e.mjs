// T22c, the books in your Logos library, in the real app: Library → From Logos. The Logos folder is
// a small fake the test builds (logoslibrary-fixture.mjs) with only a library catalogue. "Open in
// Logos" is spied on, so nothing is opened. The free copy comes from a small fake SWORD repository
// on a local server, never the real network.

import { execFileSync } from "node:child_process";
import { createReadStream, copyFileSync, existsSync, mkdirSync, mkdtempSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { makeLogosLibraryFolder } from "./logoslibrary-fixture.mjs";

let root;

// same compares lists and says what it got, on one line.
const same = (t, got, want) => t.assert.ok(JSON.stringify(got) === JSON.stringify(want), `got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);

async function openView(t) {
  await t.page.evaluate(() => { location.hash = "#fromlogos"; });
  await t.page.locator(".fromlogos-view").waitFor({ timeout: 15000 });
}

const rows = (page, n) => page.waitForFunction((n) => document.querySelectorAll("table.fromlogos-table tbody tr").length === n, n);
const titles = (page) => page.locator("table.fromlogos-table tbody tr td:first-child strong").allInnerTexts();

export default [
  {
    row: "From Logos: read the library, list its books, search, sort and filter them, and Open in Logos is handed the logosres link",
    shot: "t22c-list.png",
    async run(t) {
      const { page } = t;
      root = makeLogosLibraryFolder();
      // A list another file read stays in the library for good (there is no way to forget it), so the first-read
      // path is only checked when nothing has read one yet; otherwise this check reads its own folder by command.
      const first = (await t.app.run("logos.library", {})).refreshed === false;
      if (!first) {
        await t.app.run("logos.library_refresh", { folder: root });
        await page.reload();
        await page.waitForSelector(".shell", { timeout: 30000 });
      }
      await openView(t);
      if (first) {
        t.assert.ok(await page.locator("#fromlogos-empty").isVisible(), "before reading there is an invitation to read the library");
        await t.shot("t22c-empty.png");
      }
      // The folder field is what Read and Refresh use (empty, they look where Logos keeps its data).
      await page.locator(".fromlogos-view details.more-options").evaluate((d) => { d.open = true; });
      await page.locator("#fromlogos-folder").fill(root);
      if (first) await page.locator("#fromlogos-read").click();
      await page.locator("table.fromlogos-table").waitFor({ timeout: 30000 });
      await rows(page, 8);
      t.assert.match(await page.locator("#fromlogos-status").innerText(), /8 books · 4 public domain/);
      // The default order is by title.
      same(t, (await titles(page)).slice(0, 3), ["A Commentary on Romans", "A Greek Lexicon of Tests", "A Lecture Series"]);
      await t.shot();

      // Search: words of the title, the author, the short name.
      const search = page.locator("#fromlogos-search");
      await search.fill("easton");
      await page.locator(`tr[data-id="LLS:EASTON"]`).waitFor();
      await rows(page, 1);
      same(t, await titles(page), ["Easton’s Bible Dictionary"]);
      await search.fill("barnes gospels");
      await page.locator(`tr[data-id="LLS:BARNES"]`).waitFor();
      await rows(page, 1);
      same(t, await titles(page), ["Notes on the Gospels"]);
      await search.fill("kjv");
      await page.locator(`tr[data-id="LLS:KJV"]`).waitFor();
      await rows(page, 1);
      await t.shot("t22c-search.png");

      // Open in Logos: the page's runtime is replaced by a spy, so nothing is opened.
      await page.evaluate(() => { window.__opened = []; window.runtime.BrowserOpenURL = (u) => { window.__opened.push(u); }; });
      await page.getByRole("button", { name: "Open King James Version in Logos" }).click();
      same(t, await page.evaluate(() => window.__opened), ["logosres:LLS:KJV"]);

      // Sort by author: books without one go last; a second click reverses.
      await search.fill("");
      await rows(page, 8);
      await page.locator('.th-sort[data-sort="author"]').click();
      await page.locator('th[aria-sort="ascending"] .th-sort[data-sort="author"]').waitFor();
      let order = await titles(page);
      same(t, [order[0], order[1], order.at(-1)], ["A Greek Lexicon of Tests", "Notes on the Gospels", "King James Version"]);
      await page.locator('.th-sort[data-sort="author"]').click();
      await page.locator('th[aria-sort="descending"] .th-sort[data-sort="author"]').waitFor();
      order = await titles(page);
      t.assert.equal(order[0], "A Lecture Series");
      await t.shot("t22c-sort.png");

      // Filters: one kind of book, then public domain only.
      await page.locator("#fromlogos-type").selectOption("Commentary");
      await rows(page, 2);
      await page.locator("#fromlogos-type").selectOption("");
      await rows(page, 8);
      await page.locator(".chip-toggle", { hasText: "Public domain only" }).click();
      await rows(page, 4);
      t.assert.ok((await titles(page)).includes("Sermons of a Faithful Pastor"), "an old book with no rights note counts, by its date");
      t.assert.match(await page.locator("table.fromlogos-table").innerText(), /Public domain, going by its date/);
      await page.locator(".chip-toggle", { hasText: "Public domain only" }).click();
      await rows(page, 8);

      // Refresh from Logos keeps the list and says nothing changed.
      await page.locator("#fromlogos-refresh").click();
      await page.locator(".fromlogos-view .msg.ok", { hasText: "no changes" }).waitFor({ timeout: 30000 });
    },
  },
  {
    row: "From Logos: a public-domain book matches a free copy, which downloads only after the user confirms",
    shot: "t22c-match.png",
    timeout: 240000,
    async run(t) {
      // This check reads the fake library itself, so it does not depend on the check before it.
      root ??= makeLogosLibraryFolder();
      await t.app.run("logos.library_refresh", { folder: root });
      // A small fake SWORD repository, served from this machine, becomes the CrossWire source.
      const repo = mkdtempSync(join(tmpdir(), "glossa-e2e-repo-"));
      execFileSync("go", ["run", "./internal/bible/sword/swordtest/genrepo", repo], { cwd: resolve(import.meta.dirname, "../.."), stdio: "pipe", timeout: 120000 });
      const hits = [];
      const server = createServer((req, res) => {
        const file = join(repo, decodeURIComponent(new URL(req.url, "http://x").pathname));
        hits.push(`${req.method} ${req.url}`);
        if (!file.startsWith(repo) || !existsSync(file) || !statSync(file).isFile()) { res.writeHead(404).end(); return; }
        res.writeHead(200, { "content-length": statSync(file).size });
        if (req.method === "HEAD") res.end(); else createReadStream(file).pipe(res);
      });
      await new Promise((ok) => server.listen(0, "127.0.0.1", ok));
      const url = `http://127.0.0.1:${server.address().port}`;
      const cat = join(t.app.library, "catalogues");
      const kept = `${cat}-kept`; // the library's own catalogues, put back afterwards
      try {
        if (existsSync(cat)) renameSync(cat, kept);
        mkdirSync(cat, { recursive: true });
        copyFileSync(join(repo, "mods.d.tar.gz"), join(cat, "crosswire.mods.d.tar.gz"));
        writeFileSync(join(cat, "state.json"), JSON.stringify({ enabled: true, sources: [{ id: "crosswire", name: "CrossWire", kind: "sword", url, items: 1 }] }));
        await t.app.restart();
        const page = t.app.page;

        // The list read earlier is still there after the restart; the free copy is found at once.
        await page.evaluate(() => { location.hash = "#fromlogos"; });
        const row = page.locator('tr[data-id="LLS:KJV"]');
        await row.waitFor({ timeout: 30000 });
        t.assert.match(await row.innerText(), /Certain[\s\S]*King James Version[\s\S]*Get free copy/);
        t.assert.match(await page.locator("#fromlogos-status").innerText(), /1 with a free copy/);
        // A public-domain book with no free copy known says so and offers nothing to download.
        const easton = await page.locator('tr[data-id="LLS:EASTON"]').innerText();
        t.assert.match(easton, /No free copy known/);
        t.assert.ok(!/Get free copy/.test(easton));
        await t.shot("t22c-match.png");

        // Nothing has been downloaded yet; Get free copy asks first.
        t.assert.ok(!hits.some((h) => h.includes("/modules/")), `nothing downloaded before the user confirms: ${hits}`);
        await row.getByRole("button", { name: /Get free copy/ }).click();
        const box = page.locator('[role="dialog"]', { hasText: "King James Version" });
        await box.waitFor({ timeout: 30000 });
        t.assert.match(await box.innerText(), /same book as “King James Version” in your Logos library/);
        t.assert.ok(!hits.some((h) => h.includes("/modules/")), "still nothing downloaded while the dialog is open");
        await t.shot("t22c-confirm.png");
        await box.getByRole("button", { name: "Install", exact: true }).click();
        await page.locator(".fromlogos-view .msg.ok", { hasText: "Installed King James Version" }).waitFor({ timeout: 60000 });
        t.assert.ok(hits.some((h) => h.includes("/modules/")), "the module was downloaded after the confirmation");
        t.assert.match(await page.locator('tr[data-id="LLS:KJV"]').innerText(), /Already in Glossa/);
        const mods = await t.app.run("bible.modules", { kind: "all" });
        const kjv = mods.find((m) => m.name === "King James Version");
        t.assert.ok(kjv, "the Bible is installed");
        await t.shot("t22c-installed.png");
        await t.app.run("bible.remove", { module: kjv.id });
      } finally {
        // Leave the library as found: its own catalogues back, no fake repository.
        rmSync(cat, { recursive: true, force: true });
        if (existsSync(kept)) renameSync(kept, cat);
        server.close();
        rmSync(repo, { recursive: true, force: true });
        rmSync(root, { recursive: true, force: true });
        root = undefined;
        // Leave no books of these checks in the list for any later check: read an empty folder.
        const empty = makeLogosLibraryFolder([]);
        await t.app.run("logos.library_refresh", { folder: empty }).catch(() => {});
        rmSync(empty, { recursive: true, force: true });
        await t.app.restart();
      }
    },
  },
];
