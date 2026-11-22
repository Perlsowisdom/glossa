// The plain words the citation screens use, kept apart from the screens so they are tested on their own
// (citewords.test.ts).

export const citeWords = {
  // What a source added by hand can be, as [what the command takes, what the person reads].
  manualTypes: [["book", "Book"], ["chapter", "Chapter in a book"], ["article-journal", "Journal article"], ["webpage", "Web page"],
    ["thesis", "Thesis"], ["report", "Report"]] as [string, string][],
};

// styleKind says in plain words how a style cites: the two classes of CSL.
export function styleKind(cls: string): string {
  return cls === "note" ? "Footnotes" : "Author and date, or numbers";
}

export interface StyleLike {
  id: string;
  title: string;
  class: string;
  source: string;
}

// matchStyles keeps the styles whose title has every word typed, in the order given.
export function matchStyles<T extends { title: string; id: string }>(styles: T[], query: string): T[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  return styles.filter((s) => words.every((w) => `${s.title} ${s.id}`.toLowerCase().includes(w)));
}

// The sentence beside the switch for downloading styles; the command says it too (cite.styles_online),
// and docs/privacy.md in full.
export const STYLES_SENDS = "Asks GitHub (github.com/citation-style-language/styles) for the list of style names, and for a style’s file when you download it. "
  + "It sends nothing about you, your documents or what you search for: the words are matched on this computer.";
