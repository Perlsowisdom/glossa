// Quality pass 3 checks (branch m3/qol3): the open items from docs/qol-audit.md, each shown failing first.
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
];
