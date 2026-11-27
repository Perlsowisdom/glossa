// T32 and ADR 0009, problem reports in the real app. A report goes nowhere by itself: it is built from a
// small local log, shown as text the person can edit, and then copied, saved, or opened as a pre-filled
// public GitHub issue in the browser. The app is started with GLOSSA_BROWSER_LOG, so "open" writes the
// address to a file and no browser ever starts; no network is used. What the checks look at: Help →
// Report a problem shows the version and log lines but no path from this test library; editing the text
// changes what is copied and what the issue address holds, and a long report is cut under the limit; an
// error thrown in the window shows the notice once and Not now hides it; a crash marker left by an earlier
// run shows the notice on start with its stack stripped; turning the log off deletes it. Screenshots
// (m3-problems-<what>-1536x816.png) at 1536 x 816.

import { existsSync, mkdirSync, readdirSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const SHOT_SIZE = [1536, 816];
const MAX_URL = 7000;

const dialog = (t) => t.app.page.locator("#problem-dialog");
const notice = (t) => t.app.page.locator("#problem-banner");
const logDir = (t) => join(t.app.library, "logs");

// until waits for fn to give a value (the app's own events arrive on their own).
async function until(t, fn, what, ms = 15000) {
  for (const end = Date.now() + ms; Date.now() < end; await t.app.page.waitForTimeout(150)) {
    const v = await fn();
    if (v) return v;
  }
  throw new Error(`Timed out waiting for ${what}`);
}

async function big(t) {
  await t.app.resize(...SHOT_SIZE);
  await t.app.page.waitForTimeout(300);
}

async function normal(t) {
  await t.app.resize(1480, 950);
}

// openFromHelp opens Help on the rail and chooses Report a problem.
async function openFromHelp(t) {
  const { page } = t.app;
  await page.getByRole("button", { name: "Help", exact: true }).click();
  await page.getByRole("button", { name: "Report a problem…" }).click();
  await dialog(t).waitFor();
  await t.app.page.waitForFunction(() => document.querySelector("#problem-text")?.value.includes("## Recent log"));
}

async function closeDialog(t) {
  if (await dialog(t).count()) await dialog(t).getByRole("button", { name: "Cancel" }).click();
  await dialog(t).waitFor({ state: "detached" });
}

const text = (t) => dialog(t).locator("#problem-text").inputValue();

// quiet removes any notice and puts the app where the next check expects it.
async function quiet(t) {
  if (await dialog(t).count()) await closeDialog(t);
  if (await notice(t).count()) await notice(t).getByRole("button", { name: "Not now" }).click();
}

export default [
  {
    row: "Problems: Help → Report a problem shows the version, log lines and no paths; the text and the issue address follow what the person edits",
    shot: "m3-problems-dialog-1536x816.png",
    async run(t) {
      const { app, page } = t;
      app.clearOpened();
      // A log line that names the test library, as an error message might; the report must hide it.
      const lib = app.library;
      await app.run("problems.log", { message: `could not open ${lib}\\library.db and ${join(lib, "modules", "kjv.db")}`, where: "e2e" });
      await openFromHelp(t);
      await big(t);
      const first = await text(t);
      const version = (await app.run("app.info")).version;
      t.assert.ok(first.includes("## What were you doing?"), "the note part");
      t.assert.ok(first.includes(`- Version: ${version}`), `the version ${version} is in the report`);
      t.assert.ok(/- System: Windows \d+\.\d+\.\d+, amd64/.test(first), "the system");
      t.assert.ok(first.includes("## Recent log") && first.includes("Glossa started"), "recent log lines");
      t.assert.ok(first.includes("could not open <library> and <data>"), `the library and data folders are hidden: ${first.slice(-600)}`);
      for (const private_ of [lib, "run-", "glossa-e2e", "Xegesis-scratch3", homedir(), "webview"]) {
        t.assert.ok(!first.includes(private_), `the report holds ${private_}`);
      }
      t.assert.equal(await dialog(t).locator("#problem-note").inputValue(), "", "the box starts empty");
      t.assert.ok((await dialog(t).innerText()).includes("GitHub issues are public"), "the dialog says the issue is public");
      await t.shot();

      // The note box writes into the text; the text itself can be edited too, and that is what goes out.
      await dialog(t).locator("#problem-note").fill("I pressed Save in a sermon");
      await dialog(t).locator("#problem-text").evaluate((el) => { el.value += "\nEDITED-LINE-BY-PERSON\n"; el.dispatchEvent(new Event("input", { bubbles: true })); });
      const edited = await text(t);
      t.assert.ok(edited.includes("## What were you doing?\n\nI pressed Save in a sermon\n"), "the note is under its heading");
      t.assert.ok(edited.includes("EDITED-LINE-BY-PERSON"));

      // Copy puts exactly the shown text on the clipboard.
      await page.evaluate(() => {
        window.__copied = null;
        const real = navigator.clipboard.writeText.bind(navigator.clipboard);
        navigator.clipboard.writeText = async (s) => { window.__copied = s; return real(s).catch(() => {}); };
      });
      await dialog(t).getByRole("button", { name: "Copy" }).click();
      await page.waitForFunction(() => window.__copied !== null);
      t.assert.equal(await page.evaluate(() => window.__copied), edited, "Copy gives exactly what the box shows");
      await until(t, async () => (await dialog(t).locator(".msg").innerText()).startsWith("Copied"), "the Copied line");

      // Open as GitHub issue: the address the app would open, without a browser.
      t.assert.equal(app.opened().length, 0, "nothing was opened before the button");
      await dialog(t).getByRole("button", { name: "Open as GitHub issue (public)" }).click();
      const urls = await until(t, () => { const o = app.opened(); return o.length ? o : null; }, "the address");
      t.assert.equal(urls.length, 1);
      const [kind, address] = [urls[0].slice(0, 3), urls[0].slice(4)];
      t.assert.equal(kind, "url");
      const u = new URL(address);
      t.assert.equal(u.origin + u.pathname, "https://github.com/Perlsowisdom/glossa/issues/new");
      t.assert.ok(address.length <= MAX_URL, `the address is ${address.length} characters`);
      t.assert.equal(u.searchParams.get("body"), edited, "the issue holds exactly the edited text");
      t.assert.ok(u.searchParams.get("title").startsWith("Problem report"));
      const msg = await dialog(t).locator(".msg").innerText();
      t.assert.ok(msg.includes("Glossa has sent nothing"), msg);
      t.assert.ok(!msg.includes("too long"), "a short report is not cut");
      await t.shot();
      await closeDialog(t);
      await normal(t);
    },
  },
  {
    row: "Problems: a long report is cut to fit the address and says so; Cancel closes without opening anything",
    shot: "m3-problems-long-1536x816.png",
    async run(t) {
      const { app } = t;
      app.clearOpened();
      await openFromHelp(t);
      await big(t);
      const long = (await text(t)).replace("```\n", "```\n" + Array.from({ length: 400 }, (_, i) => `2026-11-26T12:00:00 level=INFO msg="padding line ${i}" key=value`).join("\n") + "\n");
      await dialog(t).locator("#problem-text").fill(long);
      await dialog(t).getByRole("button", { name: "Open as GitHub issue (public)" }).click();
      const urls = await until(t, () => { const o = app.opened(); return o.length ? o : null; }, "the address");
      const address = urls[0].slice(4);
      t.assert.ok(address.length <= MAX_URL, `the address is ${address.length} characters`);
      const body = new URL(address).searchParams.get("body");
      t.assert.ok(body.includes("shortened"), "the text says it was shortened");
      t.assert.ok(body.includes("padding line 399") || body.includes("Glossa"), "the newest lines are kept");
      t.assert.ok(!body.includes("padding line 0\""), "the oldest lines are cut first");
      const msg = await dialog(t).locator(".msg").innerText();
      t.assert.ok(msg.includes("too long") && msg.includes("Copy"), msg);
      t.assert.equal(await text(t), long, "the box keeps the whole text for Copy");
      await t.shot();
      app.clearOpened();
      await closeDialog(t);
      t.assert.equal(app.opened().length, 0);
      await normal(t);
    },
  },
  {
    row: "Problems: an error thrown in the window shows the notice once, beside the update notice; Not now hides it and it does not return",
    shot: "m3-problems-notice-1536x816.png",
    async run(t) {
      const { app, page } = t;
      await quiet(t);
      await big(t);
      const boom = () => page.evaluate(() => { setTimeout(() => { throw new Error("E2E interface boom"); }, 0); });
      await boom();
      await notice(t).waitFor();
      t.assert.equal(await notice(t).count(), 1);
      t.assert.ok((await notice(t).innerText()).includes("Glossa ran into a problem. Review a report?"));
      t.assert.equal(await page.evaluate(() => document.activeElement?.closest("#problem-banner") == null), true, "the notice does not take focus");
      t.assert.equal(await page.locator("dialog[open]").count(), 0, "the notice is not a dialog");
      // The same error again does not make a second notice.
      await boom();
      await page.waitForTimeout(700);
      t.assert.equal(await notice(t).count(), 1, "one notice for one problem");
      // An unhandled promise rejection is caught too, as another problem, but only one notice is up.
      await page.evaluate(() => { Promise.reject(new Error("E2E rejected promise")); });
      await page.waitForTimeout(500);
      t.assert.equal(await notice(t).count(), 1);
      // With an update notice in the corner too, the two do not overlap.
      await page.evaluate(() => {
        const fake = document.createElement("div");
        fake.className = "update-banner";
        fake.id = "fake-update";
        fake.innerHTML = "<p><strong>Glossa 9.9.9 is out.</strong> A pretend update notice.</p><div class='row'><button class='primary'>Open the download page</button><button>Later</button></div>";
        document.body.append(fake);
      });
      await page.waitForTimeout(300);
      const rects = await page.evaluate(() => ["#problem-banner", "#fake-update"].map((s) => { const r = document.querySelector(s).getBoundingClientRect(); return [r.left, r.top, r.right, r.bottom]; }));
      const [a, b] = rects;
      t.assert.ok(a[3] <= b[1] || b[3] <= a[1] || a[2] <= b[0] || b[2] <= a[0], `the notices overlap: ${JSON.stringify(rects)}`);
      await t.shot();
      await page.evaluate(() => document.querySelector("#fake-update").remove());
      await page.waitForTimeout(200);

      await notice(t).getByRole("button", { name: "Not now" }).click();
      await notice(t).waitFor({ state: "detached" });
      await until(t, async () => !(await app.run("problems.status")).pending, "Not now to mark it seen");
      await boom();
      await page.waitForTimeout(700);
      t.assert.equal(await notice(t).count(), 0, "a problem already seen is not announced again");
      // A different problem is a new one.
      await page.evaluate(() => { setTimeout(() => { throw new Error("E2E another problem"); }, 0); });
      await notice(t).waitFor();
      // Review opens the report with the error in it, and the notice goes.
      await notice(t).getByRole("button", { name: "Review" }).click();
      await dialog(t).waitFor();
      await notice(t).waitFor({ state: "detached" });
      const report = await text(t);
      t.assert.ok(report.includes("## Problem") && report.includes("Interface error: ") && report.includes("E2E another problem"), report.slice(0, 600));
      await closeDialog(t);
      await normal(t);
    },
  },
  {
    row: "Problems: a crash marker from an earlier run shows the notice on start, and the report holds its stack without paths",
    shot: "m3-problems-crash-1536x816.png",
    async run(t) {
      const { app } = t;
      await quiet(t);
      const logs = logDir(t);
      await app.restart({
        between: () => {
          // What a run that was killed leaves: its marker and the Go runtime's crash output.
          mkdirSync(logs, { recursive: true });
          writeFileSync(join(app.library, "running.marker"), "2026-11-26T10:00:00Z");
          writeFileSync(join(logs, "crash.txt"), `fatal error: e2e pretend crash\n\ngoroutine 1 [running]:\nmain.main()\n\t${join(app.library, "x.go")}:9 +0x1a\n`);
        },
      });
      await app.page.waitForSelector(".shell", { timeout: 30000 });
      await notice(t).waitFor({ timeout: 20000 });
      await big(t);
      await t.shot();
      await notice(t).getByRole("button", { name: "Review" }).click();
      await dialog(t).waitFor();
      const report = await text(t);
      t.assert.ok(report.includes("## Problem") && report.includes("did not close properly") && report.includes("(crash)"), report);
      t.assert.ok(report.includes("e2e pretend crash"), "the crash output is in the report");
      t.assert.ok(report.includes("<data>") || report.includes("<path>"), "its path is hidden");
      t.assert.ok(!report.includes(app.library) && !report.includes("run-"), "no path from this library");
      await closeDialog(t);
      await normal(t);
      // Seen once: after a normal quit and start there is no notice.
      await app.restart();
      await app.page.waitForSelector(".shell", { timeout: 30000 });
      await app.page.waitForTimeout(1200);
      t.assert.equal(await notice(t).count(), 0, "no notice at the next start");
    },
    timeout: 180000,
  },
  {
    row: "Problems: Settings → Privacy has the log switch and the log folder; turning the log off deletes it",
    shot: "m3-problems-settings-1536x816.png",
    async run(t) {
      const { app, page } = t;
      await quiet(t);
      app.clearOpened();
      await app.run("problems.log", { message: "E2E something for the log", where: "e2e" });
      t.assert.ok(existsSync(join(logDir(t), "glossa.log")), "the log is there");
      await page.evaluate(() => { location.hash = "#settings/privacy"; });
      const sec = page.locator(".settings-section:visible");
      await sec.getByRole("heading", { name: "Privacy" }).waitFor();
      await big(t);
      const sw = sec.getByRole("switch", { name: "Keep a problem log on this computer" });
      await sw.waitFor();
      t.assert.equal(await sw.isChecked(), true, "on by default");
      await sw.scrollIntoViewIfNeeded();
      await t.shot();
      // Open the log folder: asked of the app, not started here.
      await sec.getByRole("button", { name: "Open the log folder" }).click();
      const opened = await until(t, () => { const o = app.opened(); return o.length ? o : null; }, "the folder");
      t.assert.ok(opened[0].startsWith("folder ") && opened[0].endsWith("logs"), opened[0]);
      // Off: the log is deleted and nothing more is written.
      await sw.click();
      await until(t, async () => (await app.run("problems.status")).enabled === false, "the log to turn off");
      t.assert.deepEqual(existsSync(logDir(t)) ? readdirSync(logDir(t)) : [], [], "the log folder is empty");
      await page.waitForTimeout(500);
      t.assert.equal(await sw.isChecked(), false);
      await app.run("problems.log", { message: "E2E while off", where: "e2e" });
      t.assert.deepEqual(existsSync(logDir(t)) ? readdirSync(logDir(t)) : [], [], "nothing is written while off");
      // On again.
      await sw.click();
      await until(t, async () => (await app.run("problems.status")).enabled === true, "the log to turn on");
      await app.run("problems.log", { message: "E2E back on", where: "e2e" });
      t.assert.ok(existsSync(join(logDir(t), "glossa.log")), "the log is written again");
      // Leave nothing pending: a notice would sit over the checks of the files after this one.
      await app.run("problems.dismiss", { id: "" });
      await quiet(t);
      await normal(t);
    },
  },
];
