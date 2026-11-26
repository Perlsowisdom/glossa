// The Obsidian part of its card in Settings → Connections: choose the vault folder and the folder in it, Export,
// Import, when they last ran, and the choice to make for a note that changed both in Glossa and in its file. The
// switch and Test are the card's own.

import { pick, run } from "../api";
import { failureText } from "../connectwords";
import { exportPicker } from "../obsidianaction";
import {
  CHOICES, Choice, ObsidianConflict, ObsidianExport, ObsidianImport, ObsidianStatus, exportText, importText, lastText, resolvedText, vaultText,
} from "../obsidianwords";
import { excerptText } from "../readwisewords";
import { button, clear, field, h } from "../dom";
import { workChanged } from "../work";

// obsidianSection returns the controls for a card. changed asks the card to check the connection again (after the vault
// is chosen); say shows a short message in the page.
export function obsidianSection(changed: () => Promise<void>, say: (t: string, kind?: string) => void): HTMLElement {
  const el = h("div", { class: "readwise-card obsidian-card" });
  const where = h("p", { class: "hint obsidian-vault" });
  const last = h("p", { class: "hint obsidian-last" });
  const result = h("p", { class: "connection-status obsidian-result", role: "status", "aria-live": "polite" });
  const conflicts = h("div", { class: "readwise-conflicts obsidian-conflicts" });
  const folderBox = h("div", {});
  const actions = h("div", { class: "row wrap" });
  const more = h("div", { class: "row wrap" });
  let busy = false;
  let st: ObsidianStatus | undefined;

  async function refresh() {
    try {
      st = await run<ObsidianStatus>("obsidian.status");
    } catch (e) {
      return say(failureText(e), "error");
    }
    draw(st);
  }

  function draw(s: ObsidianStatus) {
    const ready = !!s.vault && !s.problem;
    where.textContent = s.problem ? `${vaultText(s)} ${s.problem}` : vaultText(s);
    last.textContent = ready ? lastText(s) : "";
    clear(folderBox, s.vault ? folderForm(s) : null);
    clear(actions,
      button(s.vault ? "Change vault…" : "Choose vault folder…", () => void chooseVault(), { class: s.vault ? "" : "primary", "data-obsidian": "vault" }),
      ready ? button("Export all notes", () => void exportAll(), { class: "primary", "data-obsidian": "export-all" }) : null,
      ready ? button("Choose what to export…", () => void exportChosen(), { "data-obsidian": "export-pick" }) : null,
      ready ? button("Import", () => void importFiles(), { "data-obsidian": "import" }) : null);
    clear(more, ready ? button("Import from another folder…", () => void importOther(), { "data-obsidian": "import-other" }) : null,
      ready ? button("Import the whole vault", () => void importFiles("."), { "data-obsidian": "import-all" }) : null);
    drawConflicts(s.conflicts);
  }

  function folderForm(s: ObsidianStatus): HTMLElement {
    const input = h("input", { type: "text", value: s.subfolder, autocomplete: "off", spellcheck: "false", "aria-label": "Folder in the vault", class: "obsidian-folder" }) as HTMLInputElement;
    const form = h("form", { class: "row wrap end obsidian-folder-form" },
      field("Folder in the vault", input, "Notes and sermons you export go here, one file each."),
      h("button", { type: "submit" }, "Save folder"));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      try {
        await run("obsidian.set_vault", { path: s.vault, subfolder: input.value.trim() || "Glossa" });
        say("Folder saved.", "ok");
      } catch (err) {
        say(failureText(err), "error");
      }
      await refresh();
      await changed();
    });
    return form;
  }

  async function chooseVault() {
    let path = "";
    try {
      path = await pick("folder", "Choose your Obsidian vault folder");
    } catch (e) {
      return say(failureText(e), "error");
    }
    if (!path) return;
    try {
      await run("obsidian.set_vault", { path });
      say("Vault chosen.", "ok");
    } catch (e) {
      say(failureText(e), "error");
    }
    await refresh();
    await changed();
  }

  async function guarded(text: string, work: () => Promise<void>) {
    if (busy) return;
    busy = true;
    for (const b of [...actions.querySelectorAll("button"), ...more.querySelectorAll("button")]) b.disabled = true;
    result.className = "connection-status obsidian-result";
    result.textContent = text;
    try {
      await work();
    } catch (e) {
      result.className = "connection-status obsidian-result bad";
      result.textContent = failureText(e);
    }
    busy = false;
    await refresh();
    workChanged();
  }

  const exportAll = () => guarded("Exporting your notes…", async () => {
    const r = await run<ObsidianExport>("obsidian.export", { all_notes: true });
    result.textContent = exportText(r);
  });

  async function exportChosen() {
    await exportPicker((r) => { result.className = "connection-status obsidian-result"; result.textContent = exportText(r); void refresh(); workChanged(); });
  }

  const importFiles = (folder?: string) => guarded("Reading the vault…", async () => {
    const r = await run<ObsidianImport>("obsidian.import", folder ? { folder } : {});
    result.textContent = importText(r);
  });

  async function importOther() {
    let path = "";
    try {
      path = await pick("folder", "Choose a folder in your vault to import from");
    } catch (e) {
      return say(failureText(e), "error");
    }
    if (path) await importFiles(path);
  }

  function drawConflicts(list: ObsidianConflict[]) {
    clear(conflicts, list.length ? [
      h("p", { class: "hint" }, "These notes changed in both Glossa and Obsidian. Nothing was changed in them. Choose what to do with each."),
      ...list.map(conflictRow),
    ] : []);
  }

  function conflictRow(c: ObsidianConflict): HTMLElement {
    return h("div", { class: "readwise-conflict obsidian-conflict", "data-note": c.id },
      h("strong", {}, c.title || c.obsidian_title || "Untitled"),
      h("small", { class: "hint" }, c.path),
      h("div", { class: "readwise-sides" },
        h("div", {}, h("small", { class: "hint" }, "In Glossa"), h("p", {}, excerptText(c.your_excerpt))),
        h("div", {}, h("small", { class: "hint" }, "In Obsidian"), h("p", {}, excerptText(c.obsidian_excerpt)))),
      h("div", { class: "row wrap" }, ...CHOICES.map((ch) => button(ch.label, () => void settle(c, ch.choice), { title: ch.help, "data-choice": ch.choice }))));
  }

  async function settle(c: ObsidianConflict, choice: Choice) {
    try {
      await run("obsidian.resolve", { kind: c.kind, id: c.id, resolve: choice });
      say(resolvedText(choice), "ok");
      workChanged();
    } catch (e) {
      say(failureText(e), "error");
    }
    await refresh();
  }

  clear(el,
    h("p", { class: "hint" }, "Notes and sermons become Markdown files with a block of properties at the top (title, passages, tags and dates). Files in your vault come in as notes. Nothing is ever deleted."),
    where, folderBox, actions, more, last, result, conflicts);
  void refresh();
  return el;
}
