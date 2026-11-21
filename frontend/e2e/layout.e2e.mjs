import { addBooks, closeBooks, openFromLibrary } from "./book.e2e.mjs";
// T38b: tab bar, collapsible properties, section field, several sermon passages, Turn into, split tree.

const LONG = "A very long title that goes on and on about the whole counsel of God";

// openNoteInStudy opens a note's card from My work in a Study tab.
export async function openNoteInStudy(t, id, title) {
  const { page } = t;
  await page.evaluate(() => { location.hash = "#notes"; });
  // an earlier check may have left a note open on its own page: go back to the My work list
  const back = page.locator(".view:not(.reader) .note-page button", { hasText: "My work" }).first();
  if (await back.isVisible().catch(() => false)) await back.click();
  const card = page.locator(".work-card:visible", { hasText: title }).first();
  await card.waitFor({ timeout: 10000 });
  await card.dblclick();
  await page.locator(".grid-tab-btn", { hasText: title.slice(0, 20) }).first().waitFor({ timeout: 10000 });
}

export default [
  {
    row: "T38b 6: long tab titles end in an ellipsis with the full title on hover; + and close never overlap a title",
    shot: "t38b-06-tabs.png",
    async run(t) {
      const { page } = t;
      const ids = [];
      for (let i = 0; i < 4; i++) {
        const title = `${LONG} ${i + 1}`;
        const n = await t.app.run("note.create", { title, body: "x" });
        ids.push(n.id);
        await openNoteInStudy(t, n.id, title);
      }
      await t.settled(); // four tabs have just opened: measure once the tab bar stops moving
      const r = await page.evaluate(() => {
        const bar = [...document.querySelectorAll(".grid-pane")].find((p) => p.offsetParent && p.querySelector(".grid-tab"))?.querySelector(".grid-bar");
        const plus = bar.querySelector(".grid-plus").getBoundingClientRect();
        const strip = bar.querySelector(".grid-tabstrip").getBoundingClientRect();
        const btns = [...bar.querySelectorAll(".grid-tab-btn")];
        const tabs = [...bar.querySelectorAll(".grid-tab")].map((e) => e.getBoundingClientRect());
        const b0 = btns.find((b) => b.title.startsWith("A very long"));
        return {
          plusLeft: plus.left, stripRight: strip.right,
          maxTab: Math.max(...tabs.map((x) => x.width)), rem: parseFloat(getComputedStyle(document.documentElement).fontSize),
          ellipsis: getComputedStyle(b0).textOverflow, clipped: b0.scrollWidth > b0.clientWidth,
          title: b0.title, n: btns.length, scrolls: bar.querySelector(".grid-tabstrip").scrollWidth > bar.querySelector(".grid-tabstrip").clientWidth,
          closeInside: [...bar.querySelectorAll(".grid-tab")].every((tb) => { const c = tb.querySelector(".grid-tab-close").getBoundingClientRect(), bb = tb.querySelector(".grid-tab-btn").getBoundingClientRect(); return c.left >= bb.right - 1 && c.right <= tb.getBoundingClientRect().right + 1; }),
        };
      });
      t.assert.ok(r.n >= 4, JSON.stringify(r));
      t.assert.ok(r.maxTab <= 12 * r.rem + 2, `tab width capped: ${JSON.stringify(r)}`);
      t.assert.equal(r.ellipsis, "ellipsis");
      t.assert.ok(r.clipped, "long title is clipped");
      t.assert.ok(r.title.startsWith(LONG), "full title on hover");
      t.assert.ok(r.stripRight <= r.plusLeft + 1, `tab strip ends before +: ${JSON.stringify(r)}`);
      t.assert.ok(r.closeInside, "close buttons sit after their title");
      t.note = `tabs scroll: ${r.scrolls}`;
      await t.shot();
      for (const id of ids) await t.app.run("note.delete", { id }).catch(() => {});
    },
  },
  {
    row: "T38b 7: the place in a book is a field: a number goes there, words list matching headings; a PDF takes a page",
    shot: "t38b-07-section-field.png",
    async run(t) {
      const { page } = t;
      await addBooks(t);
      await openFromLibrary(t, "E2E Test Book", "Open in Study");
      const pane = page.locator(".grid-pane:visible", { has: page.locator(".doc-book") }).first();
      await pane.locator(".book-text p").first().waitFor({ timeout: 15000 });
      // The place field is in the top bar, with the selected pane's other reader controls.
      const bar = page.locator(".topbar .reader-controls");
      const field = bar.locator(".book-where-input");
      t.assert.equal(await field.inputValue(), "1");
      t.assert.equal(await bar.locator(".book-of").textContent(), "of 2");
      await field.click();
      await t.type("2");
      await t.press("Enter");
      await page.waitForFunction(() => document.querySelector(".reader-controls .book-where-input:not([hidden])")?.value === "2", null, { timeout: 8000 });
      await pane.locator(".book-text p").first().waitFor();
      t.assert.equal(await field.inputValue(), "2");
      await field.click();
      await t.type("chapter one");
      await t.press("Enter");
      const hit = page.locator(".popover .toc-list button", { hasText: "Chapter One" });
      await hit.waitFor({ timeout: 5000 });
      await t.shot();
      await hit.click();
      await page.waitForFunction(() => [...document.querySelectorAll(".book-where-input")].some((i) => i.offsetParent && i.value === "1"), null, { timeout: 8000 });
      // a number out of range is refused
      await field.click();
      await t.type("99");
      await t.press("Enter");
      t.assert.equal(await field.inputValue(), "1");
      await closeBooks(t);
      await openFromLibrary(t, "e2e-test-pages", "Open in Study");
      const pp = page.locator(".grid-pane:visible", { has: page.locator(".doc-book") }).first();
      const pf = bar.locator(".book-where-input");
      await pf.waitFor({ timeout: 15000 });
      await page.waitForFunction(() => /Page/.test(document.querySelector(".reader-controls .book-unit")?.textContent ?? ""), null, { timeout: 15000 });
      await pf.click();
      await t.type("2");
      await t.press("Enter");
      await page.waitForFunction(() => [...document.querySelectorAll(".book-where-input")].some((i) => i.offsetParent && i.value === "2"), null, { timeout: 8000 });
      await closeBooks(t);
    },
  },
  {
    row: "T38b 2: dropping a tab on a pane's edge splits that pane (side by side, or above/below), so two panes sit side by side above one wide pane",
    shot: "t38b-02-splits.png",
    async run(t) {
      const { page, app } = t;
      const names = ["Split alpha", "Split beta", "Split gamma"];
      const ids = [];
      for (const title of names) {
        const n = await app.run("note.create", { title, body: "x" });
        ids.push(n.id);
        await openNoteInStudy(t, n.id, title);
      }
      const tab = (name) => page.locator(".grid-pane:visible .grid-tab-btn", { hasText: name }).first();
      const paneOf = (name) => page.locator(".grid-pane:visible", { has: page.locator(".grid-tab-btn", { hasText: name }) }).first();
      const box = (name) => paneOf(name).evaluate((e) => { const r = e.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height }; });
      // gamma onto the bottom edge of alpha's pane: a pane below
      let b = await box("Split alpha");
      await tab("Split gamma").dragTo(paneOf("Split alpha"), { targetPosition: { x: b.w / 2, y: b.h - 12 } });
      await page.waitForTimeout(400);
      const g1 = await box("Split gamma");
      const a1 = await box("Split alpha");
      t.assert.ok(g1.y > a1.y + a1.h - 4 && Math.abs(g1.w - a1.w) < 4, "gamma is a pane below alpha, as wide");
      // beta onto the right edge of alpha's (top) pane: beside alpha, gamma stays wide below
      b = await box("Split alpha");
      await tab("Split beta").dragTo(paneOf("Split alpha"), { targetPosition: { x: b.w - 12, y: b.h / 2 } });
      await page.waitForTimeout(400);
      const a = await box("Split alpha"), be = await box("Split beta"), g = await box("Split gamma");
      t.assert.ok(Math.abs(a.y - be.y) < 3 && be.x > a.x + a.w - 4, "alpha and beta are side by side");
      t.assert.ok(g.y > a.y + a.h - 4 && Math.abs(g.x - a.x) < 3 && Math.abs(g.w - (be.x + be.w - a.x)) < 4, "gamma is one wide pane below both");
      await t.shot();
      for (const id of ids) await app.run("note.delete", { id });
    },
  },
];
