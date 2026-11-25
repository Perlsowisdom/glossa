// Quality pass 2 checks (branch m3/qol2): the open items from docs/qol-audit.md, each shown failing first.
import { looks } from "./qol.e2e.mjs";

export default [
  {
    row: "Sermons: the open sermon stays in the address, so reload and Back/Forward return to it",
    shot: "qol2-sermon-address.png",
    timeout: 120000,
    async run(t) {
      const { page, app } = t;
      const d = await app.run("document.create", { kind: "sermon" });
      await app.run("document.update", { id: d.id, title: "Addressed sermon", kind: "sermon", body: "Text" });
      await page.evaluate(() => { location.hash = "#sermons"; });
      await page.locator("tr", { hasText: "Addressed sermon" }).first().waitFor({ timeout: 10000 });
      await page.locator("tr", { hasText: "Addressed sermon" }).first().locator(".more-btn").click();
      await page.locator(".popover button", { hasText: "Edit outline and text" }).click();
      await t.current(".sermon-layout");
      t.assert.equal(await page.evaluate(() => location.hash), `#sermons/${d.id}`, "the address names the open sermon");
      await looks(t, "sermon-address");
      await page.reload();
      await page.waitForSelector(".shell");
      await page.locator(".sermon-layout:visible").first().waitFor({ timeout: 15000 });
      t.assert.equal(await page.evaluate(() => location.hash), `#sermons/${d.id}`, "still named after a reload");
      // Back goes to the list, Forward returns to the sermon.
      await page.evaluate(() => { location.hash = "#sermons"; });
      await page.locator(".sermon-table").waitFor({ state: "visible", timeout: 5000 });
      t.assert.equal(await page.locator(".sermon-layout:visible").count(), 0, "the list replaced the sermon");
      await page.goBack();
      await page.locator(".sermon-layout:visible").first().waitFor({ timeout: 5000 });
      await page.goForward();
      await page.locator(".sermon-table").waitFor({ state: "visible", timeout: 5000 });
      // The page's own Back (My work) clears the address.
      await page.goBack();
      await page.locator(".sermon-layout:visible").first().waitFor({ timeout: 5000 });
      await page.locator(".crumb-back").click();
      await page.locator(".sermon-table").waitFor({ state: "visible", timeout: 5000 });
      t.assert.equal(await page.evaluate(() => location.hash), "#sermons", "closing the sermon clears the address");
    },
  },
];
