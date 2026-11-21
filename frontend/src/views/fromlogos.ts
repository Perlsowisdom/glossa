// Library → From Logos: the books in your Logos library, as a list with a link that opens each in
// Logos, and a free copy Glossa can get for the public-domain ones. Glossa reads a copy of Logos's
// library catalogue (titles, authors, types, IDs and rights notes only); no book is opened and
// nothing in Logos changes. Every action is a logos.* or bible.* command.

import { Message, pick, run } from "../api";
import { badge, button, clear, h, select } from "../dom";
import { chip } from "../controls";
import { formatTime } from "../helpers";
import {
  FreeCopy, LogosBook, LogosLibrary, LogosRefresh, SORTS, SortKey, ariaSort, countLine, freeText, nextSort, publicDomainText, refreshLine, subline, summaryLine,
} from "../fromlogoswords";
import { installFlow, size } from "./bibles";

const PAGE = 100;

interface CcelBook { title: string; size: number; rights: string; licence: string; credit: string; summary?: string }

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
  let ccelOn = true; // until known, do not offer to turn it on

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
    ccelOn = (await run<{ enabled: boolean }>("ccel.settings").catch(() => ({ enabled: true }))).enabled;
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
    hintCatalogues.hidden = !pdNoFree && ccelOn;
    clear(hintCatalogues,
      pdNoFree ? ["To see free copies of the public-domain books, turn on the Bible catalogues in ",
        h("a", { href: "#bibles" }, "Library → Get Bibles"), ". Glossa then compares titles; nothing is downloaded until you say so. "] : null,
      ccelOn || r.public_domain === 0 ? null : [
        "Free books are also available from the Christian Classics Ethereal Library (ccel.org). ",
        button("Use CCEL…", () => askCcel(), { id: "fromlogos-ccel-on" })]);
    hintCatalogues.hidden = hintCatalogues.childNodes.length === 0;

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
        h("small", { class: "hint free-name" }, f.source === "ccel" ? `CCEL: ${f.name}` : f.name), h("br"),
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
    if (f.source === "ccel") { void getCcel(b, f, cell); return; }
    const how = f.confidence === "certain" ? "This is the same book" : "This looks like the same book, but check it";
    void installFlow({ source: f.source, id: f.item }, cell, {
      msg, dialog,
      intro: `${how} as “${b.title}” in your Logos library.`,
      done: () => void load().then(() => msg.ok(`Installed ${f.name}.`)),
      failed: load,
    });
  }

  // askCcel explains what turning CCEL on sends, and turns it on only when the user agrees.
  function askCcel() {
    const box = h("div", { class: "card", role: "dialog", "aria-label": "Use CCEL for free books" },
      h("h2", {}, "Use the Christian Classics Ethereal Library?"),
      h("p", {}, "Glossa will ask ccel.org for its list of books (about 550 KB, at most once a week) and, for a book you choose, its rights note and file. " +
        "Nothing else is sent: no account, and nothing about your Logos library. You can turn it off in Settings → Privacy."),
      h("div", { class: "row" },
        button("Turn on", () => void turnOn(), { class: "primary", id: "fromlogos-ccel-confirm" }),
        button("Cancel", () => clear(dialog))));
    clear(dialog, box);
    box.querySelector<HTMLButtonElement>(".row button")?.focus();
  }

  async function turnOn() {
    clear(dialog);
    msg.info("Asking ccel.org for its list of books…");
    if ((await msg.attempt(() => run("ccel.settings", { enable: true }))) === undefined) return;
    const r = await msg.attempt(() => run<{ works: number }>("ccel.refresh"));
    await load();
    if (r) msg.ok(`CCEL is on: ${r.works.toLocaleString()} books in its list.`);
  }

  // getCcel shows the size and licence first, as for the catalogues; the book is downloaded only
  // after the user confirms, and goes into the library as a public-domain book with a credit to CCEL.
  async function getCcel(b: LogosBook, f: FreeCopy, cell: HTMLElement) {
    const pre = await msg.attempt(() => run<CcelBook>("ccel.get", { id: f.item, preview: true }));
    if (!pre) return;
    const how = f.confidence === "certain" ? "This is the same book" : "This looks like the same book, but check it";
    const box = h("div", { class: "card", role: "dialog", "aria-label": `Get ${pre.title}` },
      h("h2", {}, `Get ${pre.title}`),
      h("p", { class: "hint" }, `${how} as “${b.title}” in your Logos library.`),
      h("p", {}, `Download: ${size(pre.size)} (EPUB) from the Christian Classics Ethereal Library.`),
      h("p", {}, `Licence: ${pre.licence}. CCEL gives its rights as “${pre.rights}”. It goes into your library as a public-domain book, credited to ${pre.credit}.`),
      h("div", { class: "row" },
        button("Add to my library", () => void go(), { class: "primary", id: "fromlogos-ccel-get" }),
        button("Cancel", () => clear(dialog))));
    clear(dialog, box);
    box.querySelector<HTMLButtonElement>(".row button")?.focus();

    async function go() {
      clear(dialog);
      clear(cell, h("progress", { "aria-label": `Getting ${pre!.title}` }), " ", h("small", { class: "hint", "aria-live": "polite" }, "Downloading…"));
      const r = await msg.attempt(() => run<CcelBook>("ccel.get", { id: f.item }));
      await load();
      if (r) msg.ok(r.summary || `Added ${pre!.title}.`);
    }
  }

  return { el, show: () => void load() };
}
