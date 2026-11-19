import { describe, expect, it } from "vitest";
import { KeyStore, migrateStorage } from "./storagemigrate";

function memory(initial: Record<string, string> = {}): KeyStore & { data: Record<string, string> } {
  const data = { ...initial };
  return {
    data,
    getItem: (k) => (k in data ? data[k] : null),
    setItem: (k, v) => void (data[k] = String(v)),
    removeItem: (k) => void delete data[k],
    key: (i) => Object.keys(data)[i] ?? null,
    get length() { return Object.keys(data).length; },
  };
}

describe("storage migration", () => {
  it("moves xegesis.* settings to glossa.* and leaves other keys alone", () => {
    const s = memory({ "xegesis.theme": "dark", "xegesis.ref": "John 3", "other": "x" });
    expect(migrateStorage(s)).toBe(2);
    expect(s.data).toEqual({ "glossa.theme": "dark", "glossa.ref": "John 3", other: "x", "glossa.storageMigrated": "1" });
  });

  it("does not overwrite a value already saved under the new name", () => {
    const s = memory({ "xegesis.theme": "dark", "glossa.theme": "light" });
    migrateStorage(s);
    expect(s.data["glossa.theme"]).toBe("light");
    expect(s.data["xegesis.theme"]).toBeUndefined();
  });

  it("runs once", () => {
    const s = memory({ "xegesis.theme": "dark" });
    migrateStorage(s);
    s.setItem("xegesis.late", "1");
    expect(migrateStorage(s)).toBe(0);
    expect(s.data["xegesis.late"]).toBe("1");
  });

  it("copes with storage that throws", () => {
    const bad = { getItem: () => { throw new Error("blocked"); }, setItem: () => {}, removeItem: () => {}, key: () => null, length: 0 } as KeyStore;
    expect(migrateStorage(bad)).toBe(0);
  });
});
