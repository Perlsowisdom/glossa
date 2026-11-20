// The words for Settings → Your data → Import from Logos: what the preview and the report say.
// Kept apart from the page so they can be tested.

export interface LogosSample { title: string; text?: string; status: "new" | "changed" | "imported" }
export interface LogosKindCount {
  kind: string; label: string; found: number; new: number; changed: number; imported: number; samples: LogosSample[];
}
export interface LogosUnmapped { kind: string; label: string; reason: string; count: number; examples?: string[] }
export interface LogosPreview {
  source: string; kinds: LogosKindCount[]; folders: number; unmapped: LogosUnmapped[]; notes: string[];
}
export interface LogosKindResult { kind: string; label: string; created: number; updated: number; skipped: number; failed: number }
export interface LogosReport {
  run_id: string; kinds: LogosKindResult[]; folders: number; unmapped: LogosUnmapped[]; notes: string[]; failures?: string[];
}
export interface LogosRun { id: string; started_at: number; source?: string; report?: LogosReport; added: number; undone: boolean }

function plural(n: number, one: string, many = one + "s"): string {
  return `${n} ${n === 1 ? one : many}`;
}

// kindLine says what the preview found of one kind.
export function kindLine(k: LogosKindCount): string {
  if (k.found === 0) return `${k.label}: none to import`;
  const parts = [`${k.new} new`];
  if (k.changed) parts.push(`${k.changed} changed in Logos`);
  if (k.imported) parts.push(`${k.imported} already imported`);
  return `${k.label}: ${k.found} found, ${parts.join(", ")}`;
}

// sampleStatus is the small word beside a sample.
export function sampleStatus(s: LogosSample): string {
  return s.status === "new" ? "" : s.status === "changed" ? "changed in Logos" : "already imported";
}

// unmappedLine says what could not be placed and why.
export function unmappedLine(g: LogosUnmapped): string {
  const e = g.examples?.length ? ` (for example ${g.examples.slice(0, 3).join("; ")})` : "";
  return `${g.label}, ${g.count} left out: ${g.reason.charAt(0).toLowerCase()}${g.reason.slice(1)}${e}`;
}

// canImport is true when there is anything new or changed to bring in.
export function canImport(p: LogosPreview): boolean {
  return p.kinds.some((k) => k.new + k.changed > 0);
}

// totalToImport counts what an import would add or change.
export function totalToImport(p: LogosPreview): number {
  return p.kinds.reduce((n, k) => n + k.new + k.changed, 0);
}

// reportLine says what an import did with one kind.
export function reportLine(k: LogosKindResult): string {
  const parts = [`${k.created} added`];
  if (k.updated) parts.push(`${k.updated} changed`);
  if (k.skipped) parts.push(`${k.skipped} left as they were`);
  if (k.failed) parts.push(`${k.failed} failed`);
  return `${k.label}: ${parts.join(", ")}`;
}

// reportSummary is the one sentence after an import.
export function reportSummary(r: LogosReport): string {
  const added = r.kinds.reduce((n, k) => n + k.created, 0);
  const changed = r.kinds.reduce((n, k) => n + k.updated, 0);
  const folders = r.folders ? ` and ${plural(r.folders, "folder")}` : "";
  if (added + changed === 0) return "Nothing new to import: everything was already here.";
  return `Imported ${plural(added, "item")}${folders}${changed ? `, and changed ${plural(changed, "item")} that had changed in Logos` : ""}.`;
}

// runLine describes an earlier import in the list.
export function runLine(r: LogosRun): string {
  const when = new Date(r.started_at).toLocaleString();
  if (r.undone) return `${when}: taken back`;
  return `${when}: ${plural(r.added, "item")} added`;
}
