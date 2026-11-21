// Library → From Logos: the books in your Logos library, as a list with a link that opens each in
// Logos, and a free copy Glossa can get for the public-domain ones. Glossa reads a copy of Logos's
// library catalogue (titles, authors, types, IDs and rights notes only); no book is opened and
// nothing in Logos changes. Every action is a logos.* or bible.* command.

import { Message, pick, run } from "../api";
import { badge, button, clear, h, select } from "../dom";
import { chip } from "../controls";
import { formatTime } from "../helpers";
import {
  LogosBook, LogosLibrary, LogosRefresh, SORTS, SortKey, ariaSort, countLine, freeText, nextSort, publicDomainText, refreshLine, subline, summaryLine,
} from "../fromlogoswords";
import { installFlow } from "./bibles";

const PAGE = 100;

export function fromLogosView() {
  const msg = new Message();
  const dialog = h("div");
  const search = h("input", { type: "search", id: "fromlogos-search", placeholder: "Search title, author or ID", "aria-label": "Search your Logos books" }) as HTMLInputElement;
  const type = select([["", "All types"]], "", { "aria-label": "Type of book", id: "fromlogos-type" });
  const pdChip = chip({ label: "Public domain only", checked: false });
  const freeChip = chip({ label: "With a free copy", checked: false });
  const folder = h("input", { type: "text", id: "fromlogos-folder", placeholder: "The folder that holds Logos's Data and Documents folders", size: "48" }) as HTMLInputElement;
  const status = h("p", { class: "hint", role: "status", id: "fromlogos-status" });
  const count = h("p", { class: "hint", role: "status", id: "fromlogos-count" });
  const hintCatalogues = h("p", { class: "hint", id: "fromlogos-catalogues", hidden: true });
  const empty = h("div", { class: "card", id: "fromlogos-empty", hidden: true });
  const list = h("div", { class: "table-wrap" });
  const more = h("div", { class: "row" });

  let sort: SortKey = "title";
  let desc = false;
  let limit = PAGE;
  let timer = 0;

  const refreshButton = button("Refresh from Logos", () => void refresh(), { id: "fromlogos-refresh" });
  const toolbar = h("div", { class: "row wrap" }, h("div", { class: "grow" }, search), type, pdChip, freeChip, refreshButton);

  const el = h("div", { class: "view fromlogos-view" },
    h("div", { class: "work-head" },
      h("div", { class: "work-heading" }, h("h1", { class: "work-h1" }, "From Logos"),
        h("p", { class: "hint work-sub" }, "The books in your Logos library. Glossa reads a copy of Logos's catalogue: titles, authors, types and IDs. It never opens a book, and nothing in Logos changes."))),
    empty, toolbar,
    h("details", { class: "more-options" }, h("summary", {}, "Use another folder"),
      h("div", { class: "row wrap" }, folder, button("Choose…", () => void choose()))),
    status, hintCatalogues, count, msg.el, dialog, list, more);

  const folderParam = () => (folder.value.trim() ? { folder: folder.value.trim() } : {});

  search.addEventListener("input", () => { clearTimeout(timer); timer = window.setTimeout(() => { limit = PAGE; void load(); }, 250); });
  for (const c of [type, pdChip.querySelector("input")!, freeChip.querySelector("input")!]) c.addEventListener("change", () => { limit = PAGE; void load(); });

  async function choose() {
    const dir = await msg.attempt(() => pick("folder", "Choose the folder that holds Logos's Data and Documents folders"));
    if (dir) {
      folder.value = dir;
      await refresh();
    }
  }

  // refresh reads a fresh copy of Logos's catalogue and keeps the list.
  async function refresh() {
    msg.info("Reading your Logos library…");
    refreshButton.disabled = true;
    const r = await msg.attempt(() => run<LogosRefresh>("logos.library_refresh", folderParam()));
    refreshButton.disabled = false;
    if (!r) return;
    await load();
    msg.ok(refreshLine(r));
  }

  async function load() {
    const params: Record<string, unknown> = { sort, desc, limit };
    if (search.value.trim()) params.text = search.value.trim();
    if (type.value) params.type = type.value;
    if (pdChip.querySelector("input")!.checked) params.public_domain = true;
    if (freeChip.querySelector("input")!.checked) params.free_copy = true;
    const r = await msg.attempt(() => run<LogosLibrary>("logos.library", params));
    if (!r) return;
    show(r);
  }

  function show(r: LogosLibrary) {
    const never = !r.refreshed && r.all === 0;
    empty.hidden = !never;
    clear(empty,
      h("h2", {}, "Read your Logos library"),
      h("p", {}, "Glossa will make a temporary copy of Logos's library catalogue, list the books in it and delete the copy. Only titles, authors, types, IDs and rights notes are read. No book is opened, and nothing in Logos is changed."),
      button("Read my Logos library", () => void refresh(), { class: "primary", id: "fromlogos-read" }));
    for (const x of [toolbar, status, count, list, more]) x.hidden = never;
    if (never) return;

    // Keep the type picker's choice while its options follow the list.
    const want = type.value;
    type.replaceChildren(h("option", { value: "" }, "All types"), ...r.types.map((t) => h("option", { value: t.label }, `${t.label} (${t.count.toLocaleString()})`)));
    type.value = want;

    clear(status, summaryLine(r, r.refreshed_at ? formatTime(r.refreshed_at) : ""));
    count.textContent = countLine(r.books.length, r.total, r.all);
    const pdNoFree = r.public_domain > 0 && r.certain + r.likely === 0;
    hintCatalogues.hidden = !pdNoFree;
    clear(hintCatalogues, "To see free copies of the public-domain books, turn on the Bible catalogues in ",
      h("a", { href: "#bibles" }, "Library → Get Bibles"), ". Glossa then compares titles; nothing is downloaded until you say so.");

    clear(list, r.books.length === 0 ? h("p", { class: "empty" }, r.all ? "No books match." : "The list is empty.") :
      h("table", { class: "list fromlogos-table" },
        h("thead", {}, h("tr", {},
          ...SORTS.map((s) => header(s.key, s.label)),
          h("th", { scope: "col" }, "Logos ID"),
          h("th", { scope: "col" }, "Free copy"),
          h("th", { scope: "col" }, h("span", { class: "sr-only" }, "Open")))),
        h("tbody", {}, r.books.map(row))));
    clear(more, r.books.length < r.total ?
      button(`Show ${Math.min(PAGE, r.total - r.books.length).toLocaleString()} more`, () => { limit += PAGE; void load(); }, { id: "fromlogos-more" }) : null);
  }

  function header(key: SortKey, label: string) {
    const th = h("th", { scope: "col", "aria-sort": ariaSort(key, sort, desc) });
    th.append(button(label + (key === sort ? (desc ? " ▼" : " ▲") : ""), () => {
      ({ sort, desc } = nextSort(key, sort, desc));
      limit = PAGE;
      void load();
    }, { class: "link th-sort", "data-sort": key }));
    return th;
  }

  function row(b: LogosBook) {
    const sub = subline(b);
    const cell = h("td", { class: "free-cell" });
    if (b.free) {
      const f = b.free;
      cell.append(badge(f.confidence === "certain" ? "Certain" : "Likely", f.confidence === "certain" ? "ok" : "warn"), " ",
        h("small", { class: "hint" }, f.name), h("br"),
        f.installed ? badge("Already in Glossa", "ok") :
          button("Get free copy", () => void getFree(b, cell), { class: "primary", "aria-label": `Get free copy of ${b.title}: ${freeText(f)}` }));
    } else if (b.public_domain) {
      cell.append(badge(publicDomainText(b), ""), " ", h("small", { class: "hint" }, "No free copy known"));
    }
    return h("tr", { "data-id": b.resource_id },
      h("td", {}, h("strong", {}, b.title), sub ? h("br") : null, sub ? h("small", { class: "hint" }, sub) : null),
      h("td", {}, b.authors || ""),
      h("td", {}, b.type_label),
      h("td", {}, b.published && b.published !== "n.d." ? b.published : ""),
      h("td", { class: "path" }, b.resource_id),
      cell,
      h("td", { class: "actions" },
        button("Open in Logos", () => openInLogos(b), { "aria-label": `Open ${b.title} in Logos` })));
  }

  // The link is Logos's own protocol; the browser runtime hands it to the system, which starts Logos.
  function openInLogos(b: LogosBook) {
    const rt = (window as unknown as { runtime?: { BrowserOpenURL?: (u: string) => void } }).runtime;
    if (rt?.BrowserOpenURL) rt.BrowserOpenURL(b.link);
    else msg.info("Open in Logos works in the Glossa app.");
  }

  // getFree asks before it downloads: the size and licence are shown first, as in Get Bibles.
  function getFree(b: LogosBook, cell: HTMLElement) {
    const f = b.free;
    if (!f) return;
    const how = f.confidence === "certain" ? "This is the same book" : "This looks like the same book, but check it";
    void installFlow({ source: f.source, id: f.item }, cell, {
      msg, dialog,
      intro: `${how} as “${b.title}” in your Logos library.`,
      done: () => void load().then(() => msg.ok(`Installed ${f.name}.`)),
      failed: load,
    });
  }

  return { el, show: () => void load() };
}
