// Starts the real desktop app (Wails on WebView2) for the interface tests and connects to it over
// the Chrome DevTools Protocol. The app runs on a temporary copy of the scratch library, with its
// own WebView2 profile, so the user's library and settings are never touched. restart() quits the
// app and opens it again on the same copy; stop() closes the app and every process it started.
//
//   const app = await startApp({ build: true });
//   await app.page.keyboard.type("## Hello");
//   await app.stop();
//
// Nothing here waits without a limit: the app has GLOSSA_E2E_START_MS (60 s) to start, or the run
// stops with the app's output; each request to it is limited too. A run that was killed leaves its
// app, WebView2 processes and run folder behind; the next run closes and removes them first.

import { execFileSync, spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { chmodSync, cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { DatabaseSync } from "node:sqlite";
import { homedir, tmpdir } from "node:os";
import { delimiter, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const here = dirname(fileURLToPath(import.meta.url));
export const repo = resolve(here, "..", "..");
export const shots = join(here, "shots");
// Holds fake claude and codex commands (canned output), put first on the app's PATH.
export const fakeCli = join(here, "fake-cli");
const pathKey = Object.keys(process.env).find((k) => k.toLowerCase() === "path") ?? "PATH";

// The library the tests copy: a scratch library with the test Bibles, never your own. Set GLOSSA_E2E_SOURCE
// to its folder; the default is Documents/Xegesis-scratch3 in your user profile.
const SOURCE = process.env.GLOSSA_E2E_SOURCE ?? process.env.XEGESIS_E2E_SOURCE ?? join(homedir(), "Documents", "Xegesis-scratch3");
// One folder per checkout: two worktrees running the tests at once must not build into, or run,
// each other's executable (a running app also locks it, so the other's build fails).
const WORK = join(tmpdir(), "glossa-e2e", createHash("sha1").update(repo.toLowerCase()).digest("hex").slice(0, 10));
export const EXE = join(WORK, "glossa-e2e.exe");
// The keychain service name the app files keys under while tested (secrets.ServiceEnv).
export const KEYRING_SERVICE = "glossa-e2e";
// How long the app may take to start before the run stops with an error.
const START_MS = Number(process.env.GLOSSA_E2E_START_MS ?? process.env.XEGESIS_E2E_START_MS ?? 60000);

function sh(cmd, args, cwd) {
  execFileSync(`${cmd} ${args.map((a) => (/[\s"]/.test(a) ? `"${a}"` : a)).join(" ")}`, { cwd, stdio: "inherit", shell: true });
}

// Wails's WebView2 loader clears the WEBVIEW2_* environment variables as it starts, so the
// debugging port could not be asked for. The test build (only) uses a copy of the loader package
// that reads them first, through a go.work file in the temporary folder. go.mod and the shipped
// app are unchanged.
function workspace() {
  const mod = "github.com/wailsapp/go-webview2";
  const info = JSON.parse(execFileSync("go", ["list", "-m", "-json", mod], { cwd: repo, encoding: "utf8" }));
  const copy = join(WORK, "go-webview2");
  rmSync(copy, { recursive: true, force: true });
  cpSync(info.Dir, copy, { recursive: true });
  const file = join(copy, "webviewloader", "env_create_options.go");
  const src = readFileSync(file, "utf8");
  const patched = src
    .replace("return o.additionalBrowserArguments", 'if e2eArgs == "" {\n\t\treturn o.additionalBrowserArguments\n\t}\n\treturn o.additionalBrowserArguments + " " + e2eArgs')
    .replace("wvep.userDataFolder = folder", 'wvep.userDataFolder = folder\n\t\tif e2eData != "" {\n\t\t\twvep.userDataFolder = e2eData\n\t\t}');
  if (!patched.includes("e2eArgs") || !patched.includes("e2eData")) throw new Error("The WebView2 loader has changed; update workspace() in e2e/app.mjs.");
  chmodSync(file, 0o644);
  writeFileSync(file, patched);
  writeFileSync(join(copy, "webviewloader", "e2e_env.go"), [
    "//go:build windows", "", "package webviewloader", "", 'import "os"', "",
    "// Read before the package's init clears them.",
    'var e2eArgs = os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")',
    'var e2eData = os.Getenv("WEBVIEW2_USER_DATA_FOLDER")', "",
  ].join("\n"));
  const goVersion = /^go (\S+)/m.exec(readFileSync(join(repo, "go.mod"), "utf8"))[1];
  const work = join(WORK, "go.work");
  const slash = (p) => JSON.stringify(p);
  writeFileSync(work, `go ${goVersion}\n\nuse ${slash(repo)}\n\nreplace ${mod} ${info.Version} => ${slash(copy)}\n`);
  return work;
}

// build makes frontend/dist and the test executable that embeds it.
export function build() {
  mkdirSync(WORK, { recursive: true });
  sh("npm", ["run", "build"], join(repo, "frontend"));
  execFileSync("go", ["build", "-tags", "desktop,production", "-o", EXE, "./app"], {
    cwd: repo, stdio: "inherit", env: { ...process.env, GOWORK: workspace() },
  });
}

// within gives up on a promise after ms, so nothing the tests wait for can stall the run.
export function within(p, ms, what) {
  let timer;
  return Promise.race([
    Promise.resolve(p).finally(() => clearTimeout(timer)),
    new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`Timed out after ${ms} ms: ${what}`)), ms); }),
  ]);
}

