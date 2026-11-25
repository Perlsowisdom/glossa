// Reader: a slim top bar (Read | Study, search in what is open, reading settings, grid presets, a ⋯
// menu and the sidebar toggle), today's reading as one line, then the study grid
// (document/studygrid.ts). Study mode shows the whole grid; Read mode shows the selected pane's
// document alone, full width, and switching keeps your place because the same view stays mounted.

import { Message } from "../api";
import { segmented, settingRow } from "../controls";
import { button, h } from "../dom";
import { studyGrid } from "../document/studygrid";
import { setOpener } from "../document/open";
import { openGridPresets, setGridPresets } from "../gridhook";
import { icon } from "../icons";
import { todayCard } from "../plancard";
import { togglePopover } from "../popover";
import { bookZoom, currentLayout, setBookZoom, setLayout } from "../booklayout";
import { ReaderMode, ReadingPrefs, SPACINGS, TEXT_SIZES, gridButtonPlace, stepSize } from "../shellwords";
import { navigate, onPrefs, prefs, setPrefs, state } from "../state";
import { openReport } from "./report";
import { sidebar } from "./sidebar";

function iconButton(name: string, label: string, onclick: (e: Event) => void, attrs: Record<string, unknown> = {}): HTMLButtonElement {
  return h("button", { type: "button", class: "icon-btn", "aria-label": label, title: label, ...attrs, onclick }, icon(name));
}

const NOTHING_TO_PRINT = "Select a pane with a passage, book or note to print.";

