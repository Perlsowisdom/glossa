// The words for Library → From Logos: the summary line, the free-copy labels and the sort headers.
// Kept apart from the page so they can be tested.

export interface FreeCopy {
  source: string;
  item: string;
  name: string;
  kind: string;
  confidence: "certain" | "likely";
  installed: boolean;
}

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
  free?: FreeCopy;
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

// summaryLine is the line above the list: how many books, how many are public domain and how many
// of those have a free copy.
export function summaryLine(r: LogosLibrary, when: string): string {
  const parts = [books(r.all)];
  if (r.public_domain) parts.push(`${n(r.public_domain)} public domain`);
  const free = r.certain + r.likely;
  if (free) parts.push(`${n(free)} with a free copy${r.likely ? ` (${n(r.certain)} certain, ${n(r.likely)} likely)` : ""}`);
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
  return `${f.confidence === "certain" ? "Certain match" : "Likely match"}: ${f.name}`;
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
