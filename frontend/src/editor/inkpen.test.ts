import { describe, expect, it } from "vitest";
import { restorePen } from "./inkpen";

describe("the remembered pen", () => {
  it("starts with a medium black pen", () => {
    expect(restorePen(null)).toEqual({ tool: "pen", colour: "ink", width: 2 });
    expect(restorePen("junk")).toEqual({ tool: "pen", colour: "ink", width: 2 });
  });
  it("brings back the tool, colour and width", () => {
    expect(restorePen({ tool: "hl", colour: "green", width: 3 })).toEqual({ tool: "hl", colour: "green", width: 3 });
  });
  it("never starts on the eraser, and drops values it does not know", () => {
    expect(restorePen({ tool: "erase", colour: "teal", width: 9 })).toEqual({ tool: "pen", colour: "ink", width: 2 });
  });
});
