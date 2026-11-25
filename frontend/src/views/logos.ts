// Settings → Your data → Import from Logos: find the user's own Logos data, show what could be
// imported with samples and what cannot be placed, import after a confirmation, and take an import
// back. Glossa reads a temporary copy; nothing in Logos is ever changed.

import { Message, pick, run } from "../api";
import { button, clear, h } from "../dom";
import {
  LogosPreview, LogosReport, LogosRun, canImport, kindLine, reportLine, reportSummary, runLine, sampleStatus, totalToImport, unmappedLine,
} from "../logoswords";

export function logosSection(): { el: HTMLElement; show: () => void } {
  const msg = new Message();
  const folder = h("input", { type: "text", id: "logos-folder", placeholder: "The folder that holds Logos's Data and Documents folders", size: "48" }) as HTMLInputElement;
  const result = h("div", { class: "logos-result" });
  const runsEl = h("ul", { class: "plain logos-runs" });
  let current: LogosPreview | undefined;

  const folderParam = () => (folder.value.trim() ? { folder: folder.value.trim() } : {});

  async function preview() {
    msg.info("Looking at your Logos data…");
    clear(result);
    const p = await msg.attempt(() => run<LogosPreview>("logos.preview", folderParam()));
    if (!p) return;
    current = p;
    msg.clear();
    showPreview(p);
  }

  function showPreview(p: LogosPreview) {
    const total = totalToImport(p);
    clear(result,
      h("p", { class: "hint" }, "Read from a copy of ", h("code", {}, p.source), ". Nothing in Logos is changed."),
      h("ul", { class: "plain logos-kinds" }, p.kinds.map((k) => h("li", { "data-kind": k.kind },
        h("strong", {}, kindLine(k)),
        k.samples.length ? h("ul", { class: "plain logos-samples" }, k.samples.map((s) => h("li", {},
          s.title || "(untitled)", s.text && s.text !== s.title ? h("span", { class: "hint" }, " — ", s.text) : null,
          sampleStatus(s) ? h("span", { class: "hint" }, ` (${sampleStatus(s)})`) : null))) : null))),
      p.folders ? h("p", { class: "hint" }, `${p.folders} folder${p.folders === 1 ? "" : "s"} will be made for your notebooks, clippings and sermons.`) : null,
      p.unmapped.length ? h("details", { class: "more logos-unmapped" },
        h("summary", {}, `Not imported (${p.unmapped.reduce((n, g) => n + g.count, 0)})`),
        h("p", { class: "hint" }, "Glossa has no place for these, or they are not yours. They stay in Logos."),
        h("ul", { class: "plain" }, p.unmapped.map((g) => h("li", {}, unmappedLine(g))))) : null,
      p.notes.length ? h("ul", { class: "plain hint logos-notes" }, p.notes.map((n) => h("li", {}, n))) : null,
      total ? h("p", { class: "hint" }, "Nothing in Logos changes. Every change is recorded in history, and you can take the import back afterwards.") : null,
      h("div", { class: "row wrap" },
        button(total ? `Import ${total} item${total === 1 ? "" : "s"}` : "Nothing new to import", () => void doImport(), { class: "primary", id: "logos-import", disabled: !canImport(p) })));
  }

  async function doImport() {
    if (!current || !canImport(current)) return;
    msg.info("Importing…");
    const rep = await msg.attempt(() => run<LogosReport>("logos.import", folderParam()));
    if (!rep) return;
    msg.ok(reportSummary(rep));
    clear(result,
      h("ul", { class: "plain logos-report" }, rep.kinds.map((k) => h("li", {}, reportLine(k)))),
      rep.notes.length ? h("ul", { class: "plain hint logos-report-notes" }, rep.notes.map((n) => h("li", {}, n))) : null,
      rep.failures?.length ? h("ul", { class: "plain msg error" }, rep.failures.map((f) => h("li", {}, f))) : null,
      h("div", { class: "row wrap" }, button("Undo this import", () => void undo(rep.run_id), { id: "logos-undo" })));
    current = undefined;
    await refresh();
  }

  async function undo(id: string) {
    const r = await msg.attempt(() => run<{ removed: number; folders: number }>("logos.undo", { run_id: id }));
    if (!r) return;
    msg.ok(`Took the import back: ${r.removed} item${r.removed === 1 ? "" : "s"} went to the bin.`);
    clear(result);
    await refresh();
  }

  async function choose() {
    const dir = await msg.attempt(() => pick("folder", "Choose the folder that holds Logos's Data and Documents folders"));
    if (dir) {
      folder.value = dir;
      await preview();
    }
  }

  async function refresh() {
    const runs = await msg.attempt(() => run<LogosRun[]>("logos.runs"));
    if (!runs) return;
    if (!runs.length) return void clear(runsEl, h("li", { class: "hint" }, "No imports from Logos yet."));
    clear(runsEl, runs.slice(0, 8).map((r) => h("li", { "data-run": r.id }, runLine(r), " ",
      r.undone ? null : button("Undo", () => void undo(r.id), { class: "link" }))));
  }

  const el = h("section", { class: "card", id: "logos-card", "aria-labelledby": "logos-h" },
    h("h2", { id: "logos-h" }, "Import from Logos"),
    h("p", { class: "hint" }, "Bring in your own notes, highlights, clippings, lists, reading plans and sermons. Glossa reads a copy of your Logos files, shows you what it found, and imports only after you say so. Running it again adds only what is new or changed. Logos books themselves cannot be imported."),
    h("div", { class: "row wrap" },
      button("Look for my Logos data", () => void preview(), { class: "primary", id: "logos-find" })),
    h("details", { class: "more-options" }, h("summary", {}, "Use another folder"),
      h("div", { class: "row wrap" }, folder, button("Choose…", () => void choose()), button("Preview this folder", () => void preview(), { id: "logos-preview-folder" }))),
    result,
    h("h3", {}, "Earlier imports"),
    runsEl,
    msg.el);

  return { el, show: () => void refresh() };
}
