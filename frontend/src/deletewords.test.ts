import { describe, expect, it } from "vitest";
import { backNotice, binNotice } from "./deletewords";

describe("delete words", () => {
  it("says where a thing went, in the same words for every kind", () => {
    expect(binNotice("Easter")).toBe('"Easter" went to the bin.');
    expect(binNotice("Notes", "and its 2 notes")).toBe('"Notes" and its 2 notes went to the bin.');
    expect(backNotice("Easter")).toBe('"Easter" is back.');
  });
});
