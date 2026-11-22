// Any in the interface: "Send to Any" in a note's or sermon's ⋯ menu, "Import from Any" in My work,
// and the question asked when something changed on both sides. Every step is an any.* command.

import { Message, run } from "./api";
import {
  AnyConflict, AnyPage, AnyPages, AnySpaces, AnySyncResult, CHOICES, NOT_READY, Resolve, conflictHeading, menuLabel, pageLine, whatIs,
} from "./anywords";
import { failureText } from "./connectwords";
import { button, clear, h, select } from "./dom";
import { flash } from "./listpicker";
import { workChanged } from "./work";

interface Conn { id: string; enabled: boolean; key_set: boolean }

// anyHook keeps what a note's or sermon's menu needs to know about Any: whether it is turned on and
// signed in, and whether this item already has a page there. The menu itself is built at once, so
// refresh runs when the page opens and after every send.
export function anyHook(kind: "note" | "sermon") {
  let ready = false;
  let linked = false;
  return {
    async refresh(id: string | null): Promise<void> {
      try {
        const conns = await run<Conn[]>("connect.list");
        const any = conns.find((c) => c.id === "any");
        ready = !!any?.enabled && !!any.key_set;
        linked = false;
        if (ready && id) linked = (await run<{ linked: boolean }>("any.sync_status", { kind, id })).linked;
      } catch {
        ready = false;
      }
    },
    // entry is the line for the menu.
    entry(id: string | null, msg: Message, before?: () => Promise<void>, after?: () => void): [string, () => void] {
      return [menuLabel(linked), () => {
        if (!id) return msg.info("Write something first.");
        if (!ready) return msg.info(NOT_READY);
        void (async () => {
          await before?.();
          await sendToAny(kind, id, msg, async () => { linked = true; after?.(); });
        })();
      }];
    },
  };
}

// sendToAny sends (or updates) the page, and asks when both sides changed.
export async function sendToAny(kind: "note" | "sermon", id: string, msg: Message, done?: () => void | Promise<void>) {
  msg.info("Sending to Any…");
  const call = (resolve?: Resolve) => run<AnySyncResult>("any.send", { kind, id, ...(resolve ? { resolve } : {}) });
  await settle(call, kind, msg, done);
}

// settle runs a sync call and handles what it says: done, or a question to ask.
async function settle(call: (r?: Resolve) => Promise<AnySyncResult>, kind: "note" | "sermon", msg: Message, done?: () => void | Promise<void>, onResult?: (r: AnySyncResult) => void) {
  let res: AnySyncResult;
  try {
    res = await call();
  } catch (e) {
    msg.error(e);
    return;
  }
  const finish = async (r: AnySyncResult) => {
    msg.ok(r.message);
    flash(r.message);
    onResult?.(r);
    workChanged();
    await done?.();
  };
  if (res.state !== "both_changed" && res.state !== "any_changed") return void (await finish(res));
  msg.clear();
  askWhich(res.state, whatIs(kind), res.conflict!, async (choice) => {
    try {
      await finish(await call(choice));
    } catch (e) {
      msg.error(e);
    }
  });
}

// askWhich is the question: what each side holds, and Glossa's, Any's, or both.
export function askWhich(state: "both_changed" | "any_changed", what: string, c: AnyConflict, choose: (r: Resolve) => void) {
  const dlg = h("dialog", { class: "any-dialog", "aria-labelledby": "any-ask-title" });
  const side = (who: string, title: string, text: string) => h("div", { class: "any-side" },
    h("h4", {}, who), h("p", { class: "any-side-title" }, title || "(no title)"), h("p", { class: "any-side-text" }, text || "(empty)"));
  dlg.append(
    h("h3", { id: "any-ask-title" }, conflictHeading(state, what)),
    h("p", { class: "hint" }, "Nothing has been changed yet. Choose which to keep."),
    h("div", { class: "any-sides" }, side("In Glossa", c.glossa_title, c.glossa_excerpt), side("In Any", c.any_title, c.any_excerpt)),
    h("div", { class: "any-choices" }, CHOICES.map((o) => h("div", { class: "any-choice" },
      button(o.label, () => { dlg.close(); choose(o.resolve); }, { class: o.resolve === "glossa" ? "primary" : "" }),
      h("span", { class: "hint" }, o.hint)))),
    h("div", { class: "row end" }, button("Not now", () => dlg.close())));
  dlg.addEventListener("close", () => dlg.remove());
  document.body.append(dlg);
  dlg.showModal();
  dlg.querySelector<HTMLElement>("button")?.focus();
}

