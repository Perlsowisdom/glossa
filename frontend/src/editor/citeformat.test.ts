import { describe, expect, it } from "vitest";
import { citeMarkdown, parseCite, type CiteData } from "./citeformat";

describe("citation Markdown", () => {
  const back = "\x5c";
  const cases: CiteData[] = [
    { key: "lib:01HXABC" },
    { key: "lib:01HXABC", label: "page", locator: "12" },
    { key: "lib:01HXABC", label: "page", locator: "12-14" },
    { key: "zotero:ABCD2345", label: "chapter", locator: "3" },
    { key: "zotero:ABCD2345", label: "verse", locator: "5", prefix: "see also", suffix: "for more, and more" },
    { key: "manual:01HX", suppress: true },
    { key: "manual:01HX", suffix: "and later" },
    { key: "bible:kjv", locator: "Rom 8:28-30" },
    { key: "bible:kjv", locator: "Gen 1:1", prefix: "cf." },
    { key: "lib:01HXABC", label: "page", locator: "7", prefix: "[odd] @ words", suffix: `a [bracket] and a back${back}slash` },
  ];
  for (const c of cases) {
    it(citeMarkdown(c), () => {
      const md = citeMarkdown(c);
      expect(parseCite(md + " after")).toEqual({ cite: c, len: md.length });
    });
  }
  it("writes a range of pages as pp.", () => {
    expect(citeMarkdown({ key: "lib:A", label: "page", locator: "1,3" })).toBe("[@lib:A, pp. 1,3]");
    expect(citeMarkdown({ key: "lib:A", locator: "9" })).toBe("[@lib:A, p. 9]");
  });
  it("is not fooled by links and plain brackets", () => {
    for (const s of ["[text](#passage/Rom.8.28)", "[@nokind:abc]", "[see @lib:ABC](http://x)", "[plain]", "[ @lib:ABC]", "[@lib:]"]) {
      expect(parseCite(s)).toBeNull();
    }
  });
  it("reads the label words people write", () => {
    expect(parseCite("[@lib:A, sec. 4]")!.cite).toMatchObject({ label: "section", locator: "4" });
    expect(parseCite("[@lib:A, vv. 4-6, so]")!.cite).toMatchObject({ label: "verse", locator: "4-6", suffix: "so" });
    expect(parseCite("[@lib:A, fig. 2]")!.cite).toMatchObject({ label: "figure" });
  });
});
