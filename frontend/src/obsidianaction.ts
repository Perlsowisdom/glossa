// Obsidian in the interface: "Send to Obsidian" in a note's or sermon's ⋯ menu, the question asked when a
// note changed on both sides, and the picker for choosing which notes and sermons to export. Every step is
// an obsidian.* command.

import { Message, Note, run } from "./api";
import { failureText } from "./connectwords";
import { button, clear, h } from "./dom";
import { flash } from "./listpicker";
import {
  CHOICES, Choice, NOT_READY, ObsidianConflict, ObsidianExport, ObsidianStatus, conflictHeading, exportText, menuLabel, resolvedText, sentText,
} from "./obsidianwords";
import { workChanged } from "./work";

interface Conn { id: string; enabled: boolean }

// obsidianHook keeps what a menu needs to know about Obsidian: whether it is on and has a vault, and whether this
// item already has a file. The menu is built at once, so refresh runs when the page opens and after every send.
export function obsidianHook(kind: "note" | "sermon") {
  let ready = false;
  let linked = false;
  return {
    async refresh(id: string | null): Promise<void> {
      ready = false;
      linked = false;
      try {
        const conns = await run<Conn[]>("connect.list");
        if (!conns.find((c) => c.id === "obsidian")?.enabled) return;
        const st = await run<ObsidianStatus>("obsidian.status", id ? { kind, id } : {});
        ready = !!st.vault && !st.problem;
        linked = ready && !!st.item_path;
      } catch {
        ready = false;
      }
    },
    entry(id: string | null, msg: Message, before?: () => Promise<void>, after?: () => void): [string, () => void] {
      return [menuLabel(linked), () => {
        if (!id) return msg.info("Write something first.");
        if (!ready) return msg.info(NOT_READY);
        void (async () => {
          await before?.();
          await sendToObsidian(kind, id, msg, async () => { linked = true; after?.(); });
        })();
      }];
    },
  };
}

// sendToObsidian writes (or updates) the file, and asks when both sides changed.
export async function sendToObsidian(kind: "note" | "sermon", id: string, msg: Message, done?: () => void | Promise<void>) {
  msg.info("Sending to Obsidian…");
  let r: ObsidianExport;
  try {
    r = await run<ObsidianExport>("obsidian.export", { items: [{ kind, id }] });
  } catch (e) {
    msg.error(e);
    return;
  }
  const item = r.items[0];
  if (r.skipped.length) {
    msg.error(new Error(`${r.skipped[0].path}: ${r.skipped[0].reason}`));
    return;
  }
  if (r.conflicts.length) {
    msg.clear();
    askWhich(kind, r.conflicts[0], async (choice) => {
      try {
        const out = await run<{ message: string }>("obsidian.resolve", { kind, id, resolve: choice });
        msg.ok(out.message);
        flash(out.message);
        workChanged();
        await done?.();
      } catch (e) {
        msg.error(e);
      }
    });
    return;
  }
  const text = sentText(item?.state ?? "", item?.path);
  msg.ok(text);
  flash(text);
  if (item && item.state !== "obsidian_newer") await done?.();
}

// askWhich is the question: what each side holds, and Obsidian's, Glossa's, or both.
export function askWhich(kind: "note" | "sermon", c: ObsidianConflict, choose: (r: Choice) => void) {
  const dlg = h("dialog", { class: "any-dialog", "aria-labelledby": "obs-ask-title" });
  const side = (who: string, title: string, text: string) => h("div", { class: "any-side" },
    h("h4", {}, who), h("p", { class: "any-side-title" }, title || "(no title)"), h("p", { class: "any-side-text" }, text || "(empty)"));
  dlg.append(
    h("h3", { id: "obs-ask-title" }, conflictHeading(kind)),
    h("p", { class: "hint" }, `Nothing has been changed yet. The file is ${c.path}. Choose which to keep.`),
    h("div", { class: "any-sides" }, side("In Glossa", c.title, c.your_excerpt), side("In Obsidian", c.obsidian_title, c.obsidian_excerpt)),
    h("div", { class: "any-choices" }, CHOICES.map((o) => h("div", { class: "any-choice" },
      button(o.label, () => { dlg.close(); choose(o.choice); }, { class: o.choice === "glossa" ? "primary" : "", "data-choice": o.choice }),
      h("span", { class: "hint" }, o.help)))),
    h("div", { class: "row end" }, button("Not now", () => dlg.close())));
  dlg.addEventListener("close", () => dlg.remove());
  document.body.append(dlg);
  dlg.showModal();
  dlg.querySelector<HTMLElement>("button")?.focus();
}

