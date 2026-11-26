// T40d part 2, the Obsidian connection in the real app, against a temp "vault" folder (never a real one). The
// system folder dialog is replaced for the check by setting window.go.main.App.PickFolder in the page (the way
// sync.e2e.mjs does), so the Choose button really runs and only the native window is skipped. It checks, as a
// person sees it: Obsidian is off at first and says what it touches, choosing the vault turns it to Connected,
// Export writes one Markdown file per note with front matter into the Glossa folder, a folder outside the vault
// is refused, a file edited on disk is imported into its note (and again changes nothing), a new file and its
// [[wikilink]] and ![[embed]] come in as a note, "Send to Obsidian" in a note's menu writes its file, and a note
// edited both in Glossa and on disk is not overwritten but offered three choices. Screenshots
// (t40d2-<what>-<size>-<theme>.png) at 1536 x 816 and 900 x 700, light and dark.

import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

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
      await app.shot(`t40d2-${name}-${w}x${h}-${theme}.png`);
    }
  }
  await app.resize(1480, 950);
  await page.emulateMedia({ colorScheme: null });
}

async function card(t) {
  await t.page.evaluate(() => { location.hash = "#settings/connections"; });
  await t.page.locator(".settings-section:visible").getByRole("heading", { name: "Connections" }).waitFor();
  const c = t.page.locator(".connection[data-connection=obsidian]");
  await c.waitFor();
  return c;
}

const result = (c) => c.locator(".obsidian-result");

const S = { vault: "" };

// fileOf finds the file in the vault that carries a note's id.
function fileOf(id) {
  const walk = (dir) => readdirSync(dir, { withFileTypes: true }).flatMap((e) => e.isDirectory() ? (e.name.startsWith(".") ? [] : walk(join(dir, e.name))) : e.name.endsWith(".md") ? [join(dir, e.name)] : []);
  return walk(S.vault).find((f) => readFileSync(f, "utf8").includes(`glossa_id: ${id}`));
}

async function chooseVault(t, c) {
  await t.page.evaluate((p) => { window.go.main.App.PickFolder = async () => p; }, S.vault);
  await c.getByRole("button", { name: /vault folder…|Change vault…/ }).click();
  await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
}

const menuOf = (t, label) => t.page.locator(`${t.root} button[aria-label='${label}']`);

async function openMenu(t, label) {
  await menuOf(t, label).click();
  await t.page.locator(".popover [role=menuitem]").first().waitFor();
}

async function openNotePage(t, id) {
  await t.settled();
  await t.page.evaluate((i) => { location.hash = `#notes/note/${i}`; }, id);
  await t.current(".note-page");
  t.id = id;
}

