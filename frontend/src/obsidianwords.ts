// The plain words of the Obsidian part of Settings → Connections and of "Send to Obsidian": what an export or an
// import did, when it last ran, and how to settle a note that changed both in Glossa and in its file. The commands
// already speak plainly; this keeps the few phrases the interface chooses for itself in one place.

export interface ObsidianConflict {
  kind: "note" | "sermon";
  id: string;
  path: string;
  title: string;
  obsidian_title: string;
  your_excerpt: string;
  obsidian_excerpt: string;
}

export interface ObsidianLast {
  at: string;
  written?: number;
  created?: number;
  updated?: number;
  unchanged: number;
}

export interface ObsidianStatus {
  vault?: string;
  subfolder: string;
  problem?: string;
  last_export?: ObsidianLast;
  last_import?: ObsidianLast;
  linked: number;
  item_path?: string;
  conflicts: ObsidianConflict[];
}

export interface ObsidianExport {
  folder: string;
  written: number;
  updated: number;
  unchanged: number;
  obsidian_newer: number;
  items: { kind: "note" | "sermon"; id: string; title?: string; path?: string; state: string }[];
  conflicts: ObsidianConflict[];
  skipped: { path: string; reason: string }[];
  message: string;
}

export interface ObsidianImport {
  folder: string;
  files: number;
  created: number;
  updated: number;
  unchanged: number;
  glossa_newer: number;
  links: number;
  embeds: number;
  conflicts: ObsidianConflict[];
  skipped: { path: string; reason: string }[];
  notes: string[];
  message: string;
}

export type Choice = "obsidian" | "glossa" | "both";

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

// folderText: "Notes and sermons go to the “Glossa” folder of the vault at C:\Vault."
export function vaultText(st: ObsidianStatus): string {
  if (!st.vault) return "No vault chosen yet.";
  return `Vault: ${st.vault}. Notes and sermons go to the “${st.subfolder}” folder in it.`;
}

function when(at: string, timeZone?: string): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return "";
  return ` ${d.toLocaleDateString("en-GB", { day: "numeric", month: "long", year: "numeric", timeZone })}, ${d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", timeZone })}`;
}

// lastText: "Last exported 3 March 2026, 10:00: 4 files written, 2 already up to date. Last imported …"
export function lastText(st: Pick<ObsidianStatus, "last_export" | "last_import" | "linked">, timeZone?: string): string {
  const parts: string[] = [];
  const e = st.last_export;
  parts.push(e ? `Last exported${when(e.at, timeZone)}: ${count(e.written ?? 0, "file", "files")} written, ${e.unchanged} already up to date.` : "Nothing exported yet.");
  const i = st.last_import;
  parts.push(i ? `Last imported${when(i.at, timeZone)}: ${count(i.created ?? 0, "note", "notes")} made, ${i.updated ?? 0} updated, ${i.unchanged} already up to date.` : "Nothing imported yet.");
  if (st.linked) parts.push(`${count(st.linked, "note or sermon is", "notes and sermons are")} linked to files.`);
  return parts.join(" ");
}

// exportText: what an export did, in sentences.
export function exportText(r: ObsidianExport): string {
  const did = [r.written ? `${count(r.written, "file", "files")} made` : "", r.updated ? `${count(r.updated, "file", "files")} updated` : "",
    r.unchanged ? `${r.unchanged} already up to date` : ""].filter(Boolean);
  const parts = [did.length ? `${did.join(", ")} in “${r.folder}”.` : "Nothing was written."];
  if (r.obsidian_newer) parts.push(`${count(r.obsidian_newer, "file has", "files have")} newer changes made in Obsidian and ${r.obsidian_newer === 1 ? "was" : "were"} left alone. Import brings ${r.obsidian_newer === 1 ? "it" : "them"} in.`);
  if (r.conflicts.length) parts.push(conflictsLine(r.conflicts.length));
  if (r.skipped.length) parts.push(`${count(r.skipped.length, "item was", "items were")} skipped: ${r.skipped.slice(0, 3).map((s) => `${s.path} (${s.reason})`).join("; ")}${r.skipped.length > 3 ? "; and more" : ""}.`);
  return parts.join(" ");
}

// importText: what an import did.
export function importText(r: ObsidianImport): string {
  if (r.files === 0 && !r.skipped.length) return `There are no Markdown files in “${r.folder}”.`;
  const parts = [`${count(r.created, "note", "notes")} made, ${r.updated} updated, ${r.unchanged} already up to date.`];
  if (r.links) parts.push(`${count(r.links, "[[link]] was", "[[links]] were")} turned into plain words.`);
  if (r.embeds) parts.push(`${count(r.embeds, "embed (![[…]]) was", "embeds (![[…]]) were")} left as text, because a note cannot show another note or a picture inside it.`);
  if (r.glossa_newer) parts.push(`${count(r.glossa_newer, "note has", "notes have")} newer changes in Glossa and ${r.glossa_newer === 1 ? "was" : "were"} left alone. Export sends ${r.glossa_newer === 1 ? "it" : "them"}.`);
  if (r.conflicts.length) parts.push(conflictsLine(r.conflicts.length));
  if (r.skipped.length) parts.push(`${count(r.skipped.length, "file was", "files were")} skipped: ${r.skipped.slice(0, 3).map((s) => `${s.path} (${s.reason})`).join("; ")}${r.skipped.length > 3 ? "; and more" : ""}.`);
  return parts.join(" ");
}

function conflictsLine(n: number): string {
  return `${count(n, "note", "notes")} changed in both Glossa and Obsidian and ${n === 1 ? "was" : "were"} left as ${n === 1 ? "it is" : "they are"}. Choose what to do below.`;
}

export const CHOICES: { choice: Choice; label: string; help: string }[] = [
  { choice: "obsidian", label: "Use Obsidian’s text", help: "Replaces the note in Glossa with the text in the file." },
  { choice: "glossa", label: "Keep Glossa’s text", help: "Keeps the note and rewrites the file from it." },
  { choice: "both", label: "Keep both", help: "Puts the file’s text in a new note, then rewrites the file from Glossa’s note." },
];

export function resolvedText(choice: Choice): string {
  switch (choice) {
    case "obsidian": return "The note now has the text from Obsidian.";
    case "glossa": return "The file now has Glossa’s text.";
    case "both": return "Glossa’s text is in the file, and the text that was in the file is in a new note.";
  }
}

// menuLabel: the entry in a note's or sermon's ⋯ menu.
export function menuLabel(linked: boolean): string {
  return linked ? "Update in Obsidian" : "Send to Obsidian";
}

// NOT_READY: what a click says when Obsidian is not turned on or has no vault yet.
export const NOT_READY = "Turn on Obsidian and choose your vault in Settings → Connections first.";

export function conflictHeading(what: string): string {
  return `This ${what} changed in both Glossa and Obsidian`;
}

// sentText: what a single send said, in a few words.
export function sentText(state: string, path?: string): string {
  switch (state) {
    case "written": return `Sent to Obsidian${path ? ` as ${path}` : ""}.`;
    case "updated": return "Updated in Obsidian.";
    case "unchanged": return "Already up to date in Obsidian.";
    case "obsidian_newer": return "The file in Obsidian has newer changes than this. Nothing was changed; import it from Settings → Connections.";
    default: return "Done.";
  }
}
