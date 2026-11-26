// T23 and ADR 0016, updates in the real app, against a fake GitHub (e2e/fake-releases.mjs; the app is
// started with its addresses, so the real one is never reached). As a person sees it: a fresh install
// has updates on and is never asked; an old install that said no stays off; a newer version shows a
// notice with its one button and "Later" and an Update entry on the rail; Later hides the notice
// for that version; Settings → Updates has the switch, the Stable / Preview choice, the version and the
// last check; a failed check says nothing outside Settings; the placeholder key offers the download page, not Install. Screenshots (m3-updates-<what>-<size>-<theme>.png) at 1536 x 816 and 900 x 700,
// light and dark.

const SIZES = [[1536, 816], [900, 700]];
const THEMES = ["light", "dark"];

// What the fake publishes: a draft, a release without a manifest, a test version and a full release.
const LIST = [
  { version: "1.0.0-alpha.1", draft: true },
  { version: "0.9.0-beta.1" },
  { version: "0.8.0" },
  { version: "0.7.0-beta.1", manifest: false },
];
const LIST_NEWER = [{ version: "0.9.0-beta.2" }, ...LIST];

// eachView sets the screen up at each size and theme, checks nothing spills sideways, and takes the shot.
async function eachView(t, name, setup) {
  const { app } = t;
  for (const [w, h] of SIZES) {
    await app.resize(w, h);
    for (const theme of THEMES) {
      await app.page.emulateMedia({ colorScheme: theme });
      await app.page.waitForTimeout(400); // t.settled() watches the page the harness started with, which a restart replaces
      await setup();
      await app.page.waitForTimeout(400); // t.settled() watches the page the harness started with, which a restart replaces
      const sideways = await app.page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
      t.assert.ok(sideways <= 1, `${name}: the page scrolls sideways by ${sideways}px at ${w} x ${h}, ${theme}`);
      await app.shot(`m3-updates-${name}-${w}x${h}-${theme}.png`);
    }
  }
  await app.resize(1480, 950);
  await app.page.emulateMedia({ colorScheme: null });
}

// startFresh starts the app again as a fresh install (updates: null) or with an older install's answer.
async function startFresh(t, updates) {
  await t.app.restart({ updates });
  await t.app.page.waitForSelector(".shell", { timeout: 30000 });
}

// until waits for fn to give a value (the app's own timers and checks run on their own).
async function until(t, fn, what, ms = 20000) {
  for (const end = Date.now() + ms; Date.now() < end; await t.app.page.waitForTimeout(150)) {
    const v = await fn();
    if (v) return v;
  }
  throw new Error(`Timed out waiting for ${what}`);
}

// openUpdates opens Settings → Updates afresh (leaving first, so the page reads the settings again).
async function openUpdates(t) {
  await t.app.page.evaluate(() => { location.hash = "#reader"; });
  await t.app.page.waitForFunction(() => location.hash === "#reader" || location.hash.startsWith("#reader/"));
  await t.app.page.evaluate(() => { location.hash = "#settings/updates"; });
  const sec = t.app.page.locator(".settings-section:visible");
  await sec.getByRole("heading", { name: "Updates" }).waitFor();
  return sec;
}

const banner = (t) => t.app.page.locator("#update-banner");
const railEntry = (t) => t.app.page.locator(".rail-update");
const noticeShown = (t) => until(t, async () => ((await banner(t).count()) ? banner(t) : null), "the notice");

