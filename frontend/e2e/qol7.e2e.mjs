// Quality pass 7 checks (branch m3/qol7): audit item 30 (29 has Go tests), shown failing first.
import { startApp } from "./app.mjs";
import { addBooks } from "./book.e2e.mjs";
import { looks } from "./qol.e2e.mjs";

const AGAIN = "Look again at books without a type";

function foldOf(page) {
  return page.locator("details.more", { has: page.locator("summary", { hasText: "Traditions and rules" }) }).first();
}

async function openFold(page) {
  const fold = foldOf(page);
  await fold.waitFor({ state: "visible", timeout: 20000 });
  if (!(await fold.evaluate((el) => el.open))) await fold.locator("summary").click();
  await fold.getByLabel("Add a tradition").waitFor({ timeout: 10000 });
  return fold;
}

export default [
  {
    row: "Library, empty: the Traditions and rules fold has no \"Look again at books without a type\" button (nothing to look at)",
    shot: "qol7-library-empty.png",
    timeout: 240000,
    async run(t) {
      let inst = null;
      try {
        inst = await startApp({ build: false, sweep: false, keyring: "glossa-e2e-qol7-lib", name: "qol7-lib", lite: true });
        const { page } = inst;
        await page.evaluate(() => { location.hash = "#library"; });
        await page.locator(".empty-state", { hasText: "No books yet" }).first().waitFor({ timeout: 20000 });
        const fold = await openFold(page);
        await page.waitForTimeout(600);
        const btn = fold.getByRole("button", { name: AGAIN });
        const shown = (await btn.count()) > 0 && (await btn.first().isVisible());
        if (shown) {
          await btn.first().click();
          await page.waitForTimeout(800);
          const said = await page.locator(".msg").allInnerTexts();
          console.log("  [qol7] before the fix, pressing it in an empty library says:", JSON.stringify(said));
        }
        t.assert.ok(!shown, "the Look again button is shown although the library has no books");
        await inst.shot("qol7-library-empty.png");
        t.shotTaken = true;
        for (const [w, h] of [[1536, 816], [900, 700]]) {
          await inst.resize(w, h);
          t.assert.ok((await fold.getByRole("button", { name: AGAIN }).count()) === 0, `button shown at ${w} x ${h}`);
          const over = await page.locator(".collections").first().evaluate((el) => el.scrollWidth - el.clientWidth);
          t.assert.ok(over <= 1, `the Collections column scrolls sideways by ${over} px at ${w} x ${h} with the fold open`);
        }
        await inst.resize(1480, 950);
        await looks({ page, app: inst }, "qol7-library-empty", async () => { await openFold(page); });
      } finally {
        await inst?.stop().catch(() => {});
      }
    },
  },
  {
    row: "Library, with books: the Look again button shows, and appears once a book is added",
    shot: "qol7-library-books.png",
    timeout: 240000,
    async run(t) {
      await t.reset();
      const { page } = t;
      await addBooks(t);
      await page.evaluate(() => { location.hash = "#library"; });
      await page.locator(".collections").first().waitFor({ timeout: 20000 });
      const fold = await openFold(page);
      const btn = fold.getByRole("button", { name: AGAIN });
      await btn.first().waitFor({ timeout: 10000 }).catch(() => {});
      t.assert.ok((await btn.count()) > 0 && (await btn.first().isVisible()), "the Look again button is missing although the library has books");
      await btn.first().click();
      await page.locator(".msg", { hasText: /automatically/ }).first().waitFor({ timeout: 10000 });
      await t.shot();
      for (const [w, h] of [[1536, 816], [900, 700]]) {
        await t.app.resize(w, h);
        t.assert.ok((await fold.getByRole("button", { name: AGAIN }).count()) > 0, `button missing at ${w} x ${h}`);
        const over = await page.locator(".collections").first().evaluate((el) => el.scrollWidth - el.clientWidth);
        if (over > 1) console.log("  [qol7] wide:", JSON.stringify(await page.locator(".collections").first().evaluate((col) => { const r = col.getBoundingClientRect().right; return [...col.querySelectorAll("*")].filter((e) => e.getBoundingClientRect().right > r + 1).slice(0, 8).map((e) => `${e.tagName}.${e.className} w=${Math.round(e.getBoundingClientRect().width)} ${e.textContent.slice(0, 40)}`); })));
        t.assert.ok(over <= 1, `the Collections column scrolls sideways by ${over} px at ${w} x ${h} with the fold open`);
      }
      await t.app.resize(1480, 950);
      await looks(t, "qol7-library-books", async () => { await openFold(page); });
    },
  },
];
