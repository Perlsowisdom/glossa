// Quality pass checks (branch m3/qol): bugs the person reported, each reproduced in the real app first.
//   1. Print or export: Print really opens something, and every save format writes a file and says where.
//   2. The right sidebar closes and reopens the same way on every page.
//   3. Changing the assistant settings updates the assistant sidebar at once.

import { existsSync, mkdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";

const SIZES = [[1536, 816], [900, 700]];
const THEMES = ["light", "dark"];

// looks takes the screenshot of the state on screen at both sizes in both themes: qol-<name>-<w>x<h>-<theme>.png.
export async function looks(t, name) {
  const { page, app } = t;
  try {
    for (const [w, h] of SIZES) {
      await app.resize(w, h);
      for (const theme of THEMES) {
        await page.emulateMedia({ colorScheme: theme });
        await page.waitForTimeout(250);
        await app.shot(`qol-${name}-${w}x${h}-${theme}.png`);
      }
    }
  } finally {
    await page.emulateMedia({ colorScheme: null }).catch(() => {});
    await app.resize(1480, 950).catch(() => {});
  }
}

// ---- 1. Print or export -------------------------------------------------------------------------

// stubDialogs stands in for what a test cannot drive: the system save dialog answers with a path (or "" for
// Cancel), the system opener records the file it was given, and window.print only counts (the real one blocks
// the window in WebView2, which is why Print no longer uses it).
async function stubDialogs(page, dir) {
  await page.evaluate((dir) => {
    window.__opened = [];
    window.__printCalls = 0;
    window.__cancel = false;
    window.print = () => { window.__printCalls++; };
    window.go.main.App.SaveFile = async (_title, name) => (window.__cancel ? "" : `${dir}/${name}`);
    window.go.main.App.OpenFile = async (path) => { window.__opened.push(path); };
  }, dir);
}

const SAVES = [["PDF", "pdf", "%PDF-"], ["Word", "docx", "PK"], ["Web page", "html", "<"], ["Markdown", "md", ""]];

export default [
  {
    row: "Print or save: Print opens a PDF to print; PDF, Word, web page and Markdown each write a file and say where it went",
    shot: "qol-print-dialog.png",
    timeout: 180000,
    async run(t) {
      const { page, app } = t;
      const dir = join(dirname(app.library), "exports-qol");
      mkdirSync(dir, { recursive: true });
      await stubDialogs(page, dir);
      await t.openNote("# A note to print\n\nGrace and peace to you.", "Print me");
      await page.locator(`${t.root} button[aria-label='Note actions']`).click();
      await page.locator(".popover .menu-list button", { hasText: "Print or export…" }).click();
      const dlg = page.locator("dialog.export-dialog[open]");
      await dlg.locator(".paper *").first().waitFor({ timeout: 15000 });

      // Print: a PDF file is made and handed to the system's viewer. The window's own print is never called.
      await dlg.locator(".export-actions button.primary").click();
      await page.waitForFunction(() => window.__opened.length === 1, null, { timeout: 15000 });
      const printed = await page.evaluate(() => window.__opened[0]);
      t.assert.ok(printed.toLowerCase().endsWith(".pdf") && existsSync(printed), `a PDF was opened: ${printed}`);
      t.assert.ok(readFileSync(printed).subarray(0, 5).toString() === "%PDF-", "the opened file is a PDF");
      t.assert.equal(await page.evaluate(() => window.__printCalls), 0, "window.print is not used");
      await dlg.locator(".msg.ok", { hasText: "Opened in your PDF viewer" }).waitFor({ timeout: 5000 });
      await t.shot("qol-print-opened.png");

      // Saving: each format writes the file where the save dialog said, and the dialog says so.
      for (const [label, ext, magic] of SAVES) {
        const ok = dlg.locator(".msg.ok", { hasText: `Saved ${label}:` });
        await dlg.locator(".export-actions button", { hasText: new RegExp(`^${label}$`) }).click();
        await ok.waitFor({ timeout: 20000 });
        const path = (await ok.textContent()).replace(`Saved ${label}: `, "").split(" — ")[0].trim();
        t.assert.ok(path.startsWith(dir) && path.includes("Print me") && path.endsWith(`.${ext}`), `${label} went to ${path}`);
        t.assert.ok(existsSync(path) && statSync(path).size > 20, `${label} file exists`);
        t.assert.ok(readFileSync(path).toString("latin1").startsWith(magic), `${label} file starts like a ${label} file`);
        if (ext === "md" || ext === "html") t.assert.ok(readFileSync(path, "utf8").includes("Grace and peace"), `${label} holds the note's text`);
      }

      // Cancelling the save dialog writes nothing and says nothing new.
      await page.evaluate(() => { window.__cancel = true; });
      const before = await dlg.locator(".msg.ok").textContent();
      await dlg.locator(".export-actions button", { hasText: /^PDF$/ }).click();
      await page.waitForTimeout(800);
      t.assert.equal(await dlg.locator(".msg.ok").textContent(), before, "Cancel changes nothing");
      await t.shot();
    },
  },
  {
    row: "Print or save: the dialog fits at 1536 x 816 and 900 x 700, light and dark",
    shot: "qol-print-fit.png",
    async run(t) {
      const { page } = t;
      const dlg = page.locator("dialog.export-dialog[open]");
      if (!(await dlg.count())) {
        await t.openNote("# A note to print\n\nGrace and peace to you.", "Print me");
        await page.locator(`${t.root} button[aria-label='Note actions']`).click();
        await page.locator(".popover .menu-list button", { hasText: "Print or export…" }).click();
        await dlg.locator(".paper *").first().waitFor({ timeout: 15000 });
      }
      await looks(t, "print");
      await dlg.locator("button[aria-label=Close]").click();
    },
  },
  // ---- 2. The right sidebar --------------------------------------------------------------------
  {
    row: "Right sidebar: closes and reopens from a visible button on every page, and is remembered",
    shot: "qol-sidebar.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      const open = () => page.locator("#rightbar").isVisible().catch(() => false);
      const control = () => page.locator("button[aria-controls=rightbar]:visible").first();
      const settle = async (want) => { try { await page.locator("#rightbar").waitFor({ state: want ? "visible" : "hidden", timeout: 4000 }); } catch { /* reported below */ } };
      const problems = [];
      for (const hash of ["#reader", "#library", "#bibles", "#search", "#notes", "#settings/appearance", "#settings/model", "#settings/data"]) {
        await page.evaluate((x) => { location.hash = x; }, hash);
        await page.locator(".view:visible").first().waitFor({ timeout: 15000 });
        await t.settled();
        if (!(await open())) { await page.keyboard.press("Control+Period"); await settle(true); }
        if (!(await control().count())) { problems.push(`${hash}: no button to close the sidebar`); await page.keyboard.press("Control+Period"); await settle(false); continue; }
        await control().click();
        await settle(false);
        if (await open()) problems.push(`${hash}: the button did not close the sidebar`);
        await control().click();
        await settle(true);
        if (!(await open())) problems.push(`${hash}: the button did not open the sidebar again`);
        await control().click();
        await settle(false);
      }
      t.assert.deepEqual(problems, [], problems.join("; "));

      // Remembered: open on Library, reload, still open there and on another page; close, reload, still closed.
      await page.evaluate(() => { location.hash = "#library"; });
      await page.locator(".view:visible").first().waitFor();
      await control().click();
      await settle(true);
      await page.reload();
      await page.waitForSelector(".shell");
      t.assert.ok(await open(), "open after a reload on Library");
      await page.evaluate(() => { location.hash = "#settings/model"; });
      await page.locator(".settings-section:visible").first().waitFor();
      t.assert.ok(await open(), "open on Settings too");
      await control().click();
      await settle(false);
      await page.reload();
      await page.waitForSelector(".shell");
      t.assert.ok(!(await open()), "closed after a reload");
    },
  },
  {
    row: "Right sidebar: Library and Settings with it open and closed, at 1536 x 816 and 900 x 700, light and dark",
    shot: "qol-sidebar-fit.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      const open = () => page.locator("#rightbar").isVisible().catch(() => false);
      for (const [hash, name] of [["#library", "library"], ["#settings/model", "settings"]]) {
        await page.evaluate((x) => { location.hash = x; }, hash);
        await page.locator(".view:visible").first().waitFor();
        for (const want of [true, false]) {
          if ((await open()) !== want) await page.locator("button[aria-controls=rightbar]:visible").first().click();
          await page.waitForTimeout(300);
          await looks(t, `sidebar-${name}-${want ? "open" : "closed"}`);
        }
      }
    },
  },
  // ---- 3. Assistant settings and the assistant sidebar -------------------------------------------
  {
    row: "Assistant: changing the provider in Settings updates the open AI chat sidebar at once",
    shot: "qol-assistant-sync.png",
    timeout: 120000,
    async run(t) {
      const { page, app } = t;
      await app.run("assistant.config.set", { provider: "off" });
      await page.evaluate(() => { location.hash = "#reader"; });
      await page.locator(".view:visible").first().waitFor();
      if (!(await page.locator("#rightbar").isVisible().catch(() => false))) await page.keyboard.press("Control+Period");
      await page.locator("#rightbar [role=tab]", { hasText: "AI chat" }).click();
      const bar = page.locator("#rightbar");
      await bar.locator(".chat-off").waitFor({ timeout: 10000 });
      // The sidebar stays open on the AI chat tool while Settings changes the provider.
      await page.evaluate(() => { location.hash = "#settings/model"; });
      const sec = page.locator(".settings-section:visible");
      await sec.locator(".provider-row button", { hasText: "Change" }).click();
      await sec.locator(".provider-chip[data-id=claude-code][data-state=ready]").waitFor();
      await sec.locator(".provider-chip[data-id=claude-code]").click();
      await sec.locator(".provider-current", { hasText: "Claude Code" }).waitFor();
      await bar.locator(".chat-input").waitFor({ timeout: 5000 });
      t.assert.ok((await bar.locator(".chat-provider").textContent()).includes("Claude Code"), "the sidebar names the new provider");
      await looks(t, "assistant-sync-on");
      // Turning it off shows the off panel again, also without leaving Settings.
      await sec.locator(".provider-chip[data-id=off]").click();
      await bar.locator(".chat-off").waitFor({ timeout: 5000 });
      await looks(t, "assistant-sync-off");
      await page.keyboard.press("Control+Period");
    },
  },
  // ---- Found in the audit (docs/qol-audit.md) ----------------------------------------------------
  {
    row: "My work: the 'went to the bin' notice after deleting a folder goes away by itself",
    shot: "qol-bin-notice.png",
    timeout: 90000,
    async run(t) {
      const { page, app } = t;
      const f = await app.run("folder.create", { name: "Notice folder" });
      await page.evaluate(() => { location.hash = "#notes"; });
      await page.locator("button[aria-label='Actions for Notice folder']").click();
      await page.locator(".popover button", { hasText: "Delete folder" }).click();
      const notice = page.locator(".work-col .msg, .work-side .msg, aside .msg", { hasText: "went to the bin" }).first();
      await notice.waitFor({ timeout: 5000 });
      await page.evaluate(() => { location.hash = "#lists"; });
      await notice.waitFor({ timeout: 2000 }); // still there for a moment, on the next page too
      await page.waitForFunction(() => ![...document.querySelectorAll(".msg")].some((e) => e.textContent.includes("went to the bin")), null, { timeout: 25000 });
      await app.run("bin.restore", { kind: "folder", id: f.id }).catch(() => {});
    },
  },
  {
    row: "Lists: deleting a list says it goes to the bin (where it does go)",
    shot: "qol-list-delete.png",
    async run(t) {
      const { page, app } = t;
      await app.run("list.create", { kind: "verses", name: "Words list" });
      await page.evaluate(() => { location.hash = "#notes"; });
      await page.waitForTimeout(300);
      await page.evaluate(() => { location.hash = "#lists"; });
      await page.locator("main button", { hasText: "Words list" }).first().click();
      let said = "";
      page.once("dialog", (d) => { said = d.message(); void d.dismiss(); });
      await page.locator("main button", { hasText: "Delete list" }).first().click();
      await page.waitForTimeout(500);
      t.assert.ok(/bin/.test(said) && !/change history/.test(said), `the question says where the list goes: ${said}`);
    },
  },
  {
    row: "Sermons: choosing Sermons in My work while a sermon is open goes back to the list",
    shot: "qol-sermons-list.png",
    async run(t) {
      const { page, app } = t;
      const d = await app.run("document.create", { kind: "sermon" });
      await app.run("document.update", { id: d.id, title: "List me", kind: "sermon", body: "Text" });
      await page.evaluate((id) => { location.hash = `#sermons/${id}`; }, d.id);
      await t.current(".sermon-layout");
      await page.locator(".work-side a", { hasText: "Sermons" }).click();
      await page.locator(".sermon-table").waitFor({ state: "visible", timeout: 5000 });
      await page.locator("tr", { hasText: "List me" }).first().waitFor({ timeout: 5000 });
      t.assert.equal(await page.locator(".sermon-layout:visible").count(), 0, "the sermon page is closed");
    },
  },
];
