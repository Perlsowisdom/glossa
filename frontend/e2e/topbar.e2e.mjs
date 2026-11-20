// The selected pane's reader controls live in the app's top bar (after the search and Aa), not in
// each pane's header. They follow the focused pane, hide for panes with no reader controls, and
// overflow in steps when the bar is narrow.

import { addBooks, closeBooks, openFromLibrary } from "./book.e2e.mjs";
import { openNoteInStudy } from "./layout.e2e.mjs";

const EPUB_TITLE = "E2E Test Book";
const BAR = ".topbar";
const CONTROLS = ".topbar .reader-controls";

// size pins the page to w x h through the DevTools protocol (the harness default is 1480 x 950).
async function size(t, w, h) {
  const cdp = await t.page.context().newCDPSession(t.page);
  await cdp.send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: false });
  await t.page.waitForTimeout(300);
}
const restore = (t) => size(t, 1480, 950);

// twoPanes leaves a Bible in the left pane and, in the right pane, an EPUB tab beside a PDF tab (the PDF shown).
async function twoPanes(t) {
  const { page } = t;
  await addBooks(t);
  await page.evaluate(() => { location.hash = "#reader"; });
  const study = page.locator(".topbar button", { hasText: /^Study$/ }).first();
  if ((await study.getAttribute("aria-pressed")) !== "true") await study.click();
  await openFromLibrary(t, EPUB_TITLE, "Open in Study");
  await page.locator(".grid-pane:visible .book-text p").first().waitFor({ timeout: 15000 });
  await page.locator(".rail-grid:visible, .topbar [aria-label='Grid presets']:visible").first().click();
  await page.locator(".preset-cell[data-c='2'][data-r='1']").click();
  await page.waitForFunction(() => document.querySelectorAll(".grid-pane").length >= 2, null, { timeout: 8000 });
  await page.locator(".grid-pane", { has: page.locator(".grid-tab-btn", { hasText: EPUB_TITLE }) }).locator(".grid-tab-btn").first().click();
  await openFromLibrary(t, "e2e-test-pages", "Open in Study");
  await page.locator(".grid-pane:visible .pdf-box").first().waitFor({ timeout: 15000 });
  await t.settled();
}

// clean closes the test books and any extra panes, and goes back to the default size.
async function clean(t) {
  await restore(t).catch(() => {});
  await t.page.emulateMedia({ colorScheme: "light" }).catch(() => {});
  await closeBooks(t);
  // Back to one pane, so later checks start from the usual grid.
  await t.page.evaluate(() => { location.hash = "#reader"; });
  const note = tab(t, "Topbar note");
  if (await note.count()) await note.dispatchEvent("auxclick", { button: 1 });
  await t.page.locator(".rail-grid:visible, .topbar [aria-label='Grid presets']:visible").first().click().catch(() => {});
  await t.page.locator(".preset-cell[data-c='1'][data-r='1']").click({ timeout: 3000 }).catch(() => {});
  await t.page.waitForTimeout(300);
}

