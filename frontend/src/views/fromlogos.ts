// Library → From Logos: the books in your Logos library, as a list with a link that opens each in
// Logos, and for each the best free copy Glossa can get: already in Glossa, on CCEL, on Project
// Gutenberg, as a Bible module, or none (with a link that searches archive.org in the browser).
// Glossa reads a copy of Logos's library catalogue (titles, authors, types, IDs and rights notes
// only); no book is opened and nothing in Logos changes. Matching happens on this computer against
// the lists each source offers; nothing about your Logos library is sent. Every action is a logos.*,
// ccel.*, gutenberg.* or bible.* command.

import { Message, pick, run } from "../api";
import { badge, button, clear, h, select } from "../dom";
import { chip, segmented, segmentedValue } from "../controls";
import { formatTime } from "../helpers";
import {
  FreeCopy, LogosBook, LogosLibrary, LogosRefresh, SORTS, STATE_FILTERS, SortKey, StateFilter, ariaSort, bulkLine, bulkPlan, countLine, freeText, nextSort, otherCopies, publicDomainText,
  refreshLine, stateLabel, subline, summaryLine,
} from "../fromlogoswords";
import { installFlow, size } from "./bibles";

const PAGE = 100;

// A book from CCEL or Project Gutenberg, as ccel.get and gutenberg.get describe it.
interface HostedBook { title: string; size: number; rights?: string; licence: string; credit: string; summary?: string }

type Hosted = "ccel" | "gutenberg";
const HOSTED: Record<Hosted, { name: string; long: string; get: string; settings: string; refresh: string; sends: string }> = {
  ccel: {
    name: "CCEL", long: "the Christian Classics Ethereal Library", get: "ccel.get", settings: "ccel.settings", refresh: "ccel.refresh",
    sends: "Glossa will ask ccel.org for its list of books (about 550 KB, at most once a week) and, for a book you choose, its rights note and file. ",
  },
  gutenberg: {
    name: "Project Gutenberg", long: "Project Gutenberg", get: "gutenberg.get", settings: "gutenberg.settings", refresh: "gutenberg.refresh",
    sends: "Glossa will ask gutenberg.org for its catalogue file (a few megabytes, at most once a week) and, for a book you choose, its file. ",
  },
};

