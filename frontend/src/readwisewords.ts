// The plain words of the Readwise part of Settings → Connections: what a preview says, what an import
// did, when it last ran and how to settle a note that was changed on both sides. The commands already
// speak plainly; this keeps the few phrases the interface chooses for itself in one place.

export interface ReadwiseConflict {
  source_id: string;
  note_id: string;
  title: string;
  your_excerpt: string;
  readwise_excerpt: string;
}

export interface ReadwiseResult {
  preview: boolean;
  full: boolean;
  since?: string;
  sources: number;
  highlights: number;
  matched: number;
  ambiguous: number;
  notes_new: number;
  notes_new_on_book: number;
  notes_updated: number;
  notes_unchanged: number;
  highlights_new: number;
  highlights_changed: number;
  highlights_gone: number;
  skipped: number;
  conflicts: ReadwiseConflict[];
  message: string;
}

export interface ReadwiseLast {
  at: string;
  sources: number;
  highlights: number;
  notes_new: number;
  notes_updated: number;
}

export interface ReadwiseStatus {
  last_import?: ReadwiseLast;
  linked: number;
  conflicts: ReadwiseConflict[];
}

export type Choice = "readwise" | "mine" | "both";

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

// previewText: what an import would do, in sentences.
export function previewText(r: ReadwiseResult): string {
  if (r.sources === 0) return r.full ? "Readwise has no highlights to bring in." : "Nothing in Readwise has changed since the last import.";
  const parts = [`Readwise has ${count(r.highlights, "highlight", "highlights")} from ${count(r.sources, "source", "sources")}${r.full ? "" : ", new or changed since the last import"}.`];
  parts.push(r.matched
    ? `${count(r.matched, "source matches a book", "sources match books")} in your library by title and author; the highlights of ${r.matched === 1 ? "that book go" : "those books go"} on the book.`
    : "None of them match a book in your library by title and author.");
  const notes = [r.notes_new ? `make ${count(r.notes_new, "note", "notes")}` : "", r.notes_updated ? `update ${count(r.notes_updated, "note", "notes")}` : ""].filter(Boolean);
  parts.push(notes.length ? `Importing would ${notes.join(" and ")}.` : "Importing would change no notes.");
  if (r.ambiguous) parts.push(`${count(r.ambiguous, "source fits", "sources fit")} more than one book, so ${r.ambiguous === 1 ? "it stays" : "they stay"} separate notes.`);
  if (r.conflicts.length) { const n = r.conflicts.length; parts.push(`${count(n, "note", "notes")} you edited ${n === 1 ? "has" : "have"} newer highlights in Readwise; an import would leave ${n === 1 ? "it" : "them"} as ${n === 1 ? "it is" : "they are"}.`); }
  return parts.join(" ");
}

// importText: what an import did.
export function importText(r: ReadwiseResult): string {
  if (r.sources === 0 && !r.notes_new && !r.notes_updated && !r.highlights_gone && !r.conflicts.length) {
    return r.full ? "Readwise has no highlights to bring in." : "Nothing in Readwise has changed since the last import.";
  }
  const parts = [`Imported ${count(r.highlights, "highlight", "highlights")} from ${count(r.sources, "source", "sources")}.`];
  const did = [r.notes_new ? `${count(r.notes_new, "note", "notes")} made` : "", r.notes_updated ? `${count(r.notes_updated, "note", "notes")} updated` : "",
    r.notes_unchanged ? `${count(r.notes_unchanged, "note", "notes")} already up to date` : ""].filter(Boolean);
  if (did.length) parts.push(`${did.join(", ")}.`);
  if (r.highlights_gone) parts.push(`${count(r.highlights_gone, "highlight was", "highlights were")} taken out of their notes.`);
  if (r.skipped) parts.push(`${count(r.skipped, "source was", "sources were")} left alone because you deleted the note or chose to keep your own.`);
  if (r.conflicts.length) parts.push(conflictsLine(r.conflicts.length));
  return parts.join(" ");
}

function conflictsLine(n: number): string {
  return `${count(n, "note", "notes")} you edited ${n === 1 ? "has" : "have"} newer highlights in Readwise and ${n === 1 ? "was" : "were"} left as ${n === 1 ? "it is" : "they are"}. Choose what to do below.`;
}

// lastImportText: "Last imported 3 March 2026, 10:00: 6 highlights from 4 sources."
export function lastImportText(last: ReadwiseLast | undefined, timeZone?: string): string {
  if (!last) return "Not imported yet. The first import brings in everything; later ones bring in only what changed.";
  const d = new Date(last.at);
  const when = Number.isNaN(d.getTime()) ? "" : ` ${d.toLocaleDateString("en-GB", { day: "numeric", month: "long", year: "numeric", timeZone })}, ${d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", timeZone })}`;
  return `Last imported${when}: ${count(last.highlights, "highlight", "highlights")} from ${count(last.sources, "source", "sources")}, ${count(last.notes_new, "note", "notes")} made and ${last.notes_updated} updated.`;
}

export function linkedText(n: number): string {
  return n === 0 ? "" : `${count(n, "source has", "sources have")} a note in Glossa.`;
}

export const CHOICES: { choice: Choice; label: string; help: string }[] = [
  { choice: "readwise", label: "Use Readwise’s text", help: "Replaces your note with what Readwise has." },
  { choice: "mine", label: "Keep mine", help: "Keeps your note as it is and stops updating it." },
  { choice: "both", label: "Keep both", help: "Keeps your note, and puts Readwise’s text in a new note that stays up to date." },
];

export function resolvedText(choice: Choice): string {
  switch (choice) {
    case "readwise": return "The note now has Readwise’s text.";
    case "mine": return "Your note is kept as it is, and Readwise will not change it again.";
    case "both": return "Your note is untouched, and Readwise’s text is in a new note.";
  }
}
