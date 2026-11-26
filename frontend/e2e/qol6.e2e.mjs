// Quality pass 6 checks (branch m3/qol6): audit item 27 (25 and 26 have Go tests), shown failing first.
import { startApp } from "./app.mjs";
import { addBooks } from "./book.e2e.mjs";
import { looks } from "./qol.e2e.mjs";

// The fold must sit in the Collections column, under My collections, whether or not there are books,
// and must work (a tradition can be added) before any book is imported.
async function checkFold(t, page, label) {
  const fold = page.locator("details.more", { has: page.locator("summary", { hasText: "Traditions and rules" }) });
  await fold.first().waitFor({ state: "visible", timeout: 10000 }).catch(() => {});
  t.assert.ok((await fold.count()) > 0 && (await fold.first().isVisible()), `${label}: the Traditions and rules fold is not shown`);
  const where = await fold.first().evaluate((el) => {
    const col = el.closest(".collections");
    const mine = [...(col?.querySelectorAll("h3") ?? [])].find((x) => x.textContent === "My collections");
    return { inColumn: !!col, below: !!mine && !!(mine.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING) };
  });
  t.assert.ok(where.inColumn, `${label}: the fold is not in the Collections column`);
  t.assert.ok(where.below, `${label}: the fold is not under My collections`);
  return fold.first();
}

async function useFold(t, page, fold, name) {
  await fold.locator("summary").click();
  const input = fold.getByLabel("Add a tradition");
  await input.waitFor({ timeout: 10000 });
  await input.fill(name);
  await fold.getByRole("button", { name: "Add", exact: true }).click();
  await fold.locator("li", { hasText: name }).first().waitFor({ timeout: 10000 });
  await fold.getByRole("button", { name: `Remove ${name} from the list` }).click();
  await page.waitForTimeout(400);
  await fold.locator("summary").click();
}

export default [
  {
    row: "Library, empty: the Traditions and rules fold is in the Collections column under My collections, and traditions can be added before any import",
    shot: "qol6-library-empty.png",
    timeout: 240000,
    async run(t) {
      let inst = null;
      try {
        inst = await startApp({ build: false, sweep: false, keyring: "glossa-e2e-qol6-lib", name: "qol6-lib", lite: true });
        const { page } = inst;
        await page.evaluate(() => { location.hash = "#library"; });
        await page.locator(".collections").first().waitFor({ timeout: 20000 });
        await page.locator(".empty-state", { hasText: "No books yet" }).first().waitFor({ timeout: 20000 });
        const fold = await checkFold(t, page, "empty library");
        await useFold(t, page, fold, "Qol6 tradition");
        await inst.shot("qol6-library-empty.png");
        t.shotTaken = true;
        for (const [w, h] of [[1536, 816], [900, 700]]) {
          await inst.resize(w, h);
          await checkFold(t, page, `empty library at ${w} x ${h}`);
        }
        await inst.resize(1480, 950);
        await looks({ page, app: inst }, "qol6-library-empty");
      } finally {
        await inst?.stop().catch(() => {});
      }
    },
  },
  {
    row: "Library, with books: the Traditions and rules fold is in the Collections column under My collections",
    shot: "qol6-library-books.png",
    timeout: 240000,
    async run(t) {
      await t.reset();
      const { page } = t;
      await addBooks(t);
      await page.evaluate(() => { location.hash = "#library"; });
      await page.locator(".collections").first().waitFor({ timeout: 20000 });
      await page.locator(".bookcard, .lib-results tr, .lib-results .book", {}).first().waitFor({ timeout: 20000 }).catch(() => {});
      const fold = await checkFold(t, page, "library with books");
      await useFold(t, page, fold, "Qol6 other tradition");
      await t.shot();
      for (const [w, h] of [[1536, 816], [900, 700]]) {
        await t.app.resize(w, h);
        await checkFold(t, page, `library with books at ${w} x ${h}`);
      }
      await t.app.resize(1480, 950);
      await looks(t, "qol6-library-books");
    },
  },
];