export function fromLogosView() {
  const msg = new Message();
  const dialog = h("div");
  const search = h("input", { type: "search", id: "fromlogos-search", placeholder: "Search title, author or ID", "aria-label": "Search your Logos books" }) as HTMLInputElement;
  const type = select([["", "All types"]], "", { "aria-label": "Type of book", id: "fromlogos-type" });
  const pdChip = chip({ label: "Public domain only", checked: false });
  const stateSeg = segmented<StateFilter>({ label: "Show", options: STATE_FILTERS, value: "", small: true, onChange: () => { limit = PAGE; void load(); } });
  stateSeg.id = "fromlogos-state";
  const folder = h("input", { type: "text", id: "fromlogos-folder", placeholder: "The folder that holds Logos's Data and Documents folders", size: "48" }) as HTMLInputElement;
  const status = h("p", { class: "hint", role: "status", id: "fromlogos-status" });
  const count = h("p", { class: "hint", role: "status", id: "fromlogos-count" });
  const hintCatalogues = h("p", { class: "hint", id: "fromlogos-catalogues", hidden: true });
  const empty = h("div", { class: "card", id: "fromlogos-empty", hidden: true });
  const list = h("div", { class: "table-wrap" });
  const more = h("div", { class: "row" });
  const bulk = h("div", { class: "row wrap", id: "fromlogos-bulk" });
  const sources = h("div", { class: "row wrap", id: "fromlogos-sources" });

  let sort: SortKey = "title";
  let desc = false;
  let limit = PAGE;
  let timer = 0;
  let running = false; // a bulk download is under way
  const on: Record<Hosted, boolean> = { ccel: false, gutenberg: false };

  const refreshButton = button("Refresh from Logos", () => void refresh(), { id: "fromlogos-refresh" });
  const toolbar = h("div", { class: "row wrap" }, h("div", { class: "grow" }, search), type, pdChip, stateSeg, refreshButton);

  const el = h("div", { class: "view fromlogos-view" },
    h("div", { class: "work-head" },
      h("div", { class: "work-heading" }, h("h1", { class: "work-h1" }, "From Logos"),
        h("p", { class: "hint work-sub" }, "The books in your Logos library. Glossa reads a copy of Logos's catalogue: titles, authors, types and IDs. It never opens a book, and nothing in Logos changes. " +
          "Matching to free copies happens on this computer: nothing about your library is sent."))),
    empty, toolbar, sources,
    h("details", { class: "more-options" }, h("summary", {}, "Use another folder"),
      h("div", { class: "row wrap" }, folder, button("Choose…", () => void choose()))),
    status, hintCatalogues, count, bulk, msg.el, dialog, list, more);

  const folderParam = () => (folder.value.trim() ? { folder: folder.value.trim() } : {});

  search.addEventListener("input", () => { clearTimeout(timer); timer = window.setTimeout(() => { limit = PAGE; void load(); }, 250); });
  for (const c of [type, pdChip.querySelector("input")!]) c.addEventListener("change", () => { limit = PAGE; void load(); });

  async function choose() {
    const dir = await msg.attempt(() => pick("folder", "Choose the folder that holds Logos's Data and Documents folders"));
    if (dir) {
      folder.value = dir;
      await refresh();
    }
  }

  // refresh reads a fresh copy of Logos's catalogue and keeps the list; the free-copy lists that are on
  // are refreshed too, which sends a request only when one is more than a week old.
  async function refresh() {
    msg.info("Reading your Logos library…");
    refreshButton.disabled = true;
    const r = await msg.attempt(() => run<LogosRefresh>("logos.library_refresh", folderParam()));
    refreshButton.disabled = false;
    if (!r) return;
    await refreshSources();
    await load();
    msg.ok(refreshLine(r));
  }

  async function readSwitches(): Promise<void> {
    for (const k of Object.keys(on) as Hosted[]) on[k] = (await run<{ enabled: boolean }>(HOSTED[k].settings).catch(() => ({ enabled: false }))).enabled;
  }

  async function refreshSources() {
    await readSwitches();
    for (const k of Object.keys(on) as Hosted[]) if (on[k]) await run(HOSTED[k].refresh).catch(() => undefined);
  }

  async function load() {
    const params: Record<string, unknown> = { sort, desc, limit };
    if (search.value.trim()) params.text = search.value.trim();
    if (type.value) params.type = type.value;
    if (pdChip.querySelector("input")!.checked) params.public_domain = true;
    const st = segmentedValue(stateSeg);
    if (st) params.state = st;
    const r = await msg.attempt(() => run<LogosLibrary>("logos.library", params));
    if (!r) return;
    await readSwitches();
    show(r);
  }

  // showSources draws the row of switches for the sources that need the internet.
  function showSources() {
    clear(sources, h("span", { class: "hint" }, "Look for free copies on:"),
      ...(Object.keys(HOSTED) as Hosted[]).map((k) => {
        const c = chip({ label: HOSTED[k].name, checked: on[k], value: k });
        const input = c.querySelector("input")!;
        input.id = `fromlogos-source-${k}`;
        input.addEventListener("change", () => {
          if (input.checked) { input.checked = false; ask(k); } else void turnOff(k);
        });
        return c;
      }),
      h("span", { class: "hint" }, "These stay off until you turn them on. Bible modules use the lists in Get Bibles."));
  }

  function show(r: LogosLibrary) {
    const never = !r.refreshed && r.all === 0;
    empty.hidden = !never;
    clear(empty,
      h("h2", {}, "Read your Logos library"),
      h("p", {}, "Glossa will make a temporary copy of Logos's library catalogue, list the books in it and delete the copy. Only titles, authors, types, IDs and rights notes are read. No book is opened, and nothing in Logos is changed."),
      button("Read my Logos library", () => void refresh(), { class: "primary", id: "fromlogos-read" }));
    for (const x of [toolbar, sources, status, count, list, more, bulk]) x.hidden = never;
    if (never) return;
    showSources();

    // Keep the type picker's choice while its options follow the list.
    const want = type.value;
    type.replaceChildren(h("option", { value: "" }, "All types"), ...r.types.map((t) => h("option", { value: t.label }, `${t.label} (${t.count.toLocaleString()})`)));
    type.value = want;

    clear(status, summaryLine(r, r.refreshed_at ? formatTime(r.refreshed_at) : ""));
    count.textContent = countLine(r.books.length, r.total, r.all);
    const none = r.free_copies === 0 && r.already === 0;
    clear(hintCatalogues,
      none && (!on.ccel || !on.gutenberg) ? ["No free copies found yet. Turn on a source above, or turn on the Bible catalogues in ",
        h("a", { href: "#bibles" }, "Library → Get Bibles"), ". Nothing is downloaded until you say so."] : null);
    hintCatalogues.hidden = hintCatalogues.childNodes.length === 0;

    if (!running) clear(bulk, r.free_copies > r.already ? button("Get all free copies…", () => void planBulk(), { id: "fromlogos-bulk-start" }) : null);

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
    const cell = h("td", { class: "free-cell", "data-state": b.state ?? "" });
    const f = b.free;
    const hosted = f && (f.source === "ccel" || f.source === "gutenberg");
    if (b.state === "already") {
      cell.append(badge("Already in Glossa", "ok"));
      if (f?.installed) cell.append(" ", h("small", { class: "hint free-name" }, `${stateLabel(f)}: ${f.name}`));
      else if (b.in_library) cell.append(" ", h("small", { class: "hint free-name" }, b.in_library.name));
    } else if (b.state === "free" && f) {
      cell.append(badge(stateLabel(f), f.confidence === "certain" ? "ok" : "warn"), " ");
      if (f.confidence !== "certain") cell.append(badge("Possible match: check", "warn"), " ");
      cell.append(h("small", { class: "hint free-name" }, f.name), h("br"),
        button(hosted ? "Get free copy" : "Install", () => void getFree(b, cell), { class: "primary", "aria-label": `${hosted ? "Get free copy of" : "Install"} ${b.title}: ${freeText(f)}` }));
    } else {
      cell.append(badge("Not free", ""), " ");
      if (b.archive_link) cell.append(button("Search Internet Archive", () => openOutside(b.archive_link!), { class: "link", "aria-label": `Search Internet Archive for ${b.title}`, title: "Opens archive.org in your browser" }));
      if (b.public_domain) cell.append(h("br"), h("small", { class: "hint" }, publicDomainText(b)));
    }
    const others = otherCopies(b.others);
    if (others) cell.append(h("br"), h("small", { class: "hint other-copies" }, others));
    return h("tr", { "data-id": b.resource_id },
      h("td", {}, h("strong", {}, b.title), sub ? h("br") : null, sub ? h("small", { class: "hint" }, sub) : null),
      h("td", {}, b.authors || ""),
      h("td", {}, b.type_label),
      h("td", {}, b.published && b.published !== "n.d." ? b.published : ""),
      h("td", { class: "path" }, b.resource_id),
      cell,
      h("td", { class: "actions" },
        button("Open in Logos", () => openOutside(b.link, "Open in Logos works in the Glossa app."), { "aria-label": `Open ${b.title} in Logos` })));
  }

  // Links go through the browser runtime, which hands them to the system: Logos's own protocol starts
  // Logos, and an archive.org search opens the browser. Glossa sends nothing there itself.
  function openOutside(url: string, fallback = "Links open in the Glossa app.") {
    const rt = (window as unknown as { runtime?: { BrowserOpenURL?: (u: string) => void } }).runtime;
    if (rt?.BrowserOpenURL) rt.BrowserOpenURL(url);
    else msg.info(fallback);
  }

  // getFree asks before it downloads: the size and licence are shown first, as in Get Bibles.
  function getFree(b: LogosBook, cell: HTMLElement) {
    const f = b.free;
    if (!f) return;
    if (f.source === "ccel" || f.source === "gutenberg") { void getHosted(b, f, f.source, cell); return; }
    const how = f.confidence === "certain" ? "This is the same book" : "This looks like the same book, but check it";
    void installFlow({ source: f.source, id: f.item }, cell, {
      msg, dialog,
      intro: `${how} as “${b.title}” in your Logos library.`,
      done: () => void load().then(() => msg.ok(`Installed ${f.name}.`)),
      failed: load,
    });
  }

  // ask explains what turning a source on sends, and turns it on only when the user agrees.
  function ask(k: Hosted) {
    const s = HOSTED[k];
    const box = h("div", { class: "card", role: "dialog", "aria-label": `Use ${s.name} for free books` },
      h("h2", {}, `Use ${s.long}?`),
      h("p", {}, s.sends + "Nothing else is sent: no account, and nothing about your Logos library. You can turn it off here or in Settings → Privacy."),
      h("div", { class: "row" },
        button("Turn on", () => void turnOn(k), { class: "primary", id: `fromlogos-${k}-confirm` }),
        button("Cancel", () => { clear(dialog); showSources(); })));
    clear(dialog, box);
    box.querySelector<HTMLButtonElement>(".row button")?.focus();
  }

  async function turnOn(k: Hosted) {
    const s = HOSTED[k];
    clear(dialog);
    msg.info(`Asking ${s.name} for its list of books…`);
    if ((await msg.attempt(() => run(s.settings, { enable: true }))) === undefined) return;
    const r = await msg.attempt(() => run<{ works: number }>(s.refresh));
    await load();
    if (r) msg.ok(`${s.name} is on: ${r.works.toLocaleString()} ${r.works === 1 ? "book" : "books"} in its list.`);
  }

  async function turnOff(k: Hosted) {
    await msg.attempt(() => run(HOSTED[k].settings, { enable: false }));
    await load();
    msg.ok(`${HOSTED[k].name} is off. Nothing more will be sent to it.`);
  }

  // getHosted shows the size and licence first, as for the catalogues; the book is downloaded only
  // after the user confirms, and goes into the library as a public-domain book with a credit.
  async function getHosted(b: LogosBook, f: FreeCopy, k: Hosted, cell: HTMLElement) {
    const s = HOSTED[k];
    const pre = await msg.attempt(() => run<HostedBook>(s.get, { id: f.item, preview: true }));
    if (!pre) return;
    const how = f.confidence === "certain" ? "This is the same book" : "This looks like the same book, but check it";
    const box = h("div", { class: "card", role: "dialog", "aria-label": `Get ${pre.title}` },
      h("h2", {}, `Get ${pre.title}`),
      h("p", { class: "hint" }, `${how} as “${b.title}” in your Logos library.`),
      h("p", {}, `Download: ${size(pre.size)} (EPUB) from ${s.long}.`),
      h("p", {}, pre.rights ? `Licence: ${pre.licence}. CCEL gives its rights as “${pre.rights}”. ` : `Licence: ${pre.licence}. `,
        `It goes into your library as a public-domain book, credited to ${pre.credit}.`),
      h("div", { class: "row" },
        button("Add to my library", () => void go(), { class: "primary", id: `fromlogos-${k}-get` }),
        button("Cancel", () => clear(dialog))));
    clear(dialog, box);
    box.querySelector<HTMLButtonElement>(".row button")?.focus();

    async function go() {
      clear(dialog);
      clear(cell, h("progress", { "aria-label": `Getting ${pre!.title}` }), " ", h("small", { class: "hint", "aria-live": "polite" }, "Downloading…"));
      const r = await msg.attempt(() => run<HostedBook>(s.get, { id: f.item }));
      await load();
      if (r) msg.ok(r.summary || `Added ${pre!.title}.`);
    }
  }

  // planBulk lists what "Get all free copies" would download and asks once.
  async function planBulk() {
    const r = await msg.attempt(() => run<LogosLibrary>("logos.library", { state: "free", limit: 100000 }));
    if (!r) return;
    const plan = bulkPlan(r.books);
    const skipped = r.books.length - plan.length;
    if (plan.length === 0) {
      msg.info(skipped ? "Only possible matches are left. Check those one at a time." : "There is nothing new to get.");
      return;
    }
    const box = h("div", { class: "card", role: "dialog", "aria-label": "Get all free copies" },
      h("h2", {}, "Get all free copies?"),
      h("p", {}, bulkLine(plan, skipped)),
      h("p", { class: "hint" }, "They are downloaded one at a time, with the pause each source asks for, so a big list takes a while. You can stop at any point. " +
        "Each goes into your library with its licence and credit."),
      h("details", {}, h("summary", {}, "What will be downloaded"),
        h("ul", { class: "bulk-list" }, plan.map((b) => h("li", {}, `${b.title} (${b.free!.source === "ccel" || b.free!.source === "gutenberg" ? stateLabel(b.free!).replace("Free copy on ", "") : "Bible module"})`)))),
      h("div", { class: "row" },
        button(`Get ${plan.length.toLocaleString()} ${plan.length === 1 ? "book" : "books"}`, () => void runBulk(plan), { class: "primary", id: "fromlogos-bulk-go" }),
        button("Cancel", () => clear(dialog))));
    clear(dialog, box);
    box.querySelector<HTMLButtonElement>(".row button")?.focus();
  }

  async function runBulk(plan: LogosBook[]) {
    clear(dialog);
    running = true;
    let stop = false;
    const bar = h("progress", { max: plan.length, value: 0, "aria-label": "Getting free copies" });
    const line = h("span", { class: "hint", role: "status", "aria-live": "polite", id: "fromlogos-bulk-line" }, "Starting…");
    clear(bulk, bar, " ", line, " ", button("Stop", () => { stop = true; line.textContent = "Stopping after this book…"; }, { id: "fromlogos-bulk-stop" }));
    let added = 0;
    const failed: string[] = [];
    let i = 0;
    for (const b of plan) {
      if (stop) break;
      const f = b.free!;
      line.textContent = `Getting ${i + 1} of ${plan.length}: ${b.title}`;
      try {
        if (f.source === "ccel" || f.source === "gutenberg") await run(HOSTED[f.source].get, { id: f.item });
        else await run("bible.get", { source: f.source, item: f.item, accept_licence: true });
        added++;
      } catch {
        failed.push(b.title);
      }
      i++;
      bar.value = i;
    }
    running = false;
    await load();
    const parts = [`Added ${added} of ${plan.length} ${plan.length === 1 ? "book" : "books"}`];
    if (plan.length - i > 0) parts.push(`stopped with ${plan.length - i} left`);
    if (failed.length) parts.push(`${failed.length} could not be added (${failed.slice(0, 3).join(", ")}${failed.length > 3 ? ", …" : ""})`);
    if (failed.length) msg.info(parts.join("; ") + ".");
    else msg.ok(parts.join("; ") + ".");
  }

  return {
    el,
    // Opening the view refreshes the lists that are on: a request goes out only for one more than a week old.
    show: () => void refreshSources().then(load),
  };
}
