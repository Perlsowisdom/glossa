// Generic layout checks. Each main view is visited at the user's screen (1536 x 816) and at a narrow window
// (900 x 700), in light and dark, and audited the same way: no sideways page scroll, every Study pane fills the
// grid it sits in (the Read-mode pane too), nothing in the top bar, pane headers, page heads and the editor
// toolbar sticks out of its parent or overlaps a neighbour, text fields show their value, and no control of the
// top bar, rail or pane headers is off-screen. One screenshot per view, size and theme (ls-<size>-<theme>-<view>.png).

import { addBooks, openFromLibrary } from "./book.e2e.mjs";
import { openNoteInStudy } from "./layout.e2e.mjs";

const SIZES = [[1536, 816], [900, 700]];
const THEMES = ["light", "dark"];
const SETTINGS = ["appearance", "sidebar", "online", "privacy", "data", "sync", "connections", "citations", "updates", "model", "about"];

// audit runs in the page and says what looks wrong, one line each.
const audit = () => {
  const P = [];
  const shown = (e) => !!e.getClientRects().length && getComputedStyle(e).visibility !== "hidden" && (e.offsetParent !== null || getComputedStyle(e).position === "fixed");
  const name = (e) => `${e.tagName.toLowerCase()}${e.id ? "#" + e.id : ""}${typeof e.className === "string" && e.className.trim() ? "." + e.className.trim().split(/\s+/).slice(0, 2).join(".") : ""}${e.getAttribute("aria-label") ? `[${e.getAttribute("aria-label")}]` : ""}${e.title ? `{${e.title.slice(0, 24)}}` : ""}`;
  const box = (e) => e.getBoundingClientRect();
  const de = document.documentElement;
  if (de.scrollWidth > innerWidth + 1) P.push(`the page scrolls sideways (${de.scrollWidth} > ${innerWidth})`);
  if (document.body.scrollWidth > innerWidth + 1) P.push(`the body scrolls sideways (${document.body.scrollWidth} > ${innerWidth})`);

  // Panes fill their grid.
  for (const g of document.querySelectorAll(".study-grid")) {
    if (!shown(g)) continue;
    const gr = box(g);
    const ps = [...g.querySelectorAll(".grid-pane")].filter(shown).map(box);
    if (!ps.length) { P.push("a study grid with no pane"); continue; }
    const l = Math.min(...ps.map((r) => r.left)), r = Math.max(...ps.map((x) => x.right));
    const t = Math.min(...ps.map((x) => x.top)), b = Math.max(...ps.map((x) => x.bottom));
    if (l > gr.left + 3 || r < gr.right - 3) P.push(`the panes span x ${Math.round(l)}-${Math.round(r)} but the grid is ${Math.round(gr.left)}-${Math.round(gr.right)} wide`);
    if (t > gr.top + 3 || b < gr.bottom - 3) P.push(`the panes span y ${Math.round(t)}-${Math.round(b)} but the grid is ${Math.round(gr.top)}-${Math.round(gr.bottom)} tall`);
    const area = ps.reduce((a, x) => a + x.width * x.height, 0) / (gr.width * gr.height);
    if (area < 0.9) P.push(`the panes cover only ${Math.round(area * 100)} % of the grid`);
  }

  // Bars: children stay inside, and do not overlap each other.
  const bars = [...document.querySelectorAll(".topbar, .grid-bar, .page-head")];
  const tool = document.querySelector("[data-tool=bold]");
  const ins = document.querySelector("[data-tool=insert], [data-tool=link]");
  if (tool && ins) {
    let a = tool.parentElement;
    while (a && !a.contains(ins)) a = a.parentElement;
    if (a) bars.push(a);
  }
  for (const bar of bars) {
    if (!shown(bar)) continue;
    const br = box(bar);
    const kids = [...bar.children].filter((e) => shown(e) && getComputedStyle(e).position !== "absolute" && box(e).width > 0);
    for (const k of kids) {
      const r = box(k);
      if (r.left < br.left - 1 || r.right > br.right + 1) P.push(`${name(k)} sticks out of ${name(bar)} (${Math.round(r.left)}-${Math.round(r.right)} in ${Math.round(br.left)}-${Math.round(br.right)})`);
    }
    kids.forEach((a, i) => kids.slice(i + 1).forEach((b) => {
      const x = box(a), y = box(b);
      if (x.left < y.right - 1 && y.left < x.right - 1 && x.top < y.bottom - 1 && y.top < x.bottom - 1) P.push(`${name(a)} overlaps ${name(b)} in ${name(bar)}`);
    }));
  }

  // Text fields show what they hold.
  for (const f of document.querySelectorAll("input, textarea, select")) {
    if (!shown(f) || ["checkbox", "radio", "range", "file", "hidden", "color"].includes(f.type)) continue;
    const v = f.tagName === "SELECT" ? f.selectedOptions[0]?.textContent ?? "" : f.value;
    if (!v) continue;
    if (f.clientWidth < 14) P.push(`${name(f)} holds "${v.slice(0, 20)}" but is ${f.clientWidth}px wide`);
    else if (v.length <= 4 && f.tagName !== "SELECT" && f.scrollWidth > f.clientWidth + 1) P.push(`${name(f)} clips its short value "${v}" (${f.scrollWidth} > ${f.clientWidth})`);
  }

  // Controls that must stay on screen.
  for (const c of document.querySelectorAll(".topbar button, .topbar input, .rail button, .rail a, .grid-bar button, .grid-bar input")) {
    if (!shown(c) || !box(c).width) continue;
    const r = box(c);
    if (c.closest(".grid-tabstrip")) continue; // tabs scroll inside their strip by design
    if (r.right > innerWidth + 1 || r.left < -1 || r.bottom > innerHeight + 1 || r.top < -1) P.push(`${name(c)} is off-screen (${Math.round(r.left)},${Math.round(r.top)} to ${Math.round(r.right)},${Math.round(r.bottom)})`);
  }
  return P;
};