// waitFor retries fn until it gives a value. Each try is limited too, so a request that never
// answers (a port another program holds, a half-started WebView2) cannot hang it.
async function waitFor(fn, ms, what) {
  const until = Date.now() + ms;
  let last;
  while (Date.now() < until) {
    try {
      const v = await within(fn(), Math.max(500, Math.min(5000, until - Date.now())), what);
      if (v) return v;
    } catch (e) {
      if (e.fatal) throw e;
      last = e;
    }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`Timed out waiting for ${what}${last ? `: ${last.message}` : ""}`);
}

// freePort asks the system for a port nothing listens on.
function freePort() {
  return new Promise((ok, fail) => {
    const srv = createServer();
    srv.unref();
    srv.on("error", fail);
    srv.listen(0, "127.0.0.1", () => {
      const { port } = srv.address();
      srv.close(() => ok(port));
    });
  });
}

function killTree(pid) {
  if (!pid) return;
  try {
    if (process.platform === "win32") execFileSync("taskkill", ["/PID", String(pid), "/T", "/F"], { stdio: "ignore", timeout: 15000 });
    else process.kill(pid, "SIGKILL");
  } catch {
    // already gone
  }
}

// ps lists the processes whose path or command line mentions text (WebView2's processes name
// the profile folder they use), as [pid, name].
function ps(text) {
  const q = text.replaceAll("'", "''");
  try {
    const out = execFileSync("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command",
      `Get-CimInstance Win32_Process | Where-Object { ($_.CommandLine -and $_.CommandLine.Contains('${q}')) -or ($_.ExecutablePath -and $_.ExecutablePath.Contains('${q}')) } | ForEach-Object { "$($_.ProcessId) $($_.Name)" }`],
    { encoding: "utf8", timeout: 30000, stdio: ["ignore", "pipe", "ignore"] });
    return out.split(/\r?\n/).filter(Boolean).map((l) => {
      const [pid, ...name] = l.trim().split(" ");
      return [Number(pid), name.join(" ")];
    }).filter(([pid, name]) => pid !== process.pid && !/^(node|powershell)/i.test(name));
  } catch {
    return [];
  }
}

const pause = (ms) => new Promise((r) => setTimeout(r, ms));

// removeRun deletes a run folder, retrying while WebView2 lets go of its files. A folder still
// locked after that is left for the next run's sweep, with a warning, instead of failing this one.
async function removeRun(dir, ms = 10000) {
  const until = Date.now() + ms;
  for (;;) {
    try {
      rmSync(dir, { recursive: true, force: true, maxRetries: 2, retryDelay: 200 });
      return true;
    } catch (e) {
      if (Date.now() > until) {
        console.warn(`e2e: could not remove ${dir} yet (${e.code ?? e.message}); the next run removes it.`);
        return false;
      }
      await pause(400);
    }
  }
}