export default [
  {
    row: "Obsidian: off at first and says what it touches; choosing the vault folder connects it; Export all notes writes a Markdown file with front matter for each note; a folder outside the vault is refused",
    shot: "t40d2-card.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      await t.reset();
      S.vault = mkdtempSync(join(tmpdir(), "glossa-obsidian-e2e-"));
      // A state a stopped run left behind: no vault chosen, switched off.
      await app.run("connect.enable", { id: "obsidian" });
      await app.run("obsidian.set_vault", { path: "" });
      await app.run("connect.disable", { id: "obsidian" });
      S.n1 = await app.run("note.create", { title: "Grace in Romans", body: "Grace is **free**.\n\nSee [@lib:01HXTESTBOOK, p. 12]", ref: "Rom 8:1", tags: ["grace", "free gift"] });
      S.n2 = await app.run("note.create", { title: "What is truth?", body: "A question." });
      const d = await app.run("document.create", { title: "Faith alone", passages: ["Romans 3:28"], body: "Sermon text." });
      S.sermon = d;
      let c = await card(t);

      t.assert.equal((await c.locator(".badge").innerText()).trim(), "Off");
      const text = await c.innerText();
      t.assert.ok(text.includes("Obsidian lets you export notes and sermons as Markdown files") && text.includes("never uses the network and never deletes a file") && text.includes("Off until you turn it on."), text);
      t.assert.ok(!/connect\.|obsidian\.(export|import|set_vault)|glossa_|[a-z]+_[a-z]+/.test(text), `jargon in: ${text}`);
      t.assert.equal(await c.getByRole("button", { name: "Export all notes" }).count(), 0, "no export while off");

      // On, with no vault: it says to choose one, and offers nothing else.
      await c.getByRole("switch", { name: "Use Obsidian" }).click();
      await c.locator(".badge", { hasText: "Needs attention" }).waitFor({ timeout: 15000 });
      t.assert.ok((await c.locator(".connection-status").first().innerText()).includes("Choose your Obsidian vault folder"), "says to choose a vault");
      t.assert.equal(await c.getByRole("button", { name: "Export all notes" }).count(), 0, "no export without a vault");
      t.assert.ok((await c.innerText()).includes("No vault chosen yet."));

      // Choose the vault (the native dialog is replaced by an answer).
      await chooseVault(t, c);
      t.assert.ok((await c.innerText()).includes(`Vault: ${S.vault}.`) && (await c.innerText()).includes("“Glossa” folder"), await c.innerText());
      t.assert.equal(await c.locator("input.obsidian-folder").inputValue(), "Glossa");
      t.assert.ok((await c.locator(".connection-status").first().innerText()).includes("Ready."));
      t.assert.ok((await c.innerText()).includes("Nothing exported yet."));
      t.assert.deepEqual(readdirSync(S.vault), [], "choosing the vault wrote something in it");

      // A folder outside the vault is refused in plain words.
      await c.locator("input.obsidian-folder").fill("../elsewhere");
      await c.getByRole("button", { name: "Save folder" }).click();
      await page.locator(".msg.error", { hasText: "must be inside the vault" }).waitFor({ timeout: 10000 });
      t.assert.equal((await app.run("obsidian.status")).subfolder, "Glossa", "the bad folder was saved");

      // Export all notes.
      c = await card(t);
      await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      await c.getByRole("button", { name: "Export all notes" }).click();
      await result(c).filter({ hasText: "made in “Glossa”" }).waitFor({ timeout: 60000 });
      const made = await result(c).innerText();
      t.assert.match(made, /^\d+ files? made/, made);
      await c.locator(".obsidian-last", { hasText: "Last exported" }).waitFor({ timeout: 10000 });
      const f1 = readFileSync(join(S.vault, "Glossa", "Grace in Romans.md"), "utf8");
      for (const want of ["---\ntitle: Grace in Romans\n", `glossa_id: ${S.n1.id}\n`, "glossa_kind: note\n", "passages:\n  - Romans 8:1\n", "tags:\n  - free-gift\n  - grace\n", "created: 20", "updated: 20",
        "---\nGrace is **free**.\n\nSee [@lib:01HXTESTBOOK, p. 12]\n"]) {
        t.assert.ok(f1.includes(want), `the file lacks ${JSON.stringify(want)}:\n${f1}`);
      }
      t.assert.ok(existsSync(join(S.vault, "Glossa", "What is truth-.md")), `the unsafe character was not replaced: ${readdirSync(join(S.vault, "Glossa")).slice(0, 5)}`);
      t.assert.ok(!existsSync(join(S.vault, "Glossa", "Faith alone.md")), "Export all notes wrote a sermon");
      t.assert.deepEqual(readdirSync(S.vault), ["Glossa"], "something else was written in the vault");
      await t.shot();

      // Choose what to export: the sermon.
      await c.getByRole("button", { name: "Choose what to export…" }).click();
      const dlg = page.locator("dialog.any-picker");
      await dlg.waitFor();
      await dlg.getByLabel("Search notes and sermons").fill("Faith alone");
      await dlg.getByRole("checkbox", { name: "Export Faith alone" }).check();
      await dlg.getByRole("button", { name: "Export 1" }).click();
      await result(c).filter({ hasText: "1 file made in “Glossa”" }).waitFor({ timeout: 30000 });
      const f2 = readFileSync(join(S.vault, "Glossa", "Faith alone.md"), "utf8");
      t.assert.ok(f2.includes("glossa_kind: sermon\n") && f2.includes("  - Romans 3:28\n") && f2.endsWith("---\nSermon text.\n"), f2);

      // Exporting again rewrites nothing.
      const stamp = readFileSync(join(S.vault, "Glossa", "Grace in Romans.md"), "utf8");
      await c.getByRole("button", { name: "Export all notes" }).click();
      await result(c).filter({ hasText: "already up to date in “Glossa”" }).waitFor({ timeout: 60000 });
      t.assert.ok(!/\d+ files? made|updated/.test(await result(c).innerText()), await result(c).innerText());
      t.assert.equal(readFileSync(join(S.vault, "Glossa", "Grace in Romans.md"), "utf8"), stamp);

      await eachView(t, "card", async () => {
        const cc = await card(t);
        await cc.locator(".obsidian-last", { hasText: "Last exported" }).waitFor({ timeout: 15000 });
        const out = await cc.evaluate((e) => [...e.querySelectorAll("*")].filter((x) => x.getClientRects().length && x.getBoundingClientRect().right > e.getBoundingClientRect().right + 1).map((x) => x.className || x.tagName));
        t.assert.deepEqual(out, [], "something spills out of the card");
      });
    },
  },
  {
    row: "Obsidian: a file edited on disk is imported into its note, importing again changes nothing, a new file with a [[wikilink]] and an ![[embed]] comes in as a note, and Send to Obsidian in a note's menu writes its file",
    shot: "t40d2-import.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      t.assert.ok(S.vault && existsSync(join(S.vault, "Glossa")), "the check before did not leave an exported vault");
      const f = fileOf(S.n2.id);
      writeFileSync(f, readFileSync(f, "utf8").replace("A question.", "A question, answered in Obsidian."));
      const c = await card(t);
      await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      await c.getByRole("button", { name: "Import", exact: true }).click();
      await result(c).filter({ hasText: "1 updated" }).waitFor({ timeout: 60000 });
      t.assert.ok(/0 notes made, 1 updated, \d+ already up to date/.test(await result(c).innerText()), await result(c).innerText());
      t.assert.equal((await app.run("note.get", { id: S.n2.id })).body, "A question, answered in Obsidian.");
      await c.locator(".obsidian-last", { hasText: "Last imported" }).waitFor({ timeout: 10000 });
      const before = (await app.run("note.list", { limit: 5000 })).length;

      // Again: nothing changes.
      await c.getByRole("button", { name: "Import", exact: true }).click();
      await result(c).filter({ hasText: "0 notes made, 0 updated" }).waitFor({ timeout: 60000 });
      t.assert.equal((await app.run("note.list", { limit: 5000 })).length, before, "importing again made notes");

      // A new file by hand in Obsidian, with links and an embed.
      writeFileSync(join(S.vault, "Glossa", "From Obsidian.md"), "---\ntags: [reading]\n---\nSee [[Grace in Romans]] and [[Grace in Romans|that note]].\n\n![[diagram.png]]\n\nA #tag stays.\n");
      await c.getByRole("button", { name: "Import", exact: true }).click();
      await result(c).filter({ hasText: "1 note made" }).waitFor({ timeout: 60000 });
      const text = await result(c).innerText();
      t.assert.ok(text.includes("2 [[links]] were turned into plain words.") && text.includes("1 embed (![[…]]) was left as text"), text);
      const made = (await app.run("note.list", { limit: 5000 })).find((n) => n.title === "From Obsidian");
      t.assert.ok(made, "no note from the new file");
      t.assert.equal(made.body, "See Grace in Romans and that note.\n\n![[diagram.png]]\n\nA #tag stays.");
      t.assert.deepEqual(made.tags, ["reading"]);
      t.assert.ok(!readFileSync(join(S.vault, "Glossa", "From Obsidian.md"), "utf8").includes("glossa_id"), "importing wrote into the file");
      await t.shot();

      // Send to Obsidian from a note's menu: first a note never sent, then one that has a file.
      const fresh = await app.run("note.create", { title: "Sent from the menu", body: "Hello." });
      await openNotePage(t, fresh.id);
      await openMenu(t, "Note actions");
      let labels = await page.locator(".popover [role=menuitem]").allInnerTexts();
      t.assert.ok(labels.includes("Send to Obsidian") && !labels.includes("Update in Obsidian"), labels.join(" | "));
      await page.getByRole("menuitem", { name: "Send to Obsidian" }).click();
      await page.locator(".flash.show", { hasText: "Sent to Obsidian as Glossa/Sent from the menu.md" }).waitFor({ timeout: 15000 });
      t.assert.ok(readFileSync(join(S.vault, "Glossa", "Sent from the menu.md"), "utf8").includes("---\nHello.\n"));
      await openMenu(t, "Note actions");
      labels = await page.locator(".popover [role=menuitem]").allInnerTexts();
      t.assert.ok(labels.includes("Update in Obsidian") && !labels.includes("Send to Obsidian"), labels.join(" | "));
      await page.keyboard.press("Escape");

      await eachView(t, "imported", async () => {
        const cc = await card(t);
        await cc.locator(".obsidian-last", { hasText: "Last imported" }).waitFor({ timeout: 15000 });
      });
    },
  },
  {
    row: "Obsidian: a note edited both in Glossa and on disk is not overwritten and offers three choices; Keep both and Use Obsidian's text each do what they say",
    shot: "t40d2-conflict.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      t.assert.ok(S.vault && existsSync(join(S.vault, "Glossa")), "the checks before did not leave an exported vault");
      const n = await app.run("note.create", { title: "Both sides", body: "First line.\nSecond line." });
      await app.run("obsidian.export", { items: [{ kind: "note", id: n.id }] });
      const f = fileOf(n.id);
      const original = readFileSync(f, "utf8");
      writeFileSync(f, original.replace("Second line.", "Second line, from Obsidian."));
      await app.run("note.update", { id: n.id, body: "First line.\nSecond line, from Glossa." });
      const c = await card(t);
      await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      await c.getByRole("button", { name: "Import", exact: true }).click();
      await result(c).filter({ hasText: "left as it is" }).waitFor({ timeout: 60000 });
      const row = c.locator(".obsidian-conflict");
      await row.waitFor({ timeout: 10000 });
      const shown = await row.innerText();
      t.assert.ok(shown.includes("Both sides") && shown.includes("Second line, from Glossa.") && shown.includes("Second line, from Obsidian.") && !/>|\]\(|---/.test(shown), shown);
      t.assert.deepEqual(await row.getByRole("button").allInnerTexts(), ["Use Obsidian’s text", "Keep Glossa’s text", "Keep both"]);
      t.assert.equal((await app.run("note.get", { id: n.id })).body, "First line.\nSecond line, from Glossa.", "the note was changed before choosing");
      t.assert.ok(readFileSync(f, "utf8").includes("from Obsidian") && !readFileSync(f, "utf8").includes("from Glossa"), "the file was changed before choosing");
      await t.shot();
      await eachView(t, "conflict", async () => {
        const cc = await card(t);
        await cc.locator(".obsidian-conflict").waitFor({ timeout: 15000 });
        await cc.locator(".obsidian-conflict .row").scrollIntoViewIfNeeded();
      });

      // Keep both: the file's text goes in a new note, and the file gets Glossa's.
      const c2 = await card(t);
      await c2.locator(".obsidian-conflict").getByRole("button", { name: "Keep both" }).click();
      await page.locator(".msg", { hasText: "the text that was in the file is in a new note" }).waitFor({ timeout: 15000 });
      await c2.locator(".obsidian-conflict").waitFor({ state: "detached", timeout: 15000 });
      const notes = await app.run("note.list", { limit: 5000 });
      const copy = notes.find((x) => x.title === "Both sides (from Obsidian)");
      t.assert.ok(copy && copy.body === "First line.\nSecond line, from Obsidian.", JSON.stringify(copy));
      t.assert.equal((await app.run("note.get", { id: n.id })).body, "First line.\nSecond line, from Glossa.");
      t.assert.ok(readFileSync(f, "utf8").includes("from Glossa") && !readFileSync(f, "utf8").includes("from Obsidian"));

      // Both change again: this time Use Obsidian's text.
      writeFileSync(f, readFileSync(f, "utf8").replace("from Glossa", "from Obsidian again"));
      await app.run("note.update", { id: n.id, body: "First line.\nSecond line, Glossa again." });
      await c2.getByRole("button", { name: "Import", exact: true }).click();
      await c2.locator(".obsidian-conflict").waitFor({ timeout: 60000 });
      await c2.locator(".obsidian-conflict").getByRole("button", { name: "Use Obsidian’s text" }).click();
      await page.locator(".msg", { hasText: "The note now has the text from Obsidian." }).waitFor({ timeout: 15000 });
      await c2.locator(".obsidian-conflict").waitFor({ state: "detached", timeout: 15000 });
      t.assert.equal((await app.run("note.get", { id: n.id })).body, "First line.\nSecond line, from Obsidian again.");
      t.assert.equal((await app.run("obsidian.status")).conflicts.length, 0);

      // Tidy: the vault is forgotten and the folder removed; the notes stay in the library copy.
      await app.run("obsidian.set_vault", { path: "" });
      await app.run("connect.disable", { id: "obsidian" });
      rmSync(S.vault, { recursive: true, force: true });
    },
  },
];
