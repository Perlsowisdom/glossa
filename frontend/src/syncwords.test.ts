import { describe, expect, it } from "vitest";
import { advice, countsText, indicator, lastSyncText, percent, previewSentences, progressText, sizeText } from "./syncwords";
import type { SyncPreview, SyncStatus } from "./syncwords";

const status = (o: Partial<SyncStatus> = {}): SyncStatus => ({
  configured: true, device_id: "d", device_name: "Desk", unlocked: true, password_saved: false, last_sync: Date.parse("2026-11-12T11:55:00Z"),
  pending_changes: 0, conflicts: 0, automatic: true, syncing: false, ...o,
});

describe("countsText", () => {
  it("names what is there, singular and plural", () => {
    expect(countsText({ notes: 3, highlights: 1 })).toBe("3 notes, 1 highlight");
    expect(countsText({ documents: 2 })).toBe("2 documents and sermons");
  });
  it("ignores bookkeeping tables and says nothing yet for none", () => {
    expect(countsText({ people: 4, settings: 1 })).toBe("nothing yet");
    expect(countsText(undefined)).toBe("nothing yet");
  });
});

describe("previewSentences", () => {
  const p: SyncPreview = {
    folder_has_data: true, devices: ["Laptop", "Phone"], this_device: { notes: 2 }, in_folder: { notes: 3 }, after_merge: { notes: 5 },
    to_this_device: { changes: 3, created: 3, updated: 0, deleted: 0, purged: 0, conflicts: 0 }, to_folder: 2,
  };
  it("compares this device, the folder and the result", () => {
    const s = previewSentences(p);
    expect(s.here).toBe("This device has 2 notes.");
    expect(s.folder).toBe("The folder has 3 notes, from Laptop and Phone.");
    expect(s.after).toBe("After syncing, this device and the folder will both have 5 notes.");
  });
  it("says so for a new folder", () => {
    expect(previewSentences({ ...p, folder_has_data: false, devices: null, in_folder: {} }).folder).toBe("The folder is new and has nothing in it yet.");
  });
});

describe("sizes and times", () => {
  it("sizeText", () => {
    expect(sizeText(0)).toBe("0 bytes");
    expect(sizeText(2048)).toBe("2 KB");
    expect(sizeText(5 << 20)).toBe("5.0 MB");
  });
  it("lastSyncText", () => {
    const now = new Date("2026-11-12T12:00:00Z");
    expect(lastSyncText(undefined, now)).toBe("Not synced yet");
    expect(lastSyncText(Date.parse("2026-11-12T11:55:00Z"), now)).toBe("5 minutes ago");
  });
  it("percent and progressText", () => {
    expect(percent({ bytes_done: 0, bytes_total: 0 })).toBe(0);
    expect(percent({ bytes_done: 50, bytes_total: 200 })).toBe(25);
    expect(progressText({ active: true, direction: "download", bytes_done: 1, bytes_total: 2, files_done: 0, files_total: 1 })).toBe("Downloading… 50%");
  });
});

describe("indicator", () => {
  const now = new Date("2026-11-12T12:00:00Z");
  it("is absent until sync is set up", () => {
    expect(indicator(status({ configured: false }), now)).toBeNull();
    expect(indicator(null, now)).toBeNull();
  });
  it("covers each state", () => {
    expect(indicator(status({ last_sync: undefined }), now)?.state).toBe("pending");
    expect(indicator(status({ unlocked: false }), now)?.state).toBe("locked");
    expect(indicator(status({ syncing: true }), now)?.state).toBe("syncing");
    expect(indicator(status({ last_error: "the disk is full" }), now)?.state).toBe("problem");
    expect(indicator(status({ automatic: false }), now)?.state).toBe("off");
    expect(indicator(status(), now)).toEqual({ state: "ok", short: "Synced", label: "Synced: 5 minutes ago" });
  });
});

describe("advice", () => {
  it("is given for known problems only", () => {
    expect(advice("folder_missing")).toMatch(/sync service/);
    expect(advice("whatever")).toBe("");
  });
});
