// Citing from the writing pages: the picker behind Insert → Citation, /cite and a click on a citation
// chip; the short forms and bibliography the editor draws; and the citation style menu in a note's or
// sermon's ⋯ menu. The work is the app's (cite.* commands, internal/cite); this only asks and draws.

import { run } from "./api";
import { button, clear, h } from "./dom";
import type { CiteData } from "./editor/citeformat";
import { CITE_LABELS, isBibleKey } from "./editor/citeformat";
import type { CiteHost, CiteRun } from "./editor/citenodes";
import { citeWords } from "./citewords";
import { closePopover, openPopover } from "./popover";

export interface CiteSource {
  key: string;
  kind: "library" | "bible" | "zotero" | "manual";
  title: string;
  detail?: string;
  kept?: boolean;
}

interface SearchResult {
  sources: CiteSource[];
  notes?: string[];
}

interface StyleInfo {
  style: string;
  title?: string;
  own: boolean;
  default: string;
}

interface Rendered {
  text: string;
  runs: { text: string; italic?: boolean; bold?: boolean }[];
}

// citeHost is what a writing page hands its editor. docId says which note or sermon it is (its own
// style applies); it is empty for a note not made yet, which uses the default.
export function citeHost(o: { docId(): string | undefined }): CiteHost {
  const style = async () => (await run<StyleInfo>("cite.style", { id: o.docId() ?? "" }).catch(() => null))?.style ?? "";
  return {
    pick: (anchor, existing, done) => void citePicker(anchor, existing, done),
    async labels(cites) {
      const r = await run<{ labels: string[] }>("cite.short", { style: await style(), cites: cites.map(wire) });
      return r.labels;
    },
    async bibliography(cites) {
      const r = await run<{ style: { class: string }; bibliography: Rendered[] }>("cite.render", { style: await style(), cites: cites.map(wire) });
      const entries: CiteRun[][] = r.bibliography.map((b) => b.runs);
      return { title: r.style.class === "note" ? "Bibliography" : "References", entries };
    },
  };
}

// wire is a citation as the cite.* commands take it.
function wire(c: CiteData) {
  return { key: c.key, label: c.label, locator: c.locator, prefix: c.prefix, suffix: c.suffix, suppress_author: c.suppress };
}

// The picker ---------------------------------------------------------------------------------------

const KIND_WORDS: Record<CiteSource["kind"], string> = { library: "Library", bible: "Bible", zotero: "Zotero", manual: "By hand" };
const KIND_GLYPH: Record<CiteSource["kind"], string> = { library: "▤", bible: "✦", zotero: "Z", manual: "✎" };

