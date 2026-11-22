import { describe, expect, it } from "vitest";
import { citeWords, matchStyles, styleKind } from "./citewords";

describe("citation words", () => {
  it("names the two kinds of style in plain words", () => {
    expect(styleKind("note")).toBe("Footnotes");
    expect(styleKind("in-text")).toBe("Author and date, or numbers");
  });
  it("finds styles by the words of their title or id", () => {
    const list = [{ id: "apa", title: "APA Style 7th edition" }, { id: "chicago-author-date", title: "Chicago Manual of Style 18th edition (author-date)" },
      { id: "nature", title: "Nature" }];
    expect(matchStyles(list, "chicago author").map((s) => s.id)).toEqual(["chicago-author-date"]);
    expect(matchStyles(list, "STYLE").map((s) => s.id)).toEqual(["apa", "chicago-author-date"]);
    expect(matchStyles(list, "")).toHaveLength(3);
    expect(matchStyles(list, "zz")).toEqual([]);
  });
  it("offers the kinds of source a person can type in", () => {
    expect(citeWords.manualTypes.map(([v]) => v)).toContain("article-journal");
    expect(new Set(citeWords.manualTypes.map(([v]) => v)).size).toBe(citeWords.manualTypes.length);
  });
});
