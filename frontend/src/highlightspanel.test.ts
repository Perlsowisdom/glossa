import { describe, expect, it } from "vitest";
import { noteMatches } from "./highlightspanel";

describe("searching the notes listed with a book's highlights", () => {
  const note = { title: "On grace", body: "Paul writes about Mercy and peace" };
  it("matches every word typed, in the title or the words, whatever the case", () => {
    expect(noteMatches(note, "")).toBe(true);
    expect(noteMatches(note, "grace mercy")).toBe(true);
    expect(noteMatches(note, "PEACE")).toBe(true);
  });
  it("leaves out a note missing any word", () => {
    expect(noteMatches(note, "grace wrath")).toBe(false);
    expect(noteMatches({}, "grace")).toBe(false);
  });
});
