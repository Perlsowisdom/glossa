// T20c: folder sync between two real apps. Two copies of the app run at once, each on its own
// library copy (its own device), with one shared temporary folder standing in for the folder a sync
// service copies. A sets sync up with a password and a note; B joins with the password and sees the
// note; a book added on A opens on B (downloaded when opened, and kept when "All" is chosen); a
// note edited on both keeps both versions; a wrong password is refused; the password prompt and the
// last sync at quit work. Nothing here touches your own library or a real cloud folder.

import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { startApp } from "./app.mjs";
import { epub } from "./fixtures.mjs";

const PASSWORD = "correct horse battery staple";
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// The two apps and the shared folder live across the checks, which run in order.
const S = { a: null, b: null, folder: "", noteId: "", bookA: "", bookB: "" };

const sync = (inst) => inst.page.locator(".sync-page");
const goSync = async (inst) => {
  await inst.page.evaluate(() => { location.hash = "#settings/sync"; });
  await inst.page.locator(".settings-section h2", { hasText: /^Sync$/ }).waitFor({ timeout: 15000 });
};
const dark = async (inst, name) => {
  await inst.page.emulateMedia({ colorScheme: "dark" });
  await wait(250);
  await inst.shot(name);
  await inst.page.emulateMedia({ colorScheme: "light" });
  await wait(150);
};
// press "Sync now" on the Sync page and wait for the result line.
async function syncNow(inst) {
  await goSync(inst);
  await inst.page.getByRole("button", { name: "Sync now" }).click();
  await sync(inst).locator(".msg.ok", { hasText: /^Synced\./ }).waitFor({ timeout: 90000 });
}
async function addBook(inst, title) {
  const dir = join(mkdtempSync(join(tmpdir(), "glossa-sync-book-")), "books");
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, `${title.toLowerCase().replace(/\W+/g, "-")}.epub`), epub(title));
  const res = await inst.run("library.add", { paths: [dir] });
  if (!(res.added >= 1)) throw new Error(`book not added: ${JSON.stringify(res)}`);
  for (let i = 0; i < 40; i++) {
    const r = await inst.run("library.items", { text: title, limit: 5 });
    if (r.items?.[0]) return r.items[0];
    await wait(250);
  }
  throw new Error(`"${title}" is not in the library`);
}
// find a synced book on B by title, waiting for it to arrive with the changes.
async function findItem(inst, title) {
  for (let i = 0; i < 40; i++) {
    const r = await inst.run("library.items", { text: title, limit: 5 });
    if (r.items?.[0]) return r.items[0];
    await wait(250);
  }
  throw new Error(`"${title}" did not arrive`);
}
async function openBookFromLibrary(inst, title) {
  const { page } = inst;
  await page.evaluate(() => { location.hash = "#library"; });
  const q = page.locator("input.work-search:visible").first();
  await q.waitFor();
  await q.fill(title);
  const row = page.locator(".book-tile:visible, .lib-table tbody tr:visible").filter({ hasText: title }).first();
  await row.waitFor({ timeout: 15000 });
  await row.click();
  await page.locator("button:visible", { hasText: /Open in Study|^Read$/ }).first().click();
}

async function fillSetup(inst, { folder, password, name, remember }) {
  const { page } = inst;
  if (folder) await page.getByLabel("Sync folder").fill(folder);
  await page.getByLabel("Password", { exact: true }).fill(password);
  if (name !== undefined) await page.getByLabel("This device's name").fill(name);
  if (remember) await page.locator("#sync-remember").check();
}