export function readerView() {
  const msg = new Message();
  const side = sidebar();
  const today = todayCard();
  // The selected pane's reader controls live in the top bar, after the search and Aa.
  const controls = h("div", { class: "reader-controls", role: "group", "aria-label": "Reader controls for the selected pane", hidden: true });
  const grid = studyGrid({
    mode: () => prefs.reading.mode,
    defaultTab: () => ({ kind: "bible" }),
    controls,
  });
  setGridPresets((anchor) => grid.presets(anchor));
  setOpener((spec) => grid.open(spec));

  // Read | Study ---------------------------------------------------------------------------------
  const modes: ReaderMode[] = ["read", "study"];
  const modeBtns = modes.map((m) => {
    const b = h("button", { type: "button", "aria-pressed": "false" }, m === "read" ? "Read" : "Study");
    b.addEventListener("click", () => setMode(m));
    return b;
  });
  const modeSwitch = h("div", { class: "segmented", role: "group", "aria-label": "Mode" }, modeBtns);
  function setMode(m: ReaderMode) {
    if (prefs.reading.mode !== m) setPrefs({ reading: { ...prefs.reading, mode: m } });
  }

  // Search in what is open: the selected pane's Bibles or book ------------------------------------
  const findInput = h("input", { type: "search", "aria-label": "Search in what is open", placeholder: "Search in what is open", autocomplete: "off" });
  const findForm = h("form", { class: "find", role: "search", hidden: true }, findInput);
  const findBtn = iconButton("search", "Search in what is open", () => {
    findForm.hidden = !findForm.hidden;
    findBtn.setAttribute("aria-expanded", String(!findForm.hidden));
    if (!findForm.hidden) findInput.focus();
  }, { "aria-expanded": "false" });
  findInput.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !findInput.value) {
      findForm.hidden = true;
      findBtn.setAttribute("aria-expanded", "false");
      findBtn.focus();
    }
  });
  findForm.addEventListener("submit", (e) => {
    e.preventDefault();
    const q = findInput.value.trim();
    const v = grid.selectedView();
    if (!q) return;
    if (v?.menu?.find) void v.menu.find(q, findInput);
    else msg.info("The selected pane has nothing to search. Select a pane with a Bible or a book.");
  });

  // Reading settings, grid and the ⋯ menu ---------------------------------------------------------
  const aaBtn = h("button", { type: "button", class: "aa-btn", "aria-haspopup": "dialog", "aria-expanded": "false", "aria-label": "Reading settings", title: "Reading settings" }, "Aa");
  aaBtn.addEventListener("click", () => togglePopover(aaBtn, () => readingControls(), { label: "Reading settings", side: "below" }));

  const gridBtn: HTMLButtonElement = iconButton("grid", "Grid presets", () => openGridPresets(gridBtn), { "aria-haspopup": "dialog", "aria-expanded": "false" });

  const moreBtn: HTMLButtonElement = iconButton("more", "More", () => togglePopover(moreBtn, () => h("div", { class: "menu-list" },
    button("Passage report", () => state.parsed && openReport("passage", state.parsed.osis),
      { title: "Everything your library holds about this passage, in one place" }),
    button("Notes on this passage", () => side.notesHere()),
    printItem(),
    button("Reading plans", () => navigate("plans"))), { label: "More", side: "below", menu: true }),
  { "aria-haspopup": "menu", "aria-expanded": "false" });

  // Print or save in ⋯ is there but off, with the reason, when the selected pane has nothing to print.
  function printItem(): HTMLElement {
    const can = !!grid.selectedView()?.menu?.print;
    const b = button("Print or save…", () => print(), { title: can ? "Print, or save as PDF, Word, a web page or Markdown" : NOTHING_TO_PRINT });
    if (!can) {
      b.disabled = true;
      return h("div", {}, b, h("p", { class: "hint menu-hint" }, NOTHING_TO_PRINT));
    }
    return b;
  }
  // print is Print or save for the selected pane (also Ctrl+P); with nothing to print it says so.
  function print() {
    const p = grid.selectedView()?.menu?.print;
    if (p) p();
    else msg.info(NOTHING_TO_PRINT);
  }

  const sideBtn = iconButton("panel", "Right sidebar", () => side.toggle(), { "aria-controls": "rightbar", title: "Right sidebar (Ctrl+.)", "aria-keyshortcuts": "Control+." });

  const bar = h("div", { class: "topbar", role: "group", "aria-label": "Reader" },
    modeSwitch, findBtn, findForm, aaBtn, controls, gridBtn, h("span", { class: "spacer" }), moreBtn, sideBtn);

  const el = h("div", { class: "view reader" },
    h("h1", { class: "sr-only" }, "Reader"),
    bar, today.el, msg.el, grid.el);

  // applyPrefs shows the mode, the reading settings and where the grid button goes.
  let lastMode = prefs.reading.mode;
  function applyPrefs() {
    const r = prefs.reading;
    el.dataset.mode = r.mode;
    queueMicrotask(fit);
    modeBtns.forEach((b, i) => b.setAttribute("aria-pressed", String(modes[i] === r.mode)));
    aaBtn.hidden = r.mode !== "read" && !showsBook();
    gridBtn.hidden = gridButtonPlace(r.mode, prefs.rail) !== "toolbar";
    el.style.setProperty("--text-size", `${r.size / 100}`);
    el.dataset.font = r.font;
    el.dataset.width = r.width;
    el.dataset.theme = r.theme;
    el.dataset.tagged = String(r.taggedWords);
    el.style.setProperty("--line-height", String(SPACINGS[r.spacing]));
    sideBtn.setAttribute("aria-pressed", String(prefs.sidebar.open));
    if (r.mode !== lastMode) {
      lastMode = r.mode;
      grid.render();
    }
  }
  // In Study mode the Aa button is there while the selected pane shows a book (Pages / Scroll, PDF zoom).
  const showsBook = () => grid.selectedView()?.markScope?.()?.kind === "item";
  const syncAa = () => { aaBtn.hidden = prefs.reading.mode !== "read" && !showsBook(); };
  document.addEventListener("glossa:pane-changed", syncAa);
  // A book's pane says what it holds once the book has opened.
  document.addEventListener("glossa:book-section", syncAa);
  // fit keeps the top bar on one line: first the section label shortens ("3 / 1070"), then the
  // contents and Bibles buttons move into the pane's ⋯ menu, and as a last resort the bar wraps.
  let fitting = false;
  function fit() {
    if (fitting) return;
    fitting = true;
    try {
      const over = () => bar.scrollWidth > bar.clientWidth + 1;
      delete bar.dataset.fit;
      for (const stage of ["compact", "menu", "wrap"]) {
        if (!over()) break;
        bar.dataset.fit = stage;
      }
    } finally {
      fitting = false;
    }
  }
  new ResizeObserver(() => fit()).observe(bar);
  new MutationObserver(() => fit()).observe(controls, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ["hidden"] });
  document.addEventListener("glossa:pane-changed", fit);
  document.addEventListener("glossa:book-section", fit);
  findBtn.addEventListener("click", () => queueMicrotask(fit));

  onPrefs(applyPrefs);
  applyPrefs();

  async function show() {
    void today.refresh();
  }

  return { el, show, print, focus: () => grid.focus() };
}