const tab = (t, name) => t.page.locator(".grid-pane .grid-tab-btn", { hasText: name }).first();
// show selects a tab and waits for its place field to hold a number (a tab is mounted when it is shown).
async function show(t, name, unit) {
  await tab(t, name).click();
  await t.page.waitForFunction((unit) => /^[0-9]+$/.test(document.querySelector(".reader-controls .book-where-input")?.value ?? "") && document.querySelector(".reader-controls .book-unit")?.textContent === unit, unit, { timeout: 15000 }).catch(async (e) => {
    throw new Error(`${e.message}
${await t.page.evaluate(() => JSON.stringify({ c: document.querySelector(".reader-controls")?.outerHTML.slice(0, 900), sel: [...document.querySelectorAll(".grid-pane")].map((p) => [p.className, p.textContent.slice(0, 60)]) }))}`);
  });
  await t.settled();
}
const barState = (t) => t.page.evaluate(({ BAR, CONTROLS }) => {
  const bar = document.querySelector(BAR);
  const c = document.querySelector(CONTROLS);
  const kids = [...bar.children].filter((e) => e.offsetParent && !e.hidden);
  const rects = kids.map((e) => e.getBoundingClientRect());
  const overlap = rects.some((a, i) => rects.some((b, j) => j > i && a.width && b.width && a.left < b.right - 1 && b.left < a.right - 1 && a.top < b.bottom - 1 && b.top < a.bottom - 1));
  const input = c?.querySelector(".book-where-input");
  return {
    fit: bar.dataset.fit ?? "",
    scrolls: bar.scrollWidth > bar.clientWidth + 1,
    overlap,
    wraps: new Set(rects.map((r) => Math.round((r.top + r.bottom) / 2 / 8))).size > 1,
    controlsShown: !!c && !c.hidden && !!c.offsetParent,
    fieldValue: input?.value ?? null,
    fieldW: input?.offsetWidth ?? 0,
    fieldHidden: !input || !input.offsetParent,
    unit: c?.querySelector(".book-unit")?.offsetParent ? c.querySelector(".book-unit").textContent : "",
    ofShown: c?.querySelector(".book-of")?.textContent ?? "",
    has: (c ? [...c.querySelectorAll("button, input")].filter((e) => e.offsetParent).map((e) => e.getAttribute("aria-label") || e.className) : []),
  };
}, { BAR, CONTROLS });

