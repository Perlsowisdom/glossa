// The Readwise part of its card in Settings → Connections: Preview, Import, when it last ran, and the
// choice to make for a note that was edited in Glossa and also has newer highlights in Readwise. The
// switch, the token box and Test are the card's own.

import { run } from "../api";
import { failureText } from "../connectwords";
import {
  CHOICES, Choice, ReadwiseConflict, ReadwiseResult, ReadwiseStatus, importText, lastImportText, linkedText, previewText, resolvedText,
} from "../readwisewords";
import { button, clear, h } from "../dom";

// readwiseSection returns the import controls for a card. hasToken is whether a token is saved; say shows
// a short message in the page.
export function readwiseSection(hasToken: boolean, say: (t: string, kind?: string) => void): HTMLElement {
  const el = h("div", { class: "readwise-card" });
  const last = h("p", { class: "hint readwise-last" });
  const result = h("p", { class: "connection-status readwise-result", role: "status", "aria-live": "polite" });
  const conflicts = h("div", { class: "readwise-conflicts" });
  let busy = false;

  const preview = button("Preview", () => go(true), { "data-readwise": "preview" });
  const importBtn = button("Import highlights", () => go(false), { class: "primary", "data-readwise": "import" });
  const again = button("Import everything again", () => go(false, true), { "data-readwise": "again" });
  const buttons = [preview, importBtn, again];
  for (const b of buttons) b.disabled = !hasToken;

  async function refresh() {
    let st: ReadwiseStatus;
    try {
      st = await run<ReadwiseStatus>("readwise.status");
    } catch (e) {
      return say(failureText(e), "error");
    }
    last.textContent = [lastImportText(st.last_import), linkedText(st.linked)].filter(Boolean).join(" ");
    drawConflicts(st.conflicts);
  }

  async function go(dry: boolean, full = false) {
    if (busy) return;
    busy = true;
    for (const b of buttons) b.disabled = true;
    result.className = "connection-status readwise-result";
    result.textContent = dry ? "Asking Readwise what an import would do…" : "Importing from Readwise…";
    try {
      const r = await run<ReadwiseResult>(dry ? "readwise.preview" : "readwise.import", full ? { full: true } : {});
      result.textContent = dry ? previewText(r) : importText(r);
      if (!dry) drawConflicts(r.conflicts);
    } catch (e) {
      result.className = "connection-status readwise-result bad";
      result.textContent = failureText(e);
    }
    busy = false;
    for (const b of buttons) b.disabled = !hasToken;
    if (!dry) await refresh();
  }

  function drawConflicts(list: ReadwiseConflict[]) {
    clear(conflicts, list.length ? [
      h("p", { class: "hint" }, "These notes were edited in Glossa and have newer highlights in Readwise. Nothing was changed in them. Choose what to do with each."),
      ...list.map(conflictRow),
    ] : []);
  }

  function conflictRow(c: ReadwiseConflict): HTMLElement {
    const row = h("div", { class: "readwise-conflict", "data-source": c.source_id },
      h("strong", {}, c.title || "Untitled note"),
      h("div", { class: "readwise-sides" },
        h("div", {}, h("small", { class: "hint" }, "Your note"), h("p", {}, c.your_excerpt)),
        h("div", {}, h("small", { class: "hint" }, "Readwise"), h("p", {}, c.readwise_excerpt))),
      h("div", { class: "row wrap" }, ...CHOICES.map((ch) => button(ch.label, () => settle(c, ch.choice), { title: ch.help, "data-choice": ch.choice }))));
    return row;
  }

  async function settle(c: ReadwiseConflict, choice: Choice) {
    try {
      await run("readwise.resolve", { source_id: c.source_id, resolve: choice });
      say(resolvedText(choice), "ok");
    } catch (e) {
      say(failureText(e), "error");
    }
    await refresh();
  }

  clear(el,
    h("p", { class: "hint" }, hasToken
      ? "Each source becomes one note. When its title and author match a book in your library, the note is attached to that book."
      : "Paste your token above to bring in your highlights."),
    last,
    h("div", { class: "row wrap" }, preview, importBtn),
    result,
    conflicts,
    h("details", { class: "more" }, h("summary", {}, "More options"),
      h("p", { class: "hint" }, "Normally only highlights that changed since the last import are asked for. This asks for all of them, and also takes out highlights you deleted in Readwise."),
      h("div", { class: "row wrap" }, again)));
  void refresh();
  return el;
}
