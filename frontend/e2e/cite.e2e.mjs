// T40b part 2 in the real app: cite a library book and a Zotero item (the fake Zotero), see the chips, put in a
// bibliography, export and read the footnotes and bibliography, switch the style and see it change; and Settings →
// Citations with its download switch (the fake styles repository, never GitHub). Screenshots at 1536 x 816 and
// 900 x 700, light and dark: c40b-<what>-<size>-<theme>.png.

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { addBooks } from "./book.e2e.mjs";

const SIZES = [[1536, 816], [900, 700]];
const THEMES = ["light", "dark"];

async function insertMenu(t, label) {
  await t.page.click(`${t.root} [data-tool=insert]`);
  const menu = t.page.locator(".popover .insert-menu");
  await menu.waitFor();
  await menu.locator("button", { hasText: label }).click();
}

// cite fills the picker that is open: search, choose the first match, give the place, insert.
async function cite(t, query, place) {
  const pop = t.page.locator(".cite-pop");
  await pop.waitFor();
  await pop.locator("input[type=search]").fill(query);
  const row = pop.locator(".cite-row", { hasText: query }).first();
  await row.waitFor();
  await row.click();
  await pop.locator("input[type=text]").first().fill(place);
  await pop.getByRole("button", { name: /Insert citation|Save/ }).click();
  await pop.waitFor({ state: "detached", timeout: 8000 }).catch(async (e) => { throw new Error(`picker stayed open: ${await pop.innerText()} :: ${e.message}`); });
}

const chipTexts = (t) => t.editor().locator(".cite-chip-text").allTextContents();

