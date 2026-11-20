// T22a, importing your own Logos data in the real app: Settings → Your data → Import from Logos.
// The Logos folder is a small fake the test builds (logosfixture.mjs); the app reads a copy of it
// and imports into the test's own library. The checks run in order, on one shared import.

import { rmSync } from "node:fs";
import { makeLogosFolder } from "./logosfixture.mjs";

let root;

// accept answers the next confirmation the page asks, and gives back what it said.
function acceptNext(page) {
  return new Promise((resolve) => page.once("dialog", async (d) => { const m = d.message(); await d.accept(); resolve(m); }));
}

async function openCard(t) {
  const { page } = t;
  await page.evaluate(() => { location.hash = "#settings/data"; });
  const card = page.locator("#logos-card");
  await card.waitFor({ timeout: 15000 });
  await card.scrollIntoViewIfNeeded();
  return card;
}

async function preview(t) {
  const card = await openCard(t);
  await card.locator("details.more-options").evaluate((d) => { d.open = true; });
  await card.locator("#logos-folder").fill(root);
  await card.locator("#logos-preview-folder").click();
  await card.locator(".logos-kinds").waitFor({ timeout: 30000 });
  return card;
}

export default [
  {
    row: "Logos import: the preview counts each kind, shows samples and says what cannot be placed",
    shot: "t22a-preview.png",
    async run(t) {
      root = makeLogosFolder();
      const card = await preview(t);
      const text = await card.locator(".logos-kinds").innerText();
      t.assert.match(text, /Notes: 2 found, 2 new/, text);
      t.assert.match(text, /Highlights: 2 found, 2 new/, text);
      t.assert.match(text, /Clippings: 1 found, 1 new/, text);
      t.assert.match(text, /Passage lists: 1 found, 1 new/, text);
      t.assert.match(text, /Reading plans: 1 found, 1 new/, text);
      // A sample of the notes: the first words of the note, without the markup.
      t.assert.match(await card.locator('[data-kind="note"] .logos-samples').innerText(), /Grace alone, see this page/);
      // What cannot be placed, with the reason, behind a disclosure.
      await card.locator(".logos-unmapped summary").click();
      const unmapped = await card.locator(".logos-unmapped").innerText();
      t.assert.match(unmapped, /Highlights, 1 left out: a highlight in a book, not in a Bible \(for example Fixture Commentary on Romans\)/, unmapped);
      t.assert.match(unmapped, /Notes, 1 left out: shared with you by someone else/, unmapped);
      t.assert.match(unmapped, /Notes, 1 left out: an empty note/, unmapped);
      t.assert.match(await card.locator(".logos-notes").innerText(), /cover the whole verse/);
      // Previewing changed nothing.
      t.assert.equal((await t.app.run("note.list", {})).length, (await t.app.run("note.list", {})).length);
      t.assert.equal((await t.app.run("logos.runs")).length, 0);
      await card.locator(".logos-kinds").scrollIntoViewIfNeeded();
      await t.shot();
    },
  },
  {
    row: "Logos import: Import brings it all in through commands, with the notebook as a folder",
    shot: "t22a-imported.png",
    async run(t) {
      const { app, page } = t;
      const before = (await app.run("note.list", { limit: 1000 })).length;
      const card = await openCard(t);
      const asked = acceptNext(page);
      await card.locator("#logos-import").click();
      t.assert.match(await asked, /Nothing in Logos changes/);
      await card.locator(".logos-report").waitFor({ timeout: 30000 });
      const report = await card.locator(".logos-report").innerText();
      t.assert.match(report, /Notes: 2 added/, report);
      t.assert.match(report, /Highlights: 2 added/, report);
      t.assert.match(report, /Reading plans: 1 added/, report);
      t.assert.match(await card.locator(".msg").innerText(), /Imported 7 items and 2 folders\./);

      // What is in the library now.
      const notes = await app.run("note.list", { limit: 1000 });
      t.assert.equal(notes.length, before + 3, "two notes and a clipping");
      const verse = notes.find((n) => n.body.includes("Grace"));
      t.assert.equal(verse.body, "**Grace** alone, see [this page](https://example.org/x)");
      t.assert.equal(verse.ref.display, "Romans 8:28-30");
      t.assert.deepEqual(verse.tags, ["grace"]);
      t.assert.equal(verse.created_at, Date.parse("2020-01-22T10:57:51Z"));
      const book = notes.find((n) => n.body.includes("Check the Greek"));
      t.assert.ok(book.body.startsWith("**Logos:** *Fixture Commentary on Romans* — [Open in Logos](https://ref.ly/logosres/LLS:COMM)"), book.body);
      t.assert.deepEqual(book.tags, ["from-logos"]);
      const clip = notes.find((n) => n.body.includes("the love of God"));
      t.assert.equal(clip.title, "agape");
      t.assert.deepEqual(clip.tags, ["from-logos", "greek"]);
      const folders = await app.run("folder.list");
      t.assert.ok(folders.some((f) => f.name === "Romans study" && f.notes === 2), JSON.stringify(folders));
      t.assert.ok(folders.some((f) => f.name === "Word studies" && f.notes === 1));

      const highlights = await app.run("highlight.list", { ref: "John 3:16" });
      t.assert.ok(highlights.some((h) => h.style === "yellow"), JSON.stringify(highlights) + " all: " + JSON.stringify(await app.run("highlight.list", {})));
      const partial = await app.run("highlight.list", { ref: "Romans 5:1-2" });
      t.assert.ok(partial.some((h) => h.style === "pink-underline"), JSON.stringify(partial));
      const lists = await app.run("list.list", {});
      t.assert.ok(lists.some((l) => l.name === "Memory verses" && l.kind === "verses" && l.count === 2));
      const plans = await app.run("plan.list", {});
      const plan = plans.find((p) => p.name === "John in three days");
      t.assert.equal(plan.days, 3);
      t.assert.equal(plan.days_done, 1);
      t.assert.equal((await app.run("logos.runs")).length, 1);
      await card.locator(".logos-report").scrollIntoViewIfNeeded();
      await t.shot();
    },
  },
  {
    row: "Logos import: running it again adds nothing and the preview says so",
    shot: "t22a-again.png",
    async run(t) {
      const { app } = t;
      const before = (await app.run("note.list", { limit: 1000 })).length;
      const card = await preview(t);
      const text = await card.locator(".logos-kinds").innerText();
      t.assert.match(text, /Notes: 2 found, 0 new, 2 already imported/, text);
      t.assert.match(text, /Reading plans: 1 found, 0 new, 1 already imported/, text);
      const button = card.locator("#logos-import");
      t.assert.equal(await button.innerText(), "Nothing new to import");
      t.assert.equal(await button.isDisabled(), true);
      t.assert.equal((await app.run("note.list", { limit: 1000 })).length, before);
      // The same, run as a command: everything is skipped.
      const again = await app.run("logos.import", { folder: root });
      for (const k of again.kinds) t.assert.equal(k.created + k.updated, 0, `${k.kind} was imported twice`);
      await card.locator(".logos-kinds").scrollIntoViewIfNeeded();
      await t.shot();
    },
  },
  {
    row: "Logos import: Undo takes the import back and the folders it made",
    shot: "t22a-undone.png",
    async run(t) {
      const { app, page } = t;
      const card = await openCard(t);
      t.assert.equal(await card.locator(".logos-runs li").count(), 1, "one import; the repeat that added nothing left none");
      const asked = acceptNext(page);
      await card.locator(".logos-runs li").first().getByRole("button", { name: "Undo" }).click();
      t.assert.match(await asked, /Take this import back/);
      await page.waitForFunction(() => /taken back/.test(document.querySelector("#logos-card .logos-runs")?.innerText ?? ""), null, { timeout: 15000 });
      const notes = await app.run("note.list", { limit: 1000 });
      t.assert.ok(!notes.some((n) => n.body.includes("Grace")), "the imported note is gone");
      const folders = await app.run("folder.list");
      t.assert.ok(!folders.some((f) => f.name === "Romans study" || f.name === "Word studies"), "its folders are gone");
      t.assert.equal((await app.run("highlight.list", { ref: "John 3:16" })).length, 0);
      t.assert.ok(!(await app.run("plan.list", {})).some((p) => p.name === "John in three days"));
      await card.scrollIntoViewIfNeeded();
      await t.shot();
      rmSync(root, { recursive: true, force: true });
    },
  },
];
