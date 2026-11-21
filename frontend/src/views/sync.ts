// Settings → Sync: keep this library and your other devices level through a folder your own sync
// service (OneDrive, Dropbox, Google Drive, iCloud, Syncthing) already copies. Setup asks for the
// folder and a password, shows what will be merged, and changes nothing until you confirm. Afterwards
// the page shows the state, the devices, how books are kept, and any text kept from two edits.
// Everything calls sync.* commands; the core does the syncing (see ADR 0013).

import { Message, pick, run } from "../api";
import { segmented, settingRow, toggleSwitch } from "../controls";
import { badge, button, clear, field, h } from "../dom";
import { refreshSync, onSync, plain, showProblem as failed, syncNow } from "../syncstate";
import {
  SyncConflict, SyncPreview, SyncStatus, advice, conflictLine, countsText, lastSyncText, pendingText, percent, previewSentences, progressText, sizeText,
} from "../syncwords";

interface Strength { score: number; ok: boolean; hint: string; min_length: number }
interface SyncResult { pulled: { changes: number; conflicts: number }; pushed: number; blobs: { uploaded: number; downloaded: number; failed?: { name: string; error: string }[] } }

const STRENGTH_WORDS = ["Too short", "Weak", "Fair", "Good", "Strong"];

function hasData(counts: Record<string, number>): boolean {
  return countsText(counts) !== "nothing yet";
}

