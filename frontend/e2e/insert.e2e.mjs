// docs/editor-behaviour.md, "Inserting Bible text, references and highlights", in the real app.

async function insertMenu(t, label) {
  await t.page.click(`${t.root} [data-tool=insert]`);
  const menu = t.page.locator(".popover .insert-menu");
  await menu.waitFor();
  await menu.locator("button", { hasText: label }).click();
}

// typeInPopover types into the popover that has just opened and waits for it to settle.
async function inPopover(t, text) {
  const input = t.page.locator(".popover input").first();
  await input.waitFor();
  await t.page.keyboard.type(text, { delay: 15 });
}

const chips = (t) => t.editor().locator(".ref-chip");

// drag drags with the mouse, as a person does: press on the source, move over the target, let go.
// readMode shows the Reader in Read mode (an earlier check may have left the Study grid on a note).
async function readMode(page, hash) {
  await page.evaluate((h) => { location.hash = h; }, hash);
  await page.locator(".shell button, header button", { hasText: /^Read$/ }).first().click();
  await page.waitForSelector(".verse .vnum", { timeout: 10000 });
}

async function drag(page, from, to) {
  await from.scrollIntoViewIfNeeded().catch(() => {});
  // Lists redraw as they load; wait until the source stays put.
  let a = null;
  for (let i = 0; i < 20 && !a; i++) {
    await page.waitForTimeout(250);
    a = await from.boundingBox().catch(() => null);
  }
  const b = await to.boundingBox();
  await page.mouse.move(a.x + a.width / 2, a.y + Math.min(a.height / 2, 12));
  await page.mouse.down();
  await page.mouse.move(a.x + a.width / 2 + 8, a.y + 8, { steps: 3 });
  await page.mouse.move(b.x + 30, b.y + 15, { steps: 8 });
  await page.mouse.up();
}

