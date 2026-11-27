import { describe, expect, it } from "vitest";
import { HEAD_NOTE, ignorable, reasonText, setNote, shortStack } from "./problemwords";

const report = `${HEAD_NOTE}\n\n\n\n## Glossa\n\n- Version: 1\n\n## Recent log\n\nline\n`;

describe("setNote", () => {
  it("writes the note under its heading and keeps the rest", () => {
    const out = setNote(report, "I pressed Save");
    expect(out).toBe(`${HEAD_NOTE}\n\nI pressed Save\n\n## Glossa\n\n- Version: 1\n\n## Recent log\n\nline\n`);
  });
  it("replaces an earlier note", () => {
    const once = setNote(report, "first");
    expect(setNote(once, "second")).toBe(setNote(report, "second"));
  });
  it("empties the part for an empty note", () => {
    expect(setNote(setNote(report, "x"), "  ")).toBe(`${HEAD_NOTE}\n\n## Glossa\n\n- Version: 1\n\n## Recent log\n\nline\n`);
  });
  it("leaves the text alone when the heading was deleted", () => {
    expect(setNote("my own text", "x")).toBe("my own text");
  });
  it("handles a note part that is the last thing in the text", () => {
    expect(setNote(`${HEAD_NOTE}\n\nold`, "new")).toBe(`${HEAD_NOTE}\n\nnew\n`);
  });
});

describe("ignorable", () => {
  it("skips the browser's own notes", () => {
    expect(ignorable("ResizeObserver loop completed with undelivered notifications.")).toBe(true);
    expect(ignorable("Script error.")).toBe(true);
    expect(ignorable("Cannot read properties of undefined")).toBe(false);
  });
});

describe("shortStack and reasonText", () => {
  it("keeps the first lines", () => {
    expect(shortStack("a\nb  \nc", 2)).toBe("a\nb");
    expect(shortStack(undefined)).toBe("");
  });
  it("reads any rejection", () => {
    expect(reasonText(new Error("boom"))).toBe("boom");
    expect(reasonText("plain")).toBe("plain");
    expect(reasonText({ a: 1 })).toBe('{"a":1}');
    expect(reasonText(undefined)).toBe("undefined");
  });
});