export default [
  {
    row: "Updates: the Settings section shows the switch, the channel, the version and the last check",
    shot: "m3-updates-settings-off.png",
    async run(t) {
      const { app, page } = t;
      const sec = await openUpdates(t);
      const sw = sec.getByRole("switch", { name: "Check for updates automatically" });
      t.assert.equal(await sw.isChecked(), false, "the test copy starts with updates answered no, and that is kept");
      const check = sec.getByRole("button", { name: "Check for updates" });
      t.assert.equal(await check.isDisabled(), true, "no check while off");
      const status = await sec.locator(".update-status").innerText();
      t.assert.ok(status.includes("Not checked yet."), status);
      const info = await app.run("update.settings.get");
      t.assert.ok(status.includes(info.current), `the version ${info.current} is shown: ${status}`);
      // The channel: a development build is a pre-release, so it follows Preview until the person chooses.
      const group = sec.getByRole("radiogroup", { name: "Channel" });
      t.assert.equal(await group.getByRole("radio", { name: "Preview" }).isChecked(), true, "a pre-release build starts on Preview");
      t.assert.equal(await group.getByRole("radio", { name: "Stable" }).isChecked(), false);
      t.assert.equal(info.channel_chosen, false);
      t.assert.ok((await sec.innerText()).includes("test versions"), "Preview says what it is");
      t.assert.equal(app.releases.requests.length, 0, "nothing is asked of GitHub while updates are off");
      await t.shot();
      // Turning it on enables the button; turning it off again leaves it as it was.
      await sw.click();
      await page.waitForFunction(() => !document.querySelector(".settings-section:not([hidden]) .check-updates")?.disabled);
      t.assert.equal((await app.run("update.settings.get")).enabled, true);
      await sec.getByRole("switch", { name: "Check for updates automatically" }).click();
      await page.waitForFunction(() => document.querySelector(".settings-section:not([hidden]) .check-updates")?.disabled);
      t.assert.equal((await app.run("update.settings.get")).enabled, false);
      t.assert.equal(app.releases.requests.length, 0, "switching does not check by itself");
    },
  },
  {
    row: "Updates: a fresh install has updates on and is never asked; an install that said no stays off",
    shot: "m3-updates-fresh.png",
    async run(t) {
      const { app } = t;
      app.releases.reset();
      await startFresh(t, null);
      const info = await app.run("update.settings.get");
      t.assert.equal(info.enabled, true, "on by default");
      t.assert.equal(info.channel, "preview");
      await until(t, () => app.releases.asked("/api/releases") > 0, "the first automatic check");
      await app.page.waitForTimeout(800);
      t.assert.equal(await banner(t).count(), 0, "no first-run question, and nothing to say yet");
      const text = await app.page.locator("body").innerText();
      t.assert.ok(!text.includes("Check for updates automatically?"), "the old question is gone");
      t.assert.ok((await app.run("update.settings.get")).last_checked, "the check was recorded");
      const sec = await openUpdates(t);
      t.assert.equal(await sec.getByRole("switch", { name: "Check for updates automatically" }).isChecked(), true);
      await t.shot();

      // An install that said no before keeps it off and asks nothing of GitHub.
      app.releases.reset();
      await startFresh(t, '{"enabled":false,"asked":true}');
      await app.page.waitForTimeout(1500);
      t.assert.equal((await app.run("update.settings.get")).enabled, false, "an earlier no stays off");
      t.assert.equal(app.releases.requests.length, 0, "nothing was asked of GitHub");
      t.assert.equal(await banner(t).count(), 0);
      // One that never answered the old question is turned on.
      app.releases.reset();
      await startFresh(t, '{"enabled":false,"asked":false}');
      t.assert.equal((await app.run("update.settings.get")).enabled, true, "never answered counts as on");
      await until(t, () => app.releases.asked("/api/releases") > 0, "its automatic check");
    },
  },
  {
    row: "Updates: a newer version shows a notice with Install and Later and an Update entry on the rail; Later hides it for that version",
    shot: "m3-updates-notice.png",
    async run(t) {
      const { app } = t;
      app.releases.set({ stable: "0.8.0", releases: LIST });
      await startFresh(t, null);
      const b = await noticeShown(t);
      const text = await b.innerText();
      // Preview picks the newest release that is not a draft and has a manifest: 0.9.0-beta.1.
      t.assert.ok(text.includes("Glossa 0.9.0-beta.1 is ready."), text);
      t.assert.ok(text.includes(["What’s new:", "Notes for 0.9.0-beta.1", "A second line"].join("\n")), `the label and the notes are on their own lines: ${JSON.stringify(text)}`);
      t.assert.ok(!text.includes("1.0.0-alpha.1") && !text.includes("0.7.0-beta.1"), "a draft and a release without a manifest are not offered");
      t.assert.equal(await b.getByRole("button", { name: "Open the download page" }).count(), 1);
      t.assert.equal(await b.getByRole("button", { name: "Later" }).count(), 1);
      // The rail says so too, and the notice does not take the keyboard from what the person is doing.
      const entry = railEntry(t);
      t.assert.equal(await entry.isVisible(), true, "the rail has an Update entry");
      t.assert.ok((await entry.getAttribute("aria-label")).includes("0.9.0-beta.1"));
      t.assert.equal(await app.page.evaluate(() => document.activeElement?.closest("#update-banner") == null), true, "the notice did not take focus");
      await t.shot();
      await eachView(t, "notice", async () => { await banner(t).waitFor(); });

      // Later hides it; the rail keeps saying so.
      await banner(t).getByRole("button", { name: "Later" }).click();
      await banner(t).waitFor({ state: "detached" });
      t.assert.equal(await entry.isVisible(), true, "the rail still has the Update entry after Later");
      // The Update entry opens Settings → Updates, which has the install button too.
      await entry.click();
      const sec = app.page.locator(".settings-section:visible");
      await sec.getByRole("heading", { name: "Updates" }).waitFor();
      t.assert.ok((await sec.locator(".update-status").innerText()).includes("Version 0.9.0-beta.1 is available"));
      t.assert.equal(await sec.getByRole("button", { name: "Open the download page" }).count(), 1);
      // The same version found again stays hidden; a newer one shows again.
      await sec.getByRole("button", { name: "Check for updates" }).click();
      await sec.locator(".msg.ok").waitFor();
      await app.page.waitForTimeout(600);
      t.assert.equal(await banner(t).count(), 0, "the same version stays hidden after Later");
      app.releases.set({ stable: "0.8.0", releases: LIST_NEWER });
      await sec.getByRole("button", { name: "Check for updates" }).click();
      const again = await noticeShown(t);
      t.assert.ok((await again.innerText()).includes("Glossa 0.9.0-beta.2 is ready."));
      // Later again; at the next start the notice is back (it was only put off).
      await again.getByRole("button", { name: "Later" }).click();
      await app.page.reload();
      await app.page.waitForSelector(".shell");
      await noticeShown(t);
    },
  },
  {
    row: "Updates: Settings has the Stable and Preview choice, and switching changes what is offered",
    shot: "m3-updates-channel.png",
    async run(t) {
      const { app } = t;
      app.releases.set({ stable: "0.8.0", releases: LIST_NEWER });
      await app.page.reload();
      await app.page.waitForSelector(".shell");
      const sec = await openUpdates(t);
      const status = sec.locator(".update-status");
      const group = sec.getByRole("radiogroup", { name: "Channel" });
      const says = (text) => until(t, async () => (await status.innerText()).includes(text), `"${text}" in Settings`);
      await says("Version 0.9.0-beta.2 is available");
      // Stable: only full releases, so the newest is 0.8.0.
      const stableBefore = app.releases.asked("/stable/latest.json");
      await group.locator(".seg-opt", { hasText: "Stable" }).click();
      await says("Version 0.8.0 is available");
      t.assert.equal((await app.run("update.settings.get")).channel, "stable");
      t.assert.equal(app.releases.asked("/stable/latest.json"), stableBefore + 1, "Stable reads the latest release's manifest");
      t.assert.ok((await sec.innerText()).includes("finished releases"), "Stable says what it is");
      const b = await noticeShown(t);
      t.assert.ok((await b.innerText()).includes("Glossa 0.8.0 is ready."), "the notice follows the channel");
      // Back to Preview: the test version again.
      await group.locator(".seg-opt", { hasText: "Preview" }).click();
      await says("Version 0.9.0-beta.2 is available");
      t.assert.equal(await group.getByRole("radio", { name: "Preview" }).isChecked(), true);
      // Nothing is ever offered that is not newer than this build: no downgrade when switching.
      app.releases.set({ stable: "0.0.0-a", releases: [] });
      await group.locator(".seg-opt", { hasText: "Stable" }).click();
      await says("You have version");
      t.assert.equal(await sec.getByRole("button", { name: "Open the download page" }).count(), 0, "no install button for an older version");
      t.assert.equal(await railEntry(t).isVisible(), false, "the rail has no Update entry");
      t.assert.equal(await banner(t).count(), 0, "no notice for an older version");
      app.releases.set({ stable: "0.8.0", releases: LIST_NEWER });
      await group.locator(".seg-opt", { hasText: "Preview" }).click();
      await says("Version 0.9.0-beta.2 is available");

      await eachView(t, "settings", async () => { await openUpdates(t); });
      // Turning updates off takes the notice and the rail entry away; turning them on and checking brings them back.
      await sec.getByRole("switch", { name: "Check for updates automatically" }).click();
      await railEntry(t).waitFor({ state: "hidden" });
      t.assert.equal(await banner(t).count(), 0);
      await sec.getByRole("switch", { name: "Check for updates automatically" }).click();
      await until(t, async () => (await app.run("update.settings.get")).enabled, "updates on");
      await sec.getByRole("button", { name: "Check for updates" }).click();
      await railEntry(t).waitFor({ state: "visible" });
    },
  },
  {
    row: "Updates: a failed check says nothing outside Settings, which shows the last error",
    shot: "m3-updates-error.png",
    async run(t) {
      const { app } = t;
      // Forget what the last check found, so the next start checks at once, and make GitHub fail.
      await app.run("update.settings.set", { channel: "stable" });
      await app.run("update.settings.set", { channel: "preview" });
      app.releases.set({ stable: "0.8.0", releases: LIST_NEWER, mode: "fail" });
      await app.page.reload();
      await app.page.waitForSelector(".shell");
      await until(t, async () => (await app.run("update.settings.get")).last_error, "the failure to be recorded");
      await app.page.waitForTimeout(600);
      t.assert.equal(await banner(t).count(), 0, "no notice for a failed check");
      t.assert.equal(await app.page.locator(".msg.error:visible").count(), 0, "no error anywhere on the Reader");
      t.assert.equal(await railEntry(t).isVisible(), false);
      const sec = await openUpdates(t);
      const err = sec.locator(".update-error");
      await err.waitFor();
      t.assert.ok((await err.innerText()).includes("The last check did not work"), await err.innerText());
      await t.shot();
      // A good check clears it.
      app.releases.set({ stable: "0.8.0", releases: LIST_NEWER });
      await sec.getByRole("button", { name: "Check for updates" }).click();
      await err.waitFor({ state: "detached" });
    },
  },
  {
    row: "Updates: with the placeholder key there is no Install button, only the download page, and it says why",
    shot: "m3-updates-refused.png",
    async run(t) {
      const { app } = t;
      app.releases.set({ stable: "0.8.0", releases: LIST_NEWER });
      await app.page.reload();
      await app.page.waitForSelector(".shell");
      const b = await noticeShown(t);
      t.assert.equal((await app.run("update.settings.get")).key_set, false, "the build still has the placeholder key");
      t.assert.equal(await b.getByRole("button", { name: "Install and restart" }).count(), 0, "no Install button in the notice");
      t.assert.ok((await b.innerText()).includes("does not have its update key yet"), "the notice says why");
      // The download page opens the way the Mac path opens URLs (the runtime's BrowserOpenURL); here it is caught, not opened.
      await app.page.evaluate(() => { window.__opened = null; window.runtime.BrowserOpenURL = (u) => { window.__opened = u; }; });
      await b.getByRole("button", { name: "Open the download page" }).click();
      const opened = await until(t, () => app.page.evaluate(() => window.__opened), "the download page to open");
      t.assert.equal(opened, "https://github.com/Perlsowisdom/glossa/releases/tag/v0.9.0-beta.2");
      t.assert.equal(await b.locator(".msg.error").count(), 0, "no error box");
      t.assert.ok(!app.releases.requests.some((p) => p.endsWith("-setup.exe")), "the installer was never asked for");
      t.assert.equal(await app.page.locator(".shell").count(), 1, "the app is still open");
      await t.shot();
      // Settings: the same, with no Install button either.
      const sec = await openUpdates(t);
      t.assert.equal(await sec.getByRole("button", { name: "Install and restart" }).count(), 0);
      t.assert.equal(await sec.getByRole("button", { name: "Open the download page" }).count(), 1);
      t.assert.ok((await sec.innerText()).includes("This build does not have its update key yet, so it cannot install updates."));
    },
  },
];
