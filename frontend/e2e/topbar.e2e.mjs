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
  // Start from one fresh pane: earlier checks leave the grid as they like.
  await page.evaluate(() => { localStorage.removeItem("glossa.grid"); location.hash = "#reader"; });
  await page.reload();
  await page.waitForSelector(".grid-pane", { timeout: 15000 });
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
        // The page fills the window, not the half its pane had in Study (a lone flex item with a
        // grow factor of 0.5 takes only half the space).
        const w = await page.evaluate(() => {
          const g = document.querySelector(".study-grid");
          const panes = [...(g?.querySelectorAll(".grid-pane") ?? [])].filter((p) => p.getClientRects().length);
          return {
            grid: g?.getBoundingClientRect().width ?? 0,
            pane: Math.max(0, ...panes.map((p) => p.getBoundingClientRect().width)),
            panes: panes.map((p) => [Math.round(p.getBoundingClientRect().width), p.style.flexGrow, p.parentElement?.className]),
            mode: g?.dataset.mode,
          };
        });
        t.assert.ok(w.grid > 0 && w.pane >= w.grid - 2, `Read mode page fills the width: ${JSON.stringify(w)}`);
        await t.shot("topbar-05-read.png");
      } finally {
        await page.locator(".topbar button", { hasText: /^Study$/ }).first().click().catch(() => {});
        await clean(t);
      }
    },
  },
];