async function citePicker(anchor: HTMLElement, existing: CiteData | null, done: (c: CiteData | null) => void): Promise<void> {
  const root = h("div", { class: "pop-body pick-pop cite-pop" });
  openPopover(anchor, root, { label: existing ? "Change this citation" : "Cite a source", className: "doc-pop" });
  const close = () => closePopover(true);

  const finish = async (c: CiteData, say: (w: string) => void) => {
    try {
      await run("cite.keep", { key: c.key });
    } catch (e) {
      say(`Could not keep this source: ${(e as Error).message}`);
      return;
    }
    close();
    done(c);
  };

  // Search ------------------------------------------------------------------------------------------
  function search(first = "") {
    const input = h("input", { type: "search", placeholder: "Search your books, Bibles and Zotero", autocomplete: "off", spellcheck: "false",
      "aria-label": "Search for a source", value: first, role: "combobox", "aria-expanded": "true", "aria-autocomplete": "list" });
    const list = h("ul", { class: "pick-list", role: "listbox", "aria-label": "Sources", id: `cite-src-${Math.random().toString(36).slice(2)}` });
    input.setAttribute("aria-controls", list.id);
    const notes = h("p", { class: "ref-note", role: "status" });
    let items: CiteSource[] = [];
    let at = 0;
    let seq = 0;
    let timer = 0;
    const mark = () => {
      [...list.children].forEach((c, i) => c.setAttribute("aria-selected", String(i === at)));
      const cur = list.children[at] as HTMLElement | undefined;
      if (cur) input.setAttribute("aria-activedescendant", cur.id);
      cur?.scrollIntoView({ block: "nearest" });
    };
    const draw = () => {
      if (items.length === 0) {
        clear(list, h("li", { class: "pick-empty", role: "presentation" }, "Nothing found. You can add a source by hand."));
        return;
      }
      clear(list, items.map((s, i) => {
        const li = h("li", { role: "option", class: "pick-row cite-row", id: `${list.id}-${i}`, "aria-selected": String(i === at) },
          h("span", { class: `pick-kind cite-kind k-${s.kind}`, "aria-hidden": "true" }, KIND_GLYPH[s.kind]),
          h("span", { class: "pick-main" }, h("span", { class: "pick-text" }, s.title || "(untitled)"),
            h("span", { class: "pick-where" }, [s.detail, KIND_WORDS[s.kind]].filter(Boolean).join(" · "))));
        li.addEventListener("mousedown", (e) => e.preventDefault());
        li.addEventListener("click", () => details(s));
        li.addEventListener("mousemove", () => { if (at !== i) { at = i; mark(); } });
        return li;
      }));
      mark();
    };
    const refresh = async () => {
      const n = ++seq;
      const got = await run<SearchResult>("cite.sources", { query: input.value.trim(), limit: 12 }).catch(() => ({ sources: [] as CiteSource[], notes: [] as string[] }));
      if (n !== seq) return;
      items = got.sources;
      at = 0;
      notes.textContent = got.notes?.[0] ?? "";
      draw();
    };
    input.addEventListener("input", () => {
      clearTimeout(timer);
      timer = window.setTimeout(() => void refresh(), 180);
    });
    input.addEventListener("keydown", (e) => {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        if (items.length) { at = (at + (e.key === "ArrowDown" ? 1 : items.length - 1)) % items.length; mark(); }
      } else if (e.key === "Enter") {
        e.preventDefault();
        if (items[at]) details(items[at]);
      }
    });
    clear(root,
      h("p", { class: "pop-title" }, "Cite a source"), input, notes, list,
      h("div", { class: "cite-foot" }, button("Add a source by hand…", () => manual(), { class: "link small" })));
    input.focus();
    void refresh();
  }

  // Where in the source ----------------------------------------------------------------------------
  async function details(src: CiteSource | null, gone = "") {
    const key = src?.key ?? existing!.key;
    const info = src ?? (await run<CiteSource>("cite.source", { key }).catch(() => null)) ?? { key, kind: "library" as const, title: "Source" };
    const bible = isBibleKey(key);
    const same = existing && existing.key === key ? existing : null;
    const label = h("select", { "aria-label": "What the number is", class: "cite-label" },
      CITE_LABELS.map(([v, w]) => h("option", { value: v, selected: v === (same?.label ?? "page") }, w)));
    const loc = h("input", { type: "text", autocomplete: "off", spellcheck: "false", value: same?.locator ?? "",
      placeholder: bible ? "e.g. Rom 8:28-30" : "e.g. 12", "aria-label": bible ? "Passage" : "Page or place" });
    const note = h("p", { class: "ref-note", "aria-live": "polite" });
    const prefix = h("input", { type: "text", autocomplete: "off", value: same?.prefix ?? "", placeholder: "e.g. see", "aria-label": "Words before" });
    const suffix = h("input", { type: "text", autocomplete: "off", value: same?.suffix ?? "", placeholder: "e.g. for more", "aria-label": "Words after" });
    const suppress = h("input", { type: "checkbox", checked: !!same?.suppress, id: "cite-suppress" });
    const why = h("p", { class: "ref-note bad", role: "alert" }, gone);
    let passage = "";
    let seq = 0;
    let timer = 0;
    const check = async () => {
      const n = ++seq;
      const v = loc.value.trim();
      passage = "";
      if (!bible || !v) { note.textContent = ""; note.className = "ref-note"; return; }
      const p = await run<{ ok: boolean; passage?: string }>("cite.passage", { text: v }).catch(() => null);
      if (n !== seq) return;
      passage = p?.ok ? p.passage ?? "" : "";
      note.textContent = passage ? `Cited as ${passage}` : "Not a passage yet";
      note.className = `ref-note ${passage ? "ok" : "bad"}`;
    };
    loc.addEventListener("input", () => { clearTimeout(timer); timer = window.setTimeout(() => void check(), 160); });
    const form = h("form", { class: "cite-form" },
      h("p", { class: "pop-title" }, existing ? "Change this citation" : "Cite a source"),
      h("div", { class: "cite-chosen" },
        h("span", { class: `pick-kind cite-kind k-${info.kind}`, "aria-hidden": "true" }, KIND_GLYPH[info.kind]),
        h("span", { class: "pick-main" }, h("span", { class: "pick-text" }, info.title || "(untitled)"),
          h("span", { class: "pick-where" }, [info.detail, KIND_WORDS[info.kind]].filter(Boolean).join(" · "))),
        button("Change", () => search(), { class: "link small" })),
      h("div", { class: "cite-where" }, bible ? null : label, loc), note,
      h("details", { class: "cite-more", open: !!(same?.prefix || same?.suffix || same?.suppress) },
        h("summary", {}, "More options"),
        h("label", {}, "Words before", prefix), h("label", {}, "Words after", suffix),
        h("label", { class: "cite-check" }, suppress, "Leave out the author’s name (the sentence names them)")),
      why,
      h("div", { class: "row cite-actions" },
        h("button", { type: "submit", class: "primary" }, existing ? "Save" : "Insert citation"),
        existing ? button("Remove", () => { close(); done(null); }, { class: "link small danger-link" }) : null));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      clearTimeout(timer);
      await check();
      if (bible && loc.value.trim() && !passage) {
        why.textContent = "Type a passage such as Rom 8:28, or leave it empty.";
        loc.focus();
        return;
      }
      const c: CiteData = { key };
      if (loc.value.trim()) c.locator = bible ? passage : loc.value.trim();
      if (!bible && c.locator && label.value !== "page") c.label = label.value;
      if (!bible && c.locator && label.value === "page") c.label = "page";
      if (prefix.value.trim()) c.prefix = prefix.value.trim();
      if (suffix.value.trim()) c.suffix = suffix.value.trim();
      if (suppress.checked) c.suppress = true;
      await finish(c, (w) => (why.textContent = w));
    });
    clear(root, form);
    loc.focus();
    if (bible && loc.value) void check();
  }

  // A source added by hand --------------------------------------------------------------------------
  function manual() {
    const text = (label: string, ph = "", value = "") => h("input", { type: "text", autocomplete: "off", spellcheck: "false", placeholder: ph, value, "aria-label": label });
    const type = h("select", { "aria-label": "What it is" }, citeWords.manualTypes.map(([v, w]) => h("option", { value: v }, w)));
    const title = text("Title", "Title");
    const authors = text("Authors", "Authors, separated by ; e.g. Moo, Douglas J.; Wright, N. T.");
    const year = text("Year", "Year");
    const publisher = text("Publisher", "Publisher");
    const place = text("Place", "Place");
    const container = text("In", "The journal or the book it is in");
    const vol = text("Volume", "Volume");
    const issue = text("Issue", "Issue");
    const pages = text("Pages", "Pages, e.g. 45-67");
    const url = text("Web address", "Web address");
    const why = h("p", { class: "ref-note bad", role: "alert" });
    const form = h("form", { class: "cite-form" },
      h("p", { class: "pop-title" }, "Add a source by hand"),
      type, title, authors, h("div", { class: "row cite-two" }, year, publisher),
      h("details", { class: "cite-more" }, h("summary", {}, "More options"),
        place, container, h("div", { class: "row cite-two" }, vol, issue), pages, url),
      why,
      h("div", { class: "row cite-actions" }, h("button", { type: "submit", class: "primary" }, "Add and cite"),
        button("Back", () => search(), { class: "link small" })));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      if (!title.value.trim()) { why.textContent = "Give the source a title."; title.focus(); return; }
      const y = Number(year.value.trim());
      if (year.value.trim() && !(y > 0 && y < 3000)) { why.textContent = "The year should be a number such as 1996."; year.focus(); return; }
      try {
        const s = await run<CiteSource>("cite.manual_save", {
          type: type.value, title: title.value.trim(), authors: authors.value.split(";").map((a) => a.trim()).filter(Boolean),
          ...(y ? { year: y } : {}), publisher: publisher.value.trim(), place: place.value.trim(), container: container.value.trim(),
          volume: vol.value.trim(), issue: issue.value.trim(), pages: pages.value.trim(), url: url.value.trim(),
        });
        await details(s);
      } catch (err) {
        why.textContent = (err as Error).message;
      }
    });
    clear(root, form);
    title.focus();
  }

  if (existing) {
    const src = await run<{ gone?: string } & CiteSource>("cite.source", { key: existing.key }).catch(() => null);
    await details(src && !src.gone ? src : null, src?.gone ? `This source is missing: ${src.gone}.` : "");
  } else search();
}

