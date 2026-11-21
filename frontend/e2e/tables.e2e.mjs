// T34g section 6: tables in notes and sermons, typed in the real app.

const table = (t) => t.editor().locator("table").first();
const shape = (t) => table(t).evaluate((tb) => [...tb.rows].map((r) => r.cells.length));
const cellText = (t) => table(t).evaluate((tb) => [...tb.rows].map((r) => [...r.cells].map((c) => c.textContent)));

// tableButton presses the toolbar's Table button, through ⋯ More when it is in there.
async function tableButton(t) {
  const b = t.page.locator(`${t.root} .rich-toolbar [data-tool=table]`);
  if (await b.isVisible()) return b.click();
  await t.page.click(`${t.root} .rich-toolbar [data-tool=more]`);
  await t.page.locator(".popover .more-menu button[data-tool=table]").click();
}

async function slash(t, cmd) {
  await t.type(cmd);
  await t.page.waitForSelector(".slash-menu:not([hidden])");
  await t.press("Enter");
  await table(t).waitFor();
}

export default [
  {
    row: "Table button: a size grid to point at (R × C label), up to 10 × 10",
    shot: "tb6-01-picker.png",
    async run(t) {
      await t.openNote("", "Table picker");
      await t.focusEnd();
      await tableButton(t);
      const picker = t.page.locator(".popover .table-picker");
      await picker.waitFor();
      t.assert.equal(await picker.locator(".tp-cell").count(), 100);
      await picker.locator('.tp-cell[data-r="3"][data-c="2"]').hover();
      t.assert.equal((await picker.locator(".tp-label").textContent()).trim(), "3 × 2");
      t.assert.equal(await picker.locator(".tp-cell.on").count(), 6);
      await t.shot();
      await picker.locator('.tp-cell[data-r="3"][data-c="2"]').click();
      t.assert.deepEqual(await shape(t), [2, 2, 2]);
      t.assert.equal(await table(t).locator("th").count(), 2, "a header row");
    },
  },
  {
    row: "In a cell: formats, Markdown shortcuts and chips work; Tab, Tab in the last cell adds a row, Shift+Tab goes back",
    shot: "tb6-03-cells.png",
    async run(t) {
      await t.openNote("", "Table cells");
      await t.focusEnd();
      await slash(t, "/table2x2");
      await t.type("Point");
      await t.press("Tab");
      await t.type("Verse");
      await t.press("Tab");
      await t.type("**Grace** and __hope__ ");
      await t.press("Control+i");
      await t.type("now");
      await t.press("Control+i");
      await t.press("Tab");
      await t.type("Rom 8:28");
      await t.press("Tab");
      await table(t).locator(".ref-chip").waitFor({ timeout: 5000 });
      t.assert.deepEqual(await shape(t), [2, 2], "Tab after a reference makes a chip, not a move");
      await t.press("Tab");
      t.assert.deepEqual(await shape(t), [2, 2, 2], "Tab in the last cell adds a row");
      await t.type("Third");
      await t.press("Shift+Tab");
      await t.type("x");
      const cells = await cellText(t);
      t.assert.equal(cells[1][0], "Grace and hope now");
      t.assert.ok(cells[1][1].includes("Romans 8:28"));
      t.assert.equal(cells[2][0], "Third", "the new row's first cell");
      t.assert.ok(cells[1][1].endsWith("x"), `Shift+Tab went back: ${JSON.stringify(cells)}`);
      t.assert.equal(await table(t).locator(".rt-b, strong").first().textContent(), "Grace");
      t.assert.equal(await table(t).locator(".rt-u").first().textContent(), "hope");
      t.assert.equal(await table(t).locator(".rt-i, em").first().textContent(), "now");
      const md = await t.saved();
      t.assert.ok(md.includes("| Point | Verse |\n| --- | --- |\n| **Grace** and <u>hope</u> *now* | [Romans 8:28](#passage/Rom.8.28)"), md);
      await t.shot();
    },
  },
];
