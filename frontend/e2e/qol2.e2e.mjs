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
  {
    row: "Reader: Print or save in the ⋯ menu is disabled, with a plain reason, when the selected pane has nothing to print",
    shot: "qol2-print-disabled.png",
    timeout: 90000,
    async run(t) {
      const { page } = t;
      await page.evaluate(() => { location.hash = "#reader"; });
      await page.locator(".view.reader:visible").waitFor();
      await openNothingToPrint(t);
      const openMore = async () => { await page.keyboard.press("Escape"); await page.locator(".topbar button[aria-label=More]").click(); await page.locator(".popover").waitFor(); };
      await openMore();
      const item = page.locator(".popover .menu-list button", { hasText: "Print or save" });
      await item.waitFor();
      t.assert.ok(await item.isDisabled(), "Print or save is disabled");
      await page.locator(".popover", { hasText: "to print" }).locator("text=/Select a pane with a passage, book or note to print/").waitFor({ timeout: 3000 });
      await t.shot();
      await looks(t, "print-disabled", openMore);
      await page.keyboard.press("Escape");
    },
  },
  {
    row: "Ctrl+P: prints the open note, sermon or pane; with nothing to print it says so in the Reader and does nothing on other pages, never the window's own print",
    shot: "qol2-ctrl-p.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      await page.evaluate(() => {
        window.__printCalls = 0;
        window.print = () => { window.__printCalls++; };
        window.__prevented = null;
        window.addEventListener("keydown", (e) => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "p") window.__prevented = e.defaultPrevented; });
      });
      const dialogOpen = () => page.locator("dialog.export-dialog[open]").count();
      const press = async () => { await page.evaluate(() => { window.__prevented = null; }); await page.keyboard.press("Control+p"); await page.waitForTimeout(500); };
      // Pages with no item: the key is taken (so the window never prints) and nothing opens.
      const problems = [];
      for (const hash of ["#library", "#bibles", "#search", "#lists", "#plans", "#bin", "#settings/data", "#settings/model", "#notes", "#sermons"]) {
        await page.evaluate((x) => { location.hash = x; }, hash);
        await page.locator(".view:visible").first().waitFor({ timeout: 15000 });
        await t.settled();
        await page.locator("main").click({ position: { x: 5, y: 5 } }).catch(() => {});
        await press();
        if (!(await page.evaluate(() => window.__prevented))) problems.push(`${hash}: Ctrl+P was left to the window`);
        if (await dialogOpen()) { problems.push(`${hash}: a print dialog opened`); await page.keyboard.press("Escape"); }
      }
      t.assert.deepEqual(problems, [], problems.join("; "));
      t.assert.equal(await page.evaluate(() => window.__printCalls), 0, "window.print is not used");
      // Reader, a pane with nothing to print: a plain notice.
      await page.evaluate(() => { location.hash = "#reader"; });
      await page.locator(".view.reader:visible").waitFor();
      await openNothingToPrint(t);
      await page.locator("main").click({ position: { x: 5, y: 5 } }).catch(() => {});
      await press();
      await page.locator(".view.reader .msg", { hasText: "Select a pane with a passage, book or note to print" }).waitFor({ timeout: 3000 });
      t.assert.equal(await dialogOpen(), 0, "no dialog");
      // A note and a sermon open in My work: Ctrl+P opens Print or save for it.
      await t.openNote("# Printed by key\n\nGrace.", "Key note");
      await page.locator(`${t.root} .rich-text`).click();
      await press();
      await page.locator("dialog.export-dialog[open]").waitFor({ timeout: 10000 });
      t.assert.ok((await page.locator("dialog.export-dialog[open]").textContent()).includes("Key note"), "the note's dialog");
      await page.keyboard.press("Escape");
      await page.locator("dialog.export-dialog[open]").waitFor({ state: "detached", timeout: 5000 }).catch(() => {});
      await t.openSermon("Text", "Key sermon");
      await page.locator(`${t.root} .rich-text`).click();
      await press();
      await page.locator("dialog.export-dialog[open]").waitFor({ timeout: 10000 });
      t.assert.ok((await page.locator("dialog.export-dialog[open]").textContent()).includes("Key sermon"), "the sermon's dialog");
      await t.shot();
      await page.keyboard.press("Escape");
    },
  },
];

// openNothingToPrint puts the Reader in Study mode with a Library browser tab selected: a pane with nothing to print.
async function openNothingToPrint(t) {
  const { page } = t;
  const study = page.locator(".topbar .segmented button", { hasText: "Study" });
  if ((await study.getAttribute("aria-pressed")) !== "true") await study.click();
  await page.locator(".grid-pane:visible").first().waitFor();
  await page.locator(".grid-plus:visible").first().click();
  await page.locator(".popover .menu-list button", { hasText: "Library browser" }).click();
  await page.locator(".grid-pane:visible .doc").first().waitFor();
  await t.settled();
}