// sweep closes what an earlier, interrupted run left behind (its app and WebView2 processes)
// and removes its run folders.
async function sweep() {
  if (!existsSync(WORK)) return;
  for (const [pid] of [...ps(EXE), ...ps(join(WORK, "run-"))]) killTree(pid);
  for (const f of readdirSync(WORK)) if (f.startsWith("run-")) await removeRun(join(WORK, f), 3000);
}

// The first-run question about updates (T23) would sit over every test, so the copy starts with it
// answered "no". updates.e2e.mjs asks for it again to test it. This writes the copy, never the
// user's library.
function markUpdateQuestionAsked(db) {
  const d = new DatabaseSync(db);
  try {
    d.prepare("INSERT OR REPLACE INTO settings (key, value_json) VALUES ('update.settings', ?)").run('{"enabled":false,"asked":true}');
  } finally {
    d.close();
  }
}

// newDevice makes the copy start as a new device: the app gives it an ID of its own at start.
function newDevice(db) {
  const d = new DatabaseSync(db);
  try {
    d.prepare("DELETE FROM settings WHERE key = 'device_id'").run();
  } finally {
    d.close();
  }
}

// More than one app can run at once (the sync checks start two, each on its own library copy):
// pass sweep: false so starting the second does not close the first, keyring: a name of its own
// so their saved keys do not meet, and name: a label for its run folder. lite: true copies only
// library.db (not the Bible modules) and gives the copy a device of its own, as a second computer has.
export async function startApp({ build: doBuild = true, port, sweep: doSweep = true, keyring = KEYRING_SERVICE, name = "", lite = false } = {}) {
  if (process.platform !== "win32") throw new Error("The interface tests drive WebView2 and run on Windows.");
  if (!existsSync(join(SOURCE, "library.db"))) throw new Error(`No library to copy at ${SOURCE}`);
  if (doSweep) await sweep();
  if (doBuild || !existsSync(EXE)) build();
  mkdirSync(shots, { recursive: true });
  port ??= await freePort();

  const run = join(WORK, `run-${Date.now()}${name ? `-${name}` : ""}`);
  const lib = join(run, "library");
  if (lite) {
    mkdirSync(lib, { recursive: true });
    cpSync(join(SOURCE, "library.db"), join(lib, "library.db"));
    newDevice(join(lib, "library.db"));
  } else {
    cpSync(SOURCE, lib, { recursive: true });
  }
  markUpdateQuestionAsked(join(lib, "library.db"));
  const profile = join(run, "webview");
  mkdirSync(profile, { recursive: true });

  // launch starts the app on the run folder's library and connects to it. Each launch uses the same
  // library and WebView2 profile, so a restart shows what a person who quits and reopens sees.
  let log = "";
  const seen = [];
  const remember = (m) => { seen.push(m.slice(0, 300)); if (seen.length > 50) seen.shift(); };
  const launch = async (port) => {
    const child = spawn(EXE, [], {
      cwd: run,
      env: {
        ...process.env,
        // Fake claude and codex commands go first on PATH, so the real ones are never run.
        [pathKey]: `${fakeCli}${delimiter}${process.env[pathKey] ?? ""}`,
        GLOSSA_LIBRARY: join(lib, "library.db"),
        WEBVIEW2_USER_DATA_FOLDER: profile,
        // Keys the tests save go to their own keychain entries, never the person's.
        GLOSSA_KEYRING_SERVICE: keyring,
        // The occlusion and background flags keep the page painting and its timers running when another
        // window covers the app or the screen dims: a hidden page never finishes document.fonts.ready,
        // and every screenshot then waits "for fonts to load" until it times out.
        WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: `--remote-debugging-port=${port} --remote-allow-origins=* --disable-features=CalculateNativeWinOcclusion --disable-backgrounding-occluded-windows --disable-renderer-backgrounding --disable-background-timer-throttling`,
      },
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: false,
    });
    const s = { child, exited: false, browser: null, page: null };
    child.stdout.on("data", (d) => (log += d));
    child.stderr.on("data", (d) => (log += d));
    child.on("exit", () => (s.exited = true));
    cur = s;
    const started = Date.now();
    const left = () => Math.max(1000, START_MS - (Date.now() - started));
    await waitFor(async () => {
      if (s.exited) throw Object.assign(new Error("the app quit"), { fatal: true });
      const r = await fetch(`http://127.0.0.1:${port}/json/version`, { signal: AbortSignal.timeout(2000) });
      return r.ok;
    }, left(), `the debugging port ${port}`);
    s.browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`, { timeout: left() });
    s.page = await waitFor(() => s.browser.contexts().flatMap((c) => c.pages()).find((p) => p.url().startsWith("http")), left(), "the app page");
    s.page.setDefaultTimeout(30000);
    // What the page says on the way (console errors, uncaught exceptions), for the state dump of a failed check.
    s.page.on("console", (m) => { if (m.type() === "error" || m.type() === "warning") remember(`console.${m.type()}: ${m.text()}`); });
    s.page.on("pageerror", (e) => remember(`pageerror: ${e.message}`));
    // Count the page as focused and visible whatever is in front of it.
    s.cdp = await s.page.context().newCDPSession(s.page);
    await s.cdp.send("Emulation.setFocusEmulationEnabled", { enabled: true }).catch(() => {});
    // The checks were written for a 1480 x 950 page. Pin that size, so a smaller screen (a laptop
    // at 125 % scaling gives about 1230 x 650) does not push toolbar buttons into "More" and fail
    // checks that have nothing to do with screen size. GLOSSA_E2E_SIZE=native keeps the window's
    // own size; WxH sets another.
    const size = process.env.GLOSSA_E2E_SIZE ?? process.env.XEGESIS_E2E_SIZE ?? "1480x950";
    if (size !== "native") {
      const [width, height] = size.split("x").map(Number);
      await s.cdp.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false });
    }
    await s.page.waitForFunction(() => !!(window.go && window.go.main && window.go.main.App), null, { timeout: left() });
    await s.page.waitForSelector("#app, body", { timeout: left() });
    return s;
  };
  // quit asks the app to quit, then closes whatever of it is left, its WebView2 processes too.
  const quit = async (s) => {
    if (!s) return;
    try {
      if (s.page) {
        // Quit in a moment, so the call returns before the page goes.
        await within(s.page.evaluate(() => { setTimeout(() => window.runtime?.Quit?.(), 0); }), 3000, "asking the app to quit").catch(() => {});
        await waitFor(() => s.exited, 5000, "the app to quit").catch(() => {});
      }
    } finally {
      await within(s.browser?.close(), 5000, "closing the connection").catch(() => {});
      killTree(s.child.pid);
      for (const [pid] of ps(run)) killTree(pid);
      await pause(500);
    }
  };
  let cur = null;
  const start = async (port) => {
    try {
      return await launch(port);
    } catch (e) {
      await quit(cur);
      await removeRun(run);
      throw new Error(`The app did not start within ${Math.round(START_MS / 1000)} s: ${e.message}\nApp output:\n${log.slice(-2000) || "(none)"}`);
    }
  };

  await start(port);
  const app = {
    page: cur.page,
    browser: cur.browser,
    library: lib,
    log: () => log,
    forget: () => { seen.length = 0; },
    // The settings that checks change through commands, as the run found them (see rememberSettings).
    baseline: null,
    // rememberSettings notes the settings the checks may change, so restoreSettings can put them back.
    async rememberSettings() {
      const get = (id, p = {}) => app.run(id, p).catch(() => null);
      app.baseline = {
        update: await get("update.settings.get"), ccel: await get("ccel.settings"),
        assistant: await get("assistant.config.get"), sources: await get("bible.sources"),
      };
    },
    // restoreSettings puts them back where a check left them different. A restart, or a command that fails, never stops it.
    async restoreSettings() {
      const b = app.baseline;
      if (!b) return;
      const same = (a, c) => JSON.stringify(a) === JSON.stringify(c);
      const put = async (id, p) => { await app.run(id, p).catch((e) => remember(`restore ${id}: ${e.message}`)); };
      const u = await app.run("update.settings.get").catch(() => null);
      if (b.update && u && (u.enabled !== b.update.enabled || u.asked !== b.update.asked)) await put("update.settings.set", { enabled: b.update.enabled, asked: b.update.asked });
      const c = await app.run("ccel.settings").catch(() => null);
      if (b.ccel && c && c.enabled !== b.ccel.enabled) await put("ccel.settings", { enable: b.ccel.enabled });
      const a = await app.run("assistant.config.get").catch(() => null);
      if (b.assistant && a && !same(a.provider, b.assistant.provider)) await put("assistant.config.set", { provider: b.assistant.provider });
      const s = await app.run("bible.sources").catch(() => null);
      if (b.sources && s && s.enabled !== b.sources.enabled) await put("bible.sources", { enable: b.sources.enabled });
    },
    // run calls a command the way the interface does.
    run: (id, params = {}) => within(app.page.evaluate(async ([id, p]) => JSON.parse((await window.go.main.App.Run(id, JSON.stringify(p))) || "null"), [id, params]), 30000, `the command ${id}`),
    // shot saves a screenshot. It first waits for fonts for a moment, asking the page to the front; if
    // Playwright's own screenshot still stalls (it waits on document.fonts.ready), the page is captured
    // straight through DevTools, which does not.
    async shot(name) {
      const path = join(shots, name);
      await within(app.page.evaluate(() => document.fonts.ready.then(() => true)), 3000, "fonts").catch(() => {});
      try {
        await app.page.screenshot({ path, timeout: 8000, animations: "disabled", caret: "hide" });
      } catch (e) {
        const { data } = await within(cur.cdp.send("Page.captureScreenshot", { format: "png" }), 10000, "a screenshot through DevTools");
        writeFileSync(path, Buffer.from(data, "base64"));
        remember(`screenshot ${name}: Playwright stalled (${String(e.message).split("\n")[0]}); saved through DevTools`);
      }
    },
    // dump says what the page is in, as a few lines for a failed check: the view, visibility, open
    // dialogs and popovers, and what the console reported.
    async dump() {
      const state = await within(app.page.evaluate(() => {
        const vis = (e) => e.offsetParent !== null || getComputedStyle(e).position === "fixed";
        const text = (e) => (e.getAttribute("aria-label") || e.className || e.tagName).toString().slice(0, 60);
        return {
          hash: location.hash, visibility: document.visibilityState, focused: document.hasFocus(), fonts: document.fonts.status,
          dialogs: [...document.querySelectorAll("[role=dialog], dialog[open]")].filter(vis).map(text),
          popovers: [...document.querySelectorAll(".popover, [role=menu], [role=listbox]")].filter(vis).map(text),
          rightbar: !!document.querySelector("#rightbar") && vis(document.querySelector("#rightbar")),
          panes: document.querySelectorAll(".grid-pane").length, tabs: document.querySelectorAll(".grid-tab").length,
          busy: [...document.querySelectorAll("[aria-busy=true], .spinner, .loading")].filter(vis).length,
        };
      }), 3000, "the state dump").catch((e) => ({ unreachable: String(e.message) }));
      return `state ${JSON.stringify(state)}${seen.length ? `\n      console: ${seen.slice(-6).join(" | ")}` : ""}`;
    },
    // restart quits the app and opens it again on the same library copy; app.page is then the
    // new window's page.
    async restart() {
      await quit(cur);
      await start(await freePort());
      app.page = cur.page;
      app.browser = cur.browser;
    },
    async stop() {
      try {
        await quit(cur);
      } finally {
        await removeRun(run);
      }
    },
  };
  return app;
}