// importFromAny opens a picker of the pages in a space: search, then bring one in as a note.
export async function importFromAny(onImported?: (noteId: string) => void) {
  const msg = new Message();
  const dlg = h("dialog", { class: "any-dialog any-picker", "aria-labelledby": "any-pick-title" });
  const body = h("div", { class: "any-pick-body" });
  dlg.append(h("div", { class: "row between" }, h("h3", { id: "any-pick-title" }, "Import from Any"), button("Close", () => dlg.close(), { "aria-label": "Close" })), msg.el, body);
  dlg.addEventListener("close", () => dlg.remove());
  document.body.append(dlg);
  dlg.showModal();

  let spaces: AnySpaces;
  try {
    spaces = await run<AnySpaces>("any.spaces");
  } catch (e) {
    clear(body, h("p", { class: "hint" }, failureText(e)));
    return;
  }
  const space = select(spaces.spaces.map((s) => [s.id, s.name] as [string, string]), spaces.default_space_id ?? spaces.spaces[0]?.id ?? "", { "aria-label": "Space in Any" });
  const search = h("input", { type: "search", placeholder: "Search pages", "aria-label": "Search pages in Any", class: "any-search" }) as HTMLInputElement;
  const list = h("ul", { class: "plain any-pages", "aria-label": "Pages in Any" });
  const more = h("div", { class: "row" });
  const empty = h("p", { class: "hint", hidden: true }, "No pages found.");
  clear(body, h("div", { class: "row wrap" }, h("div", { class: "grow" }, search), spaces.spaces.length > 1 ? space : null), list, empty, more);

  let seq = 0;
  async function load(offset: number) {
    const mine = ++seq;
    let r: AnyPages;
    try {
      r = await run<AnyPages>("any.pages", { space_id: space.value, query: search.value.trim(), offset, limit: 30 });
    } catch (e) {
      msg.error(e);
      return;
    }
    if (mine !== seq) return;
    msg.clear();
    if (!offset) list.replaceChildren();
    for (const p of r.pages) list.append(row(p));
    empty.hidden = list.children.length > 0;
    clear(more, r.next_offset ? button("Show more", () => void load(r.next_offset!)) : null);
  }

  function row(p: AnyPage): HTMLElement {
    const li = h("li", { class: "any-page", "data-page": p.id });
    const act = button(p.linked_to ? "Update" : "Bring in", async () => {
      act.disabled = true;
      await settle((resolve) => run<AnySyncResult>("any.import", { space_id: space.value, object_id: p.id, ...(resolve ? { resolve } : {}) }),
        "note", msg, async () => { await load(0); }, (r) => { if (r.state === "created") onImported?.(r.item_id); });
      act.disabled = false;
    }, { "aria-label": `${p.linked_to ? "Update from" : "Bring in"} ${p.title || "untitled page"}` });
    li.append(h("div", { class: "any-page-text" }, h("strong", {}, p.title || "(untitled)"), h("span", { class: "hint" }, pageLine(p))), act);
    return li;
  }

  let timer = 0;
  search.addEventListener("input", () => { clearTimeout(timer); timer = window.setTimeout(() => void load(0), 250); });
  space.addEventListener("change", () => void load(0));
  await load(0);
  search.focus();
}