async function mode(t, which) {
  const b = t.page.locator(".topbar button", { hasText: new RegExp(`^${which}$`) }).first();
  await b.waitFor({ timeout: 10000 });
  if ((await b.getAttribute("aria-pressed")) !== "true") await b.click();
  await t.page.waitForFunction((w) => [...document.querySelectorAll(".topbar button")].some((x) => x.textContent.trim() === w && x.getAttribute("aria-pressed") === "true"), which, { timeout: 8000 });
}

// freshGrid forgets the saved panes and reloads, leaving one pane in the given mode.
async function freshGrid(t, which) {
  await t.page.evaluate(() => { localStorage.removeItem("glossa.grid"); location.hash = "#reader"; });
  await t.page.reload();
  await t.page.waitForSelector(".grid-pane", { timeout: 20000 });
  await mode(t, which);
  await t.settled();
}

async function preset(t, c, r) {
  await t.page.locator(".rail-grid:visible, .topbar [aria-label='Grid presets']:visible").first().click();
  await t.page.locator(`.preset-cell[data-c='${c}'][data-r='${r}']`).click();
  await t.page.waitForFunction((n) => document.querySelectorAll(".grid-pane").length >= n, c * r, { timeout: 8000 });
  await t.settled();
}

export default SIZES.flatMap(([w, h]) => THEMES.map((theme) => ({
  row: `Layout sanity at ${w} x ${h}, ${theme}: every main view fits, panes fill their grid, nothing overlaps or sticks out`,
  shot: `ls-${w}x${h}-${theme}.png`,
  timeout: 420000,
  async run(t) {
    const { page, app } = t;
    const problems = [];
    const ids = { notes: [] };
    // look audits the view now on screen, saves its screenshot and notes what is wrong.
    const look = async (view) => {
      await t.settled();
      const found = await page.evaluate(audit);
      for (const p of found) problems.push(`${view}: ${p}`);
      await app.shot(`ls-${w}x${h}-${theme}-${view}.png`);
    };
    const go = async (hash, selector) => {
      await page.evaluate((x) => { location.hash = x; }, hash);
      if (selector) await page.locator(selector).first().waitFor({ state: "visible", timeout: 15000 });
    };
    try {
      await t.reset(); // the saved panes of the check before would turn the note page into a tab
      await app.resize(w, h);
      await page.emulateMedia({ colorScheme: theme });
      await addBooks(t);
      const note = await app.run("note.create", { title: "Layout sanity note", body: "# Heading\n\nSome text in a note with a [link](https://example.com) and **bold** words." });
      const sermon = await app.run("document.create", { kind: "sermon" });
      await app.run("document.update", { id: sermon.id, title: "Layout sanity sermon", kind: "sermon", body: "# Point\n\nText" });
      ids.note = note.id; ids.sermon = sermon.id;
      await t.settled();

      for (const [hash, sel, view] of [["#library", ".view:visible", "library"], ["#bibles", ".view:visible", "get-bibles"], ["#fromlogos", ".fromlogos-view", "from-logos"], ["#search", "#search-q", "search"], ["#commands", ".view:visible", "commands"]]) {
        await go(hash, sel);
        await look(view);
      }
      for (const id of SETTINGS) {
        await go(`#settings/${id}`, ".settings-section:visible");
        await look(`settings-${id}`);
      }
      await go("#notes", ".work-card:visible");
      await look("my-work");
      await go(`#notes/note/${note.id}`, ".note-page");
      await t.current(".note-page");
      await look("note");
      await go(`#sermons/${sermon.id}`, ".sermon-layout");
      await t.current(".sermon-layout");
      await look("sermon");

      // Read mode: a Bible, then a book.
      await freshGrid(t, "Read");
      await go("#passage/Rom.8", ".doc-bible .vt");
      await look("read-bible");
      await openFromLibrary(t, "E2E Test Book", "Read");
      await page.locator(".doc-book:visible .book-text p").first().waitFor({ timeout: 15000 });
      await look("read-book");

      // Study: one pane, two side by side, then two above one.
      await freshGrid(t, "Study");
      await look("study-1");
      await preset(t, 2, 1);
      await look("study-2");
      await freshGrid(t, "Study");
      const names = ["Split alpha", "Split beta", "Split gamma"];
      for (const title of names) ids.notes.push((await app.run("note.create", { title, body: "x" })).id);
      for (let i = 0; i < 3; i++) await openNoteInStudy(t, ids.notes[i], names[i]);
      const tab = (n) => page.locator(".grid-pane:visible .grid-tab-btn", { hasText: n }).first();
      const paneOf = (n) => page.locator(".grid-pane:visible", { has: page.locator(".grid-tab-btn", { hasText: n }) }).first();
      const box = (n) => paneOf(n).evaluate((e) => { const r = e.getBoundingClientRect(); return { w: r.width, h: r.height }; });
      let b = await box(names[0]);
      await tab(names[2]).dragTo(paneOf(names[0]), { targetPosition: { x: b.w / 2, y: b.h - 12 } });
      await t.settled();
      b = await box(names[0]);
      await tab(names[1]).dragTo(paneOf(names[0]), { targetPosition: { x: b.w - 12, y: b.h / 2 } });
      await page.waitForFunction(() => document.querySelectorAll(".grid-pane").length >= 3, null, { timeout: 8000 });
      await look("study-2-plus-1");
    } finally {
      for (const id of ids.notes) await app.run("note.delete", { id }).catch(() => {});
      if (ids.note) await app.run("note.delete", { id: ids.note }).catch(() => {});
      if (ids.sermon) await app.run("document.delete", { id: ids.sermon }).catch(() => {});
      await page.emulateMedia({ colorScheme: null }).catch(() => {});
      await app.resize(1480, 950).catch(() => {});
    }
    if (problems.length) console.log(`      layout problems at ${w} x ${h}, ${theme}:\n        ${problems.join("\n        ")}`);
    t.assert.deepEqual(problems, [], `${problems.length} layout problems at ${w} x ${h}, ${theme}:\n  ${problems.join("\n  ")}`);
  },
})));
