// The Bin: deleted notes, sermons, lists, reading plans, collections and folders wait here. Restore brings one back (a folder
// with everything deleted with it); "Delete for good" asks first, then removes it and its text from
// the history too (bin.purge).

import { Message, run } from "../api";
import { button, clear, h } from "../dom";
import { formatTime } from "../helpers";
import { icon } from "../icons";
import { ask } from "../ask";
import { emptyState, loadFolders, onWorkChanged, workChanged } from "../work";
import { BinEntry, binContents, binKind, binTitle, purgeQuestion } from "../workwords";

const ICON: Record<BinEntry["kind"], string> = { folder: "folder", note: "notes", document: "sermon", list: "list", plan: "plan", collection: "library" };

// The list loads a page at a time; the number in the heading is the true count (bin.count).
const PAGE = 200;

export function binView() {
  let shown = PAGE; // how many are loaded; it grows with Show more and is kept when the page refreshes
  const msg = new Message();
  const sub = h("p", { class: "hint work-sub", "aria-live": "polite" });
  const list = h("ul", { class: "bin-list", "aria-label": "In the bin" });
  const el = h("div", { class: "view work-view" },
    h("div", { class: "work-head" }, h("div", { class: "work-heading" }, h("h1", { class: "work-h1" }, icon("bin"), h("span", {}, "Bin")), sub)),
    msg.el, list);

  async function refresh() {
    const [got, total] = await Promise.all([
      msg.attempt(() => run<BinEntry[]>("bin.list", { limit: shown })),
      msg.attempt(() => run<{ count: number }>("bin.count")),
    ]);
    if (!got || !total) return;
    const n = total.count;
    sub.textContent = n
      ? `${n} ${n === 1 ? "thing" : "things"}, most recently deleted first. Restore puts each back where it was.`
      : "Deleted notes, sermons, lists, reading plans, collections and folders wait here until you restore them.";
    clear(list, got.length === 0
      ? h("li", { class: "work-empty" }, emptyState("bin", "The bin is empty", "Anything you delete in My work comes here first, so nothing is lost by mistake."))
      : [...got.map(row), ...(n > got.length ? [more(got.length, n)] : [])]);
  }

  function more(loaded: number, n: number): HTMLElement {
    return h("li", { class: "bin-more" },
      button(`Show more (${n - loaded} left)`, () => { shown = loaded + PAGE; void refresh(); }));
  }

  function row(e: BinEntry): HTMLElement {
    const inside = binContents(e);
    return h("li", { class: "bin-row" },
      h("span", { class: `bin-icon kind-${e.kind}` }, icon(ICON[e.kind])),
      h("div", { class: "bin-main" },
        h("strong", { class: e.title ? "" : "untitled" }, binTitle(e)),
        h("span", { class: "hint" }, [binKind(e), e.kind === "folder" || e.kind === "collection" ? inside : "", `Deleted ${formatTime(e.deleted_at)}`].filter(Boolean).join(" · "))),
      h("div", { class: "bin-actions" },
        button("Restore", () => void restore(e), { "aria-label": `Restore ${binTitle(e)}` }),
        button("Delete for good", () => void purge(e), { class: "danger", "aria-label": `Delete ${binTitle(e)} for good` })));
  }

  async function restore(e: BinEntry) {
    if (await msg.attempt(() => run("bin.restore", { kind: e.kind, id: e.id })) === undefined) return;
    msg.ok(`Restored "${binTitle(e)}".`);
    if (e.kind === "folder") await loadFolders();
    workChanged();
  }

  async function purge(e: BinEntry) {
    if (!(await ask("Delete for good?", purgeQuestion(e), "Delete for good"))) return;
    if (await msg.attempt(() => run("bin.purge", { kind: e.kind, id: e.id })) === undefined) return;
    msg.ok(`Deleted "${binTitle(e)}" for good.`);
    workChanged();
  }

  onWorkChanged(() => { if (el.offsetParent !== null) void refresh(); });
  return { el, show: refresh, focus: () => (list.querySelector<HTMLElement>("button") ?? el).focus() };
}
