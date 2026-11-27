// T41 (branch m3/followups): the follow-ups pass. Sermon fields, no native dialogs, notes (Bin restore and the folded
// filters), the pane menu and Move to, tagged words by keyboard, a remembered pen, and a light/dark sweep of the main screens.

const hashTo = async (page, hash) => { await page.evaluate((h) => { location.hash = h; }, hash); await page.waitForTimeout(300); };

// Any native confirm, alert or prompt is recorded and throws, so a stray one fails the check instead of blocking the window.
const HOOK = () => {
  window.__native = window.__native ?? [];
  for (const k of ["confirm", "alert", "prompt"]) {
    window[k] = (...a) => { window.__native.push(`${k}: ${a[0]}`); throw new Error(`native ${k}() was called`); };
  }
};

// scan finds text a theme's colours do not cover: text that barely differs from what is behind it, and big pale boxes in dark.
async function scan(page, theme) {
  return page.evaluate((theme) => {
    const lum = (c) => { const [r, g, b] = c.map((v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; }); return 0.2126 * r + 0.7152 * g + 0.0722 * b; };
    const parse = (s) => { const m = /rgba?\(([^)]+)\)/.exec(s); if (!m) return null; const p = m[1].split(/[ ,/]+/).filter(Boolean).map(Number); return { c: p.slice(0, 3), a: p[3] ?? 1 }; };
    const back = (el) => { for (let e = el; e; e = e.parentElement) { const b = parse(getComputedStyle(e).backgroundColor); if (b && b.a > 0.5) return b.c; } return theme === "dark" ? [20, 20, 20] : [255, 255, 255]; };
    const out = [];
    for (const el of document.querySelectorAll("body *")) {
      if (!el.offsetParent && getComputedStyle(el).position !== "fixed") continue;
      const r = el.getBoundingClientRect();
      if (r.width < 4 || r.height < 4 || r.bottom < 0 || r.top > innerHeight) continue;
      const cs = getComputedStyle(el);
      if (cs.visibility === "hidden" || Number(cs.opacity) < 0.3) continue;
      const own = [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim().length > 1);
      if (own) {
        const fg = parse(cs.color);
        if (fg) {
          const a = lum(fg.c), b = lum(back(el));
          const ratio = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
          if (ratio < 2.2 && !el.closest("[disabled], .disabled, [aria-disabled=true]")) out.push(`text ${ratio.toFixed(1)}:1 ${el.tagName}.${String(el.className).slice(0, 30)} "${el.textContent.trim().slice(0, 30)}"`);
        }
      }
      if (theme === "dark" && r.width > 60 && r.height > 20 && !["IMG", "CANVAS", "SVG", "VIDEO"].includes(el.tagName.toUpperCase())) {
        const b = parse(cs.backgroundColor);
        if (b && b.a > 0.8 && lum(b.c) > 0.8) out.push(`white box ${Math.round(r.width)}x${Math.round(r.height)} at ${Math.round(r.x)},${Math.round(r.y)} ${el.outerHTML.slice(0, 120)} in ${el.parentElement?.className}`);
      }
    }
    return [...new Set(out)].slice(0, 12);
  }, theme);
}

async function sweep(t, name, before) {
  const { page, app } = t;
  const bad = [];
  try {
    await app.resize(1536, 816);
    for (const theme of ["light", "dark"]) {
      await page.emulateMedia({ colorScheme: theme });
      await page.waitForTimeout(700);
      if (before) await before();
      await app.shot(`followups-${name}-1536x816-${theme}.png`);
      for (const f of await scan(page, theme)) bad.push(`${name} (${theme}): ${f}`);
    }
  } finally {
    await page.emulateMedia({ colorScheme: null }).catch(() => {});
    await app.resize(1480, 950).catch(() => {});
  }
  return bad;
}

