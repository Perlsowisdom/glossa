import { describe, expect, it } from "vitest";
import { CHOICES, ObsidianExport, ObsidianImport, conflictHeading, exportText, importText, lastText, menuLabel, resolvedText, sentText, vaultText } from "./obsidianwords";

const ex: ObsidianExport = { folder: "Glossa", written: 3, updated: 1, unchanged: 2, obsidian_newer: 0, items: [], conflicts: [], skipped: [], message: "" };
const im: ObsidianImport = { folder: "Glossa", files: 4, created: 2, updated: 1, unchanged: 1, glossa_newer: 0, links: 0, embeds: 0, conflicts: [], skipped: [], notes: [], message: "" };
const conflict = { kind: "note" as const, id: "n", path: "Glossa/a.md", title: "A", obsidian_title: "A", your_excerpt: "", obsidian_excerpt: "" };

describe("exportText", () => {
  it("says what was written and where", () => {
    expect(exportText(ex)).toBe("3 files made, 1 file updated, 2 already up to date in “Glossa”.");
  });
  it("says so when nothing was written", () => {
    expect(exportText({ ...ex, written: 0, updated: 0, unchanged: 0 })).toBe("Nothing was written.");
  });
  it("mentions files changed in Obsidian, conflicts and skipped items", () => {
    const t = exportText({ ...ex, obsidian_newer: 1, conflicts: [conflict], skipped: [{ path: "Glossa/x.md", reason: "it is not valid UTF-8 text" }] });
    expect(t).toContain("1 file has newer changes made in Obsidian and was left alone. Import brings it in.");
    expect(t).toContain("1 note changed in both Glossa and Obsidian and was left as it is. Choose what to do below.");
    expect(t).toContain("1 item was skipped: Glossa/x.md (it is not valid UTF-8 text).");
  });
  it("uses no command names or codes", () => {
    expect(exportText({ ...ex, obsidian_newer: 2, conflicts: [conflict, conflict] })).not.toMatch(/obsidian\.|_[a-z]+|glossa_/);
  });
});

describe("importText", () => {
  it("counts notes", () => {
    expect(importText(im)).toBe("2 notes made, 1 updated, 1 already up to date.");
  });
  it("explains links, embeds and newer notes", () => {
    const t = importText({ ...im, links: 2, embeds: 1, glossa_newer: 2 });
    expect(t).toContain("2 [[links]] were turned into plain words.");
    expect(t).toContain("1 embed (![[…]]) was left as text");
    expect(t).toContain("2 notes have newer changes in Glossa and were left alone. Export sends them.");
  });
  it("says when the folder is empty", () => {
    expect(importText({ ...im, files: 0 })).toBe("There are no Markdown files in “Glossa”.");
  });
});

describe("the rest", () => {
  it("says which vault and folder", () => {
    expect(vaultText({ subfolder: "Glossa", linked: 0, conflicts: [] })).toBe("No vault chosen yet.");
    expect(vaultText({ vault: "C:\\Vault", subfolder: "Notes/Glossa", linked: 0, conflicts: [] })).toBe("Vault: C:\\Vault. Notes and sermons go to the “Notes/Glossa” folder in it.");
  });
  it("says when it last ran", () => {
    const t = lastText({ linked: 2, last_export: { at: "2026-03-03T10:00:00Z", written: 1, unchanged: 3 }, last_import: { at: "2026-03-04T09:30:00Z", created: 2, updated: 0, unchanged: 1 } }, "UTC");
    expect(t).toBe("Last exported 3 March 2026, 10:00: 1 file written, 3 already up to date. Last imported 4 March 2026, 09:30: 2 notes made, 0 updated, 1 already up to date. 2 notes and sermons are linked to files.");
    expect(lastText({ linked: 0 })).toBe("Nothing exported yet. Nothing imported yet.");
  });
  it("offers the three choices", () => {
    expect(CHOICES.map((c) => c.choice)).toEqual(["obsidian", "glossa", "both"]);
    for (const c of CHOICES) expect(resolvedText(c.choice)).not.toBe("");
  });
  it("names the menu entry by whether the file exists", () => {
    expect(menuLabel(false)).toBe("Send to Obsidian");
    expect(menuLabel(true)).toBe("Update in Obsidian");
    expect(conflictHeading("sermon")).toBe("This sermon changed in both Glossa and Obsidian");
    expect(sentText("written", "Glossa/a.md")).toBe("Sent to Obsidian as Glossa/a.md.");
    expect(sentText("obsidian_newer")).toContain("newer changes");
  });
});
