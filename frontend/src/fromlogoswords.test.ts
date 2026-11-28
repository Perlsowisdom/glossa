import { describe, expect, it } from "vitest";
import { LogosBook, LogosLibrary, ariaSort, bulkLine, bulkPlan, countLine, freeText, nextSort, otherCopies, publicDomainText, refreshLine, sourceName, stateLabel, subline, summaryLine } from "./fromlogoswords";

const lib = (o: Partial<LogosLibrary> = {}): LogosLibrary => ({
  books: [], total: 0, refreshed: true, all: 4169, public_domain: 1750, certain: 180, likely: 132, free_copies: 312, already: 12, not_free: 3500, types: [], ...o,
});
const book = (o: Partial<LogosBook> = {}): LogosBook => ({
  resource_id: "LLS:KJV", title: "King James Version", type: "text.monograph.bible", type_label: "Bible", public_domain: true, link: "logosres:LLS:KJV", ...o,
});

describe("summaryLine", () => {
  it("says how many of the books have a free copy, the coverage", () => {
    expect(summaryLine(lib(), "20 Nov 2026")).toBe("312 of your 4,169 Logos books have a free copy · 12 already in Glossa · 1,750 public domain · read 20 Nov 2026");
  });
  it("leaves out what is zero", () => {
    expect(summaryLine(lib({ all: 1, public_domain: 0, free_copies: 0, already: 0 }), "")).toBe("1 Logos book, none with a free copy found yet");
    expect(summaryLine(lib({ all: 1, free_copies: 1, already: 0, public_domain: 0 }), "")).toBe("1 of your 1 Logos book has a free copy");
  });
});

describe("countLine", () => {
  it("says how many are shown", () => {
    expect(countLine(100, 4169, 4169)).toBe("Showing 100 of 4,169.");
    expect(countLine(3, 3, 4169)).toBe("3 books.");
    expect(countLine(0, 0, 4169)).toBe("No books match.");
    expect(countLine(0, 0, 0)).toBe("");
  });
});

describe("refreshLine", () => {
  it("reports what changed", () => {
    const r = { books: 10, added: 1, changed: 2, removed: 0, public_domain: 3, source: "x", refreshed_at: 1 };
    expect(refreshLine(r)).toBe("Read 10 books from Logos: 1 new, 2 changed.");
    expect(refreshLine({ ...r, added: 0, changed: 0 })).toBe("Read 10 books from Logos, no changes since the last time.");
  });
});

describe("labels", () => {
  it("says why a book is public domain and how sure a match is", () => {
    expect(publicDomainText(book())).toBe("Public domain");
    expect(publicDomainText(book({ pd_basis: "date" }))).toBe("Public domain, going by its date");
    expect(freeText({ source: "s", item: "i", name: "KJV", kind: "bible", confidence: "certain", installed: false })).toBe("Certain match: KJV");
    expect(freeText({ source: "s", item: "i", name: "KJV", kind: "bible", confidence: "likely", installed: false })).toBe("Possible match: check: KJV");
  });
  it("shows the short name under the title", () => {
    expect(subline(book({ abbreviation: "KJV", published: "1900" }))).toBe("KJV");
    expect(subline(book())).toBe("");
  });
});

describe("sorting", () => {
  it("sorts by a column and turns the order round on a second click", () => {
    expect(nextSort("author", "title", false)).toEqual({ sort: "author", desc: false });
    expect(nextSort("author", "author", false)).toEqual({ sort: "author", desc: true });
    expect(ariaSort("title", "title", true)).toBe("descending");
    expect(ariaSort("type", "title", true)).toBe("none");
  });
});

describe("sources", () => {
  const fc = (source: string, o = {}) => ({ source, item: "i", name: "Name", kind: "book", confidence: "certain" as const, installed: false, ...o });
  it("names the source in plain words", () => {
    expect(stateLabel(fc("ccel"))).toBe("Free copy on CCEL");
    expect(stateLabel(fc("gutenberg"))).toBe("Free copy on Project Gutenberg");
    expect(stateLabel(fc("crosswire"))).toBe("Free as a Bible module");
    expect(sourceName("ebible")).toBe("a Bible module");
  });
  it("lists the other free copies", () => {
    expect(otherCopies(undefined)).toBe("");
    expect(otherCopies([fc("gutenberg", { name: "Institutes" })])).toBe("Other free copies: Project Gutenberg: Institutes");
  });
  it("plans a bulk download of certain copies that are not in Glossa yet", () => {
    const b = (state: string, free: ReturnType<typeof fc>) => book({ state: state as "free", free });
    const plan = bulkPlan([b("free", fc("ccel")), b("free", fc("gutenberg", { confidence: "likely" })), b("already", fc("ccel", { installed: true })), b("free", fc("crosswire", { size: 2097152 }))]);
    expect(plan.length).toBe(2);
    expect(bulkLine(plan, 1)).toBe("2 books: 1 from CCEL, 1 from Bible modules; at least 2.0 MB (1 size is not known until the download). 1 possible match is left out: check those one at a time.");
    expect(bulkLine([plan[0]], 0)).toBe("1 book: 1 from CCEL; the sizes are not known until each download.");
  });
});
