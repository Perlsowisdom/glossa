// T34g sections 4 and 5: every Markdown format is a toolbar button (with ⋯ More when the row does not
// fit), and the / menu lists every format and insert alphabetically.

const TOOLS = ["bold", "italic", "underline", "code", "|", "heading", "|", "bullets", "numbers", "checks", "|", "quote", "divider", "ink", "link", "table", "insert"];

// selectWord selects the last word typed, with the keyboard.
async function selectBack(t, n) {
  for (let i = 0; i < n; i++) await t.press("Shift+ArrowLeft");
}

const tool = (t, id) => t.page.click(`${t.root} .rich-toolbar [data-tool=${id}]`);

export default [
  {
    row: "Toolbar: bold, italic, underline, code | heading | three lists | quote, divider, link, table, Insert, in groups",
    shot: "fm-01-toolbar.png",
    async run(t) {
      await t.openNote("", "Buttons");
      const order = await t.page.locator(`${t.root} .rich-toolbar > :is(button[data-tool], .rich-sep)`).evaluateAll((els) =>
        els.filter((e) => e.dataset.tool !== "more").map((e) => (e.classList.contains("rich-sep") ? "|" : e.dataset.tool)));
      t.assert.deepEqual(order.slice(0, TOOLS.length), TOOLS, JSON.stringify(order));
      await t.focusEnd();
      for (const [id, sel] of [["bold", "strong, .rt-b"], ["italic", "em, .rt-i"], ["underline", ".rt-u"], ["code", "code, .rt-code"]]) {
        await t.type(` ${id}`);
        await selectBack(t, id.length);
        await tool(t, id);
        await t.press("End");
        await tool(t, id); // off again for what is typed next
        t.assert.equal(await t.editor().locator(sel).last().textContent(), id, id);
      }
      const md = await t.saved();
      t.assert.equal(md, " **bold** *italic* <u>underline</u> `code`", md);
      await t.press("Enter");
      await t.type("Point");
      await tool(t, "heading");
      await t.page.locator(".popover .heading-menu button", { hasText: "Medium heading" }).click();
      t.assert.equal((await t.blocks()).at(-1).tag, "h2");
      for (const [id, tag, cls] of [["bullets", "ul", ""], ["numbers", "ol", ""], ["checks", "ul", "rt-checklist"]]) {
        await t.press("End");
        await t.press("Enter");
        await t.type(id);
        await tool(t, id);
        const b = (await t.blocks()).at(-1);
        t.assert.equal(b.tag, tag, id);
        if (cls) t.assert.ok(b.cls.includes(cls), id);
        await tool(t, id);
      }
      await t.type(" quoted");
      await tool(t, "quote");
      t.assert.equal((await t.blocks()).at(-1).tag, "blockquote");
      await tool(t, "quote");
      await tool(t, "divider");
      t.assert.ok((await t.blocks()).some((b) => b.cls.includes("rich-divider") || b.tag === "hr"), "a divider");
      await t.type("site");
      await selectBack(t, 4);
      await tool(t, "link");
      await t.page.locator(".popover input[type=url]").fill("https://example.com");
      await t.press("Enter");
      t.assert.equal(await t.editor().locator("a.rt-link").last().getAttribute("href"), "https://example.com");
      await tool(t, "table");
      await t.page.locator(".popover .table-picker").waitFor();
      await t.press("Escape");
      await tool(t, "insert");
      await t.page.locator(".popover .insert-menu").waitFor();
      await t.shot();
      await t.press("Escape");
    },
  },
];
