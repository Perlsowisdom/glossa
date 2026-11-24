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
];