export default [
  {
    row: "Sync: two apps start, each with a library of its own",
    shot: "sync-00-start.png",
    timeout: 300000,
    async run(t) {
      S.folder = mkdtempSync(join(tmpdir(), "glossa-sync-folder-"));
      S.a = await startApp({ build: false, sweep: false, keyring: "glossa-e2e-sync-a", name: "sync-a", lite: true });
      S.b = await startApp({ build: false, sweep: false, keyring: "glossa-e2e-sync-b", name: "sync-b", lite: true });
      const a = await S.a.run("sync.status");
      const b = await S.b.run("sync.status");
      t.assert.equal(a.configured, false);
      t.assert.notEqual(a.device_id, b.device_id, "the two apps must be two devices");
      const n = await S.a.run("note.create", { title: "Sync note", body: "Written on A." });
      S.noteId = n.id;
      await S.a.shot("sync-00-start.png");
      t.shotTaken = true;
    },
  },
  {
    row: "Sync setup on A: password rules, confirm, preview, and nothing changes until confirmed",
    shot: "sync-01-setup.png",
    timeout: 240000,
    async run(t) {
      const a = S.a;
      await goSync(a);
      await a.page.getByLabel("Sync folder").waitFor();
      await a.shot("sync-01-setup.png");
      await dark(a, "sync-01b-setup-dark.png");
      // A short password is refused in plain words.
      await fillSetup(a, { folder: S.folder, password: "short", name: "Study laptop", remember: true });
      await a.page.getByRole("button", { name: "Continue" }).click();
      await sync(a).locator(".msg.error").waitFor();
      t.assert.match(await sync(a).locator(".msg.error").innerText(), /too short/i);
      await a.shot("sync-02-short-password.png");
      // A good password shows a strength hint; a new folder asks for it twice.
      await a.page.getByLabel("Password", { exact: true }).fill(PASSWORD);
      await sync(a).locator(".sync-strength .badge").waitFor();
      await a.page.getByRole("button", { name: "Continue" }).click();
      const again = a.page.getByLabel("Type the password again");
      await again.waitFor({ state: "visible" });
      await again.fill(PASSWORD + "x");
      await a.page.getByRole("button", { name: "Continue" }).click();
      await sync(a).locator(".msg.error", { hasText: /not the same/i }).waitFor();
      await again.fill(PASSWORD);
      await a.page.getByRole("button", { name: "Continue" }).click();
      await sync(a).locator(".sync-preview").waitFor();
      const rows = await sync(a).locator(".sync-counts li").allInnerTexts();
      t.assert.match(rows[0], /^This device has .*note/);
      t.assert.match(rows[1], /folder is new/);
      t.assert.match(rows[2], /After syncing/);
      await a.shot("sync-03-preview.png");
      await dark(a, "sync-03b-preview-dark.png");
      // The preview changed nothing: no sync folder, nothing configured.
      t.assert.ok(!existsSync(join(S.folder, "Glossa Sync")), "the preview must not write to the folder");
      t.assert.equal((await a.run("sync.status")).configured, false);
      t.shotTaken = true;
    },
  },
  {
    row: "Sync starts on A after confirming: backup first, status page, rail indicator",
    shot: "sync-04-status.png",
    timeout: 180000,
    async run(t) {
      const a = S.a;
      await a.page.getByRole("button", { name: "Merge and start syncing" }).click();
      await sync(a).locator(".sync-now").waitFor({ timeout: 90000 });
      const s = await a.run("sync.status");
      t.assert.ok(s.configured && s.last_sync > 0, JSON.stringify(s));
      t.assert.ok(s.password_saved, "Remember on this device should have saved the password");
      t.assert.equal(s.device_name, "Study laptop");
      t.assert.ok(existsSync(join(S.folder, "Glossa Sync")));
      t.assert.ok((await a.run("backup.list")).length >= 1, "a backup is made before the first sync");
      const dot = a.page.locator(".rail-sync");
      await dot.waitFor();
      t.assert.equal(await dot.getAttribute("data-state"), "ok");
      await a.page.evaluate(() => (document.activeElement)?.blur());
      await a.shot("sync-04-status.png");
      await dark(a, "sync-04b-status-dark.png");
      t.assert.match(await sync(a).innerText(), /Change password[\s\S]*Not available yet/);
      t.shotTaken = true;
    },
  },
  {
    row: "A wrong password on B is refused and changes nothing",
    shot: "sync-05-wrong-password.png",
    timeout: 120000,
    async run(t) {
      const b = S.b;
      await goSync(b);
      // The folder is chosen with the Choose button; the system dialog is replaced by this answer.
      await b.page.evaluate((p) => { window.go.main.App.PickFolder = async () => p; }, S.folder);
      await b.page.getByRole("button", { name: "Choose…" }).click();
      t.assert.equal(await b.page.getByLabel("Sync folder").inputValue(), S.folder);
      await fillSetup(b, { password: "not the right password", name: "Office desk" });
      await b.page.getByRole("button", { name: "Continue" }).click();
      const err = sync(b).locator(".msg.error");
      await err.waitFor();
      const text = await err.innerText();
      t.assert.match(text, /doesn't match/);
      t.assert.match(text, /Nothing was changed/);
      t.assert.ok(!text.includes("not the right password"), "the password is never shown back");
      t.assert.equal((await b.run("sync.status")).configured, false);
      await b.shot("sync-05-wrong-password.png");
      t.shotTaken = true;
    },
  },
  {
    row: "B joins with the password and the note from A appears",
    shot: "sync-06-join-preview.png",
    timeout: 180000,
    async run(t) {
      const b = S.b;
      await fillSetup(b, { password: PASSWORD });
      await b.page.getByRole("button", { name: "Continue" }).click();
      await sync(b).locator(".sync-preview").waitFor();
      const rows = await sync(b).locator(".sync-counts li").allInnerTexts();
      t.assert.match(rows[1], /The folder has .*note.*Study laptop/);
      t.assert.equal((await b.run("sync.status")).configured, false, "nothing is set up until confirmed");
      await b.shot("sync-06-join-preview.png");
      await dark(b, "sync-06b-join-preview-dark.png");
      await b.page.getByRole("button", { name: "Merge and start syncing" }).click();
      await sync(b).locator(".sync-now").waitFor({ timeout: 90000 });
      const note = await b.run("note.get", { id: S.noteId });
      t.assert.equal(note.body.trim(), "Written on A.");
      await b.page.evaluate((id) => { location.hash = `#notes/note/${id}`; }, S.noteId);
      await b.page.locator(".note-page .rich-text").filter({ hasText: "Written on A." }).first().waitFor({ timeout: 15000 });
      await b.shot("sync-07-note-on-b.png");
      t.shotTaken = true;
    },
  },
  {
    row: "A book added on A opens on B, downloaded when opened",
    shot: "sync-08-downloading.png",
    timeout: 240000,
    async run(t) {
      const { a, b } = S;
      // B keeps books on demand.
      await goSync(b);
      await b.page.locator(".seg-opt", { hasText: "When opened" }).click();
      await sync(b).locator(".msg.ok", { hasText: /download when you open/ }).waitFor();
      t.assert.equal((await b.run("sync.books")).mode, "opened");
      const item = await addBook(a, "Sync Book One");
      S.bookA = item.id;
      await syncNow(a);
      await syncNow(b);
      const it = await findItem(b, "Sync Book One");
      S.bookB = it.id;
      const books = await b.run("sync.books");
      t.assert.ok(books.remote_only >= 1, `the book waits in the folder: ${JSON.stringify(books)}`);
      // Hold the download's answer, so the pane can be seen while it runs.
      await b.page.evaluate(() => {
        const app = window.go.main.App;
        const real = app.Run.bind(app);
        window.__gate = new Promise((r) => { window.__release = r; });
        app.Run = async (id, p) => {
          const out = await real(id, p);
          if (id === "sync.fetch") await window.__gate;
          return out;
        };
      });
      await openBookFromLibrary(b, "Sync Book One");
      const pane = b.page.locator(".msg", { hasText: /Downloading… \d+%/ }).first();
      await pane.waitFor({ timeout: 30000 });
      await b.shot("sync-08-downloading.png");
      await dark(b, "sync-08b-downloading-dark.png");
      await b.page.evaluate(() => window.__release());
      await b.page.locator(".book-text p").first().waitFor({ timeout: 30000 });
      await b.shot("sync-09-opened-on-b.png");
      t.assert.ok((await b.run("sync.books")).remote_only < books.remote_only, "the book is on this device now");
      t.shotTaken = true;
    },
  },
  {
    row: "With All chosen, books arrive with the sync and open at once",
    shot: "sync-10-all-mode.png",
    timeout: 240000,
    async run(t) {
      const { a, b } = S;
      await addBook(a, "Sync Book Two");
      await syncNow(a);
      await goSync(b);
      await b.page.locator(".seg-opt", { hasText: "All of them" }).click();
      await sync(b).locator(".msg.ok", { hasText: /kept on this device/ }).waitFor();
      await syncNow(b);
      const it = await findItem(b, "Sync Book Two");
      const books = await b.run("sync.books");
      t.assert.equal(books.remote_only, 0, `everything is here: ${JSON.stringify(books)}`);
      const opened = await b.run("book.open", { item_id: it.id });
      t.assert.ok(opened.sections >= 1);
      await goSync(b);
      await b.shot("sync-10-all-mode.png");
      t.shotTaken = true;
    },
  },
  {
    row: "A note edited on both devices keeps both versions, with a notice",
    shot: "sync-11-conflict.png",
    timeout: 240000,
    async run(t) {
      const { a, b } = S;
      await a.run("note.update", { id: S.noteId, body: "Edited on A first." });
      await wait(1200);
      await b.run("note.update", { id: S.noteId, body: "Edited on B later, differently." });
      await syncNow(a);
      await syncNow(b);
      await syncNow(a);
      await syncNow(b);
      const [na, nb] = [await a.run("note.get", { id: S.noteId }), await b.run("note.get", { id: S.noteId })];
      t.assert.equal(na.body.trim(), nb.body.trim(), "both devices end with the same text");
      t.assert.match(na.body, /Edited on B later/, "the newer edit wins");
      const ca = (await a.run("sync.conflicts")) ?? [];
      const cb = (await b.run("sync.conflicts")) ?? [];
      const all = [...ca.map((c) => ({ on: "a", ...c })), ...cb.map((c) => ({ on: "b", ...c }))];
      t.assert.ok(all.length >= 1, "one device is told about the replaced text");
      const kept = all.find((c) => /Edited on A first/.test(c.text));
      t.assert.ok(kept, `the older version is kept: ${JSON.stringify(all)}`);
      const inst = kept.on === "a" ? a : b;
      await goSync(inst);
      const line = sync(inst).locator(".sync-conflict");
      await line.waitFor({ timeout: 15000 });
      t.assert.match(await line.first().innerText(), /was changed on .* too/);
      await line.first().scrollIntoViewIfNeeded();
      await inst.shot("sync-11-conflict.png");
      await dark(inst, "sync-11b-conflict-dark.png");
      await line.first().getByRole("button", { name: "Open" }).click();
      await inst.page.locator(".note-page .rich-text").first().waitFor({ timeout: 15000 });
      await goSync(inst);
      await sync(inst).locator(".sync-conflict").first().getByRole("button", { name: "Dismiss" }).click();
      await sync(inst).locator(".msg.ok", { hasText: /Dismissed/ }).waitFor();
      t.assert.equal(((await inst.run("sync.conflicts")) ?? []).length, 0);
      t.shotTaken = true;
    },
  },
  {
    row: "After a restart the password is asked for once, and sync carries on; quitting syncs last changes",
    shot: "sync-12-unlock.png",
    timeout: 300000,
    async run(t) {
      const { a, b } = S;
      // B did not remember the password, so after a restart it asks.
      await b.restart();
      const prompt = b.page.locator("#sync-unlock");
      await prompt.waitFor({ timeout: 30000 });
      await b.shot("sync-12-unlock.png");
      await prompt.locator("input[type=password]").fill("wrong wrong wrong");
      await prompt.getByRole("button", { name: "Unlock" }).click();
      await prompt.locator(".msg.error").waitFor();
      await prompt.locator("input[type=password]").fill(PASSWORD);
      await prompt.getByRole("button", { name: "Unlock" }).click();
      await prompt.waitFor({ state: "detached", timeout: 30000 });
      t.assert.equal((await b.run("sync.status")).unlocked, true);
      // A remembered the password; a note made just before quitting reaches the folder at quit.
      const n = await a.run("note.create", { title: "Last words", body: "Made just before quitting A." });
      await a.restart();
      await wait(1000);
      await syncNow(b);
      const got = await b.run("note.get", { id: n.id });
      t.assert.match(got.body, /just before quitting/);
      const s = await a.run("sync.status");
      t.assert.ok(s.unlocked && s.password_saved, "A kept its password in the credential store");
      t.shotTaken = true;
    },
  },
  {
    row: "Sync closes down: turn it off, close both apps, remove the temporary folders",
    shot: "sync-13-off.png",
    timeout: 120000,
    async run(t) {
      const errors = [];
      try {
        // Turning sync off forgets the saved password (the test keychain entry).
        if (S.a) {
          await S.a.run("sync.stop").catch((e) => errors.push(String(e)));
          await S.a.page.evaluate(() => { location.hash = "#settings/sync"; }).catch(() => {});
        }
        if (S.b) await S.b.run("sync.stop").catch((e) => errors.push(String(e)));
      } finally {
        for (const inst of [S.a, S.b]) await inst?.stop().catch((e) => errors.push(String(e)));
        S.a = S.b = null;
        if (S.folder) rmSync(S.folder, { recursive: true, force: true });
      }
      t.assert.deepEqual(errors, []);
      t.assert.ok(!existsSync(S.folder));
      t.shotTaken = true;
    },
  },
];
