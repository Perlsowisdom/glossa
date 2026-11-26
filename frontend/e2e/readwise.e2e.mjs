// T40d, the Readwise connection in the real app, against a fake Readwise (e2e/fake-readwise.mjs; the app
// is started with its address, which it honours only for this computer, so readwise.io is never reached).
// It checks, as a person sees it: Readwise is off at first and says what it sends, a wrong token is refused
// in plain words and a right one connects (and is shown nowhere), Preview says what an import would do and
// changes nothing, Import makes one note per source that matches no library book and puts the matched
// book's highlights on the book, importing again changes nothing, and a note edited in Glossa is not
// overwritten but offered three choices. Screenshots (t40d-<what>-<size>-<theme>.png) at 1536 x 816 and
// 900 x 700, light and dark.

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { epub } from "./fixtures.mjs";

const SIZES = [[1536, 816], [900, 700]];
const THEMES = ["light", "dark"];

async function eachView(t, name, setup) {
  const { app, page } = t;
  for (const [w, h] of SIZES) {
    await app.resize(w, h);
    for (const theme of THEMES) {
      await page.emulateMedia({ colorScheme: theme });
      await t.settled();
      await setup();
      await t.settled();
      const sideways = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
      t.assert.ok(sideways <= 1, `${name}: the page scrolls sideways by ${sideways}px at ${w} x ${h}, ${theme}`);
      await app.shot(`t40d-${name}-${w}x${h}-${theme}.png`);
    }
  }
  await app.resize(1480, 950);
  await page.emulateMedia({ colorScheme: null });
}

async function card(t) {
  await t.page.evaluate(() => { location.hash = "#settings/connections"; });
  await t.page.locator(".settings-section:visible").getByRole("heading", { name: "Connections" }).waitFor();
  const c = t.page.locator(".connection[data-connection=readwise]");
  await c.waitFor();
  return c;
}

async function readwiseOn(t) {
  await t.app.run("connect.enable", { id: "readwise" });
  await t.app.run("connect.set_key", { id: "readwise", key: t.app.readwise.token });
}

// addBook puts a book called Mere Christianity by C. S. Lewis in the library, for Readwise's source to match.
async function addBook(t) {
  const dir = join(dirname(t.app.library), "readwise-books");
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "mere-christianity.epub"), epub("Mere Christianity", { author: "C. S. Lewis" }));
  await t.app.run("library.add", { paths: [dir] });
  for (let i = 0; i < 40; i++) {
    const r = await t.app.run("library.items", { text: "Mere Christianity", limit: 5 });
    const it = r.items.find((x) => x.title === "Mere Christianity");
    if (it) return it;
    await t.page.waitForTimeout(250);
  }
  throw new Error("Mere Christianity is not in the library");
}

const result = (c) => c.locator(".readwise-result");

