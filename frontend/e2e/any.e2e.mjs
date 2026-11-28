// T40c, the Any connection in the real app, against a fake Any (e2e/fake-any.mjs; the app is started
// with its address, so the real Any app on this computer is never reached). It checks, as a person
// sees it: Any is off at first and says it only talks to this computer, pairing takes the 4-digit
// code (a wrong one is refused), a note is sent as a page and "Update in Any" updates the same page,
// a page is imported as a note, and when both sides changed Glossa asks instead of overwriting.
// Screenshots (c40c-<what>-<size>-<theme>.png) at 1536 x 816 and 900 x 700, light and dark.

import { startFakeAny } from "./fake-any.mjs";

const SIZES = [[1536, 816], [900, 700]];
const THEMES = ["light", "dark"];

// eachView runs setup, takes the four screenshots (and checks nothing spills sideways), then tears down.
async function eachView(t, name, setup, teardown) {
  const { app, page } = t;
  for (const [w, h] of SIZES) {
    await app.resize(w, h);
    for (const theme of THEMES) {
      await page.emulateMedia({ colorScheme: theme });
      await t.settled();
      await setup();
      await t.settled();
      const sideways = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
      t.assert.ok(sideways <= 1, `${name}: the page scrolls sideways by ${sideways}px at ${w} x ${h}, ${theme}`);
      await app.shot(`c40c-${name}-${w}x${h}-${theme}.png`);
      await teardown?.();
    }
  }
  await app.resize(1480, 950);
  await page.emulateMedia({ colorScheme: null });
}

async function card(t) {
  await t.page.evaluate(() => { location.hash = "#settings/connections"; });
  await t.page.locator(".settings-section:visible").getByRole("heading", { name: "Connections" }).waitFor();
  const c = t.page.locator(".connection[data-connection=any]");
  await c.waitFor();
  return c;
}

// shot1536 takes one screenshot at 1536 x 816 (light), then puts the window back.
async function shot1536(t, name) {
  await t.app.resize(1536, 816);
  await t.page.emulateMedia({ colorScheme: "light" });
  await t.settled();
  await t.app.shot(name);
  await t.app.resize(1480, 950);
  await t.page.emulateMedia({ colorScheme: null });
}

const menuOf = (t, label) => t.page.locator(`${t.root} button[aria-label='${label}']`);

async function openMenu(t, label) {
  await menuOf(t, label).click();
  await t.page.locator(".popover [role=menuitem]").first().waitFor();
}

const pages = (t) => t.app.any.pages();