export default [
  {
    row: "Top bar: two Study panes keep their reader controls in the top bar, headers hold tabs, + and the link",
    shot: "topbar-01-two-panes.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      try {
        await size(t, 1536, 816);
        await twoPanes(t);
        await show(t, EPUB_TITLE, "Section");
        const s = await barState(t);
        t.assert.ok(s.controlsShown, "controls in the top bar");
        t.assert.ok(!s.fieldHidden && /^\d+$/.test(s.fieldValue), `the section field shows a number: ${JSON.stringify(s)}`);
        t.assert.ok(s.fieldW >= 3 * 7, `the field is wide enough for its digits (${s.fieldW}px)`);
        t.assert.ok(!s.scrolls && !s.overlap && !s.wraps, `top bar fits: ${JSON.stringify(s)}`);
        t.assert.ok(await page.locator(`${CONTROLS} [aria-label='Pane menu']`).isVisible(), "pane menu in the top bar");
        t.assert.ok(await page.locator(`${CONTROLS} .grid-contents`).isVisible(), "contents in the top bar");
        // The headers hold only tabs, + and the link.
        const header = await page.evaluate(() => [...document.querySelectorAll(".grid-pane .grid-bar")].map((b) => ({
          nav: !!b.querySelector(".book-nav, .passage-box, .bibles-btn, .grid-contents, [aria-label='Pane menu']"),
          plus: !!b.querySelector(".grid-plus"), link: !!b.querySelector(".grid-link"), tabs: b.querySelectorAll(".grid-tab").length,
        })));
        t.assert.ok(header.length >= 2 && header.every((x) => !x.nav && x.plus && x.tabs >= 1), JSON.stringify(header));
        t.assert.equal(await page.locator(".grid-pane.selected").count(), 1, "one pane is visibly selected");
        await t.shot("topbar-01-two-panes.png");
        await page.emulateMedia({ colorScheme: "dark" });
        await page.waitForTimeout(300);
        await t.shot("topbar-01-two-panes-dark.png");
      } finally {
        await clean(t);
      }
    },
  },
  {
    row: "Top bar: clicking another pane swaps the controls, and a pane with none (a note) hides them",
    shot: "topbar-02-focus.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      let noteId;
      try {
        await size(t, 1536, 816);
        await twoPanes(t);
        await show(t, EPUB_TITLE, "Section");
        let s = await barState(t);
        t.assert.ok(s.has.includes("Pane menu") && s.has.includes("Contents"), JSON.stringify(s));
        // Into the Bible pane: its reference field and Bibles chip replace the book's.
        await page.locator(".grid-pane", { has: page.locator(".doc-bible") }).locator(".grid-tab-btn").first().click();
        await page.locator(`${CONTROLS} .place-input`).waitFor({ timeout: 8000 });
        t.assert.equal(await page.locator(".book-where-input:visible").count(), 0, "the book's field is gone");
        t.assert.ok(await page.locator(`${CONTROLS} .bibles-btn`).isVisible(), "the Bibles chip is in the bar");
        await show(t, "e2e-test-pages", "Page");
        s = await barState(t);
        t.assert.equal(s.unit, "Page", `the PDF pane's page field is in the bar: ${JSON.stringify(s)}`);
        t.assert.equal(await page.locator(".book-where-input:visible").count(), 1, "only the selected pane's field is on screen");
        await t.shot("topbar-02-focus.png");
        await show(t, EPUB_TITLE, "Section");
        // A note has no reader controls.
        const n = await t.app.run("note.create", { title: "Topbar note", body: "x" });
        noteId = n.id;
        await openNoteInStudy(t, n.id, "Topbar note");
        await page.waitForFunction(() => document.querySelector(".grid-pane.selected .note-page"), null, { timeout: 8000 });
        s = await barState(t);
        t.assert.ok(!s.controlsShown, `no reader controls for a note: ${JSON.stringify(s)}`);
        await tab(t, "Topbar note").dispatchEvent("auxclick", { button: 1 });
      } finally {
        if (noteId) await t.app.run("note.delete", { id: noteId }).catch(() => {});
        await clean(t);
      }
    },
  },
  {
    row: "Top bar: typing a section number in the top bar jumps the pane, and the buttons and Contents work from there",
    shot: "topbar-03-jump.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      try {
        await size(t, 1536, 816);
        await twoPanes(t);
        await show(t, EPUB_TITLE, "Section");
        const field = page.locator(`${CONTROLS} .book-where-input`);
        await field.waitFor();
        await field.click();
        await page.keyboard.type("2");
        await page.keyboard.press("Enter");
        await page.waitForFunction(() => document.querySelector(".grid-pane.selected .book-where-input, .reader-controls .book-where-input")?.value === "2", null, { timeout: 8000 });
        t.assert.equal(await field.inputValue(), "2");
        await page.locator(`${CONTROLS} [aria-label='Previous section']`).click();
        await page.waitForFunction(() => document.querySelector(".reader-controls .book-where-input")?.value === "1", null, { timeout: 8000 });
        await page.locator(`${CONTROLS} [aria-label='Next section']`).click();
        await page.waitForFunction(() => document.querySelector(".reader-controls .book-where-input")?.value === "2", null, { timeout: 8000 });
        await page.locator(`${CONTROLS} .grid-contents`).click();
        await page.locator(".popover").first().waitFor({ timeout: 5000 });
        await t.shot("topbar-03-jump.png");
        await page.keyboard.press("Escape");
      } finally {
        await clean(t);
      }
    },
  },
  {
    row: "Top bar: a Bible pane's reference field and Bibles chip work from the top bar",
    shot: "topbar-04-bible.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      try {
        await size(t, 1536, 816);
        await twoPanes(t);
        await page.locator(".grid-pane", { has: page.locator(".doc-bible") }).locator(".grid-tab-btn").first().click();
        await page.waitForFunction(() => document.querySelector(".grid-pane.selected .doc-bible"), null, { timeout: 8000 });
        const ref = page.locator(`${CONTROLS} .place-input`);
        await ref.waitFor();
        t.assert.ok(await page.locator(`${CONTROLS} .bibles-btn`).isVisible(), "the Bibles chip is in the bar");
        await ref.click();
        await ref.fill("Romans 8");
        await page.keyboard.press("Enter");
        await page.waitForFunction(() => /Romans 8/.test(document.querySelector(".reader-controls .place-input")?.value ?? "") && /Romans/.test(document.querySelector(".grid-pane.selected .doc-bible")?.textContent ?? ""), null, { timeout: 10000 });
        await page.locator(`${CONTROLS} .bibles-btn`).click();
        await page.locator(".popover", { hasText: "Bibles to show" }).waitFor({ timeout: 5000 });
        await t.shot("topbar-04-bible.png");
        await page.keyboard.press("Escape");
        const s = await barState(t);
        t.assert.ok(!s.scrolls && !s.overlap, JSON.stringify(s));
      } finally {
        await clean(t);
      }
    },
  },
  {
    row: "Top bar: Read mode shows the same controls in the top bar, with no separate navigation row",
    shot: "topbar-05-read.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      try {
        await size(t, 1536, 816);
        await twoPanes(t);
        await show(t, EPUB_TITLE, "Section");
        await page.locator(".topbar button", { hasText: /^Read$/ }).first().click();
        await page.waitForFunction(() => document.querySelector(".study-grid")?.dataset.mode === "read", null, { timeout: 5000 });
        await page.waitForTimeout(300);
        const s = await barState(t);
        t.assert.ok(s.controlsShown && !s.fieldHidden && /^\d+$/.test(s.fieldValue), JSON.stringify(s));
        t.assert.equal(await page.locator(".grid-bar:visible").count(), 0, "no pane header row in Read mode");
        t.assert.equal(await page.locator(".grid-pane:visible .book-nav:visible").count(), 0, "no navigation row inside the page");
        await t.shot("topbar-05-read.png");
      } finally {
        await page.locator(".topbar button", { hasText: /^Study$/ }).first().click().catch(() => {});
        await clean(t);
      }
    },
  },
  {
    row: "Top bar: a narrow bar shortens the section label, then folds Contents into the pane menu, and never overlaps",
    shot: "topbar-06-narrow.png",
    timeout: 180000,
    async run(t) {
      const { page } = t;
      try {
        await size(t, 1536, 816);
        await twoPanes(t);
        await show(t, EPUB_TITLE, "Section");
        const seen = new Map();
        for (let w = 1100; w >= 400; w -= 20) {
          await size(t, w, 816);
          const s = await barState(t);
          t.assert.ok(!s.scrolls && !s.overlap, `at ${w}px the bar does not overflow or overlap: ${JSON.stringify(s)}`);
          t.assert.ok(!s.fieldHidden && /^\d+$/.test(s.fieldValue) && s.fieldW >= 21, `the number shows at ${w}px: ${JSON.stringify(s)}`);
          if (!seen.has(s.fit)) seen.set(s.fit, w);
        }
        t.assert.ok(seen.has("compact") && seen.has("menu"), `the bar goes through compact then menu: ${JSON.stringify([...seen])}`);
        t.assert.ok(seen.get("compact") > seen.get("menu"), "compact comes first");
        // Compact: "/ N" is drawn and the word Section is gone; the label's text stays "of N" for screen readers.
        await size(t, seen.get("compact"), 816);
        const label = await page.evaluate(() => {
          const o = document.querySelector(".reader-controls .book-of");
          return { text: o.textContent, before: getComputedStyle(o, "::before").content, unit: !!o.parentElement.querySelector(".book-unit")?.offsetParent };
        });
        t.assert.ok(/^"\/ \d+"$/.test(label.before) && !label.unit && /^of \d+$/.test(label.text), JSON.stringify(label));
        await t.shot("topbar-06-compact.png");
        // Menu: Contents is folded away and offered in the pane menu.
        await size(t, seen.get("menu"), 816);
        t.assert.ok(!(await page.locator(`${CONTROLS} .grid-contents`).isVisible()), "the Contents button is folded away");
        await page.locator(`${CONTROLS} [aria-label='Pane menu']`).click();
        await page.locator(".pane-menu button", { hasText: "Contents…" }).waitFor({ timeout: 5000 });
        await t.shot("topbar-06-narrow.png");
        await page.keyboard.press("Escape");
      } finally {
        await clean(t);
      }
    },
  },
];
