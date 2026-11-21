// What the rest of the interface knows about sync: the last status, the transfer under way, and the
// small unlock prompt at start. The core does the syncing (at start, every few minutes and at quit);
// this only shows it. Everything calls sync.* commands (views/sync.ts has the Settings page).

import { Message, RunError, onEvent, run } from "./api";
import { button, h } from "./dom";
import { SyncProgress, SyncStatus, advice, progressText } from "./syncwords";

export const syncNow: { status: SyncStatus | null; progress: SyncProgress | null } = { status: null, progress: null };
const listeners = new Set<() => void>();

// onSync calls fn whenever the status or the transfer changes; it returns a function that stops that.
export function onSync(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function changed() {
  for (const fn of [...listeners]) fn();
}

// plain is a command error's own message, which for sync is already in plain words.
export function plain(e: unknown): { text: string; code: string } {
  if (e instanceof RunError) return { text: e.info.message.replace(/^[a-z_]+\.[a-z_.]+: /, ""), code: e.info.code };
  return { text: e instanceof Error ? e.message : String(e), code: "failed" };
}

export function problemText(e: unknown): string {
  const { text, code } = plain(e);
  const more = advice(code);
  return more ? `${text}. ${more}` : text;
}

// showProblem shows a failure in plain words: the core's own message, then what to do next.
export function showProblem(m: Message, e: unknown, extra = "") {
  m.el.className = "msg error";
  m.el.setAttribute("role", "alert");
  m.el.textContent = problemText(e) + (extra ? ` ${extra}` : "");
}

// fetchBook downloads a book that came from another device and is not on this one yet, showing
// "Downloading… n%" in m while it does. It says "none" when sync has nothing to offer for the
// book (sync is off or locked, or no device shared it) so the caller shows its own error, and
// "failed" after showing why the download failed.
export async function fetchBook(itemID: string, m: Message): Promise<"done" | "none" | "failed"> {
  if (!syncNow.status) await refreshSync();
  if (!syncNow.status?.configured || !syncNow.status.unlocked) return "none";
  m.info("Downloading… 0%");
  syncNow.progress = null;
  const stop = onSync(() => {
    const p = syncNow.progress;
    if (p?.active) m.info(progressText(p));
  });
  try {
    await run("sync.fetch", { item_id: itemID });
    return "done";
  } catch (e) {
    if (e instanceof RunError && e.info.code === "not_found") {
      m.clear();
      return "none";
    }
    showProblem(m, e);
    return "failed";
  } finally {
    stop();
  }
}

// refreshSync reads sync.status. It does nothing without sync commands or an open library.
export async function refreshSync(): Promise<SyncStatus | null> {
  try {
    syncNow.status = await run<SyncStatus>("sync.status");
  } catch {
    syncNow.status = null;
  }
  changed();
  return syncNow.status;
}

let prompt: HTMLElement | null = null;

function closePrompt() {
  prompt?.remove();
  prompt = null;
}

// askPassword is the small prompt at start when the password is not remembered on this device.
function askPassword() {
  if (prompt) return;
  const pw = h("input", { type: "password", autocomplete: "off", spellcheck: "false", "aria-label": "Sync password", placeholder: "Sync password" });
  const remember = h("input", { type: "checkbox", id: "sync-unlock-remember" });
  const msg = h("p", { class: "msg", role: "alert" });
  const go = async () => {
    const p = pw.value;
    if (!p) return;
    try {
      await run("sync.unlock", { password: p, remember: remember.checked });
      pw.value = "";
      closePrompt();
      await run("sync.auto").catch(() => {});
      await refreshSync();
    } catch (e) {
      msg.className = "msg error";
      msg.textContent = plain(e).text;
    }
  };
  const form = h("form", { class: "update-banner sync-unlock", role: "region", "aria-label": "Unlock sync", id: "sync-unlock" },
    h("p", {}, h("strong", {}, "Enter your sync password")),
    h("p", { class: "hint" }, "Sync is waiting for it. Your notes and books on this device are fine without it."),
    pw,
    h("label", { class: "check" }, remember, " Remember on this device"),
    msg,
    h("div", { class: "row" }, h("button", { type: "submit", class: "primary" }, "Unlock"), button("Later", closePrompt)));
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    void go();
  });
  prompt = form;
  document.body.append(form);
  pw.focus();
}

// startSync listens to what the core reports and, if sync is set up but locked, asks for the password.
export async function startSync(): Promise<void> {
  onEvent("sync:progress", (p) => {
    syncNow.progress = p as SyncProgress;
    changed();
  });
  onEvent("sync:done", () => {
    void refreshSync();
  });
  const s = await refreshSync();
  if (s?.configured && s.last_sync && !s.unlocked) askPassword();
  // "x minutes ago" stays right without asking the core.
  setInterval(changed, 60000);
}
