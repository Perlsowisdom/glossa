// Updates for the direct download. On unless the person turns them off (ADR 0016): the core checks once
// a day. When a newer version is known, a small notice offers "Install and restart" or "Later", and the
// rail shows an Update entry until the version is installed. "Later" hides the notice for that version
// until the next start. A failed check says nothing here; Settings → Updates shows the last error.
// The Microsoft Store build has no updater, so the commands are missing and nothing shows.

import { Message, onEvent, run } from "./api";
import { button, h } from "./dom";
import { plain } from "./syncstate";
import { NO_KEY_LINE, UpdateInfo, installFailure, noticeHelp, noticeTitle, shortNotes, updateAction } from "./updatewords";

export interface Available {
  version: string;
  notes?: string;
  manual?: boolean;
  noKey?: boolean; // this build has no update key: it can only open the download page
}

let available: Available | null = null;
const dismissed = new Set<string>(); // versions the person said "Later" to, until the next start
const listeners = new Set<() => void>();
let banner: HTMLElement | null = null;

// updateReady is the newer version that is waiting, or null.
export function updateReady(): Available | null {
  return available;
}

// onUpdateReady calls fn when a newer version becomes known or is no longer waiting.
export function onUpdateReady(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function setAvailable(a: Available | null) {
  if (available?.version === a?.version && available?.notes === a?.notes) return;
  available = a;
  paint();
  for (const fn of [...listeners]) fn();
}

function hide() {
  banner?.remove();
  banner = null;
}

function paint() {
  hide();
  if (available && !dismissed.has(available.version)) banner = notice(available);
}

// restartToUpdate downloads, verifies and starts the installer; the app quits when it is running.
export async function restartToUpdate(msg: Message): Promise<void> {
  try {
    const r = await run<{ open_url?: string; manual?: boolean }>("update.install");
    if (r?.open_url && !r.manual) {
      openDownload(r.open_url);
      msg.ok("The download page is open in your browser.");
      return;
    }
    if (r?.open_url) {
      openDownload(r.open_url);
      msg.ok("The download page is open in your browser. Open the new disk image and drag Glossa to Applications, replacing the old one.");
      return;
    }
    msg.ok("Updating. Glossa will close and open again in a moment.");
  } catch (e) {
    // One plain sentence, not the raw error and its code.
    msg.el.className = "msg error";
    msg.el.setAttribute("role", "alert");
    msg.el.textContent = installFailure(plain(e).text);
  }
}

// openDownload opens the new version's download in the browser (macOS).
function openDownload(url: string) {
  const rt = (window as unknown as { runtime?: { BrowserOpenURL?: (u: string) => void } }).runtime;
  if (rt?.BrowserOpenURL) rt.BrowserOpenURL(url);
  else window.open(url, "_blank", "noopener");
}

// notice is the small, non-blocking panel in the corner. It takes no focus; Tab reaches it.
function notice(a: Available): HTMLElement {
  const msg = new Message();
  const notes = shortNotes(a.notes);
  const el = h("div", { class: "update-banner update-notice", role: "region", "aria-label": "Update ready", id: "update-banner" },
    h("p", {}, h("strong", {}, noticeTitle(a.version, a.noKey)), " ", noticeHelp(a.manual, a.noKey)),
    notes ? h("p", { class: "update-notes" }, h("strong", {}, "What’s new:"), h("br"), notes) : null,
    a.noKey && !a.manual ? h("p", { class: "hint" }, NO_KEY_LINE) : null,
    h("div", { class: "row" },
      button(updateAction(a.manual, a.noKey), () => void restartToUpdate(msg), { class: "primary" }),
      button("Later", () => {
        dismissed.add(a.version);
        hide();
      })),
    msg.el);
  document.body.append(el);
  return el;
}

// refreshUpdates reads what the core knows and shows or clears the notice. Settings calls it after a
// check or a change of channel.
export async function refreshUpdates(): Promise<UpdateInfo | undefined> {
  try {
    const info = await run<UpdateInfo>("update.settings.get");
    setAvailable(info.enabled && info.newer && info.latest ? { version: info.latest, notes: info.notes, manual: info.manual, noKey: !info.key_set && !info.manual } : null);
    return info;
  } catch {
    return undefined; // no update commands: the Store build, or no library open
  }
}

// startUpdates listens for a newer version and shows one found earlier. update.auto checks if a day has
// passed (the core also does this on a timer).
export async function startUpdates(): Promise<void> {
  onEvent("update:available", () => void refreshUpdates());
  if (!(await refreshUpdates())) return;
  try {
    await run("update.auto");
  } catch {
    return;
  }
  await refreshUpdates();
}
