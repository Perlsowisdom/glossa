import { describe, expect, it } from "vitest";
import { backNotice, binNotice, forGoodNotice, forGoodQuestion } from "./deletewords";

describe("delete words", () => {
  it("says where a thing went, in the same words for every kind", () => {
    expect(binNotice("Easter")).toBe('"Easter" went to the bin.');
    expect(binNotice("Notes", "and its 2 notes")).toBe('"Notes" and its 2 notes went to the bin.');
    expect(backNotice("Easter")).toBe('"Easter" is back.');
  });
  it("says plainly that what the bin cannot hold is deleted for good", () => {
    expect(forGoodQuestion("90 days", "Reading plans")).toBe('Delete "90 days" for good? Reading plans are not kept in the bin, so it cannot be brought back.');
    expect(forGoodNotice("90 days")).toBe('"90 days" was deleted for good.');
  });
});
