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
  {
    row: "Delete: notes, sermons, folders and lists all go to the bin at once with the same notice and Undo that clears itself; a reading plan asks first and says it is deleted for good",
    shot: "qol2-delete.png",
    timeout: 240000,
    async run(t) {
      const { page, app } = t;
      const bin = (name) => page.locator(".msg", { hasText: `"${name}" went to the bin.` }).filter({ has: page.locator("button", { hasText: "Undo" }) }).first();
      const go = async (hash, sel) => { await page.evaluate((x) => { location.hash = x; }, hash); await page.locator(sel).first().waitFor({ timeout: 10000 }); };
      let asked = 0;
      page.on("dialog", (d) => { asked++; void d.dismiss(); });
      const note = await app.run("note.create", { title: "Gone note", body: "x" });
      const sermon = await app.run("document.create", { kind: "sermon" });
      await app.run("document.update", { id: sermon.id, title: "Gone sermon", kind: "sermon", body: "x" });
      const list = await app.run("list.create", { kind: "verses", name: "Gone list" });
      const folder = await app.run("folder.create", { name: "Gone folder" });
      const kept = [];

      // Note, from its card.
      await go("#notes", ".work-card");
      await page.locator("button[aria-label='Actions for Gone note']").click();
      await page.locator(".popover button", { hasText: /^Delete$/ }).click();
      await bin("Gone note").waitFor({ timeout: 5000 });
      // Sermon, from its row.
      await go("#sermons", ".sermon-table");
      await page.locator("button[aria-label='Actions for Gone sermon']").click();
      await page.locator(".popover button", { hasText: /^Delete$/ }).click();
      await bin("Gone sermon").waitFor({ timeout: 5000 });
      // List: no question, same notice.
      await go("#lists", "main");
      await page.locator("main button", { hasText: "Gone list" }).first().click();
      await page.locator("main button", { hasText: "Delete list" }).first().click();
      await bin("Gone list").waitFor({ timeout: 5000 });
      // Folder.
      await go("#notes", ".work-card, .work-side");
      await page.locator("button[aria-label='Actions for Gone folder']").click();
      await page.locator(".popover button", { hasText: "Delete folder" }).click();
      await page.locator(".msg", { hasText: "\"Gone folder\" went to the bin." }).first().waitFor({ timeout: 5000 });
      t.assert.equal(asked, 0, "none of the four asked a question first");
      await t.shot();
      await looks(t, "delete-notice");

      // Undo brings each back, with the same words.
      await go("#lists", "main");
      await bin("Gone list").locator("button", { hasText: "Undo" }).click();
      await page.locator(".msg", { hasText: "\"Gone list\" is back." }).first().waitFor({ timeout: 5000 });
      await go("#sermons", ".sermon-table");
      await bin("Gone sermon").locator("button", { hasText: "Undo" }).click();
      await page.locator(".msg", { hasText: "\"Gone sermon\" is back." }).first().waitFor({ timeout: 5000 });
      await go("#notes", ".work-side");
      await bin("Gone note").locator("button", { hasText: "Undo" }).click();
      await page.locator(".msg", { hasText: "\"Gone note\" is back." }).first().waitFor({ timeout: 5000 });
      const back = await app.run("bin.list", {});
      t.assert.ok(!back.some((e) => ["Gone note", "Gone sermon", "Gone list"].includes(e.title)), "Undo took them out of the bin");

      // The notices clear themselves (all four started together; wait once).
      await go("#notes", ".work-side");
      await page.locator("button[aria-label='Actions for Gone note']").click();
      await page.locator(".popover button", { hasText: /^Delete$/ }).click();
      await bin("Gone note").waitFor({ timeout: 5000 });
      await page.waitForFunction(() => ![...document.querySelectorAll(".msg")].some((e) => e.textContent.includes("went to the bin")), null, { timeout: 25000 });

      // A reading plan cannot go to the bin, so it asks, and says so plainly.
      const plan = await app.run("plan.create", { template: "nt-90" });
      await go("#plans", "main");
      let said = "";
      page.removeAllListeners("dialog");
      page.once("dialog", (d) => { said = d.message(); void d.accept(); });
      await page.locator(`button[aria-label^='Delete ']`).first().click();
      await page.locator(".msg", { hasText: "deleted for good" }).first().waitFor({ timeout: 5000 });
      t.assert.ok(/for good/.test(said) && /bin/.test(said) && !/goes to the bin/.test(said), `the plan question is plain: ${said}`);
      void kept; void plan; void folder; void list; void sermon; void note;
    },
  },
  {
    row: "Settings, Model: the Provider row reads in one or two lines at 900 x 700 with the sidebar open",
    shot: "qol2-provider-row.png",
    timeout: 90000,
    async run(t) {
      const { page, app } = t;
      await app.run("assistant.config.set", { provider: "claude-code" });
      await app.resize(900, 700);
      await page.evaluate(() => { location.hash = "#settings/model"; });
      const sec = page.locator(".settings-section:visible");
      await sec.locator(".provider-current", { hasText: "Claude Code" }).waitFor({ timeout: 10000 });
      if (!(await page.locator("#rightbar").isVisible().catch(() => false))) await page.keyboard.press("Control+Period");
      await page.locator("#rightbar").waitFor({ timeout: 5000 });
      await page.waitForTimeout(300);
      const m = await sec.locator(".provider-current").evaluate((e) => {
        const r = e.getBoundingClientRect();
        const lh = parseFloat(getComputedStyle(e).lineHeight) || parseFloat(getComputedStyle(e).fontSize) * 1.4;
        return { lines: Math.round(r.height / lh), width: Math.round(r.width), h: Math.round(r.height) };
      });
      await looks(t, "provider-row");
      await app.resize(900, 700);
      await page.keyboard.press("Control+Period");
      await app.resize(1480, 950);
      t.assert.ok(m.lines <= 2, `the Provider value takes ${m.lines} lines (${m.width}px wide)`);
    },
  },
  {
    row: "Settings, Your data: one heading, and a clear gap between the chips and the history switch",
    shot: "qol2-your-data.png",
    timeout: 90000,
    async run(t) {
      const { page, app } = t;
      await page.evaluate(() => { location.hash = "#settings/data"; });
      const sec = page.locator(".settings-section:visible");
      await sec.locator("fieldset .chip-toggle").first().waitFor({ timeout: 10000 });
      const check = async () => {
        const heads = await sec.evaluate((s) => [...s.querySelectorAll("h1,h2,h3")].map((x) => x.textContent.trim()));
        const gap = await sec.evaluate((s) => {
          const sw = s.querySelector("fieldset label.check");
          const chips = [...s.querySelectorAll("fieldset .chip-toggle")];
          const a = sw.getBoundingClientRect();
          let best = 1e9;
          for (const c of chips) {
            const b = c.getBoundingClientRect();
            const dx = Math.max(b.left - a.right, a.left - b.right);
            const dy = Math.max(b.top - a.bottom, a.top - b.bottom);
            best = Math.min(best, Math.max(dx, dy));
          }
          return Math.round(best);
        });
        return { heads, gap };
      };
      const r = await check();
      t.assert.equal(r.heads.filter((x) => x === "Your data").length, 1, `"Your data" once: ${r.heads.join(" | ")}`);
      t.assert.ok(r.gap >= 8, `a gap of ${r.gap}px between the last chip and the switch`);
      await looks(t, "your-data");
      await app.resize(900, 700);
      const r2 = await check();
      await app.resize(1480, 950);
      t.assert.ok(r2.gap >= 8, `at 900 wide: a gap of ${r2.gap}px`);
    },
  },
  {
    row: "Settings, Sync: the password warning shows once something is typed, and the boxes are the same width",
    shot: "qol2-sync-warning.png",
    timeout: 90000,
    async run(t) {
      const { page } = t;
      await page.evaluate(() => { location.hash = "#settings/sync"; });
      const form = page.locator("form.sync-setup");
      await form.waitFor({ timeout: 10000 });
      const warn = form.locator(".sync-warning");
      t.assert.ok(!(await warn.isVisible()), "no warning while the password is empty");
      const widths = await form.evaluate((f) => ({ folder: f.querySelector("#sync-folder").getBoundingClientRect().width, pw: f.querySelector("input[type=password]").getBoundingClientRect().width }));
      t.assert.ok(Math.abs(widths.folder - widths.pw) < 2, `the boxes: folder ${widths.folder}, password ${widths.pw}`);
      await looks(t, "sync-empty");
      await form.locator("input[type=password]").first().fill("x");
      await warn.waitFor({ state: "visible", timeout: 3000 });
      await form.locator("input[type=password]").first().fill("");
      await warn.waitFor({ state: "hidden", timeout: 3000 });
      await form.locator("input[type=password]").first().fill("a long enough password 123");
      await warn.waitFor({ state: "visible", timeout: 3000 });
      await looks(t, "sync-typed");
      await t.shot();
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
