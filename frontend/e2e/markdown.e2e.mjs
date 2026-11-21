// docs/editor-behaviour.md, "Markdown as you type": each row typed into a note in the real app.

const fresh = async (t) => {
  await t.openNote("");
  await t.focusEnd();
};

export default [
  {
    row: "Type `# ` at the start of a line: a large heading (H1)",
    shot: "md-01-h1.png",
    async run(t) {
      await fresh(t);
      await t.type("# Large heading");
      const [b] = await t.blocks();
      t.assert.deepEqual([b.tag, b.text], ["h1", "Large heading"]);
      t.assert.equal(await t.saved(), "# Large heading");
    },
  },
  {
    row: "Type `**word**`: bold, and the stars disappear",
    shot: "md-04-bold.png",
    async run(t) {
      await fresh(t);
      await t.type("Grace is **free** today");
      t.assert.equal(await t.editor().locator("strong").first().textContent(), "free");
      t.assert.equal((await t.blocks())[0].text, "Grace is free today");
      t.assert.equal(await t.saved(), "Grace is **free** today");
    },
  },
  {
    row: "Type `- ` or `* `: a bulleted list",
    shot: "md-07-bullets.png",
    async run(t) {
      await fresh(t);
      await t.type("- dash item");
      await t.press("Enter");
      await t.press("Enter");
      await t.type("between");
      await t.press("Enter");
      await t.type("* star item");
      const bs = await t.blocks();
      t.assert.deepEqual(bs.map((b) => [b.tag, b.text]), [["ul", "dash item"], ["p", "between"], ["ul", "star item"]]);
      t.assert.equal(await t.saved(), "- dash item\n\nbetween\n\n- star item");
    },
  },
];