// readingControls are the Read mode settings, in the Aa popover and in Settings → Appearance.
export function readingControls(): HTMLElement {
  const size = h("output", { "aria-live": "polite", class: "size-out" }, `${prefs.reading.size}%`);
  const set = (patch: Partial<ReadingPrefs>) => {
    setPrefs({ reading: { ...prefs.reading, ...patch } });
    size.textContent = `${prefs.reading.size}%`;
  };
  const choice = <K extends "font" | "width" | "spacing" | "theme">(key: K, label: string, hint: string | undefined, options: [ReadingPrefs[K], string][]) =>
    settingRow(label, hint, segmented({ label, value: prefs.reading[key], small: true, options, onChange: (v) => set({ [key]: v } as Partial<ReadingPrefs>) }));
  const stepper = h("div", { class: "stepper", role: "group", "aria-label": "Text size" },
    button("A−", () => set({ size: stepSize(prefs.reading.size, -1) }), { "aria-label": "Smaller text", title: `Smaller (down to ${TEXT_SIZES[0]}%)` }),
    size,
    button("A+", () => set({ size: stepSize(prefs.reading.size, 1) }), { "aria-label": "Larger text", title: `Larger (up to ${TEXT_SIZES[TEXT_SIZES.length - 1]}%)` }));
  const zoomOut = h("output", { "aria-live": "polite", class: "size-out" }, `${Math.round(bookZoom() * 100)}%`);
  const stepZoom = (d: number) => { zoomOut.textContent = `${Math.round(setBookZoom(Math.round((bookZoom() + d) * 10) / 10) * 100)}%`; };
  const zoom = h("div", { class: "stepper", role: "group", "aria-label": "PDF zoom" },
    button("−", () => stepZoom(-0.1), { "aria-label": "Zoom out" }), zoomOut, button("+", () => stepZoom(0.1), { "aria-label": "Zoom in" }));
  return h("div", { class: "pop-body reading-controls" },
    h("h2", { class: "pop-title" }, "Reading"),
    settingRow("Text size", undefined, stepper),
    choice("font", "Typeface", undefined, [["serif", "Book"], ["sans", "Plain"]]),
    choice("width", "Line length", undefined, [["narrow", "Comfortable"], ["wide", "Full width"]]),
    choice("spacing", "Line spacing", undefined, [["tight", "Tight"], ["normal", "Normal"], ["loose", "Loose"]]),
    choice("theme", "Page", "Read mode's own page colour.", [["auto", "App"], ["light", "Light"], ["sepia", "Sepia"], ["dark", "Dark"]]),
    settingRow("Books", "Scroll runs a book's pages or chapters together. Kept on this device.", segmented({ label: "Books", value: currentLayout(), small: true,
      options: [["pages", "Pages"], ["scroll", "Scroll"]], onChange: (v) => setLayout(v) })),
    settingRow("PDF zoom", "How large a PDF's pages are drawn. Kept on this device.", zoom));
}