// The citation style of a note or sermon -------------------------------------------------------------

interface StyleEntry { id: string; title: string; class: string; source: string }

// citeStyleMenu is the ⋯ menu's "Citation style": the styles to choose from, the one in use ticked, and
// a way back to the default from Settings. changed is called once a style was chosen.
export async function citeStyleMenu(anchor: HTMLElement, docId: string, changed: () => void, say: (why: string) => void): Promise<void> {
  let cur: StyleInfo;
  let styles: StyleEntry[];
  try {
    [cur, styles] = await Promise.all([run<StyleInfo>("cite.style", { id: docId }), run<{ styles: StyleEntry[] }>("csl.styles").then((r) => r.styles)]);
  } catch (e) {
    say((e as Error).message);
    return;
  }
  const defaultName = styles.find((s) => s.id === cur.default)?.title ?? cur.default;
  const choose = (style: string) => async () => {
    closePopover(true);
    try {
      await run("cite.set_style", { id: docId, style });
      changed();
    } catch (e) {
      say((e as Error).message);
    }
  };
  const group = (title: string, class_: string) => {
    const mine = styles.filter((s) => s.class === class_);
    return mine.length === 0 ? null : [h("p", { class: "pop-title" }, title),
      ...mine.map((s) => button(s.title, choose(s.id), { class: cur.own && s.id === cur.style ? "on" : "" }))];
  };
  openPopover(anchor, h("div", { class: "menu-list cite-style-menu" },
    h("p", { class: "pop-title" }, "Citation style"),
    button(`Use the default: ${defaultName}`, choose(""), { class: !cur.own ? "on" : "" }),
    group("Footnotes", "note"), group("Author and date, or numbers", "in-text"),
  ), { label: "Citation style", menu: true });
}
