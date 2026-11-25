// Quality pass 4 checks (branch m3/qol4): the open items from docs/qol-audit.md, each shown failing first.
import { looks } from "./qol.e2e.mjs";

export default [
  {
    row: "Bin: the count beside Bin in My work agrees with the Bin page after a reading plan and a list are deleted from their own pages",
    shot: "qol4-bin-count.png",
    timeout: 180000,
    async run(t) {
      await t.reset();
      const { page, app } = t;
      const go = async (hash, sel) => { await page.evaluate((x) => { location.hash = x; }, hash); await page.locator(sel).first().waitFor({ timeout: 10000 }); };
      const column = async () => {
        const text = ((await page.locator(".work-bin .count").first().textContent()) ?? "").trim();
        return text === "" ? 0 : Number(text);
      };
      const agrees = async (want, why) => {
        for (let i = 0; i < 20 && (await column()) !== want; i++) await page.waitForTimeout(250);
        t.assert.equal(await column(), want, `${why}: the Bin entry says ${await column()}, the bin holds ${want}`);
      };
      await app.run("plan.create", { template: "nt-90", name: "Count plan" });
      await app.run("list.create", { kind: "verses", name: "Count list" });
      const before = (await app.run("bin.list", {})).length;

      await go("#plans", "button[aria-label='Delete Count plan']");
      await agrees(before, "before deleting");
      await page.locator("button[aria-label='Delete Count plan']").click();
      await page.locator(".msg", { hasText: "\"Count plan\" went to the bin." }).first().waitFor({ timeout: 5000 });
      await agrees(before + 1, "after deleting a reading plan");

      await go("#lists", "main");
      await page.locator("main button", { hasText: "Count list" }).first().click();
      await page.locator("main button", { hasText: "Delete list" }).first().click();
      await page.locator(".msg", { hasText: "\"Count list\" went to the bin." }).first().waitFor({ timeout: 5000 });
      await agrees(before + 2, "after deleting a list");

      // Undo brings the number back down.
      await page.locator(".msg button", { hasText: "Undo" }).first().click();
      await page.locator(".msg", { hasText: "\"Count list\" is back." }).first().waitFor({ timeout: 5000 });
      await agrees(before + 1, "after Undo");
      await page.locator("main button", { hasText: "Delete list" }).first().waitFor({ timeout: 5000 }).catch(() => {});

      // The Bin page says the same number.
      await go("#bin", ".bin-row");
      const said = (await page.locator(".work-sub").first().textContent()) ?? "";
      const n = Number((/^(\d+) thing/.exec(said) ?? [])[1]);
      t.assert.equal(n, before + 1, `the Bin page says: ${said}`);
      await agrees(n, "beside the Bin page");
      t.assert.equal((await app.run("bin.list", {})).length, n, "bin.list agrees");
      await t.shot();
      await looks(t, "bin-count");
    },
  },
  {
    row: "Collections: Delete goes to the bin with the shared notice and Undo, Restore brings it back from the Bin page, and Delete for good removes it",
    shot: "qol4-collection-bin.png",
    timeout: 240000,
    async run(t) {
      await t.reset();
      const { page, app } = t;
      const go = async (hash, sel) => { await page.evaluate((x) => { location.hash = x; }, hash); await page.locator(sel).first().waitFor({ timeout: 10000 }); };
      let asked = 0;
      page.on("dialog", (d) => { asked++; void d.dismiss(); });
      const coll = await app.run("collection.create", { name: "Gone collection" });
      const here = async () => (await app.run("collection.list", {})).tree.some((c) => c.id === coll.id);
      const notice = () => page.locator(".collections .msg", { hasText: "\"Gone collection\" went to the bin." }).filter({ has: page.locator("button", { hasText: "Undo" }) }).first();
      const del = async () => {
        await go("#library", ".collections .coll-pick");
        await page.locator(".collections .coll-pick", { hasText: "Gone collection" }).click();
        await page.locator(".coll-bar details.more > summary").click();
        await page.locator(".coll-bar button", { hasText: "Delete this collection" }).click();
        await notice().waitFor({ timeout: 5000 });
      };

      await del();
      t.assert.equal(asked, 0, "no native question was asked");
      t.assert.equal(await here(), false, "the collection left the Library");
      t.assert.ok((await app.run("bin.list", {})).some((e) => e.kind === "collection" && e.id === coll.id), "bin.list holds the collection");
      await t.shot();
      await looks(t, "collection-bin-notice");

      // Undo.
      await notice().locator("button", { hasText: "Undo" }).click();
      await page.locator(".msg", { hasText: "\"Gone collection\" is back." }).first().waitFor({ timeout: 5000 });
      await page.locator(".collections .coll-pick", { hasText: "Gone collection" }).waitFor({ timeout: 5000 });
      t.assert.equal(await here(), true, "Undo brought it back");

      // Restore from the Bin page.
      await del();
      await go("#bin", ".bin-row");
      const row = page.locator(".bin-row", { hasText: "Gone collection" });
      await row.waitFor({ timeout: 5000 });
      t.assert.ok(/Collection/.test(await row.textContent()), `the bin names the kind: ${await row.textContent()}`);
      await looks(t, "collection-in-bin");
      await row.locator("button", { hasText: "Restore" }).click();
      await page.locator(".msg", { hasText: "Restored \"Gone collection\"" }).first().waitFor({ timeout: 5000 });
      t.assert.equal(await here(), true, "Restore from the Bin brought it back");

      // Delete for good (the app's own question).
      await del();
      await go("#bin", ".bin-row");
      await page.locator(".bin-row", { hasText: "Gone collection" }).locator("button", { hasText: "Delete for good" }).click();
      await page.locator("dialog.confirm[open] button.danger-solid").click();
      await page.locator(".msg", { hasText: "Deleted \"Gone collection\" for good." }).first().waitFor({ timeout: 5000 });
      t.assert.ok(!(await app.run("bin.list", {})).some((e) => e.id === coll.id), "gone from the bin");
      t.assert.equal(await here(), false, "and not in the Library");
      t.assert.equal(asked, 0, "no native dialog the whole way");
    },
  },
];