export default [
  {
    row: "One Insert control (book-and-plus icon) in notes and sermons; no sparkle; no duplicate Quote verses",
    shot: "in-01-insert-control.png",
    async run(t) {
      for (const open of [() => t.openNote("", "Insert control"), () => t.openSermon("", "Insert control sermon")]) {
        await open();
        const bar = t.page.locator(`${t.root} .rich-toolbar`);
        t.assert.equal(await bar.locator("[data-tool=insert]").count(), 1, "one Insert control");
        t.assert.equal(await bar.locator("[data-tool=insert] svg.icon path").count(), 3, "the book-and-plus icon");
        t.assert.equal(await bar.locator("[data-tool=verse], .verse-tool").count(), 0, "no separate verse or Quote verses buttons");
        t.assert.ok(!(await bar.textContent()).includes("✦"), "no sparkle in the toolbar");
        t.assert.ok(!(await bar.textContent()).includes("Quote verses"));
      }
      await t.page.click(`${t.root} [data-tool=insert]`);
      const names = await t.page.locator(".popover .insert-menu .im-name").allTextContents();
      t.assert.deepEqual(names, ["Verse reference", "Verse text", "Highlight", "Link to a note or sermon"]);
      await t.shot();
      await t.press("Escape");
    },
  },
  {
    row: "Verse reference: a linked chip that opens the passage in the Study grid",
    shot: "in-02-verse-ref.png",
    async run(t) {
      const n = await t.openNote("", "Verse chip");
      await t.focusEnd();
      await t.type("Read ");
      await insertMenu(t, "Verse reference");
      await inPopover(t, "Rom 8:28");
      await t.page.waitForSelector(".popover .ref-note.ok");
      await t.press("Enter");
      await chips(t).first().waitFor();
      t.assert.equal(await chips(t).first().getAttribute("data-href"), "#passage/Rom.8.28");
      t.assert.ok((await chips(t).first().textContent()).includes("Romans 8:28"));
      t.assert.ok((await t.editor().getAttribute("class")).includes("rich-text"));
      t.assert.equal(await t.saved(), "Read [Romans 8:28](#passage/Rom.8.28) ");
      await t.shot();
      // Reopened, it is still a chip.
      await t.page.evaluate(() => { location.hash = "#reader"; });
      await t.page.waitForTimeout(300);
      await t.page.evaluate((id) => { location.hash = `#notes/note/${id}`; }, n.id);
      await t.current(".note-page");
      await t.page.locator("[data-e2e=current] .rich-text .ref-chip").click();
      await t.page.waitForFunction(() => location.hash.startsWith("#reader") && !!document.querySelector(".study-grid, .grid-body"), null, { timeout: 8000 });
      await t.page.waitForTimeout(800);
      const places = await t.page.locator("input").evaluateAll((els) => els.filter((e) => e.offsetParent).map((e) => e.value));
      t.assert.ok(places.some((v) => /Romans 8/.test(v)), `the Study grid shows Romans 8 (place boxes: ${places})`);
      await t.shot("in-02b-verse-ref-opened.png");
    },
  },
  {
    row: "Verse text: quoted verses in a quote block with the Bible's credit line",
    shot: "in-03-verse-text.png",
    async run(t) {
      await t.openNote("", "Verse text");
      await t.focusEnd();
      await insertMenu(t, "Verse text");
      await t.page.locator(".popover input").first().fill("");
      await inPopover(t, "John 3:16");
      await t.page.waitForFunction(() => !document.querySelector(".popover .quote-pop button[type=submit]")?.disabled, null, { timeout: 8000 });
      await t.press("Enter");
      const q = t.editor().locator(".verse-quote");
      await q.waitFor();
      t.assert.ok((await q.locator(".vq-text").textContent()).includes("God"), "the verse's words");
      t.assert.ok((await q.locator(".vq-credit").textContent()).includes("John 3:16"), "the credit line");
      const body = await t.saved();
      t.assert.match(body, /^> .*\n> — \[John 3:16\]\(#passage\/John\.3\.16\)/s);
    },
  },
  {
    row: "Highlight: searchable picker with colours; inserts the words as a quote with place and link back",
    shot: "in-04-highlight.png",
    async run(t) {
      await t.app.run("highlight.create", { ref: "Romans 8:28", style: "green" });
      await t.app.run("highlight.create", { ref: "Psalm 23:1", style: "pink" });
      await t.openNote("", "Highlight insert");
      await t.focusEnd();
      await insertMenu(t, "Highlight");
      const rows = t.page.locator(".popover .pick-row");
      await rows.first().waitFor();
      t.assert.ok(await rows.count() >= 2);
      t.assert.ok(await t.page.locator(".popover .pick-row .mark-dot.dot-green").count() >= 1, "colours shown");
      await inPopover(t, "Romans");
      await t.page.waitForFunction(() => [...document.querySelectorAll(".popover .pick-row")].every((r) => r.textContent.includes("Romans")), null, { timeout: 5000 });
      await t.shot();
      await t.press("Enter");
      const q = t.editor().locator(".verse-quote");
      await q.waitFor();
      t.assert.ok((await q.locator(".vq-credit").textContent()).includes("Romans 8:28"));
      t.assert.match(await t.saved(), /— \[Romans 8:28\]\(#passage\/Rom\.8\.28\)/);
    },
  },
  {
    row: "A plain click on an http(s) or mailto link in a note asks the system to open it, and the app stays put",
    shot: "in-07-external-link.png",
    async run(t) {
      await t.openNote("Visit [the site](https://example.com/glossa-e2e) or [write](mailto:e2e@example.com) today", "External link");
      const links = t.editor().locator("a.rt-link");
      await links.first().waitFor();
      await t.settled(); // the note has finished drawing and saving; a redraw would detach the link under the click
      // Record the calls instead of opening a browser.
      await t.page.evaluate(() => {
        window.__opened = [];
        window.__windows = [];
        window.runtime.BrowserOpenURL = (u) => { window.__opened.push(u); };
        window.open = (u) => { window.__windows.push(String(u)); return null; };
      });
      const here = await t.page.evaluate(() => location.href);
      await links.nth(0).click();
      await links.nth(1).click();
      await t.page.waitForFunction(() => window.__opened.length >= 2, null, { timeout: 5000 });
      t.assert.equal(JSON.stringify(await t.page.evaluate(() => window.__opened)), JSON.stringify(["https://example.com/glossa-e2e", "mailto:e2e@example.com"]), "BrowserOpenURL got both links");
      t.assert.equal(await t.page.evaluate(() => window.__windows.length), 0, "no pop-up window was opened");
      t.assert.equal(await t.page.evaluate(() => location.href), here, "the app did not navigate");
      await t.shot();
    },
  },
];
