// Problem reports (T32, ADR 0009). Glossa keeps a small log on this computer. When something goes wrong
// (a crash last time, an error in Go that was caught, an error in this window) a quiet notice offers a
// report. The report is plain text the person can read and change; it goes nowhere unless they open it as
// a pre-filled GitHub issue (public) in their browser and press Submit there. Copy and Save to file are
// the other ways out. Nothing here uses the network.

import { Message, bound, onEvent, run } from "./api";
import { ask } from "./ask";
import { button, h } from "./dom";
import { errorText, parseError } from "./helpers";
import { closePopover } from "./popover";
import {
  DIALOG_INTRO, DIALOG_TITLE, NOTE_LABEL, NOTICE_TEXT, OPENED_LINE, OPEN_LABEL, PUBLIC_LINE, SHORTENED_LINE, TEXT_LABEL,
  ignorable, reasonText, setNote, shortStack,
} from "./problemwords";

interface Problem {
  id: string;
  kind: string;
  summary: string;
}

interface Status {
  enabled: boolean;
  folder: string;
  pending?: Problem;
}

let notice: HTMLElement | null = null;
let logging = false; // an error while reporting an error is not reported again

// startProblems listens for errors in this window and for problems Go caught, and shows the notice for
// one the person has not seen: at once for a new one, and at start for one left by the last run.
export function startProblems() {
  window.addEventListener("error", (e) => {
    if (e.message && !ignorable(e.message)) void logError(e.message, e.error instanceof Error ? e.error.stack : "", e.filename ? `${e.filename.split("/").pop()}:${e.lineno}` : "");
  });
  window.addEventListener("unhandledrejection", (e) => {
    const message = reasonText(e.reason);
    if (!ignorable(message)) void logError(message, e.reason instanceof Error ? e.reason.stack : "", "promise");
  });
  onEvent("problem:new", (p) => showNotice(p as Problem));
  new MutationObserver(place).observe(document.body, { childList: true });
  void problemStatus().then((s) => {
    if (s?.pending) showNotice(s.pending);
  });
}

async function problemStatus(): Promise<Status | undefined> {
  try {
    return await run<Status>("problems.status");
  } catch {
    return undefined;
  }
}

async function logError(message: string, stack: string | undefined, where: string) {
  if (logging) return;
  logging = true;
  try {
    const r = await run<{ problem?: Problem }>("problems.log", { message, stack: shortStack(stack), where });
    if (r?.problem) showNotice(r.problem);
  } catch {
    // The log is off, or Go is not there: nothing to do.
  } finally {
    logging = false;
  }
}

function hideNotice() {
  notice?.remove();
  notice = null;
}

// showNotice is the quiet notice in the corner: not a dialog, once per problem, beside the update notice.
function showNotice(p: Problem) {
  if (notice) return;
  const dismiss = () => {
    hideNotice();
    void run("problems.dismiss", { id: p.id }).catch(() => undefined);
  };
  notice = h("div", { class: "update-banner problem-notice", role: "region", "aria-label": "Problem notice", id: "problem-banner" },
    h("p", {}, NOTICE_TEXT),
    h("div", { class: "row" },
      button("Review", () => {
        dismiss();
        void reportProblem();
      }, { class: "primary" }),
      button("Not now", dismiss)));
  document.body.append(notice);
  place();
}

// place stacks the notice above any other corner notice (an update, a sync unlock), so none covers another.
function place() {
  if (!notice) return;
  let bottom = 16;
  for (const other of document.querySelectorAll<HTMLElement>(".update-banner")) {
    if (other !== notice) bottom += other.offsetHeight + 12;
  }
  notice.style.bottom = `${bottom}px`;
}

// reportProblem opens the report dialog: the version, system, the error and the latest log lines, ready to read and change.
export async function reportProblem(): Promise<void> {
  closePopover();
  if (document.getElementById("problem-dialog")) return;
  const msg = new Message();
  let text: string;
  try {
    text = (await run<{ text: string }>("problems.report", {})).text;
  } catch (e) {
    await ask(DIALOG_TITLE, "Glossa could not build the report: " + errorText(parseError(e)), "OK", false);
    return;
  }
  const idLabel = "problem-title";
  const note = h("input", { type: "text", id: "problem-note", autocomplete: "off", maxlength: "300" });
  const area = h("textarea", { id: "problem-text", class: "problem-text", rows: "14", spellcheck: "false", "aria-describedby": "problem-public" });
  area.value = text;
  note.addEventListener("input", () => {
    area.value = setNote(area.value, note.value);
  });

  const open = button(OPEN_LABEL, () => void openIssue(), { class: "primary" });
  const dlg = h("dialog", { id: "problem-dialog", class: "confirm problem-dialog", "aria-labelledby": idLabel },
    h("h2", { id: idLabel }, DIALOG_TITLE),
    h("p", {}, DIALOG_INTRO),
    h("p", { id: "problem-public", class: "problem-public" }, h("strong", {}, PUBLIC_LINE)),
    h("div", { class: "field" }, h("label", { for: "problem-note" }, NOTE_LABEL), note),
    h("div", { class: "field" }, h("label", { for: "problem-text" }, TEXT_LABEL), area),
    msg.el,
    h("div", { class: "row confirm-actions" },
      button("Cancel", () => close()),
      button("Save to file…", () => void save()),
      button("Copy", () => void copy()),
      open));
  dlg.addEventListener("close", () => dlg.remove());
  dlg.addEventListener("cancel", (e) => {
    e.preventDefault();
    close();
  });
  document.body.append(dlg);
  dlg.showModal();
  note.focus();

  function close() {
    dlg.close();
    dlg.remove();
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(area.value);
    } catch {
      area.focus();
      area.select();
      if (!document.execCommand("copy")) {
        msg.info("Copying did not work here. The text is selected: press Ctrl+C.");
        return;
      }
    }
    msg.ok("Copied. You can paste it anywhere.");
  }

  async function save() {
    const app = bound();
    const path = await app?.SaveFile?.("Save report", "glossa-problem-report.txt", "txt");
    if (!path) return;
    if ((await msg.attempt(() => run("problems.save", { path, text: area.value }))) !== undefined) msg.ok(`Saved to ${path}.`);
  }

  async function openIssue() {
    const r = await msg.attempt(() => run<{ url: string; shortened: boolean }>("problems.issue_url", { text: area.value }));
    if (!r) return;
    const app = bound();
    try {
      if (app?.OpenReportURL) await app.OpenReportURL(r.url);
      else window.open(r.url, "_blank", "noopener");
    } catch (e) {
      msg.error(e);
      return;
    }
    msg.ok(OPENED_LINE + (r.shortened ? SHORTENED_LINE : ""));
  }
}

// openLogFolder shows the folder that holds the log.
export async function openLogFolder(msg: Message) {
  try {
    await bound()?.OpenLogFolder?.();
  } catch (e) {
    msg.error(e);
  }
}
