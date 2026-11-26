// Quality pass 5 checks (branch m3/qol5): audit items 21 to 23 (24 has Go tests), each shown failing first.
import { mkdtempSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { startApp } from "./app.mjs";
import { looks } from "./qol.e2e.mjs";

export default [
  {
    row: "Library, empty with the Collections column: the Traditions and rules fold is not left floating under the empty state",
    shot: "qol5-library-empty.png",
    timeout: 240000,
    async run(t) {
      // A library of its own with no books (the shared one may hold books earlier checks added).
      let inst = null;
      try {
        inst = await startApp({ build: false, sweep: false, keyring: "glossa-e2e-qol5-lib", name: "qol5-lib", lite: true });
        const { page } = inst;
        await page.evaluate(() => { location.hash = "#library"; });
        await page.locator(".collections").first().waitFor({ timeout: 20000 });
        await page.locator(".empty-state", { hasText: "No books yet" }).first().waitFor({ timeout: 20000 });
        const fold = page.locator("details.more", { has: page.locator("summary", { hasText: "Traditions and rules" }) });
        const where = async () => {
          const n = await fold.count();
          if (n === 0 || !(await fold.first().isVisible())) return "hidden";
          return await fold.first().evaluate((el) => (el.closest(".collections") ? "with the Collections column" : `floating in ${el.parentElement?.className || el.parentElement?.tagName}`));
        };
        const state = await where();
        t.assert.ok(state === "hidden" || state === "with the Collections column", `the fold is ${state}`);
        await inst.shot("qol5-library-empty.png");
        t.shotTaken = true;
        for (const [w, h] of [[1536, 816], [900, 700]]) {
          await inst.resize(w, h);
          const now = await where();
          t.assert.ok(["hidden", "with the Collections column"].includes(now), `at ${w} x ${h} the fold is ${now}`);
        }
        await inst.resize(1480, 950);
        await looks({ page, app: inst }, "library-empty");
      } finally {
        await inst?.stop().catch(() => {});
      }
    },
  },
  {
    row: "Bin: a bin of more than 500 things is counted truly (bin.count, the Bin entry and the Bin page), and the Bin page lists it a page at a time with Show more",
    shot: "qol5-bin-many.png",
    timeout: 600000,
    async run(t) {
      await t.reset();
      const { page, app } = t;
      const go = async (hash, sel) => { await page.evaluate((x) => { location.hash = x; }, hash); await page.locator(sel).first().waitFor({ timeout: 15000 }); };
      const before = (await app.run("bin.count")).count;
      const N = 510;
      const made = [];
      try {
        // One at a time: the database takes one write at a time.
        for (let i = 0; i < N; i++) {
          const n = await app.run("note.create", { title: `Bin many ${i}`, body: "x" });
          made.push(n.id);
          await app.run("note.delete", { id: n.id });
        }
        const total = before + N;
        t.assert.equal((await app.run("bin.count")).count, total, "bin.count is the true number");
        t.assert.equal((await app.run("bin.list", {})).length, 500, "bin.list still stops at its page of 500");
        t.assert.equal((await app.run("bin.list", { offset: 500 })).length, total - 500, "the rest comes with an offset");

        await go("#bin", ".bin-row");
        const said = async () => (((await page.locator(".work-sub").first().textContent()) ?? "").match(/^(\d+) thing/) ?? [])[1];
        for (let i = 0; i < 20 && (await said()) !== String(total); i++) await page.waitForTimeout(250);
        t.assert.equal(await said(), String(total), "the Bin page says the true count");
        const rail = async () => ((await page.locator(".work-bin .count").first().textContent()) ?? "").trim();
        for (let i = 0; i < 20 && (await rail()) !== String(total); i++) await page.waitForTimeout(250);
        t.assert.equal(await rail(), String(total), "the Bin entry says the true count");

        // A page at a time.
        const rows = () => page.locator(".bin-row").count();
        t.assert.equal(await rows(), 200, "the first page holds 200");
        const more = page.locator(".bin-more button");
        await more.waitFor({ timeout: 5000 });
        t.assert.ok(/Show more/.test(await more.textContent()), `the button: ${await more.textContent()}`);
        await t.shot();
        await more.click();
        await page.waitForFunction(() => document.querySelectorAll(".bin-row").length === 400, null, { timeout: 10000 });
        await more.click();
        await page.waitForFunction((n) => document.querySelectorAll(".bin-row").length === n, Math.min(600, total), { timeout: 10000 });
        // Restoring one keeps the pages already shown and lowers the count by one.
        await page.locator(".bin-row button", { hasText: "Restore" }).first().click();
        await page.locator(".msg", { hasText: "Restored" }).first().waitFor({ timeout: 5000 });
        for (let i = 0; i < 20 && (await said()) !== String(total - 1); i++) await page.waitForTimeout(250);
        t.assert.equal(await said(), String(total - 1), "restoring one lowers the count by one");
        t.assert.equal(await rows(), Math.min(600, total - 1), "the pages stay open");
        await looks(t, "bin-many");
      } finally {
        // Leave the bin as it was: later checks count it.
        const left = (await app.run("bin.list", { limit: 2000 })).filter((e) => made.includes(e.id));
        for (const e of left) await app.run("bin.purge", { kind: e.kind, id: e.id }).catch(() => {});
      }
    },
  },
  {
    row: "Sync: a device that has joined but not written anything is listed under Devices and can be forgotten",
    shot: "qol5-joined-device.png",
    timeout: 400000,
    async run(t) {
      const PASSWORD = "joined device 12345";
      const folder = mkdtempSync(join(tmpdir(), "glossa-qol5-join-"));
      let a = null, b = null;
      try {
        a = await startApp({ build: false, sweep: false, keyring: "glossa-e2e-qol5-a", name: "qol5-a", lite: true });
        b = await startApp({ build: false, sweep: false, keyring: "glossa-e2e-qol5-b", name: "qol5-b", lite: true });
        const native = [];
        a.page.on("dialog", (d) => { native.push(d.message()); void d.dismiss(); });
        await a.run("sync.setup", { folder, password: PASSWORD, device_name: "Laptop A" });
        await a.run("sync.now");
        // B joins and does nothing else: no note, no sync.
        await b.run("sync.setup", { folder, password: PASSWORD, device_name: "Desk B" });
        const bId = (await b.run("sync.status")).device_id;
        const names = async () => ((await a.run("sync.devices")).devices ?? []).map((d) => d.name);
        t.assert.ok((await names()).includes("Desk B"), `A lists B as soon as B has joined: ${await names()}`);
        t.assert.ok((await names()).includes("Laptop A"), "and itself");

        await a.page.evaluate(() => { location.hash = "#settings/sync"; });
        const row = a.page.locator(".sync-device", { hasText: "Desk B" });
        await row.waitFor({ timeout: 20000 });
        await a.shot("qol5-joined-device.png");
        t.shotTaken = true;
        await row.locator("button", { hasText: "Forget this device" }).click();
        const dlg = a.page.locator("dialog.confirm[open]");
        await dlg.waitFor({ timeout: 5000 });
        await dlg.locator("button", { hasText: /^Forget$/ }).click();
        await dlg.waitFor({ state: "detached", timeout: 3000 });
        await a.page.locator(".msg", { hasText: "Desk B was removed from the folder." }).first().waitFor({ timeout: 20000 });
        t.assert.ok(!(await names()).includes("Desk B"), "the device is gone from the list");
        t.assert.ok(!readdirSync(join(folder, "Glossa Sync")).some((n) => n.includes(bId)), "its folder left the shared folder");
        t.assert.deepEqual(native, [], "no native dialog");
        let said = "";
        try { await b.run("sync.now"); } catch (e) { said = String(e?.message ?? e); }
        t.assert.ok(said !== "", "the forgotten device's sync was refused");
      } finally {
        for (const inst of [a, b]) {
          await inst?.run("sync.stop").catch(() => {});
          await inst?.stop().catch(() => {});
        }
        rmSync(folder, { recursive: true, force: true });
      }
    },
  },
];