export function syncSection(): { el: HTMLElement; show: () => void; focus: () => void } {
  const body = h("div", { class: "sync-page" });
  let busy = false;
  // notice is what to say once the page has been redrawn after an action.
  let notice = "";

  async function show() {
    const s = await refreshSync();
    if (!s) return void clear(body, h("p", { class: "hint" }, "Sync is not available: no library is open."));
    if (!s.configured) return void setup(s);
    if (!s.last_sync) return void finishSetup(s);
    manage(s);
  }

  // Setup -----------------------------------------------------------------------------------------

  function setup(s: SyncStatus, keep?: { folder: string; name: string }) {
    const msg = new Message();
    const folder = h("input", { type: "text", spellcheck: "false", autocomplete: "off", placeholder: "For example D:\\OneDrive\\Glossa", id: "sync-folder", value: keep?.folder ?? "" }) as HTMLInputElement;
    const choose = button("Choose…", async () => {
      const dir = await msg.attempt(() => pick("folder", "Choose the folder your sync service already copies"));
      if (dir) folder.value = dir;
    });
    const pw = h("input", { type: "password", autocomplete: "new-password", spellcheck: "false" }) as HTMLInputElement;
    const again = h("input", { type: "password", autocomplete: "new-password", spellcheck: "false" }) as HTMLInputElement;
    const againField = field("Type the password again", again, "This folder is new, so this password will protect it. Type it once more to be sure.");
    againField.hidden = true;
    const name = h("input", { type: "text", autocomplete: "off", spellcheck: "false", value: keep?.name ?? s.device_name }) as HTMLInputElement;
    const remember = h("input", { type: "checkbox", role: "switch", class: "switch", id: "sync-remember" }) as HTMLInputElement;
    const strength = h("p", { class: "sync-strength hint", "aria-live": "polite" });
    let timer = 0;
    pw.addEventListener("input", () => {
      clearTimeout(timer);
      timer = window.setTimeout(async () => {
        if (!pw.value) return void clear(strength);
        const r = await run<Strength>("sync.password.strength", { password: pw.value }).catch(() => null);
        if (!r) return;
        clear(strength, badge(STRENGTH_WORDS[r.score] ?? "", r.ok ? (r.score >= 3 ? "ok" : "warn") : "bad"), " ", r.hint);
      }, 150);
    });

    const go = h("button", { type: "submit", class: "primary" }, "Continue");
    const form = h("form", { class: "sync-setup", "aria-label": "Set up sync" },
      h("div", { class: "field" },
        h("label", { for: "sync-folder" }, "Sync folder"),
        h("div", { class: "row" }, folder, choose),
        h("small", { class: "hint" }, "Pick a folder that OneDrive, Dropbox, Google Drive, iCloud or Syncthing already copies between your devices. Glossa makes a \"Glossa Sync\" folder inside it and sends nothing over the internet itself.")),
      field("Password", pw, "At least 10 characters. Everything in the folder is encrypted with it, so the sync service never sees your notes or books."),
      strength,
      againField,
      h("div", { class: "sync-warning", role: "note" },
        h("strong", {}, "Keep this password safe."),
        " If you lose it, Glossa cannot read the synced copy in the folder, and nobody can recover it for you. Nothing on this device is lost: your notes, highlights and books here stay as they are."),
      h("div", { class: "settings-card" },
        settingRow("Remember on this device", "Saves the password in this computer's credential store so sync carries on after a restart. Off: Glossa asks for it each time it starts.", remember, "sync-remember")),
      field("This device's name", name, "Shown on your other devices, for example next to a note changed on both."),
      h("div", { class: "row wrap" }, go),
      msg.el);

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      if (busy) return;
      msg.clear();
      if (!folder.value.trim()) return msg.info("Choose the folder first.");
      if (!pw.value) return msg.info("Choose a password.");
      busy = true;
      go.disabled = true;
      try {
        // Looking at a folder changes nothing: a wrong password is refused here, before anything is set up.
        const pv = await run<SyncPreview>("sync.preview", { folder: folder.value.trim(), password: pw.value });
        if (pv.new_folder && againField.hidden) {
          againField.hidden = false;
          again.focus();
          return msg.info("This folder has no sync data yet. Type the password again to confirm it.");
        }
        if (pv.new_folder && again.value !== pw.value) return failed(msg, new Error("The two passwords are not the same."));
        preview(pv, {
          intro: pv.new_folder ? "You are starting sync in a new folder." : "You are joining a folder another device started.",
          start: async (backup) => {
            if (backup) await run("backup.create");
            await run("sync.setup", { folder: folder.value.trim(), password: pw.value, device_name: name.value.trim(), remember: remember.checked });
            await run("sync.now");
          },
          back: () => setup(s, { folder: folder.value, name: name.value }),
        });
      } catch (e2) {
        failed(msg, e2, plain(e2).code === "wrong_password" ? "Nothing was changed." : "");
      } finally {
        busy = false;
        go.disabled = false;
      }
    });
    clear(body, h("p", { class: "hint" }, "Sync keeps your notes, highlights, sermons, lists, plans and books the same on every device you use, through a folder you already sync."), form);
  }

  // finishSetup is for a device that chose a folder but has not confirmed the first sync.
  async function finishSetup(s: SyncStatus) {
    const msg = new Message();
    clear(body, h("p", { class: "hint" }, `Sync is set up with ${s.folder} but has not run yet. Check what will be merged, then start.`), msg.el);
    if (!s.unlocked) return clear(body, unlockForm(s));
    try {
      const pv = await run<SyncPreview>("sync.preview");
      preview(pv, {
        intro: "Sync is set up but has not run yet.",
        start: async (backup) => {
          if (backup) await run("backup.create");
          await run("sync.now");
        },
        back: () => stop(),
      });
    } catch (e) {
      failed(msg, e);
    }
  }

  // preview shows what the first sync would do and does nothing until the person confirms.
  function preview(pv: SyncPreview, o: { intro: string; start: (backup: boolean) => Promise<void>; back: () => void }) {
    const msg = new Message();
    const t = previewSentences(pv);
    const backup = h("input", { type: "checkbox", checked: true, id: "sync-backup" }) as HTMLInputElement;
    const data = hasData(pv.this_device);
    const start = h("button", { type: "button", class: "primary" }, "Merge and start syncing") as HTMLButtonElement;
    start.addEventListener("click", async () => {
      if (busy) return;
      busy = true;
      start.disabled = true;
      msg.info(backup.checked && data ? "Backing up this library, then syncing…" : "Syncing…");
      try {
        await o.start(data && backup.checked);
        await show();
      } catch (e) {
        failed(msg, e, "Nothing was merged into this library.");
        start.disabled = false;
      } finally {
        busy = false;
      }
    });
    clear(body,
      h("section", { class: "sync-preview", "aria-labelledby": "sync-preview-h" },
        h("h3", { id: "sync-preview-h" }, "Check before anything changes"),
        h("p", { class: "hint" }, o.intro),
        h("ul", { class: "plain sync-counts" },
          h("li", { "data-row": "here" }, t.here), h("li", { "data-row": "folder" }, t.folder), h("li", { "data-row": "after" }, t.after)),
        pv.to_this_device.changes > 0 || pv.to_folder > 0
          ? h("p", { class: "hint" }, `${pv.to_this_device.changes} change${pv.to_this_device.changes === 1 ? "" : "s"} will come to this device and ${pv.to_folder} will go to the folder.`) : null,
        h("p", { class: "hint" }, "Nothing on this device or in the folder has been changed yet."),
        data ? h("label", { class: "check" }, backup, " Back up this library first (recommended). It saves a full copy of your library file in your backups folder.") : null,
        h("div", { class: "row wrap" }, start, button("Back", o.back)),
        msg.el));
  }

  function unlockForm(s: SyncStatus): HTMLElement {
    const msg = new Message();
    const pw = h("input", { type: "password", autocomplete: "off", spellcheck: "false", "aria-label": "Sync password" }) as HTMLInputElement;
    const remember = h("input", { type: "checkbox" }) as HTMLInputElement;
    const form = h("form", { class: "sync-unlock-form", "aria-label": "Enter the sync password" },
      h("p", {}, "Sync is waiting for its password."),
      h("div", { class: "row wrap" }, pw, h("button", { type: "submit", class: "primary" }, "Unlock")),
      h("label", { class: "check" }, remember, " Remember on this device"),
      msg.el);
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      try {
        await run("sync.unlock", { password: pw.value, remember: remember.checked });
        pw.value = "";
        await show();
      } catch (e2) {
        failed(msg, e2, "Nothing was changed.");
      }
    });
    void s;
    return form;
  }

  async function stop() {
    if (!confirm("Turn sync off on this device? The folder and everything on this device are left as they are, and the saved password is forgotten.")) return;
    const m = new Message();
    try {
      await run("sync.stop");
      await show();
    } catch (e) {
      failed(m, e);
      body.append(m.el);
    }
  }

  // After setup -------------------------------------------------------------------------------------

  function manage(s: SyncStatus) {
    const msg = new Message();
    if (notice) msg.ok(notice);
    notice = "";
    const bar = h("progress", { class: "sync-bar", max: "100", value: "0", "aria-label": "Sync progress" }) as HTMLProgressElement;
    const barText = h("span", { class: "sync-bar-text" });
    const progress = h("div", { class: "sync-progress", hidden: true, role: "status" }, bar, barText);
    const paint = () => {
      const p = syncNow.progress;
      progress.hidden = !p?.active;
      if (p?.active) {
        bar.value = percent(p);
        barText.textContent = `${progressText(p)} ${p.name ? `(${p.name})` : ""}`.trim();
      }
    };
    const unsub = onSync(() => {
      if (!progress.isConnected) return unsub();
      paint();
    });
    paint();

    const now = button("Sync now", async () => {
      if (busy) return;
      busy = true;
      now.disabled = true;
      msg.info("Syncing…");
      try {
        const r = await run<SyncResult>("sync.now");
        notice = resultText(r);
        syncNow.progress = null;
        await show();
      } catch (e) {
        failed(msg, e);
        now.disabled = false;
        void refreshSync();
      } finally {
        busy = false;
      }
    }, { class: "primary sync-now" }) as HTMLButtonElement;

    const status = h("dl", { class: "info sync-status" },
      h("dt", {}, "Last synced"), h("dd", { "data-sync": "last" }, lastSyncText(s.last_sync)),
      h("dt", {}, "Waiting to be sent"), h("dd", { "data-sync": "pending" }, pendingText(s.pending_changes)),
      h("dt", {}, "Folder"), h("dd", {}, h("code", {}, s.folder ?? "")),
      h("dt", {}, "Space used"), h("dd", { "data-sync": "size" }, sizeText(s.folder_bytes)),
      h("dt", {}, "This device"), h("dd", {}, s.device_name || s.device_id));

    const problem = s.folder_error ? { text: s.folder_error, code: "" } : s.last_error ? { text: s.last_error, code: s.last_error_code ?? "" } : null;
    const auto = toggleSwitch({
      label: "Sync automatically", checked: s.automatic,
      hint: "Syncs when Glossa starts, every few minutes while it is open, and when you quit. Off: only when you press Sync now.",
      onChange: async (on) => {
        try {
          await run("sync.automatic", { enabled: on });
          msg.ok(on ? "Sync is on." : "Automatic sync is off. Sync now still works.");
        } catch (e) {
          failed(msg, e);
        }
        void refreshSync();
      },
    });

    const devices = h("ul", { class: "plain sync-devices" });
    clear(devices, (s.devices ?? []).map((d) => h("li", { class: "sync-device", "data-device": d.name },
      h("span", { class: "sync-device-name" }, d.name || d.id, d.self ? " " : null, d.self ? badge("This device", "ok") : null),
      h("span", { class: "hint" }, ` ${sizeText(d.bytes)}`),
      d.self ? null : button("Forget this device", async () => {
        if (!confirm(`Forget ${d.name || "this device"}? Its files are removed from the folder, and it must be set up again to rejoin. Changes it already sent stay.`)) return;
        try {
          await run("sync.forget_device", { device_id: d.id });
          notice = `${d.name || "The device"} was removed from the folder.`;
          await show();
        } catch (e) {
          failed(msg, e);
        }
      }, { class: "danger" }))));

    const books = bookSettings(s, msg);
    const conflicts = h("div", { class: "sync-conflicts" });
    void loadConflicts(conflicts, msg);

    clear(body,
      s.unlocked ? null : unlockForm(s),
      h("div", { class: "settings-card" }, auto),
      h("div", { class: "row wrap" }, now,
        h("span", { class: "sync-state" }, s.syncing ? badge("Syncing…") : problem ? badge("Problem", "bad") : badge("Up to date", "ok"))),
      progress,
      problem ? h("p", { class: "msg error", role: "alert" }, `${problem.text}. `, advice(problem.code)) : null,
      msg.el,
      status,
      h("h3", {}, "Devices"),
      h("p", { class: "hint" }, "Devices that have synced through this folder."),
      devices,
      h("h3", {}, "Books"),
      books,
      conflicts,
      h("h3", {}, "Password"),
      h("div", { class: "settings-card" },
        settingRow("Change password", "Not available yet. To use a new password for now, turn sync off here, choose a new folder and set sync up again.",
          h("button", { type: "button", disabled: true, "aria-disabled": "true" }, "Change password"))),
      h("div", { class: "row wrap" }, button("Turn sync off on this device", () => void stop(), { class: "danger" })));
  }

  function bookSettings(s: SyncStatus, msg: Message): HTMLElement {
    const b = s.books;
    const dl = button("Download all now", async () => {
      if (busy) return;
      busy = true;
      dl.disabled = true;
      msg.info("Downloading…");
      try {
        const r = await run<{ all?: { downloaded: number; linked: number; failed?: unknown[]; waiting: number } }>("sync.fetch", { all: true });
        const n = (r.all?.downloaded ?? 0) + (r.all?.linked ?? 0);
        notice = `${n} book${n === 1 ? "" : "s"} and module${n === 1 ? "" : "s"} now on this device.${r.all?.waiting ? ` ${r.all.waiting} still on the way from your sync service.` : ""}`;
        syncNow.progress = null;
        await show();
      } catch (e) {
        failed(msg, e);
        dl.disabled = false;
      } finally {
        busy = false;
      }
    }) as HTMLButtonElement;
    dl.disabled = !b || b.remote_only === 0;
    const mode = segmented<"all" | "opened">({
      label: "Keep books on this device", value: b?.mode ?? "all", options: [["all", "All of them"], ["opened", "When opened"]],
      onChange: async (v) => {
        try {
          await run("sync.books", { mode: v });
          notice = v === "all" ? "Every book will be kept on this device." : "Books from other devices download when you open them.";
          void show();
        } catch (e) {
          failed(msg, e);
        }
      },
    });
    return h("div", { class: "settings-card sync-books" },
      settingRow("Keep books on this device", "All: every book downloads at sync time. When opened: a book from another device downloads the first time you open it, which saves space.", mode),
      settingRow("Books and modules in the folder",
        b ? `${b.files} in all (${sizeText(b.bytes)}); ${b.on_device} on this device${b.remote_only ? `, ${b.remote_only} not here yet (${sizeText(b.remote_bytes)})` : ""}.` : "Not known yet.", dl));
  }

  async function loadConflicts(host: HTMLElement, msg: Message) {
    let list: SyncConflict[] = [];
    try {
      list = (await run<SyncConflict[] | null>("sync.conflicts")) ?? [];
    } catch {
      return;
    }
    if (!list.length) return;
    clear(host,
      h("h3", {}, "Changed on two devices"),
      h("p", { class: "hint" }, "These were edited on another device as well. The newer edit is shown; the other version is kept in the item's history."),
      h("ul", { class: "plain sync-conflict-list" }, list.map((c) => {
        const to = c.object_type === "notes" ? `#notes/note/${c.object_id}` : c.object_type === "documents" ? `#sermons/${c.object_id}` : "";
        return h("li", { class: "sync-conflict", "data-conflict": c.object_id },
          h("span", {}, conflictLine(c)), " ",
          h("details", { class: "sync-kept" }, h("summary", {}, "Show the replaced text"), h("pre", {}, c.text)),
          to ? button("Open", () => { location.hash = to; }) : null,
          button("Dismiss", async () => {
            try {
              await run("sync.conflicts.dismiss", { id: c.id });
              notice = "Dismissed. The other version stays in the history.";
              void show();
            } catch (e) {
              failed(msg, e);
            }
          }));
      })));
  }

  return { el: h("div", {}, body), show: () => void show(), focus: () => body.querySelector<HTMLElement>("input, button")?.focus() };
}

function resultText(r: SyncResult): string {
  const got = r.pulled?.changes ?? 0;
  const parts = [`Synced. ${got} change${got === 1 ? "" : "s"} received, ${r.pushed} sent.`];
  if (r.pulled?.conflicts) parts.push(`${r.pulled.conflicts} text edit${r.pulled.conflicts === 1 ? " was" : "s were"} changed on two devices; both versions are kept.`);
  if (r.blobs?.failed?.length) parts.push(`${r.blobs.failed.length} book file${r.blobs.failed.length === 1 ? "" : "s"} could not be copied.`);
  return parts.join(" ");
}
