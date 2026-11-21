import { describe, expect, it } from "vitest";
import { LogosBook, LogosLibrary, ariaSort, countLine, freeText, nextSort, publicDomainText, refreshLine, subline, summaryLine } from "./fromlogoswords";

const lib = (o: Partial<LogosLibrary> = {}): LogosLibrary => ({
  books: [], total: 0, refreshed: true, all: 4169, public_domain: 1750, certain: 180, likely: 132, types: [], ...o,
});
const book = (o: Partial<LogosBook> = {}): LogosBook => ({
  resource_id: "LLS:KJV", title: "King James Version", type: "text.monograph.bible", type_label: "Bible", public_domain: true, link: "logosres:LLS:KJV", ...o,
});

describe("summaryLine", () => {
  it("counts the books, public domain and free copies", () => {
    expect(summaryLine(lib(), "20 Nov 2026")).toBe("4,169 books · 1,750 public domain · 312 with a free copy (180 certain, 132 likely) · read 20 Nov 2026");
  });
  it("leaves out what is zero", () => {
    expect(summaryLine(lib({ all: 1, public_domain: 0, certain: 0, likely: 0 }), "")).toBe("1 book");
    expect(summaryLine(lib({ likely: 0 }), "")).toBe("4,169 books · 1,750 public domain · 180 with a free copy");
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
    expect(freeText({ source: "s", item: "i", name: "KJV", kind: "bible", confidence: "likely", installed: false })).toBe("Likely match: KJV");
  });
  it("shows the short name and date under the title", () => {
    expect(subline(book({ abbreviation: "KJV", published: "1900" }))).toBe("KJV · 1900");
    expect(subline(book({ published: "n.d." }))).toBe("");
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