interface Doc { id: string; title: string; kind: string }

// exportPicker lists the notes and sermons with check boxes, and exports the ones that are ticked.
export async function exportPicker(done: (r: ObsidianExport) => void) {
  const msg = new Message();
  const dlg = h("dialog", { class: "any-dialog any-picker", "aria-labelledby": "obs-pick-title" });
  const list = h("ul", { class: "plain obsidian-pick", "aria-label": "Notes and sermons" });
  const search = h("input", { type: "search", placeholder: "Search by title", "aria-label": "Search notes and sermons", class: "any-search" }) as HTMLInputElement;
  const go = h("button", { type: "button", class: "primary", "data-obsidian": "export-chosen" }, "Export") as HTMLButtonElement;
  const empty = h("p", { class: "hint", hidden: true }, "Nothing matches.");
  dlg.append(h("div", { class: "row between" }, h("h3", { id: "obs-pick-title" }, "Choose what to export"), button("Close", () => dlg.close(), { "aria-label": "Close" })),
    msg.el, search, list, empty, h("div", { class: "row end" }, go));
  dlg.addEventListener("close", () => dlg.remove());
  document.body.append(dlg);
  dlg.showModal();

  type Row = { kind: "note" | "sermon"; id: string; title: string; box: HTMLInputElement; li: HTMLElement };
  const rows: Row[] = [];
  try {
    const notes = await run<Note[]>("note.list", { limit: 1000 });
    const docs = await run<Doc[]>("document.list", {});
    const add = (kind: "note" | "sermon", id: string, title: string, label: string) => {
      const box = h("input", { type: "checkbox", "aria-label": `Export ${title}` }) as HTMLInputElement;
      const li = h("li", { class: "any-page", "data-title": title.toLowerCase() }, h("label", { class: "any-page-text" }, box, " ", h("strong", {}, title), " ", h("span", { class: "hint" }, label)));
      rows.push({ kind, id, title, box, li });
      list.append(li);
    };
    for (const n of notes) add("note", n.id, n.title || firstWords(n.body), "Note");
    for (const d of docs) add("sermon", d.id, d.title, d.kind === "lesson" ? "Lesson" : d.kind === "study" ? "Study" : "Sermon");
  } catch (e) {
    clear(list, h("li", { class: "hint" }, failureText(e)));
  }
  const refresh = () => {
    const q = search.value.trim().toLowerCase();
    let shown = 0;
    for (const r of rows) { r.li.hidden = !!q && !r.title.toLowerCase().includes(q); if (!r.li.hidden) shown++; }
    empty.hidden = shown > 0 || rows.length === 0;
    const n = rows.filter((r) => r.box.checked).length;
    go.disabled = n === 0;
    go.textContent = n === 0 ? "Export" : `Export ${n}`;
  };
  search.addEventListener("input", refresh);
  list.addEventListener("change", refresh);
  refresh();
  go.addEventListener("click", async () => {
    go.disabled = true;
    try {
      const items = rows.filter((r) => r.box.checked).map((r) => ({ kind: r.kind, id: r.id }));
      const r = await run<ObsidianExport>("obsidian.export", { items });
      dlg.close();
      done(r);
    } catch (e) {
      msg.error(e);
      refresh();
    }
  });
  search.focus();
}

function firstWords(body: string): string {
  const t = body.replace(/[#>*_`\-\[\]]+/g, " ").replace(/\s+/g, " ").trim();
  return t ? t.slice(0, 50) : "Untitled note";
}

export { exportText, resolvedText };
