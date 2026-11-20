import { describe, expect, it } from "vitest";
import { canImport, kindLine, reportLine, reportSummary, runLine, sampleStatus, totalToImport, unmappedLine, LogosPreview } from "./logoswords";

const kind = { kind: "note", label: "Notes", found: 12, new: 10, changed: 0, imported: 2, samples: [] };

describe("kindLine", () => {
  it("counts new and already imported", () => {
    expect(kindLine(kind)).toBe("Notes: 12 found, 10 new, 2 already imported");
  });
  it("names what changed in Logos", () => {
    expect(kindLine({ ...kind, new: 8, changed: 2, imported: 2 })).toBe("Notes: 12 found, 8 new, 2 changed in Logos, 2 already imported");
  });
  it("says when there is none", () => {
    expect(kindLine({ ...kind, found: 0, new: 0, imported: 0 })).toBe("Notes: none to import");
  });
});

describe("unmappedLine", () => {
  it("gives the reason in lower case with examples", () => {
    expect(unmappedLine({ kind: "highlight", label: "Highlights", reason: "A highlight in a book, not in a Bible", count: 3, examples: ["Lexicon", "Commentary"] }))
      .toBe("Highlights, 3 left out: a highlight in a book, not in a Bible (for example Lexicon; Commentary)");
  });
  it("works without examples", () => {
    expect(unmappedLine({ kind: "note", label: "Notes", reason: "An empty note", count: 1 })).toBe("Notes, 1 left out: an empty note");
  });
});

describe("import totals", () => {
  const p: LogosPreview = { source: "x", kinds: [kind, { ...kind, new: 0, imported: 5 }], folders: 0, unmapped: [], notes: [] };
  it("counts new and changed", () => {
    expect(totalToImport(p)).toBe(10);
    expect(canImport(p)).toBe(true);
    expect(canImport({ ...p, kinds: [{ ...kind, new: 0, changed: 0 }] })).toBe(false);
  });
});

describe("report", () => {
  const k = { kind: "note", label: "Notes", created: 4, updated: 1, skipped: 2, failed: 0 };
  it("says what happened to each kind", () => {
    expect(reportLine(k)).toBe("Notes: 4 added, 1 changed, 2 left as they were");
    expect(reportLine({ ...k, updated: 0, skipped: 0, failed: 1 })).toBe("Notes: 4 added, 1 failed");
  });
  it("sums up", () => {
    expect(reportSummary({ run_id: "r", kinds: [k], folders: 2, unmapped: [], notes: [] })).toBe("Imported 4 items and 2 folders, and changed 1 item that had changed in Logos.");
    expect(reportSummary({ run_id: "r", kinds: [{ ...k, created: 0, updated: 0 }], folders: 0, unmapped: [], notes: [] })).toBe("Nothing new to import: everything was already here.");
  });
});

describe("sampleStatus and runLine", () => {
  it("is quiet for new things", () => {
    expect(sampleStatus({ title: "a", status: "new" })).toBe("");
    expect(sampleStatus({ title: "a", status: "imported" })).toBe("already imported");
  });
  it("describes a run", () => {
    expect(runLine({ id: "1", started_at: 0, added: 1, undone: true })).toMatch(/: taken back$/);
    expect(runLine({ id: "1", started_at: 0, added: 1, undone: false })).toMatch(/: 1 item added$/);
  });
});
