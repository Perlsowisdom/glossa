import { describe, expect, it } from "vitest";
import { CHOICES, ReadwiseResult, importText, lastImportText, linkedText, previewText, resolvedText } from "./readwisewords";

const base: ReadwiseResult = {
  preview: true, full: true, sources: 4, highlights: 6, matched: 1, ambiguous: 0, notes_new: 4, notes_new_on_book: 1, notes_updated: 0,
  notes_unchanged: 0, highlights_new: 6, highlights_changed: 0, highlights_gone: 0, skipped: 0, conflicts: [], message: "",
};

describe("previewText", () => {
  it("says what is there, what matches and what an import would do", () => {
    expect(previewText(base)).toBe("Readwise has 6 highlights from 4 sources. 1 source matches a book in your library by title and author; the highlights of that book go on the book. Importing would make 4 notes.");
  });
  it("says so when nothing matches", () => {
    expect(previewText({ ...base, matched: 0 })).toContain("None of them match a book in your library by title and author.");
  });
  it("is quiet when nothing has changed", () => {
    expect(previewText({ ...base, full: false, sources: 0, highlights: 0 })).toBe("Nothing in Readwise has changed since the last import.");
    expect(previewText({ ...base, sources: 0, highlights: 0 })).toBe("Readwise has no highlights to bring in.");
  });
  it("says only changes are counted after the first import", () => {
    expect(previewText({ ...base, full: false, sources: 1, highlights: 1, matched: 0, notes_new: 0, notes_updated: 1 }))
      .toContain("1 highlight from 1 source, new or changed since the last import. None of them match a book in your library by title and author. Importing would update 1 note.");
  });
  it("mentions books that fit more than once, and edited notes", () => {
    const t = previewText({ ...base, ambiguous: 2, conflicts: [{ source_id: "1", note_id: "n", title: "T", your_excerpt: "", readwise_excerpt: "" }] });
    expect(t).toContain("2 sources fit more than one book, so they stay separate notes.");
    expect(t).toContain("1 note you edited has newer highlights in Readwise; an import would leave it as it is.");
  });
  it("uses no command names or codes", () => {
    expect(previewText(base)).not.toMatch(/readwise\.|_[a-z]+|item_id/);
  });
});

describe("importText", () => {
  it("says what was made and updated", () => {
    expect(importText({ ...base, preview: false, notes_unchanged: 2, notes_updated: 1, highlights_gone: 1 }))
      .toBe("Imported 6 highlights from 4 sources. 4 notes made, 1 note updated, 2 notes already up to date. 1 highlight was taken out of their notes.");
  });
  it("is plain when nothing changed", () => {
    expect(importText({ ...base, preview: false, full: false, sources: 0, highlights: 0, notes_new: 0 })).toBe("Nothing in Readwise has changed since the last import.");
  });
  it("tells about left-alone notes", () => {
    expect(importText({ ...base, skipped: 1 })).toContain("1 source was left alone because you deleted the note or chose to keep your own.");
  });
});

describe("lastImportText", () => {
  it("says when and how much", () => {
    expect(lastImportText({ at: "2026-03-03T10:05:00Z", sources: 4, highlights: 6, notes_new: 4, notes_updated: 0 }, "UTC"))
      .toBe("Last imported 3 March 2026, 10:05: 6 highlights from 4 sources, 4 notes made and 0 updated.");
  });
  it("explains the first import before there is one", () => {
    expect(lastImportText(undefined)).toContain("Not imported yet.");
  });
});

describe("the choices", () => {
  it("name three plain outcomes", () => {
    expect(CHOICES.map((c) => c.label)).toEqual(["Use Readwise’s text", "Keep mine", "Keep both"]);
    expect(resolvedText("both")).toContain("new note");
    expect(linkedText(1)).toBe("1 source has a note in Glossa.");
    expect(linkedText(3)).toBe("3 sources have a note in Glossa.");
    expect(linkedText(0)).toBe("");
  });
});

import { excerptText, plainText, shortText } from "./readwisewords";

describe("plainText", () => {
  it("shows a highlight as readable words", () => {
    const md = "---\n\n> Now I saw in my dream.\n\nLocation 40 · [Open in Readwise](https://readwise.io/open/1022)";
    expect(plainText(md)).toBe("Now I saw in my dream.\n\nLocation 40 · Open in Readwise");
  });
  it("drops the header's emphasis and links, rules and quote marks", () => {
    expect(plainText("*[From Readwise](https://readwise.io/bookreview/3)* · *[Original](https://x.org/a)*\n\n---\n\n> A **bold** word\n> and more"))
      .toBe("From Readwise · Original\n\nA bold word\nand more");
  });
  it("keeps ordinary asterisks and underscores in words", () => {
    expect(plainText("2 * 3 and snake_case_name")).toBe("2 * 3 and snake_case_name");
  });
  it("leaves nothing but the text", () => {
    expect(plainText("")).toBe("");
    expect(plainText("> a\n\n\n\n> b")).toBe("a\n\nb");
    expect(plainText("a\n\n***\n\nb")).toBe("a\n\nb");
  });
});

describe("shortText and excerptText", () => {
  it("cuts at a word and says so", () => {
    expect(shortText("one two three four five", 12)).toBe("one two…");
    expect(shortText("short", 12)).toBe("short");
  });
  it("keeps the leading mark for text that starts later", () => {
    expect(excerptText("… > one two three four five six seven", 14)).toBe("… one two three…");
    expect(excerptText("> plain [link](http://a.b)")).toBe("plain link");
  });
});
