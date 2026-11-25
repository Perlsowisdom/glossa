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
];
