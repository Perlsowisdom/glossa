// docs/editor-behaviour.md, "Headings and structure", in the real app.

const SECTIONS = "# Alpha\n\nAlpha text\n\n## Beta\n\nBeta text\n\n# Gamma\n\nGamma text";

const visible = (t, text) => t.editor().locator(":scope > *", { hasText: text }).first().isVisible();

async function foldAlpha(t) {
  const alpha = t.editor().locator("h1", { hasText: "Alpha" });
  await t.still(alpha);
  await alpha.hover();
  const arrow = t.page.locator(`${t.root} .rt-fold.hover`);
  await arrow.waitFor({ timeout: 5000 });
  t.assert.equal(await arrow.count(), 1, "an arrow shows beside the heading under the pointer");
  await arrow.click();
  t.assert.equal(await visible(t, "Alpha text"), false);
  t.assert.equal(await visible(t, "Beta"), false);
  t.assert.equal(await visible(t, "Beta text"), false);
  t.assert.equal(await visible(t, "Gamma text"), true);
  t.assert.equal(await alpha.isVisible(), true);
}

export default [
  {
    row: "Heading button offers Large, Medium and Small, with Ctrl+Alt+1/2/3",
    shot: "st-01-heading-sizes.png",
    async run(t) {
      await t.openSermon("A point", "Heading sizes");
      await t.focusEnd();
      await t.page.click(`${t.root} [data-tool=heading]`);
      const menu = t.page.locator(".popover .heading-menu");
      t.assert.deepEqual(await menu.locator(".hm-name").allTextContents(), ["Large heading", "Medium heading", "Small heading", "Normal text"]);
      await t.shot();
      await menu.locator("button", { hasText: "Small heading" }).click();
      t.assert.equal((await t.blocks())[0].tag, "h3");
      for (const [key, tag] of [["1", "h1"], ["2", "h2"], ["3", "h3"]]) {
        await t.press(`Control+Alt+${key}`);
        t.assert.equal((await t.blocks())[0].tag, tag, `Ctrl+Alt+${key}`);
      }
      t.assert.equal(await t.saved(), "### A point");
    },
  },
  {
    row: "Headings fold: an arrow beside each heading hides its section (notes), remembered",
    shot: "st-02-fold-note.png",
    async run(t) {
      const n = await t.openNote(SECTIONS, "Folding");
      await foldAlpha(t);
      await t.shot();
      // Display only: the text is unchanged.
      t.assert.equal((await t.app.run("note.get", { id: n.id })).body, SECTIONS);
      // Leave and come back: still folded.
      await t.page.evaluate(() => { location.hash = "#reader"; });
      await t.page.waitForTimeout(400);
      await t.page.evaluate((id) => { location.hash = `#notes/note/${id}`; }, n.id);
      await t.current(".note-page");
      t.assert.equal(await visible(t, "Alpha text"), false, "the fold is remembered");
      await t.page.locator("[data-e2e=current] .rt-fold.closed").first().click();
      t.assert.equal(await visible(t, "Alpha text"), true);
    },
  },
];
