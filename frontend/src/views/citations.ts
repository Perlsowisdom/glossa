// Settings → Citations: the style notes and sermons cite in unless they choose their own, and more styles
// from the Citation Style Language project. Downloading is a network feature, so it has a switch that
// is off until turned on, with the exact sentence beside it. The words matched when searching never leave
// this computer (internal/cite/styles.go).

import { Message, run } from "../api";
import { STYLES_SENDS, StyleLike, styleKind } from "../citewords";
import { toggleSwitch } from "../controls";
import { badge, button, clear, h } from "../dom";

interface Online { enabled: boolean; sends: string }
interface Found { id: string; name: string; dependent: boolean; have: boolean }
interface Searched { styles: Found[]; total: number }
interface StyleInfo { style: string; default: string }

export function citationsSection(): { el: HTMLElement; show: () => void; focus: () => void } {
  const msg = new Message();
  const chosen = h("div", { class: "settings-card" });
  const more = h("div", { class: "settings-card" });
  const el = h("div", { class: "citations-page" },
    h("h3", {}, "Default style"), chosen,
    h("h3", {}, "More styles"), more, msg.el);

  let styles: (StyleLike & { source: string })[] = [];
  let online: Online = { enabled: false, sends: STYLES_SENDS };
  let query = "";
  let found: Searched | null = null;
  let searching = false;
  let busy = "";

  async function load() {
    const [list, on] = await Promise.all([
      run<{ styles: (StyleLike & { source: string })[] }>("csl.styles"),
      run<Online>("cite.styles_online"),
    ]);
    styles = list.styles;
    online = on;
  }

  async function show() {
    try {
      await load();
    } catch (e) {
      msg.error(e);
      return;
    }
    const cur = await run<StyleInfo>("cite.style", { id: "" }).catch(() => null);
    drawChosen(cur?.default ?? "");
    drawMore();
  }

  function drawChosen(current: string) {
    const opt = (cls: string) => styles.filter((s) => s.class === cls);
    const sel = h("select", { id: "cite-default", "aria-label": "Citation style" },
      ["note", "in-text"].map((cls) => h("optgroup", { label: styleKind(cls) },
        opt(cls).map((s) => h("option", { value: s.id, selected: s.id === current }, s.title)))));
    sel.addEventListener("change", async () => {
      const ok = await msg.attempt(() => run("cite.set_style", { style: sel.value }));
      if (ok !== undefined) msg.info("Saved. Notes and sermons that have not chosen their own style use it.");
    });
    clear(chosen,
      h("p", { class: "hint" }, "Footnotes at the foot of the page, or the author and year in the text. Each note or sermon can choose its own from its ⋯ menu."),
      h("div", { class: "field" }, h("label", { for: "cite-default" }, "Cite in this style"), sel));
  }

  function drawMore() {
    const added = styles.filter((s) => s.source === "added");
    const parts: (Node | null)[] = [
      toggleSwitch({
        label: "Download more styles", checked: online.enabled, hint: online.sends || STYLES_SENDS,
        onChange: async (on) => {
          const ok = await msg.attempt(() => run(on ? "cite.styles_enable" : "cite.styles_disable"));
          if (ok === undefined) return;
          online.enabled = on;
          found = null;
          drawMore();
        },
      }),
    ];
    if (online.enabled) parts.push(searchBox());
    if (added.length) {
      parts.push(h("h4", { class: "cite-added-title" }, "Styles you added"),
        h("ul", { class: "plain cite-added" }, added.map((s) => h("li", { class: "row between" },
          h("span", {}, s.title, " ", h("span", { class: "hint" }, styleKind(s.class))),
          button("Remove", async () => {
            const ok = await msg.attempt(() => run("cite.styles_remove", { id: s.id }));
            if (ok !== undefined) { await load(); drawMore(); }
          }, { "aria-label": `Remove ${s.title}` })))),
        h("p", { class: "hint" }, "Each style keeps its licence beside it, in the styles folder of your data folder. Styles come from the Citation Style Language project (CC BY-SA 3.0)."));
    }
    clear(more, parts);
  }

  function searchBox(): HTMLElement {
    const input = h("input", { type: "search", placeholder: "Search by name, e.g. chicago author-date or nature", value: query, "aria-label": "Search for a style",
      autocomplete: "off", spellcheck: "false" });
    const go = async () => {
      query = input.value.trim();
      if (!query) { found = null; drawMore(); return; }
      searching = true;
      drawMore();
      found = (await msg.attempt(() => run<Searched>("cite.styles_search", { query, limit: 30 }))) ?? null;
      searching = false;
      drawMore();
      more.querySelector<HTMLInputElement>('input[type="search"]')?.focus();
    };
    const form = h("form", { class: "row cite-search" }, input, h("button", { type: "submit", class: "primary" }, "Search"));
    form.addEventListener("submit", (e) => { e.preventDefault(); void go(); });
    const out = h("div", { class: "cite-found", "aria-live": "polite" });
    if (searching) out.append(h("p", { class: "hint" }, "Searching…"));
    else if (found) {
      const list = found.styles;
      out.append(list.length === 0 ? h("p", { class: "hint" }, "No style has those words in its name.")
        : h("ul", { class: "plain cite-found-list" }, list.map((s) => h("li", { class: "row between" },
          h("span", {}, s.name, s.dependent ? h("span", { class: "hint" }, " · follows another style") : null),
          s.have ? badge("Added", "ok") : button(busy === s.id ? "Downloading…" : "Download", async () => {
            busy = s.id;
            drawMore();
            const got = await msg.attempt(() => run<{ style: { title: string } }>("cite.styles_download", { id: s.id }));
            busy = "";
            if (got) { msg.info(`Added ${got.style.title}. Choose it above, or in a note's ⋯ menu.`); await load(); found = null; drawChosen((await run<StyleInfo>("cite.style", { id: "" })).default); }
            drawMore();
          }, { "aria-label": `Download ${s.name}`, disabled: !!busy })))),
        found.total > list.length ? h("p", { class: "hint" }, `Showing ${list.length} of ${found.total}. Add a word to narrow it.`) : "");
    }
    return h("div", { class: "cite-more-styles" }, form, out);
  }

  return { el, show, focus: () => el.querySelector<HTMLElement>("select, input, button")?.focus() };
}

