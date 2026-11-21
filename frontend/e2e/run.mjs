// npm run e2e: builds the desktop app, starts it on a copy of the scratch library, runs every
// *.e2e.mjs file in this folder against it by typing with real key events, and closes the app.
//
//   npm run e2e                     build, then run everything
//   npm run e2e -- --no-build       use the last build
//   npm run e2e -- markdown         only files or checks whose name contains "markdown"
//   npm run e2e -- --no-keep-awake  let the PC sleep during the run (the default keeps it awake)
//   npm run e2e -- --shuffle        run the files in random order (prints the seed); --seed=N repeats an order
//
// Each file's default export is a list of checks { row, shot, run(t) }. A check passes when run
// returns; it saves its screenshot to e2e/shots/<shot>. Results go to e2e/shots/results.md.

import { spawn } from "node:child_process";
import { readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { shots, startApp, within } from "./app.mjs";
import { helpers } from "./helpers.mjs";

const args = process.argv.slice(2);
const filters = args.filter((a) => !a.startsWith("--")).map((a) => a.toLowerCase());
const here = import.meta.dirname;
let files = readdirSync(here).filter((f) => f.endsWith(".e2e.mjs")).sort();
// --shuffle exposes order dependence: a check that needs what an earlier file left behind fails in some orders.
if (args.includes("--shuffle") || args.some((a) => a.startsWith("--seed="))) {
  let seed = Number(args.find((a) => a.startsWith("--seed="))?.slice(7)) || (Date.now() % 1000000);
  console.log(`Shuffled with --seed=${seed}`);
  const rnd = () => { seed = (seed * 1664525 + 1013904223) % 4294967296; return seed / 4294967296; };
  files = files.map((f) => [rnd(), f]).sort((a, b) => a[0] - b[0]).map((x) => x[1]);
}

// A run takes a while and types into a real window, so the PC must not sleep or turn the screen off
// part-way. On Windows a small PowerShell asks for ES_CONTINUOUS | ES_SYSTEM_REQUIRED |
// ES_DISPLAY_REQUIRED (0x80000003) for as long as this run lives; it ends when this process does (it
// watches our PID), and stopping it gives the setting back. --no-keep-awake skips it.
function keepAwake() {
  if (process.platform !== "win32" || args.includes("--no-keep-awake")) return () => {};
  const script = [
    "Add-Type -Namespace W -Name P -MemberDefinition '[DllImport(\"kernel32.dll\")] public static extern uint SetThreadExecutionState(uint f);'",
    "[void][W.P]::SetThreadExecutionState(0x80000003)",
    `while (Get-Process -Id ${process.pid} -ErrorAction SilentlyContinue) { Start-Sleep -Seconds 5 }`,
    "[void][W.P]::SetThreadExecutionState(0x80000000)",
  ].join("; ");
  const ps = spawn("powershell.exe", ["-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script], { stdio: "ignore", windowsHide: true });
  ps.on("error", () => {});
  return () => { try { ps.kill(); } catch { /* already gone */ } };
}
const releaseAwake = keepAwake();
process.on("exit", releaseAwake);

const app = await startApp({ build: !args.includes("--no-build") });
let stopping;
const stop = () => (stopping ??= app.stop());
for (const sig of ["SIGINT", "SIGTERM", "SIGBREAK"]) process.on(sig, () => void stop().finally(() => process.exit(130)));

// alive says whether the app still answers; a crashed or hung app is restarted instead of failing every later check.
const alive = () => within(app.page.evaluate(() => 1 + 1), 5000, "the app to answer").then(() => true, () => false);

const results = [];
try {
  await app.rememberSettings();
  for (const f of files) {
    const checks = (await import(pathToFileURL(join(here, f)).href)).default;
    const wanted = checks.filter((c) => !filters.length || filters.some((x) => f.toLowerCase().includes(x) || c.row.toLowerCase().includes(x)));
    if (!wanted.length) continue;
    // Each file starts from a known state, whatever the file before it did (see helpers.reset).
    try {
      if (!(await alive())) { console.log("      (the app did not answer; restarting it)"); await app.restart(); }
      await helpers(app, "").reset();
    } catch (e) {
      console.log(`      (could not reset before ${f}: ${String(e.message).split("\n")[0]}; restarting the app)`);
      await app.restart();
      await helpers(app, "").reset();
    }
    for (const c of checks) {
      if (filters.length && !filters.some((x) => f.toLowerCase().includes(x) || c.row.toLowerCase().includes(x))) continue;
      const t = helpers(app, c.shot);
      const started = Date.now();
      try {
        // A check that stops responding fails on its own instead of stalling the run.
        await within(c.run(t), c.timeout ?? 120000, "the check to finish");
        if (!t.shotTaken) await t.shot();
        results.push({ file: f, row: c.row, ok: true, shot: c.shot, note: t.note });
        console.log(`PASS  ${c.row}  (${Date.now() - started} ms)${t.note ? `\n      note: ${t.note}` : ""}`);
      } catch (e) {
        const state = await app.dump();
        await app.shot(`FAIL-${c.shot}`).catch(() => {});
        results.push({ file: f, row: c.row, ok: false, shot: `FAIL-${c.shot}`, error: String(e?.message ?? e).split("\n")[0] });
        console.log(`FAIL  ${c.row}\n      ${String(e?.stack ?? e).split("\n").slice(0, 4).join("\n      ")}\n      ${state}`);
      }
    }
  }
} finally {
  await stop();
  releaseAwake();
}

const failed = results.filter((r) => !r.ok).length;
const cell = (s) => String(s).replaceAll("|", "\\|");
writeFileSync(join(shots, "results.md"), [
  "| Check | Result | Screenshot |", "| --- | --- | --- |",
  ...results.map((r) => `| ${cell(r.row)} | ${r.ok ? `PASS${r.note ? ` (${cell(r.note)})` : ""}` : `FAIL: ${cell(r.error)}`} | ${r.shot} |`),
].join("\n") + "\n");
console.log(`\n${results.length - failed} passed, ${failed} failed (${results.length} checks). Screenshots in e2e/shots.`);
process.exit(failed ? 1 : 0);
