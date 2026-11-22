import { describe, expect, it } from "vitest";
import { CHOICES, conflictHeading, menuLabel, pageLine } from "./anywords";

describe("the Any words", () => {
  it("names the menu entry by whether the page exists", () => {
    expect(menuLabel(false)).toBe("Send to Any");
    expect(menuLabel(true)).toBe("Update in Any");
  });
  it("asks the right question", () => {
    expect(conflictHeading("both_changed", "note")).toBe("This note changed in both Glossa and Any");
    expect(conflictHeading("any_changed", "sermon")).toContain("newer changes");
  });
  it("offers the three choices", () => {
    expect(CHOICES.map((c) => c.resolve)).toEqual(["glossa", "any", "copy"]);
  });
  it("marks pages already brought in", () => {
    expect(pageLine({ id: "1", title: "t", snippet: "a  b", linked_to: "note" })).toBe("Already a note · a b");
    expect(pageLine({ id: "1", title: "t" })).toBe("");
  });
});
