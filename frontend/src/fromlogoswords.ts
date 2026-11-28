// The words for Library → From Logos: the summary line, the free-copy labels and the sort headers.
// Kept apart from the page so they can be tested.

export interface FreeCopy {
  source: string;
  item: string;
  name: string;
  kind: string;
  confidence: "certain" | "likely";
  installed: boolean;
  size?: number;
}

export type BookState = "already" | "free" | "none";

export interface LogosBook {
  resource_id: string;
  title: string;
  abbreviation?: string;
  authors?: string;
  type: string;
  type_label: string;
  published?: string;
  edition?: string;
  languages?: string;
  rights?: string;
  public_domain: boolean;
  pd_basis?: "rights" | "date";
  link: string;
  state?: BookState;
  free?: FreeCopy;
  others?: FreeCopy[];
  in_library?: FreeCopy;
  archive_link?: string;
}

export interface LogosLibrary {
  books: LogosBook[];
  total: number;
  refreshed: boolean;
  refreshed_at?: number;
  source?: string;
  all: number;
  public_domain: number;
  certain: number;
  likely: number;
  free_copies: number;
  already: number;
  not_free: number;
  types: { label: string; count: number }[];
}

export interface LogosRefresh {
  books: number; added: number; changed: number; removed: number; public_domain: number; source: string; refreshed_at: number;
}

export type SortKey = "title" | "author" | "type" | "published";
export const SORTS: { key: SortKey; label: string }[] = [
  { key: "title", label: "Title" }, { key: "author", label: "Author" }, { key: "type", label: "Type" }, { key: "published", label: "Published" },
];

const n = (x: number) => x.toLocaleString();
const books = (x: number) => `${n(x)} ${x === 1 ? "book" : "books"}`;

// summaryLine is the line above the list: how many of the books have a free copy (the coverage), how
// many are in Glossa already and how many are public domain.
export function summaryLine(r: LogosLibrary, when: string): string {
  const parts = [r.free_copies > 0 ? `${n(r.free_copies)} of your ${n(r.all)} Logos ${r.all === 1 ? "book has" : "books have"} a free copy` : `${n(r.all)} Logos ${r.all === 1 ? "book" : "books"}, none with a free copy found yet`];
  if (r.already) parts.push(`${n(r.already)} already in Glossa`);
  if (r.public_domain) parts.push(`${n(r.public_domain)} public domain`);
  if (when) parts.push(`read ${when}`);
  return parts.join(" · ");
}

// countLine says how many of the list are shown.
export function countLine(shown: number, total: number, all: number): string {
  if (all === 0) return "";
  if (total === 0) return "No books match.";
  return shown < total ? `Showing ${n(shown)} of ${n(total)}.` : `${books(total)}.`;
}

// refreshLine reports what a refresh changed.
export function refreshLine(r: LogosRefresh): string {
  const parts: string[] = [];
  if (r.added) parts.push(`${n(r.added)} new`);
  if (r.changed) parts.push(`${n(r.changed)} changed`);
  if (r.removed) parts.push(`${n(r.removed)} no longer in Logos`);
  return `Read ${books(r.books)} from Logos${parts.length ? `: ${parts.join(", ")}` : ", no changes since the last time"}.`;
}

// publicDomainText says why a book counts as public domain.
export function publicDomainText(b: LogosBook): string {
  return b.pd_basis === "date" ? "Public domain, going by its date" : "Public domain";
}

// freeText labels the free copy of a book: how sure the match is and what it is.
export function freeText(f: FreeCopy): string {
  return `${f.confidence === "certain" ? "Certain match" : "Possible match: check"}: ${f.name}`;
}

// sourceName is the source's name in plain words.
export function sourceName(source: string): string {
  return source === "ccel" ? "CCEL" : source === "gutenberg" ? "Project Gutenberg" : source === "library" ? "your library" : "a Bible module";
}

// stateLabel is what a free copy is called: where it is.
export function stateLabel(f: FreeCopy): string {
  return f.source === "ccel" || f.source === "gutenberg" ? `Free copy on ${sourceName(f.source)}` : "Free as a Bible module";
}

// otherCopies lists the other free copies in one line.
export function otherCopies(o: FreeCopy[] | undefined): string {
  return o && o.length ? `Other free copies: ${o.map((f) => `${sourceName(f.source)}: ${f.name}`).join("; ")}` : "";
}

export type StateFilter = "" | "free" | "already" | "none";
export const STATE_FILTERS: [StateFilter, string][] = [["", "All"], ["free", "Free copy found"], ["already", "Already in Glossa"], ["none", "Not free"]];

// bulkPlan picks the books "Get all free copies" would download: a free copy that is certain, not in
// Glossa yet, from a source that can be fetched one at a time.
export function bulkPlan(books: LogosBook[]): LogosBook[] {
  return books.filter((b) => b.state === "free" && b.free && b.free.confidence === "certain" && !b.free.installed);
}

// bulkLine says what a bulk download holds: how many books, from where, and the size where known.
export function bulkLine(plan: LogosBook[], skipped: number): string {
  const by = new Map<string, number>();
  let bytes = 0;
  let unknown = 0;
  for (const b of plan) {
    const f = b.free!;
    const k = f.source === "ccel" || f.source === "gutenberg" ? sourceName(f.source) : "Bible modules";
    by.set(k, (by.get(k) ?? 0) + 1);
    if (f.size) bytes += f.size; else unknown++;
  }
  const where = [...by].map(([k, v]) => `${n(v)} from ${k}`).join(", ");
  const size = bytes ? `; ${unknown ? "at least " : ""}${(bytes / 1048576).toFixed(1)} MB${unknown ? ` (${n(unknown)} ${unknown === 1 ? "size is" : "sizes are"} not known until the download)` : ""}` : "; the sizes are not known until each download";
  return `${books(plan.length)}: ${where}${size}.${skipped ? ` ${n(skipped)} possible ${skipped === 1 ? "match is" : "matches are"} left out: check those one at a time.` : ""}`;
}

// ariaSort is the aria-sort value of a column header.
export function ariaSort(col: SortKey, sort: SortKey, desc: boolean): "ascending" | "descending" | "none" {
  return col !== sort ? "none" : desc ? "descending" : "ascending";
}

// nextSort is what clicking a header does: sort by that column, or turn the order round.
export function nextSort(col: SortKey, sort: SortKey, desc: boolean): { sort: SortKey; desc: boolean } {
  return col === sort ? { sort, desc: !desc } : { sort: col, desc: false };
}

// subline shows a book's short name under its title.
export function subline(b: LogosBook): string {
  return b.abbreviation ?? "";
}
