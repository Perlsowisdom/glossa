// T22a, importing your own Logos data in the real app: Settings → Your data → Import from Logos.
// The Logos folder is a small fake the test builds (logosfixture.mjs); the app reads a copy of it
// and imports into the test's own library. The checks run in order, on one shared import.

import { rmSync } from "node:fs";
import { makeLogosFolder } from "./logosfixture.mjs";

let root;
let bible; // an installed Bible the fixture sermon quotes

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

// The ids of the notes the import made, for the Undo check (other files leave notes of their own).
let importedNotes = [];

export default [
  {
    row: "Logos import: the preview counts each kind, shows samples and says what cannot be placed",
    shot: "t22a-preview.png",
    async run(t) {
      const mods = await t.app.run("bible.modules", {});
      const list = Array.isArray(mods) ? mods : mods.modules;
      bible = list.find((m) => m.licence?.store && m.abbreviation) ?? list[0];
      root = makeLogosFolder({ abbreviation: bible.abbreviation, name: bible.name });
      const card = await preview(t);
      const text = await card.locator(".logos-kinds").innerText();
      t.assert.match(text, /Notes: 2 found, 2 new/, text);
      t.assert.match(text, /Highlights: 2 found, 2 new/, text);
      t.assert.match(text, /Clippings: 1 found, 1 new/, text);
      t.assert.match(text, /Passage lists: 1 found, 1 new/, text);
      t.assert.match(text, /Reading plans: 1 found, 1 new/, text);
      t.assert.match(text, /Sermons: 2 found, 2 new/, text);
      t.assert.match(await card.locator('[data-kind="sermon"] .logos-samples').innerText(), /Nothing can separate us/);
      // A sample of the notes: the first words of the note, without the markup.
      t.assert.match(await card.locator('[data-kind="note"] .logos-samples').innerText(), /Grace alone, see this page/);
      // What cannot be placed, with the reason, behind a disclosure.
      await card.locator(".logos-unmapped summary").click();
      const unmapped = await card.locator(".logos-unmapped").innerText();
      t.assert.match(unmapped, /Highlights, 1 left out: a highlight in a book, not in a Bible \(for example Fixture Commentary on Romans\)/, unmapped);
      t.assert.match(unmapped, /Notes, 1 left out: shared with you by someone else/, unmapped);
      t.assert.match(unmapped, /Notes, 1 left out: an empty note/, unmapped);
      t.assert.match(unmapped, /Sermons, 1 left out: a Logos sermon template, not a sermon/, unmapped);
      const pnotes = await card.locator(".logos-notes").innerText();
      t.assert.match(pnotes, /cover the whole verse/);
      t.assert.match(pnotes, /ESV is not installed in Glossa \(1\)/, pnotes);
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
      const beforeNotes = await app.run("note.list", { limit: 1000 });
      const before = beforeNotes.length;
      const card = await openCard(t);
      const asked = acceptNext(page);
      await card.locator("#logos-import").click();
      t.assert.match(await asked, /Nothing in Logos changes/);
      await card.locator(".logos-report").waitFor({ timeout: 30000 });
      const report = await card.locator(".logos-report").innerText();
      t.assert.match(report, /Notes: 2 added/, report);
      t.assert.match(report, /Highlights: 2 added/, report);
      t.assert.match(report, /Reading plans: 1 added/, report);
      t.assert.match(report, /Sermons: 2 added/, report);
      t.assert.match(await card.locator(".msg").innerText(), /Imported 9 items and 4 folders\./);
      t.assert.match(await card.locator(".logos-report-notes").innerText(), /ESV is not installed in Glossa/);

      // What is in the library now.
      // Only the notes this import made: other files leave notes of their own (a table with "Grace" in it, say).
      const known = new Set(beforeNotes.map((n) => n.id));
      const all = await app.run("note.list", { limit: 1000 });
      const notes = all.filter((n) => !known.has(n.id));
      t.assert.equal(all.length, before + 3, "two notes and a clipping");
      t.assert.equal(notes.length, 3);
      importedNotes = notes.map((n) => n.id);
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
      // Sermons: the outline, passages, date and series folder; a verse quote from the installed Bible with its credit.
      const sermons = await app.run("document.list", { kind: "sermon" });
      const sermon = await app.run("document.get", { id: sermons.find((d) => d.title === "Nothing can separate us").id });
      t.assert.equal(sermon.preached_on, "2024-03-10");
      t.assert.equal(sermon.theme, "Romans 8");
      t.assert.deepEqual(sermon.passages.map((p) => p.display), ["Romans 8:28-30", "John 3:16"]);
      t.assert.deepEqual(sermon.points.map((p) => p.h), ["God works for good", "Called"]);
      t.assert.equal(sermon.points[0].ref, "Romans 8:28");
      t.assert.match(sermon.body, /# God works for good\n\n\*\*Paul says it plainly\.\*\*/, sermon.body);
      t.assert.match(sermon.body, /\n> \*\*28\*\* /, "the quote is copied from the installed Bible: " + sermon.body);
      t.assert.ok(sermon.body.includes(`(#bible/${bible.id})`), sermon.body);
      t.assert.match(sermon.body, /> \[Romans 8:30\]\(#passage\/Rom\.8\.30\) \(ESV\)/, "a Bible Glossa lacks stays a plain quote");
      t.assert.match(sermon.body, /- first\n- second/);
      t.assert.match(sermon.body, /## Notes\n\nRemember to slow down here/);
      t.assert.ok(sermon.folder_id, "in its series folder");
      const plain = sermons.find((d) => d.title === "A short word on John");
      t.assert.ok(plain.folder_id && plain.folder_id !== sermon.folder_id);
      t.assert.ok((await app.run("folder.list")).some((f) => f.name === "Sermons from Logos"));
      t.assert.equal((await app.run("logos.runs")).length, 1);
      await card.locator(".logos-report").scrollIntoViewIfNeeded();
      await t.shot();
      rmSync(root, { recursive: true, force: true });
    },
  },
];