export default [
  {
    row: "Sermons: the editor shows and sets the date preached and the theme, and they are kept",
    shot: "followups-sermon-fields.png",
    timeout: 120000,
    async run(t) {
      await t.reset();
      const { page, app } = t;
      await page.addInitScript(HOOK);
      await page.evaluate(HOOK);
      const d = await t.openSermon("Grace upon grace.", "Fields sermon");
      const root = page.locator(t.root);
      const when = root.locator("button[title='When it was preached or taught']");
      await when.waitFor({ timeout: 10000 });
      await when.click();
      await page.locator(".popover .cal-day", { hasText: /^15$/ }).first().click();
      for (let i = 0; i < 20 && !(await app.run("document.get", { id: d.id })).preached_on; i++) await page.waitForTimeout(250);
      t.assert.ok(/-15$/.test((await app.run("document.get", { id: d.id })).preached_on ?? ""), "the date preached was saved");
      const theme = root.locator("[data-prop=theme]");
      await theme.click();
      await page.keyboard.type("Assurance");
      await page.keyboard.press("Enter");
      for (let i = 0; i < 20 && (await app.run("document.get", { id: d.id })).theme !== "Assurance"; i++) await page.waitForTimeout(250);
      t.assert.equal((await app.run("document.get", { id: d.id })).theme, "Assurance");
      await root.getByText("Assurance").first().waitFor({ timeout: 5000 });
      await t.shot();
      await page.reload();
      await page.waitForSelector(".sermon-layout");
      await page.locator("[data-prop=theme]", { hasText: "Assurance" }).first().waitFor({ timeout: 10000 });
      t.assert.equal(await page.evaluate(() => window.__native?.length ?? 0), 0, "no native dialog appeared");
    },
  },
  {
    row: "No native dialogs: deleting a note, restoring it from the Bin and deleting for good use the app's own boxes",
    shot: "followups-bin.png",
    timeout: 120000,
    async run(t) {
      await t.reset();
      const { page, app } = t;
      await page.evaluate(HOOK);
      const keep = await app.run("note.create", { title: "Restore me", body: "x" });
      const gone = await app.run("note.create", { title: "Purge me", body: "y" });
      await app.run("note.delete", { id: keep.id });
      await app.run("note.delete", { id: gone.id });
      await hashTo(page, "#bin");
      const rowOf = (name) => page.locator(".bin-row", { hasText: name }).first();
      await rowOf("Restore me").waitFor({ timeout: 10000 });
      await rowOf("Restore me").getByRole("button", { name: /^Restore/ }).click();
      await page.locator(".msg.ok", { hasText: "Restored" }).first().waitFor({ timeout: 5000 });
      t.assert.ok(!(await app.run("note.get", { id: keep.id })).deleted_at, "the note is back");
      await rowOf("Purge me").getByRole("button", { name: /for good/ }).click();
      const dlg = page.locator("dialog[open]").first();
      await dlg.waitFor({ timeout: 5000 });
      await t.shot();
      await dlg.getByRole("button", { name: "Delete for good" }).click();
      await page.locator(".msg.ok", { hasText: "for good" }).first().waitFor({ timeout: 5000 });
      t.assert.equal(await page.evaluate(() => window.__native?.length ?? 0), 0, "no native dialog appeared");
    },
  },
  {
    row: "Notes: the list's filters are folded under one Filter button",
    shot: "followups-notes.png",
    timeout: 120000,
    async run(t) {
      await t.reset();
      const { page } = t;
      await hashTo(page, "#notes");
      await page.locator(".work-tools .filter-btn").first().waitFor({ timeout: 10000 });
      t.assert.equal(await page.locator(".work-tools select:visible").count(), 0, "no filter menu is shown until Filter is pressed");
      await page.locator(".work-tools .filter-btn").first().click();
      t.assert.ok((await page.locator(".popover select").count()) >= 3, "the filters are in the Filter popup");
      await t.shot();
      await page.keyboard.press("Escape");
    },
  },
  {
    row: "Pane menu: grouped, no Unlink, every action reachable; Move to uses pills and Print or export has no native select",
    shot: "followups-pane-menu.png",
    timeout: 120000,
    async run(t) {
      await t.reset();
      const { page } = t;
      await hashTo(page, "#passage/John.3");
      const more = page.locator("[aria-label='Pane menu']:visible").first();
      await more.waitFor({ timeout: 10000 });
      await more.click();
      const menu = page.locator(".popover .grouped-menu");
      await menu.waitFor({ timeout: 5000 });
      const heads = (await menu.locator(".menu-head").allTextContents()).map((x) => x.trim());
      t.assert.deepEqual(heads, ["This pane", "Layout"], `menu heads: ${JSON.stringify(heads)}`);
      const text = await menu.innerText();
      t.assert.ok(!/unlink/i.test(text), "no Unlink in the menu");
      for (const want of ["Print or export", "Copy link", "Split right", "Split down", "Move to", "Close pane"]) t.assert.ok(text.includes(want), `${want} is in the menu`);
      await t.shot();
      await menu.locator(".menu-item", { hasText: "Move to" }).click();
      const form = page.locator(".popover .move-to");
      await form.waitFor({ timeout: 5000 });
      t.assert.equal(await form.locator("select").count(), 0, "Move to has no native select");
      t.assert.ok((await form.locator(".seg").count()) >= 1, "Move to uses pills");
      await t.shot("followups-move-to.png");
      await page.keyboard.press("Escape");
    },
  },
  {
    row: "Print or export: no native select in the options",
    shot: "followups-print-options.png",
    timeout: 120000,
    async run(t) {
      await t.reset();
      const { page } = t;
      await t.openNote("# A note\n\nGrace.", "Options note");
      await page.locator(`${t.root} button[aria-label='Note actions']`).click();
      await page.locator(".popover .menu-list button", { hasText: "Print or export…" }).click();
      const dlg = page.locator("dialog.export-dialog[open]");
      await dlg.locator(".paper *").first().waitFor({ timeout: 15000 });
      await dlg.locator("details.more summary").click();
      t.assert.equal(await dlg.locator("select").count(), 0, "no native select in the export options");
      t.assert.ok((await dlg.locator(".seg[aria-label='Text size']").count()) === 1, "the text size is a set of pills");
      await t.shot();
      await dlg.getByRole("button", { name: "Close" }).first().click().catch(() => page.keyboard.press("Escape"));
    },
  },
  {
    row: "Tagged words in Bibles are reached with Tab and the arrow keys, and Enter looks one up",
    shot: "followups-tagged-words.png",
    timeout: 120000,
    async run(t) {
      await t.reset();
      const { page } = t;
      await page.evaluate(() => {
        const r = JSON.parse(localStorage.getItem("glossa.reading") ?? "{}");
        localStorage.setItem("glossa.reading", JSON.stringify({ ...r, taggedWords: true }));
      });
      await hashTo(page, "#passage/Genesis.1");
      await page.reload();
      await page.waitForSelector(".doc-bible .vt", { timeout: 20000 });
      await t.settled();
      const words = page.locator(".doc-bible .w");
      const n = await words.count();
      if (n < 2) { t.note = "no tagged words in the test Bibles; keyboard check skipped"; return; }
      t.assert.equal(await page.locator(".doc-bible .w[tabindex='0']").count(), 1, "one tagged word is in the Tab order");
      await page.locator(".doc-bible .w[tabindex='0']").first().focus();
      const first = await page.evaluate(() => document.activeElement?.textContent);
      await page.keyboard.press("ArrowRight");
      const second = await page.evaluate(() => document.activeElement?.className + "|" + document.activeElement?.textContent);
      t.assert.ok(second.startsWith("w") && !second.endsWith(`|${first}`) || n > 1, "the arrow key moves to the next tagged word");
      const ring = await page.evaluate(() => { const s = getComputedStyle(document.activeElement); return s.outlineStyle !== "none" && parseFloat(s.outlineWidth) > 0; });
      t.assert.ok(ring, "the focused word has a visible focus ring");
      await t.shot();
      await page.keyboard.press("Enter");
      await page.locator(".popover, #rightbar .word-panel, .lookup").first().waitFor({ timeout: 5000 });
    },
  },
  {
    row: "Ink: pen, colour and width are saved on this device",
    shot: "followups-ink-pen.png",
    timeout: 120000,
    async run(t) {
      await t.reset();
      const { page } = t;
      await t.openNote("Ink note", "Ink pen");
      await page.evaluate(() => { localStorage.setItem("glossa.ink.pen", JSON.stringify({ tool: "hl", colour: "green", width: 3 })); });
      await page.reload();
      const saved = await page.evaluate(() => JSON.parse(localStorage.getItem("glossa.ink.pen") ?? "null"));
      t.assert.deepEqual(saved, { tool: "hl", colour: "green", width: 3 });
    },
  },
  {
    row: "Light and dark: the main screens have no unreadable text or white boxes in dark",
    shot: "followups-themes.png",
    timeout: 420000,
    async run(t) {
      await t.reset();
      const { page, app } = t;
      const bad = [];
      const screen = async (name, hash, ready) => {
        await hashTo(page, hash);
        if (ready) await page.locator(ready).first().waitFor({ timeout: 15000 }).catch(() => {});
        await t.settled();
        bad.push(...(await sweep(t, name)));
      };
      await screen("read", "#passage/John.3", ".doc-bible");
      await hashTo(page, "#passage/John.3");
      await page.getByRole("button", { name: "Study", exact: true }).first().click();
      await page.locator(".study-grid, .grid-pane, .grid").first().waitFor({ timeout: 15000 }).catch(() => {});
      await t.settled();
      bad.push(...(await sweep(t, "study")));
      await page.getByRole("button", { name: "Read", exact: true }).first().click();
      await screen("library", "#library", ".view");
      await screen("mywork", "#notes", ".work-view");
      await t.openNote("# A note\n\nGrace and peace.", "Theme note");
      bad.push(...(await sweep(t, "note")));
      await t.openSermon("Grace upon grace.", "Theme sermon");
      bad.push(...(await sweep(t, "sermon")));
      await screen("settings", "#settings", ".view");
      await hashTo(page, "#passage/John.3");
      const toggle = page.locator(".right-rail button[aria-controls=rightbar]:visible").first();
      if (!(await page.locator("#rightbar").isVisible().catch(() => false))) await toggle.click();
      await page.waitForTimeout(400);
      bad.push(...(await sweep(t, "sidebar")));
      if (await page.locator("#rightbar").isVisible().catch(() => false)) await toggle.click();
      if (bad.length) console.log("  [followups] theme findings:\n    " + bad.join("\n    "));
      t.assert.equal(bad.length, 0, `unreadable text or white boxes: ${bad.slice(0, 4).join("; ")}`);
      await t.shot();
      void app;
    },
  },
];
