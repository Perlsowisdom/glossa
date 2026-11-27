// The words and small rules of the problem report (T32): what the notice and the dialog say, how the
// "What were you doing?" box writes into the report text, and which window errors are not worth a report.

export const NOTICE_TEXT = "Glossa ran into a problem. Review a report?";

export const DIALOG_TITLE = "Report a problem";

export const DIALOG_INTRO =
  "The report below holds the Glossa version, your system, the error and the latest lines of Glossa’s problem log. " +
  "It never holds your notes, book titles, searches, file locations, keys or e-mail addresses. " +
  "You can change or delete anything in it. Glossa sends nothing by itself.";

export const PUBLIC_LINE =
  "GitHub issues are public: anyone can read what you submit. Read the text once more before you press Submit on GitHub.";

export const NOTE_LABEL = "What were you doing? (optional)";
export const TEXT_LABEL = "The report (this exact text is what you copy, save or open)";

export const OPEN_LABEL = "Open as GitHub issue (public)";

export const OPENED_LINE = "Your browser has the issue page open. Read it there and press Submit when you are happy. Glossa has sent nothing.";
export const SHORTENED_LINE =
  " The text was too long for a web address, so the oldest log lines were left out of it. Use Copy to paste the whole report into the issue.";

export const HEAD_NOTE = "## What were you doing?";

// setNote puts the note into the report's "What were you doing?" part. When the person has deleted that
// heading from the text, the text is left alone.
export function setNote(text: string, note: string): string {
  const i = text.indexOf(HEAD_NOTE);
  if (i < 0) return text;
  const start = i + HEAD_NOTE.length;
  const next = text.indexOf("\n## ", start);
  const rest = next < 0 ? "" : text.slice(next);
  const body = note.trim();
  return text.slice(0, start) + (body ? "\n\n" + body + "\n" : "\n") + rest;
}

// ignorable says whether a window error is noise that is not worth a report: the browser's own note that
// it skipped a frame, and script errors from other origins that carry no information.
export function ignorable(message: string): boolean {
  return /ResizeObserver loop/i.test(message) || /^Script error\.?$/i.test(message.trim());
}

// shortStack keeps the part of a stack worth sending: the first lines, trimmed.
export function shortStack(stack: string | undefined, lines = 12): string {
  return (stack ?? "").split("\n").slice(0, lines).map((l) => l.trimEnd()).join("\n");
}

// reasonText turns what a rejected promise carried (an Error, a string or anything) into a message.
export function reasonText(reason: unknown): string {
  if (reason instanceof Error) return reason.message || reason.name;
  if (typeof reason === "string") return reason;
  try {
    return JSON.stringify(reason) ?? String(reason);
  } catch {
    return String(reason);
  }
}
