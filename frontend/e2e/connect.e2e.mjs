// T40a, Settings → Connections in the real app, against a fake Zotero (e2e/fake-zotero.mjs; the app is
// started with its address, so the real Zotero and zotero.org are never reached). It checks, in plain
// words as a person sees them: Zotero is off at first and says what it sends, nothing is asked of it
// while off, turning it on connects, a Zotero that refuses says how to allow it, a key reaches
// zotero.org instead and is never shown back, and the page fits at 1536 x 816 and 900 x 700 in light
// and dark (screenshots c40a-<size>-<theme>.png).

const SIZES = [[1536, 816], [900, 700]];
const THEMES = ["light", "dark"];

async function openPage(t) {
  await t.page.evaluate(() => { location.hash = "#settings/connections"; });
  await t.page.locator(".settings-section:visible").getByRole("heading", { name: "Connections" }).waitFor();
  return t.page.locator(".connection[data-connection=zotero]");
}

export default [
  {
    row: "Connections: Zotero is off and says what it sends; on, it connects; a refusal says how to allow it; a key uses zotero.org and is never shown",
    shot: "t40a-connections.png",
    timeout: 180000,
    async run(t) {
      const { app, page } = t;
      const fake = app.zotero;
      await t.reset();
      fake.forget();
      const card = await openPage(t);
      await card.waitFor();

      // Off at first, with the sentence about what it sends beside the switch.
      t.assert.equal((await card.locator(".badge").innerText()).trim(), "Off");
      const text = await card.innerText();
      t.assert.ok(text.includes("Zotero lets you bring in your sources and cite them in your writing."), text);
      t.assert.ok(text.includes("asks the Zotero app on this computer") && text.includes("Off until you turn it on."), text);
      const sw = card.getByRole("switch", { name: "Use Zotero" });
      t.assert.equal(await sw.isChecked(), false);
      t.assert.equal(await card.getByRole("button", { name: "Test connection" }).count(), 0, "no test while off");
      await page.waitForTimeout(400);
      t.assert.equal(fake.requests().length, 0, "nothing is asked of Zotero while it is off");
      // No command names or codes in the card.
      t.assert.ok(!/connect\.|zotero\.(items|search|item)|[a-z]+_[a-z]+/.test(text.replace(/Zotero/g, "")), `jargon in: ${text}`);

      // On: it connects to the app on this computer.
      await sw.click();
      await card.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      t.assert.ok((await card.locator(".connection-status").innerText()).includes("Connected to Zotero"), "status line");
      t.assert.equal((await app.run("connect.list")).find((x) => x.id === "zotero").enabled, true);
      t.assert.ok(fake.requests().some((r) => r.includes("/api/users/0/")), "asked the app on this computer");

      // Zotero refuses other programs: the card says what to tick, in plain words.
      fake.mode = "off";
      await card.getByRole("button", { name: "Test connection" }).click();
      await card.locator(".badge", { hasText: "Needs attention" }).waitFor({ timeout: 15000 });
      const refusal = await card.locator(".connection-status").innerText();
      t.assert.ok(refusal.includes("Allow other applications on this computer to communicate with Zotero"), refusal);
      await t.shot();

      // A key: one step away, in More options. It is saved, the card falls back to zotero.org, and the key is never shown.
      const more = card.locator("details.connection-more");
      t.assert.equal(await more.evaluate((d) => d.open), false, "the key is one step away");
      await more.locator("summary").click();
      await card.locator("input[type=password]").fill(fake.key);
      await card.getByRole("button", { name: "Save key" }).click();
      await card.locator(".badge", { hasText: "Connected" }).waitFor({ timeout: 15000 });
      const via = await card.locator(".connection-status").innerText();
      t.assert.ok(via.includes("zotero.org"), via);
      t.assert.ok(fake.requests().some((r) => r.includes("/users/4242/")), "asked zotero.org");
      t.assert.equal(await more.evaluate((d) => d.open), true, "More options stays open after saving");
      t.assert.equal(await card.locator("input[type=password]").inputValue(), "", "the key field is emptied");
      const everything = await page.evaluate(() => document.body.innerText + [...document.querySelectorAll("input")].map((i) => i.value).join(" "));
      t.assert.ok(!everything.includes(fake.key), "the key is shown nowhere");
      t.assert.equal((await app.run("connect.list")).find((x) => x.id === "zotero").key_set, true);
      t.assert.ok(!JSON.stringify(await app.run("connect.list")).includes(fake.key), "the key is not in the command's answer");

      // Screenshots: the connected card at both sizes, light and dark (the key box open in the first).
      for (const [w, h] of SIZES) {
        await app.resize(w, h);
        for (const theme of THEMES) {
          await page.emulateMedia({ colorScheme: theme });
          await t.settled();
          const sideways = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
          t.assert.ok(sideways <= 1, `the page scrolls sideways by ${sideways}px at ${w} x ${h}, ${theme}`);
          const out = await card.evaluate((c) => [...c.querySelectorAll("*")].filter((e) => e.getClientRects().length && e.getBoundingClientRect().right > c.getBoundingClientRect().right + 1).map((e) => e.className || e.tagName));
          t.assert.deepEqual(out, [], `inside the card at ${w} x ${h}, ${theme}`);
          await app.shot(`c40a-${w}x${h}-${theme}.png`);
        }
      }
      await app.resize(1480, 950);
      await page.emulateMedia({ colorScheme: null });

      // Remove the key and turn it off: nothing more is asked.
      page.once("dialog", (d) => d.accept());
      await card.getByRole("button", { name: "Remove key" }).click();
      await card.locator("input[type=password]").waitFor();
      await page.waitForFunction(() => !document.querySelector(".connection .danger"), null, { timeout: 10000 });
      t.assert.equal((await app.run("connect.list")).find((x) => x.id === "zotero").key_set, false);
      fake.mode = "ok";
      await card.getByRole("switch", { name: "Use Zotero" }).click();
      await card.locator(".badge", { hasText: /^Off$/ }).waitFor({ timeout: 10000 });
      fake.forget();
      await page.waitForTimeout(500);
      t.assert.equal(fake.requests().length, 0, "nothing is asked of Zotero once it is off");
    },
  },
  {
    row: "Connections: what each connection sends is listed on the Privacy page too",
    shot: "t40a-privacy.png",
    async run(t) {
      const { page } = t;
      await page.evaluate(() => { location.hash = "#settings/privacy"; });
      const sec = page.locator(".settings-section:visible");
      await sec.getByRole("heading", { name: "Privacy" }).waitFor();
      const row = sec.locator("dt", { hasText: /^Zotero$/ });
      await row.waitFor();
      const dd = await row.locator("xpath=following-sibling::dd[1]").innerText();
      t.assert.ok(dd.includes("Off") && dd.includes("only reads"), dd);
    },
  },
];