export default [
  {
    row: "Readwise: off at first and says what it sends; a wrong token is refused in plain words; the right one connects and is shown nowhere",
    shot: "t40d-card.png",
    timeout: 240000,
    async run(t) {
      const { app, page } = t;
      const fake = app.readwise;
      await t.reset();
      fake.reset();
      await app.run("connect.clear_key", { id: "readwise" }); // a token a stopped run left in the test keychain
      const c = await card(t);

      t.assert.equal((await c.locator(".badge").innerText()).trim(), "Off");
      const text = await c.innerText();
      t.assert.ok(text.includes("Readwise lets you bring in your highlights as notes") && text.includes("sends your Readwise token to readwise.io") && text.includes("Off until you turn it on."), text);
      t.assert.ok(!/connect\.|readwise\.(test|import|preview)|[a-z]+_[a-z]+/.test(text.replace(/Readwise/g, "").replace("readwise.io/access_token", "")), `jargon in: ${text}`);
      t.assert.equal(await c.getByRole("button", { name: "Import highlights" }).count(), 0, "no import while off");
      await page.waitForTimeout(400);
      t.assert.equal(fake.requests().length, 0, "nothing is asked of Readwise while it is off");

      // On, without a token: it says to paste one, and still asks nothing.
      await c.getByRole("switch", { name: "Use Readwise" }).click();
      await c.locator(".badge", { hasText: "Needs attention" }).waitFor({ timeout: 15000 });
      t.assert.ok((await c.locator(".connection-status").first().innerText()).includes("Paste your Readwise token"), "says to paste a token");
      t.assert.equal(fake.requests().length, 0, "nothing is sent without a token");
      t.assert.equal(await c.getByRole("button", { name: "Import highlights" }).isDisabled(), true, "import waits for a token");

      // A wrong token is refused in plain words.
      await c.locator("input[type=password]").fill("not-the-token");
      await c.getByRole("button", { name: "Save key" }).click();
      await c.locator(".connection-status", { hasText: "did not accept this token" }).first().waitFor({ timeout: 15000 });
      t.assert.ok(fake.requests().some((r) => r.startsWith("GET /api/v2/auth/")), "asked Readwise to check it");

      // The right one connects.
      await c.locator("input[type=password]").fill(fake.token);
      await c.getByRole("button", { name: "Save key" }).click();
      await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      t.assert.ok((await c.locator(".connection-status").first().innerText()).includes("Connected to Readwise"), "status line");
      t.assert.equal(await c.getByRole("button", { name: "Import highlights" }).isDisabled(), false);
      const everything = await page.evaluate(() => document.body.innerText + [...document.querySelectorAll("input")].map((i) => i.value).join(" "));
      t.assert.ok(!everything.includes(fake.token) && !everything.includes("not-the-token"), "the token is shown nowhere");
      t.assert.ok(!JSON.stringify(await app.run("connect.list")).includes(fake.token), "the token is not in the command's answer");
      t.assert.equal((await app.run("connect.list")).find((x) => x.id === "readwise").key_set, true);
      await t.shot();

      await eachView(t, "card", async () => {
        const cc = await card(t);
        await cc.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
        const out = await cc.evaluate((e) => [...e.querySelectorAll("*")].filter((x) => x.getClientRects().length && x.getBoundingClientRect().right > e.getBoundingClientRect().right + 1).map((x) => x.className || x.tagName));
        t.assert.deepEqual(out, [], "something spills out of the card");
      });
    },
  },
  {
    row: "Readwise: Preview changes nothing; Import makes a note per unmatched source and puts the matched book's highlights on the book; importing again changes nothing",
    shot: "t40d-import.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      const fake = app.readwise;
      await t.reset();
      fake.reset();
      await readwiseOn(t);
      const book = await addBook(t);
      const before = (await app.run("note.list", { limit: 500 })).length;
      const c = await card(t);
      await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      t.assert.ok((await c.innerText()).includes("Not imported yet."), "says nothing was imported");

      // Preview: says what would happen, writes nothing.
      fake.forget();
      await c.getByRole("button", { name: "Preview" }).click();
      await result(c).filter({ hasText: "Importing would make 4 notes." }).waitFor({ timeout: 30000 });
      const preview = await result(c).innerText();
      t.assert.ok(preview.includes("Readwise has 5 highlights from 4 sources.") && preview.includes("1 source matches a book in your library by title and author"), preview);
      t.assert.equal((await app.run("note.list", { limit: 500 })).length, before, "a preview made notes");
      t.assert.ok((await c.innerText()).includes("Not imported yet."), "a preview counts as an import");
      t.assert.ok(fake.requests().length >= 2 && fake.requests().every((r) => r.startsWith("GET /api/v2/export/?includeDeleted=true")), `requests: ${fake.requests()}`);

      // Import: one note per source; the matched book's note is on the book.
      await c.getByRole("button", { name: "Import highlights" }).click();
      await result(c).filter({ hasText: "Imported 5 highlights from 4 sources." }).waitFor({ timeout: 30000 });
      t.assert.ok((await result(c).innerText()).includes("4 notes made"), await result(c).innerText());
      await c.locator(".readwise-last", { hasText: "Last imported" }).waitFor({ timeout: 10000 });
      const notes = await app.run("note.list", { limit: 500 });
      t.assert.equal(notes.length, before + 4);
      const titles = notes.map((n) => n.title);
      for (const want of ["Mere Christianity — Lewis, C.S.", "The Pilgrim's Progress — John Bunyan", "Knowing God — J. I. Packer", "A short note on prayer — Some Writer"]) {
        t.assert.ok(titles.includes(want), `no note "${want}" in ${titles.join(" | ")}`);
      }
      const onBook = await app.run("note.list", { item_id: book.id, limit: 50 });
      t.assert.equal(onBook.length, 1, "the matched book has one note");
      t.assert.ok(onBook[0].body.includes("Pride is the complete anti-god state of mind.") && onBook[0].body.includes("Read this twice."), onBook[0].body);
      const free = notes.filter((n) => (n.title ?? "").startsWith("Knowing God"));
      t.assert.ok(!free[0].item_id, "an unmatched source is attached to a book");
      t.assert.ok(!free[0].body.includes("Thrown away."), "a discarded highlight came in");

      // The notes are there to read.
      await page.evaluate((id) => { location.hash = `#notes/note/${id}`; }, free[0].id);
      await t.current(".note-page");
      await page.locator(".note-page", { hasText: "What were we made for? To know God." }).waitFor({ timeout: 10000 });
      await page.getByText("All notes", { exact: true }).first().click();
      await t.settled();
      await page.locator("body", { hasText: "A short note on prayer — Some Writer" }).waitFor({ timeout: 10000 });
      await t.shot();

      // Again: nothing changes, and only what changed is asked for.
      const c2 = await card(t);
      fake.forget();
      await c2.getByRole("button", { name: "Import highlights" }).click();
      await result(c2).filter({ hasText: "Nothing in Readwise has changed since the last import." }).waitFor({ timeout: 30000 });
      t.assert.ok(fake.requests().length === 1 && fake.requests()[0].includes("updatedAfter="), `requests: ${fake.requests()}`);
      t.assert.equal((await app.run("note.list", { limit: 500 })).length, before + 4, "importing again made notes");
      // Everything again: every note is found up to date.
      await c2.locator("details.more summary").click();
      await c2.getByRole("button", { name: "Import everything again" }).click();
      await result(c2).filter({ hasText: "4 notes already up to date" }).waitFor({ timeout: 30000 });
      const again = await app.run("note.list", { limit: 500 });
      t.assert.equal(again.length, before + 4, "importing everything again made notes");
      t.assert.deepEqual(again.map((n) => n.body).sort(), notes.map((n) => n.body).sort(), "importing everything again changed a note");
      t.assert.equal((await app.run("readwise.status")).linked, 4);

      await eachView(t, "imported", async () => {
        const cc = await card(t);
        await cc.locator(".readwise-last", { hasText: "Last imported" }).waitFor({ timeout: 15000 });
      });
    },
  },
  {
    row: "Readwise: a note edited in Glossa is not overwritten when Readwise has newer highlights; Keep both makes a second note",
    shot: "t40d-conflict.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      const fake = app.readwise;
      // (the check before leaves the library imported; this one starts from there, or from nothing)
      await readwiseOn(t);
      if (!(await app.run("readwise.status")).last_import) await app.run("readwise.import", {});
      const pilgrim = (await app.run("note.list", { limit: 500 })).find((n) => (n.title ?? "").startsWith("The Pilgrim"));
      await app.run("note.update", { id: pilgrim.id, body: `${pilgrim.body}\n\nMy own thought.` });
      fake.add(102, 1022, "Now I saw in my dream.", 40);
      const c = await card(t);
      await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      await c.getByRole("button", { name: "Import highlights" }).click();
      await result(c).filter({ hasText: "left as it is" }).waitFor({ timeout: 30000 });
      const row = c.locator(".readwise-conflict");
      await row.waitFor({ timeout: 10000 });
      t.assert.ok((await row.innerText()).includes("The Pilgrim's Progress — John Bunyan"), await row.innerText());
      t.assert.equal(await row.getByRole("button").count(), 3);
      const labels = await row.getByRole("button").allInnerTexts();
      t.assert.deepEqual(labels, ["Use Readwise’s text", "Keep mine", "Keep both"]);
      const mine = (await app.run("note.get", { id: pilgrim.id })).body;
      t.assert.ok(mine.includes("My own thought.") && !mine.includes("Now I saw in my dream."), "the edited note was changed");
      await t.shot();
      await eachView(t, "conflict", async () => {
        const cc = await card(t);
        await cc.locator(".readwise-conflict").waitFor({ timeout: 15000 });
      });

      // Keep both: mine stays, Readwise's text goes in a new note.
      const c2 = await card(t);
      await c2.locator(".readwise-conflict").getByRole("button", { name: "Keep both" }).click();
      await page.locator(".msg", { hasText: "Readwise’s text is in a new note" }).waitFor({ timeout: 15000 });
      await c2.locator(".readwise-conflict").waitFor({ state: "detached", timeout: 15000 });
      const notes = await app.run("note.list", { limit: 500 });
      t.assert.equal((await app.run("note.get", { id: pilgrim.id })).body, mine, "keep both changed my note");
      const theirs = notes.filter((n) => (n.title ?? "").startsWith("The Pilgrim") && n.id !== pilgrim.id);
      t.assert.equal(theirs.length, 1, "no second note");
      t.assert.ok(theirs[0].body.includes("Now I saw in my dream.") && !theirs[0].body.includes("My own thought."), theirs[0].body);

      await app.run("connect.disable", { id: "readwise" });
      await app.run("connect.clear_key", { id: "readwise" });
      // Tidy: the notes this check made leave the library as it was (they stay in the bin).
      for (const n of (await app.run("note.list", { limit: 500 })).filter((x) => (x.tags ?? []).includes("readwise"))) await app.run("note.delete", { id: n.id });
    },
  },
];