export default [
  {
    row: "Citations: cite a library book and a Zotero item, see the chips, add a bibliography, export footnotes, switch the style",
    shot: "c40b-picker.png",
    timeout: 420000,
    async run(t) {
      const { app, page } = t;
      await t.reset();
      const { epub } = await addBooks(t);
      await app.run("connect.enable", { id: "zotero" });
      await t.openNote("", "Cite note");
      await t.focusEnd();
      await t.type("Grace is plain ");

      // Insert → Citation: a library book, page 12.
      await insertMenu(t, "Citation");
      await page.locator(".cite-pop").waitFor();
      await t.shot("c40b-picker.png");
      await cite(t, "E2E Test", "12");
      await page.locator(`${t.root} .cite-chip`).first().waitFor();
      await t.type("and Romans ");

      // /cite: a Zotero item, page 5.
      await t.type("/cite");
      await page.keyboard.press("Enter");
      await cite(t, "Epistle", "5");
      await t.press("End");
      await t.type(" ");
      await insertMenu(t, "Bibliography");
      await page.locator(`${t.root} .cite-bib`).waitFor();

      // Short forms replace the stand-ins; the bibliography lists both works.
      await page.waitForFunction((root) => [...document.querySelectorAll(`${root} .cite-chip-text`)].every((e) => e.textContent !== "source" && !e.textContent.startsWith("source,")), t.root, { timeout: 15000 });
      const labels = await chipTexts(t);
      t.assert.equal(labels.length, 2, `two chips: ${labels}`);
      t.assert.ok(labels[0].includes("E2E Test Book") && labels[0].includes("12"), labels[0]);
      t.assert.ok(labels[1].includes("Moo") && labels[1].includes("5"), labels[1]);
      await page.waitForFunction((root) => document.querySelectorAll(`${root} .cite-bib-entry`).length === 2, t.root, { timeout: 15000 });
      t.assert.equal((await app.run("zotero.cached")).items.length, 1, "the Zotero item is kept for offline");

      // The Markdown holds readable citations and the marker; clicking a chip edits it.
      const md = await t.saved();
      t.assert.ok(md.includes(`[@lib:${epub.id}, p. 12]`), md);
      t.assert.ok(md.includes("[@zotero:AAAAAAAA, p. 5]") && md.includes("<!-- bibliography -->"), md);
      await t.editor().locator(".cite-chip").first().click();
      const pop = page.locator(".cite-pop");
      await pop.waitFor();
      t.assert.ok((await pop.innerText()).toLowerCase().includes("change this citation"));
      t.assert.equal(await pop.locator("input[type=text]").first().inputValue(), "12");
      await page.keyboard.press("Escape");
      await pop.waitFor({ state: "detached" });

      // Export in the default style (SBL): footnotes and a bibliography.
      const note = { kind: "note", id: t.id };
      const made = async (options) => readFileSync((await app.run("export.make", { sources: [note], format: "md", options, replace: true,
        path: join(dirname(app.library), `cite-${Date.now()}.md`) })).path, "utf8");
      let out = await made({});
      t.assert.ok(/plain\[\^1\]/.test(out) && out.includes("Romans[^2]"), out);
      t.assert.ok(out.includes("[^2]:") && out.includes("### Bibliography") && out.includes("Eerdmans"), out);

      // Switch the style from the ⋯ menu: author and date in the text, a list of references.
      await page.locator(`${t.root} .page-more`).click();
      await page.getByRole("menuitem", { name: "Citation style…" }).click();
      await page.locator(".cite-style-menu").getByRole("menuitem", { name: /^APA/ }).click();
      await page.waitForFunction((root) => [...document.querySelectorAll(`${root} .cite-chip-text`)].some((e) => e.textContent.startsWith("(Moo")), t.root, { timeout: 15000 });
      out = await made({});
      t.assert.ok(out.includes("(Moo, 1996, p. 5)") && out.includes("### References") && !out.includes("[^1]"), out);
      await page.locator(`${t.root} .cite-bib-title`).filter({ hasText: "References" }).waitFor();

      // Screenshots of the page with its chips and bibliography.
      for (const [w, h] of SIZES) {
        await app.resize(w, h);
        for (const theme of THEMES) {
          await page.emulateMedia({ colorScheme: theme });
          await t.settled();
          const sideways = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
          t.assert.ok(sideways <= 1, `sideways by ${sideways}px at ${w} x ${h}, ${theme}`);
          await app.shot(`c40b-note-${w}x${h}-${theme}.png`);
        }
      }
      await app.resize(1480, 950);
      await page.emulateMedia({ colorScheme: null });
      await app.run("cite.set_style", { id: t.id });
    },
  },
  {
    row: "Citations settings: choose the default style; the download switch is off, says what it sends, and a style downloads through it",
    shot: "c40b-settings.png",
    timeout: 240000,
    async run(t) {
      const { app, page } = t;
      await t.reset();
      app.styles.forget();
      await page.evaluate(() => { location.hash = "#settings/citations"; });
      const sec = page.locator(".settings-section:visible");
      await sec.getByRole("heading", { name: "Citations", level: 2 }).waitFor();
      const sw = sec.getByRole("switch", { name: "Download more styles" });
      await sw.waitFor();
      t.assert.equal(await sw.isChecked(), false);
      const text = await sec.innerText();
      t.assert.ok(text.includes("github.com/citation-style-language/styles") && text.includes("nothing about you"), text);
      await page.waitForTimeout(400);
      t.assert.equal(app.styles.requests().length, 0, "nothing is asked while the switch is off");

      // The default style.
      await sec.locator("#cite-default").selectOption("apa");
      await page.waitForFunction(async () => JSON.parse(await window.go.main.App.Run("cite.style", "{}")).default === "apa", null, { timeout: 10000 });

      // On: search and download; the dependent style brings its parent.
      await sw.click();
      await sec.locator("input[type=search]").fill("ibid");
      await sec.getByRole("button", { name: "Search" }).click();
      await sec.locator(".cite-found-list li").first().waitFor();
      t.assert.equal(await sec.locator(".cite-found-list li").count(), 2);
      await sec.locator(".cite-found-list li").nth(1).getByRole("button", { name: /Download/ }).click();
      await page.waitForFunction(async () => JSON.parse(await window.go.main.App.Run("csl.styles", "{}")).styles.filter((s) => s.source === "added").length === 2, null, { timeout: 15000 });
      const added = (await app.run("csl.styles")).styles.filter((s) => s.source === "added").map((s) => s.id).sort();
      t.assert.deepEqual(added, ["ibid-notes", "ibid-notes-journal"]);
      t.assert.ok(app.styles.requests().every((r) => !r.includes("?")), "no query is sent");
      await t.shot();

      for (const [w, h] of SIZES) {
        await app.resize(w, h);
        for (const theme of THEMES) {
          await page.emulateMedia({ colorScheme: theme });
          await t.settled();
          const sideways = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
          t.assert.ok(sideways <= 1, `sideways by ${sideways}px at ${w} x ${h}, ${theme}`);
          await app.shot(`c40b-settings-${w}x${h}-${theme}.png`);
        }
      }
      await app.resize(1480, 950);
      await page.emulateMedia({ colorScheme: null });

      // Privacy lists it; remove the styles again.
      await page.evaluate(() => { location.hash = "#settings/privacy"; });
      await page.locator(".settings-section:visible dt", { hasText: /^Citation styles$/ }).waitFor();
      for (const id of added) await app.run("cite.styles_remove", { id });
    },
  },
];