export default [
  {
    row: "Any: off at first; on, it asks for the 4-digit code (a wrong one is refused), then shows it is connected and which space it sends to",
    shot: "c40c-card.png",
    timeout: 240000,
    async run(t) {
      const { app, page } = t;
      await t.reset();
      await app.run("any.unpair"); // a key a stopped run left in the test keychain
      app.any.forget();
      const c = await card(t);

      t.assert.equal((await c.locator(".badge").innerText()).trim(), "Off");
      const text = await c.innerText();
      t.assert.ok(text.includes("Any lets you send notes and sermons") && text.includes("nothing leaves this computer") && text.includes("Off until you turn it on."), text);
      t.assert.ok(!/connect\.|any\.(pair|send|spaces)|[a-z]+_[a-z]+/.test(text.replace(/Any/g, "")), `jargon in: ${text}`);
      await page.waitForTimeout(400);
      t.assert.equal(app.any.requests().length, 0, "nothing is asked of Any while it is off");

      // On: Any is running, Glossa is not signed in yet.
      const sw = c.getByRole("switch", { name: "Use Any" });
      await sw.click();
      await c.locator(".badge", { hasText: "Needs attention" }).waitFor({ timeout: 15000 });
      t.assert.ok((await c.locator(".connection-status").innerText()).includes("not signed in yet"), "says to sign in");

      // Pairing: a wrong code is refused in plain words, the right one signs in.
      await c.getByRole("button", { name: "Sign in to Any" }).click();
      const code = c.locator("input.any-code");
      await code.waitFor();
      await code.fill("0000");
      await c.getByRole("button", { name: "Finish" }).click();
      await page.locator(".msg.error", { hasText: "did not accept that code" }).waitFor({ timeout: 10000 });
      t.assert.equal((await app.run("connect.list")).find((x) => x.id === "any").key_set, false, "no key after a wrong code");
      await code.fill(app.any.code);
      await c.getByRole("button", { name: "Finish" }).click();
      await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      await c.getByText("Glossa is signed in to Any.").waitFor();
      t.assert.ok((await c.locator(".connection-status").innerText()).includes("Connected to Any"), "status line");

      // The key is kept, never shown or returned.
      const info = (await app.run("connect.list")).find((x) => x.id === "any");
      t.assert.equal(info.key_set, true);
      t.assert.ok(!JSON.stringify(await app.run("connect.list")).includes(app.any.key), "the key is not in the command's answer");
      const everything = await page.evaluate(() => document.body.innerText + [...document.querySelectorAll("input")].map((i) => i.value).join(" "));
      t.assert.ok(!everything.includes(app.any.key) && !everything.includes(app.any.code), "the key and code are shown nowhere");

      // Choose the space.
      const space = c.getByLabel("Send to this space");
      await space.waitFor();
      await space.selectOption({ label: "Study" });
      await page.locator(".msg", { hasText: "Saved." }).waitFor({ timeout: 10000 });
      t.assert.equal((await app.run("any.spaces")).default_space_id, "space-study");
      await t.shot();

      await eachView(t, "card", async () => {
        const cc = await card(t);
        await cc.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
        const out = await cc.evaluate((e) => [...e.querySelectorAll("*")].filter((x) => x.getClientRects().length && x.getBoundingClientRect().right > e.getBoundingClientRect().right + 1).map((x) => x.className || x.tagName));
        t.assert.deepEqual(out, [], "something spills out of the card");
      });
    },
  },
  {
    row: "Any: Send to Any makes a page; Update in Any changes the same page; a page is imported as a note",
    shot: "c40c-send.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      app.any.forget();
      const n = await app.run("note.create", { title: "Romans note", body: "Grace is **free**.", ref: "Rom 8:1" });
      await t.settled();
      await page.evaluate((id) => { location.hash = `#notes/note/${id}`; }, n.id);
      await t.current(".note-page");
      t.id = n.id;

      // Not sent yet: the menu says Send to Any.
      await openMenu(t, "Note actions");
      const labels = await page.locator(".popover [role=menuitem]").allInnerTexts();
      t.assert.ok(labels.includes("Send to Any") && !labels.includes("Update in Any"), labels.join(" | "));
      await t.shot("c40c-send.png");
      await eachView(t, "menu", async () => { await page.keyboard.press("Escape"); await openMenu(t, "Note actions"); }, async () => { await page.keyboard.press("Escape"); });
      await openMenu(t, "Note actions");
      await page.getByRole("menuitem", { name: "Send to Any" }).click();
      await page.locator(".flash.show", { hasText: "Sent to Any" }).waitFor({ timeout: 15000 });
      let sent = pages(t);
      t.assert.equal(sent.length, 1);
      t.assert.equal(sent[0].name, "Romans note");
      t.assert.equal(sent[0].space, "space-study");
      t.assert.ok(sent[0].markdown.includes("Grace is **free**."), sent[0].markdown);
      t.assert.deepEqual(app.any.forbidden(), [], "Glossa made a request it must never make");

      // Linked now: the menu says Update in Any. Edit and update: the same page changes, none is added.
      await t.focusEnd();
      await t.type(" And more.");
      await t.saved();
      await openMenu(t, "Note actions");
      const after = await page.locator(".popover [role=menuitem]").allInnerTexts();
      t.assert.ok(after.includes("Update in Any") && !after.includes("Send to Any"), after.join(" | "));
      await page.getByRole("menuitem", { name: "Update in Any" }).click();
      await page.locator(".flash.show", { hasText: "Updated in Any" }).waitFor({ timeout: 15000 });
      sent = pages(t);
      t.assert.equal(sent.length, 1, "re-sending made a duplicate");
      t.assert.ok(sent[0].markdown.includes("And more."), sent[0].markdown);

      // Import: a page seeded in Any comes in as a new note, through the picker in My work.
      const seeded = app.any.seed("space-study", "Sermon seed", "A thought on **mercy**.");
      await page.keyboard.press("Escape");
      await page.evaluate(() => { location.hash = "#settings/connections"; });
      await t.settled();
      await page.evaluate(() => { location.hash = "#notes"; });
      await t.settled();
      await page.locator(".new-btn:visible").first().click();
      await page.getByRole("menuitem", { name: "Import from Any…" }).click();
      const dlg = page.locator("dialog.any-picker");
      await dlg.locator(".any-page").first().waitFor({ timeout: 15000 });
      t.assert.equal(await dlg.locator(".any-page").count(), 2);
      t.assert.ok((await dlg.innerText()).includes("Already a note"), "the sent page is marked");
      await t.shot("c40c-import.png");
      await eachView(t, "import", async () => {}, async () => {});
      await dlg.getByRole("searchbox").fill("mercy");
      await dlg.locator(".any-page").nth(0).waitFor();
      await page.waitForFunction(() => document.querySelectorAll("dialog.any-picker .any-page").length === 1, null, { timeout: 10000 });
      await dlg.getByRole("button", { name: /Bring in Sermon seed/ }).click();
      await page.locator(".flash.show", { hasText: "Brought in from Any" }).waitFor({ timeout: 15000 });
      const notes = await app.run("note.list");
      const got = notes.find((x) => x.title === "Sermon seed");
      t.assert.ok(got && got.body === "A thought on **mercy**.", JSON.stringify(got));
      // The same page again: it is that note now, so it says Update and makes no second note.
      await dlg.locator(".any-page", { hasText: "Already a note" }).waitFor({ timeout: 10000 });
      await dlg.getByRole("button", { name: "Close" }).click();
      t.assert.equal((await app.run("note.list")).filter((x) => x.title === "Sermon seed").length, 1);
      t.assert.ok(pages(t).some((p) => p.id === seeded) && pages(t).length === 2);
    },
  },
  {
    row: "Any: when both sides changed, Glossa asks which to keep and changes nothing until told; Keep both saves a copy",
    shot: "c40c-conflict.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      const n = await app.run("note.create", { title: "Two edits", body: "first" });
      const sent = await app.run("any.send", { kind: "note", id: n.id });
      const pageId = sent.object_id;
      await app.run("note.update", { id: n.id, body: "glossa edit" });
      app.any.edit(pageId, "Two edits", "any edit");
      await t.settled();
      await page.evaluate((id) => { location.hash = `#notes/note/${id}`; }, n.id);
      await t.current(".note-page");
      const ask = async () => {
        await openMenu(t, "Note actions");
        await page.getByRole("menuitem", { name: "Update in Any" }).click();
        await page.locator("dialog.any-dialog").waitFor({ timeout: 15000 });
      };
      await ask();
      const dlg = page.locator("dialog.any-dialog");
      const text = await dlg.innerText();
      t.assert.ok(text.includes("changed in both Glossa and Any") && text.includes("glossa edit") && text.includes("any edit"), text);
      for (const label of ["Keep Glossa’s version", "Keep Any’s version", "Keep both"]) t.assert.ok(text.includes(label), `${label} offered`);
      t.assert.equal(pages(t).find((p) => p.id === pageId).markdown, "any edit", "nothing was sent while asking");
      t.assert.equal((await app.run("note.get", { id: n.id })).body, "glossa edit", "nothing was changed in Glossa while asking");
      await t.shot("c40c-conflict.png");
      await eachView(t, "conflict", async () => { if (!(await dlg.isVisible())) await ask(); }, async () => { await dlg.getByRole("button", { name: "Not now" }).click(); });
      await ask();
      await dlg.getByRole("button", { name: "Keep both" }).click();
      await page.locator(".flash.show", { hasText: "Updated in Any" }).waitFor({ timeout: 15000 });
      t.assert.equal(pages(t).find((p) => p.id === pageId).markdown, "glossa edit", "Glossa's version went to Any");
      const notes = await app.run("note.list");
      const copy = notes.find((x) => x.title === "Two edits (from Any)");
      t.assert.ok(copy && copy.body === "any edit", "Any's version was kept as a new note");
      t.assert.equal(pages(t).filter((p) => p.name === "Two edits").length, 1, "still one page in Any");
    },
  },
  {
    row: "Any: found on the second address it tries; when Any asks for no key it connects with no sign-in, and says so",
    shot: "t44-keyless.png",
    timeout: 240000,
    async run(t) {
      const { app, page } = t;
      await app.run("any.unpair");
      await app.run("any.set_address", { address: "" });
      app.any.keyless = true;
      app.any.forget();
      const c = await card(t);
      await c.getByRole("button", { name: "Test connection" }).click();
      await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      // The app was told to look at a closed port first and this fake second: it found it.
      t.assert.equal((await c.locator(".connection-status").innerText()).trim(), "Connected (Any didn’t ask for a key). 2 spaces are available.");
      await c.getByText("Any didn’t ask for a key, so there is nothing to sign in to.").waitFor();
      t.assert.equal(await c.getByRole("button", { name: "Sign in to Any" }).count(), 0, "no sign-in is offered");
      t.assert.equal((await app.run("connect.list")).find((x) => x.id === "any").key_set, false, "no key was saved");
      t.assert.ok(app.any.requests().length > 0, "Any was asked");
      await c.getByLabel("Send to this space").waitFor();
      // Advanced holds the address, closed while all is well, and says where Any was found.
      const adv = c.locator("details.any-advanced");
      t.assert.equal(await adv.evaluate((d) => d.open), false, "Advanced is closed while Any is found");
      await adv.locator("summary").click();
      await adv.getByText(`found it at 127.0.0.1:${app.any.port} last time`).waitFor();
      t.assert.equal(await adv.getByLabel("Address of the Any app").inputValue(), "", "nothing typed: it finds Any by itself");
      await shot1536(t, "t44-keyless-1536x816.png");
      await adv.locator("summary").click();
      // The keyless Any works: spaces are listed through the commands with no key.
      t.assert.equal((await app.run("any.spaces")).spaces.length, 2);
    },
  },
  {
    row: "Any: not found says so in plain words; the Address field (this computer only) points Glossa at Any; clearing it finds Any again",
    shot: "t44-notfound.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      const other = await startFakeAny();
      try {
        other.keyless = true;
        app.any.forget();
        const c = await card(t);
        await app.any.pause();
        await c.getByRole("button", { name: "Test connection" }).click();
        await c.locator(".badge", { hasText: "Needs attention" }).waitFor({ timeout: 15000 });
        const msg = "Glossa couldn’t find the Any app on this computer. Open Any, or enter its address under Advanced.";
        t.assert.equal((await c.locator(".connection-status").innerText()).trim(), msg);
        t.assert.equal(await c.getByRole("button", { name: "Sign in to Any" }).count(), 0, "no sign-in is offered when Any is not found");
        const adv = c.locator("details.any-advanced");
        t.assert.equal(await adv.evaluate((d) => d.open), true, "Advanced opens by itself when Any is not found");
        const input = adv.getByLabel("Address of the Any app");
        await input.waitFor();
        await t.shot("t44-notfound.png");
        await shot1536(t, "t44-notfound-1536x816.png");

        // An address off this computer is refused in plain words, and is not saved.
        await input.fill("example.com:31310");
        await adv.getByRole("button", { name: "Use this address" }).click();
        await page.locator(".msg.error", { hasText: "not on this computer" }).waitFor({ timeout: 10000 });
        const kept = await app.run("any.address");
        t.assert.ok(!kept.manual && !String(kept.address).includes("example.com"), `the refused address was kept: ${JSON.stringify(kept)}`);

        // An address on this computer where Any answers: connected, with no key.
        await input.fill(`127.0.0.1:${other.port}`);
        await adv.getByRole("button", { name: "Use this address" }).click();
        await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
        t.assert.ok((await c.locator(".connection-status").innerText()).startsWith("Connected (Any didn’t ask for a key)"), "status line");
        const saved = await app.run("any.address");
        t.assert.ok(saved.manual && saved.address === other.url, JSON.stringify(saved));
        t.assert.ok(other.requests().length > 0, "the typed address was asked");
        t.assert.equal(app.any.requests().length, 0, "the usual address was not asked while one is typed");
        const adv2 = c.locator("details.any-advanced");
        t.assert.equal(await adv2.getByLabel("Address of the Any app").inputValue(), other.url, "the typed address is shown");
        await shot1536(t, "t44-address-1536x816.png");

        // Back to finding it by itself: the main fake, started again, is found.
        await app.any.resume();
        await adv2.getByRole("button", { name: "Find it by itself" }).click();
        await c.getByRole("button", { name: "Find it by itself" }).waitFor({ state: "detached", timeout: 15000 });
        let cleared = await app.run("any.address");
        for (let i = 0; i < 50 && cleared.address !== app.any.url; i++) { await page.waitForTimeout(200); cleared = await app.run("any.address"); }
        t.assert.ok(!cleared.manual && cleared.address === app.any.url, JSON.stringify(cleared));
        await c.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      } finally {
        await app.any.resume().catch(() => {});
        await other.close();
        app.any.keyless = false;
        await app.run("any.set_address", { address: "" });
      }
    },
  },
  {
    row: "Any: when it asks for a key, signing in is offered; a key Any no longer accepts is said so and pairing again is offered",
    shot: "t44-stale.png",
    timeout: 240000,
    async run(t) {
      const { app } = t;
      app.any.keyless = false;
      await app.run("any.unpair");
      const c = await card(t);
      await c.getByRole("button", { name: "Test connection" }).click();
      await c.locator(".connection-status", { hasText: "not signed in yet" }).waitFor({ timeout: 15000 });
      await c.getByRole("button", { name: "Sign in to Any" }).waitFor();
      // Pair, then Any stops accepting the key: the card says so and offers to pair again.
      const st = await app.run("any.pair_start");
      await app.run("any.pair_finish", { challenge_id: st.challenge_id, code: app.any.code });
      await c.getByRole("button", { name: "Test connection" }).click();
      await c.locator(".connection-status", { hasText: "Connected to Any." }).waitFor({ timeout: 15000 });
      app.any.rejectKeys = true;
      await c.getByRole("button", { name: "Test connection" }).click();
      const want = "Any no longer accepts Glossa’s key. Choose “Sign in to Any” to pair again.";
      await c.locator(".connection-status", { hasText: want }).waitFor({ timeout: 15000 });
      await c.getByRole("button", { name: "Sign in to Any" }).waitFor();
      await t.shot("t44-stale.png");
      await shot1536(t, "t44-stale-1536x816.png");
      app.any.rejectKeys = false;
      await app.run("any.unpair");
    },
  },
  {
    row: "Any: a page that changed in Any while updating says so in plain words; a page that is gone says it was deleted; nothing forbidden is ever asked",
    shot: "t45-messages.png",
    timeout: 300000,
    async run(t) {
      const { app, page } = t;
      app.any.keyless = true;
      await app.run("any.unpair");
      app.any.forget();
      await app.run("any.set_space", { space_id: "space-study" });
      const n = await app.run("note.create", { title: "Messages", body: "one" });
      await app.run("any.send", { kind: "note", id: n.id });
      await app.run("note.update", { id: n.id, body: "two" });
      await t.settled();
      await page.keyboard.press("Escape");
      await page.evaluate((id) => { location.hash = `#notes/note/${id}`; }, n.id);
      await t.current(".note-page");

      // The page changes in Any between Glossa reading it and writing: a plain sentence, and a retry works.
      app.any.conflictNext = true;
      await openMenu(t, "Note actions");
      await page.getByRole("menuitem", { name: "Update in Any" }).click();
      await page.locator(".msg.error", { hasText: "changed in Any" }).waitFor({ timeout: 15000 });
      t.assert.ok((await page.locator(".msg.error").first().innerText()).includes("Try again"), "says to try again");
      await t.shot("t45-messages.png");
      await shot1536(t, "t45-conflict-1536x816.png");
      await openMenu(t, "Note actions");
      await page.getByRole("menuitem", { name: "Update in Any" }).click();
      await page.locator(".flash.show", { hasText: "Updated in Any" }).waitFor({ timeout: 15000 });
      t.assert.ok(pages(t).some((p) => p.markdown === "two"), "the retry went through");

      // A page that was deleted in Any between the list and the click.
      const seeded = app.any.seed("space-study", "Soon gone", "text");
      await page.keyboard.press("Escape");
      await page.evaluate(() => { location.hash = "#settings/connections"; });
      await t.settled();
      await page.evaluate(() => { location.hash = "#notes"; });
      await t.settled();
      await page.locator(".new-btn:visible").first().click();
      await page.getByRole("menuitem", { name: "Import from Any…" }).click();
      const dlg = page.locator("dialog.any-picker");
      await dlg.locator(".any-page", { hasText: "Soon gone" }).waitFor({ timeout: 15000 });
      app.any.bin(seeded);
      await dlg.getByRole("button", { name: /Bring in Soon gone/ }).click();
      await dlg.locator(".msg.error", { hasText: "no such page" }).waitFor({ timeout: 15000 });
      await shot1536(t, "t45-notfound-1536x816.png");
      await dlg.getByRole("button", { name: "Close" }).click();

      t.assert.deepEqual(app.any.forbidden(), [], "Glossa made a request it must never make");
      app.any.keyless = false;
    },
  },
  {
    row: "Any: what it sends is on the Privacy page; turning it off asks nothing more of Any",
    shot: "c40c-off.png",
    async run(t) {
      const { app, page } = t;
      await page.evaluate(() => { location.hash = "#settings/privacy"; });
      const sec = page.locator(".settings-section:visible");
      await sec.getByRole("heading", { name: "Privacy" }).waitFor();
      const dt = sec.locator("dt", { hasText: /^Any$/ });
      await dt.waitFor();
      const dd = await dt.locator("xpath=following-sibling::dd[1]").innerText();
      t.assert.ok(dd.includes("nothing leaves this computer"), dd);
      const c = await card(t);
      await c.getByRole("switch", { name: "Use Any" }).click();
      await c.locator(".badge", { hasText: /^Off$/ }).waitFor({ timeout: 10000 });
      app.any.forget();
      await page.waitForTimeout(500);
      t.assert.equal(app.any.requests().length, 0, "nothing is asked of Any once it is off");
      await app.run("any.unpair");
    },
  },
];
