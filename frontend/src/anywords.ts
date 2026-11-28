// The plain words of the Any connection: the menu entry, the question asked when a note changed on
// both sides, and what a result says. The commands already speak plainly; this keeps the few phrases
// the interface chooses for itself in one place.

export type SyncState = "created" | "updated" | "unchanged" | "both_changed" | "any_changed" | "gone";

export interface AnyConflict {
  glossa_title: string;
  glossa_excerpt: string;
  any_title: string;
  any_excerpt: string;
}

export interface AnySyncResult {
  state: SyncState;
  kind: "note" | "sermon";
  item_id: string;
  space_id?: string;
  object_id?: string;
  title?: string;
  copy_id?: string;
  message: string;
  conflict?: AnyConflict;
}

export interface AnySpaces {
  spaces: { id: string; name: string }[];
  default_space_id?: string;
}

export interface AnyPage {
  id: string;
  title: string;
  snippet?: string;
  modified?: string;
  linked_to?: "note" | "sermon";
  item_id?: string;
}

export interface AnyPages {
  space_id: string;
  pages: AnyPage[];
  total: number;
  next_offset?: number;
}

export type Resolve = "glossa" | "any" | "copy";

// menuLabel: the entry in a note's or sermon's ⋯ menu.
export function menuLabel(linked: boolean): string {
  return linked ? "Update in Any" : "Send to Any";
}

// notReady: what a click says when Any is not turned on or signed in yet.
export const NOT_READY = "Turn on Any and pair Glossa with it in Settings → Connections first.";

export function conflictHeading(state: SyncState, what: string): string {
  return state === "any_changed" ? `The page in Any has newer changes than this ${what}` : `This ${what} changed in both Glossa and Any`;
}

export const CHOICES: { resolve: Resolve; label: string; hint: string }[] = [
  { resolve: "glossa", label: "Keep Glossa’s version", hint: "The page in Any is replaced." },
  { resolve: "any", label: "Keep Any’s version", hint: "What is here is replaced." },
  { resolve: "copy", label: "Keep both", hint: "Any’s version is saved as a new note, and Glossa’s goes to Any." },
];

// whatIs: "note" or "sermon", as a person says it.
export function whatIs(kind: "note" | "sermon"): string {
  return kind === "note" ? "note" : "sermon";
}

// pageLine: the small line under a page in the picker.
export function pageLine(p: AnyPage): string {
  const bits: string[] = [];
  if (p.linked_to) bits.push(p.linked_to === "note" ? "Already a note" : "Already a sermon");
  if (p.snippet) bits.push(p.snippet.replace(/[*_#`>]+/g, "").replace(/\s+/g, " ").trim().slice(0, 120));
  return bits.join(" · ");
}
