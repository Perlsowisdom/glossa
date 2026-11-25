// Quality pass 3 checks (branch m3/qol3): the open items from docs/qol-audit.md, each shown failing first.
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { looks } from "./qol.e2e.mjs";

export default [
  {
    row: "Reading plans: Delete goes to the bin with the shared notice and Undo, Restore brings it back from the Bin page, and Delete for good removes it",
    shot: "qol3-plan-bin.png",
    timeout: 240000,
    async run(t) {
      const { page, app } = t;
      const go = async (hash, sel) => { await page.evaluate((x) => { location.hash = x; }, hash); await page.locator(sel).first().waitFor({ timeout: 10000 }); };
      let asked = 0;
      page.on("dialog", (d) => { asked++; void d.dismiss(); });
      const plan = await app.run("plan.create", { template: "nt-90", name: "Gone plan" });
      const notice = () => page.locator(".msg", { hasText: "\"Gone plan\" went to the bin." }).filter({ has: page.locator("button", { hasText: "Undo" }) }).first();

      await go("#plans", "main");
      await page.locator("button[aria-label='Delete Gone plan']").click();
      await notice().waitFor({ timeout: 5000 });
      t.assert.equal(asked, 0, "no native question was asked");
      t.assert.equal(await page.locator("button[aria-label='Delete Gone plan']").count(), 0, "the plan left the list");
      await t.shot();
      await looks(t, "plan-bin-notice");
      t.assert.ok((await app.run("bin.list", {})).some((e) => e.kind === "plan" && e.id === plan.id), "bin.list holds the plan");

      // Undo.
      await notice().locator("button", { hasText: "Undo" }).click();
      await page.locator(".msg", { hasText: "\"Gone plan\" is back." }).first().waitFor({ timeout: 5000 });
      await page.locator("button[aria-label='Delete Gone plan']").waitFor({ timeout: 5000 });

      // Restore from the Bin page.
      await page.locator("button[aria-label='Delete Gone plan']").click();
      await notice().waitFor({ timeout: 5000 });
      await go("#bin", ".bin-row");
      const row = page.locator(".bin-row", { hasText: "Gone plan" });
      await row.waitFor({ timeout: 5000 });
      t.assert.ok(/Reading plan/.test(await row.textContent()), `the bin names the kind: ${await row.textContent()}`);
      await looks(t, "plan-in-bin");
      await row.locator("button", { hasText: "Restore" }).click();
      await page.locator(".msg", { hasText: "Restored \"Gone plan\"" }).first().waitFor({ timeout: 5000 });
      await go("#plans", "main");
      await page.locator("button[aria-label='Delete Gone plan']").waitFor({ timeout: 5000 });
      t.assert.equal((await app.run("plan.list", {})).filter((p) => p.id === plan.id).length, 1, "the plan is back, with its days");
      t.assert.equal((await app.run("plan.days", { plan_id: plan.id })).length, 90, "all 90 days came back");

      // Delete for good (the app's own question) removes it and its history values.
      await page.locator("button[aria-label='Delete Gone plan']").click();
      await go("#bin", ".bin-row");
      await page.locator(".bin-row", { hasText: "Gone plan" }).locator("button", { hasText: "Delete for good" }).click();
      await page.locator("dialog.confirm[open] button.danger-solid").click();
      await page.locator(".msg", { hasText: "Deleted \"Gone plan\" for good." }).first().waitFor({ timeout: 5000 });
      t.assert.ok(!(await app.run("bin.list", {})).some((e) => e.id === plan.id), "gone from the bin");
      t.assert.equal(asked, 0, "no native dialog the whole way");
    },
  },
  {
    row: "Settings, Sync: the password strength line says each word once (not \"Strong. Strong.\")",
    shot: "qol3-sync-strength.png",
    timeout: 90000,
    async run(t) {
      const { page } = t;
      await page.evaluate(() => { location.hash = "#settings/sync"; });
      const form = page.locator("form.sync-setup");
      await form.waitFor({ timeout: 10000 });
      const line = form.locator(".sync-strength");
      const type = async (pw) => { await form.locator("input[type=password]").first().fill(pw); await page.waitForFunction(() => document.querySelector(".sync-strength")?.textContent.trim(), null, { timeout: 5000 }); await page.waitForTimeout(300); return (await line.textContent()).trim(); };
      const strong = await type("a long enough password 123");
      t.assert.equal(strong, "Strong", `strong reads once: ${strong}`);
      const good = await type("abcdefghijklmn");
      t.assert.equal(good, "Good", `good reads once: ${good}`);
      const weak = await type("abcdefghij");
      t.assert.ok(/^Weak\b/.test(weak) && weak.includes("longer password") && !/weak.*weak/i.test(weak), `weak keeps its advice, once: ${weak}`);
      await type("a long enough password 123");
      await t.shot();
      await looks(t, "sync-strength");
    },
  },
  {
    row: "No native box: Library, Sync, Bibles and Settings ask (Connections: connect.e2e) in the app's own dialog, or just do it with Undo; the browser's confirm never opens",
    shot: "qol3-own-dialog.png",
    timeout: 300000,
    async run(t) {
      const { page, app } = t;
      const native = [];
      page.on("dialog", (d) => { native.push(`${d.type()}: ${d.message()}`); void d.accept(); });
      const go = async (hash, sel) => { await page.evaluate((x) => { location.hash = x; }, hash); await page.locator(sel).first().waitFor({ timeout: 10000 }); };
      const dlg = page.locator("dialog.confirm[open]");
      // Press a button, expect the app's own question, answer Cancel (nothing changes), press again, answer yes.
      const ownQuestion = async (press, yes, after, label) => {
        await press();
        await dlg.waitFor({ timeout: 5000 });
        const said = (await dlg.textContent()).trim();
        t.assert.ok(said.length > 20 && !/confirm|alert|prompt|\bAPI\b.*\bcommand/i.test(said), `${label}: plain wording: ${said}`);
        await dlg.locator("button", { hasText: /^Cancel$/ }).click();
        await dlg.waitFor({ state: "detached", timeout: 3000 });
        t.assert.equal(await after(), false, `${label}: Cancel changed nothing`);
        await press();
        await dlg.waitFor({ timeout: 5000 });
        if (label === "Library") await t.shot("qol3-own-dialog.png");
        if (label === "Library") await looks(t, "own-dialog", async () => { if (!(await dlg.count())) await press(); await dlg.waitFor({ timeout: 5000 }); });
        await dlg.locator("button", { hasText: new RegExp(`^${yes}$`) }).click();
        await dlg.waitFor({ state: "detached", timeout: 3000 });
        for (let i = 0; i < 40 && !(await after()); i++) await page.waitForTimeout(250);
        t.assert.equal(await after(), true, `${label}: the answer was acted on`);
      };

      // Library: remove a folder.
      const dir = join(dirname(app.library), "qol3-books");
      mkdirSync(dir, { recursive: true });
      await app.run("library.add_folder", { path: dir });
      await go("#library", "main");
      const folders = async () => (await app.run("library.folders", {})).filter((f) => f.path.includes("qol3-books")).length;
      t.assert.equal(await folders(), 1, "the folder was added");
      await page.locator("details.lib-folders > summary").click();
      const rm = page.locator("button[aria-label^='Remove '][aria-label*='qol3-books']");
      await rm.first().waitFor({ timeout: 10000 });
      await ownQuestion(() => rm.first().click(), "Remove", async () => (await folders()) === 0, "Library");

      // Bibles: remove an installed Bible.
      const usfm = join(dirname(app.library), "qol3-bible.usfm");
      const BS = String.fromCharCode(92); // the USFM marker character
      writeFileSync(usfm, [`${BS}id JHN`, `${BS}c 3`, `${BS}p`, `${BS}v 16-17 For God so loved the world, that he gave his one and only Son.`, `${BS}q1`, `${BS}v 18 He who believes in him is not judged.`, ""].join(String.fromCharCode(10)));
      await app.run("bible.install", { path: usfm, format: "usfm", id: "qol3bible", name: "Qol3 Bible", abbreviation: "Q3B", language: "en" });
      const have = async () => (await app.run("bible.modules", { kind: "all" })).some((m) => m.id === "qol3bible");
      t.assert.equal(await have(), true, "the Bible was installed");
      await go("#bibles", "main");
      await page.getByRole("button", { name: "Installed", exact: true }).click();
      const rmBible = page.locator(`button[aria-label='Remove ${(await app.run("bible.modules", { kind: "all" })).find((m) => m.id === "qol3bible").name || "qol3bible"}']`);
      await rmBible.waitFor({ timeout: 10000 });
      await ownQuestion(() => rmBible.click(), "Remove", async () => !(await have()), "Bibles");

      // Connections: Remove key is in connect.e2e.mjs, which fails on a native box.

      // Sync: turn it off on this device (a folder of its own, no network).
      const syncDir = join(dirname(app.library), "qol3-sync");
      mkdirSync(syncDir, { recursive: true });
      await app.run("sync.setup", { folder: syncDir, password: "a long enough password 123", device_name: "Qol3 test" });
      await app.run("sync.now");
      await page.reload();
      await go("#settings/sync", ".settings-section:visible");
      const off = page.locator("button", { hasText: "Turn sync off on this device" });
      await off.waitFor({ timeout: 10000 });
      await ownQuestion(() => off.click(), "Turn off", async () => !(await app.run("sync.status")).configured, "Sync");

      // Settings: the online Bibles key.
      await app.run("apibible.set_key", { key: "qol3-test-key" }).catch(() => {});
      await go("#settings/online", ".settings-section:visible");
      const rmApi = page.locator(".settings-section:visible form[aria-label='API.Bible key'] button", { hasText: "Remove key" });
      await rmApi.waitFor({ timeout: 10000 });
      await rmApi.click();
      await dlg.waitFor({ timeout: 5000 });
      await dlg.locator("button", { hasText: /^Remove$/ }).click();
      await dlg.waitFor({ state: "detached", timeout: 3000 });

      t.assert.deepEqual(native, [], "no native confirm, alert or prompt opened");
    },
  },
  {
    row: "No native box in the source: confirm(, alert( and prompt( never appear in frontend/src",
    shot: "qol3-no-native.png",
    timeout: 30000,
    async run(t) {
      const hits = [];
      const walk = (d) => { for (const e of readdirSync(d, { withFileTypes: true })) { const p = join(d, e.name); if (e.isDirectory()) walk(p); else if (/\.(ts|html)$/.test(e.name) && !/\.test\.ts$/.test(e.name)) readFileSync(p, "utf8").split("\n").forEach((l, i) => { if (/(^|[^\w.])(window\.)?(confirm|alert|prompt)\s*\(/.test(l.replace(/\/\/.*$/, "")) ) hits.push(`${p}:${i + 1}`); }); } };
      walk(join(import.meta.dirname, "../src"));
      t.assert.deepEqual(hits, [], "native dialog calls found");
    },
  },
];
