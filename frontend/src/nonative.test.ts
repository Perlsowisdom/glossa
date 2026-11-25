import { describe, expect, it } from "vitest";

// The app asks and tells in its own dialogs and notices. The browser's confirm(), alert() and prompt() boxes
// look foreign, block the window and cannot be tested, so none may come back (use ask.ts or a Message).
const files = import.meta.glob(["./**/*.ts", "!./**/*.test.ts"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;

describe("native dialogs", () => {
  it("never appear in the interface's source", () => {
    expect(Object.keys(files).length).toBeGreaterThan(50);
    const hits: string[] = [];
    for (const [file, text] of Object.entries(files)) {
      text.split("\n").forEach((line, i) => {
        if (/(^|[^\w.])(window\.)?(confirm|alert|prompt)\s*\(/.test(line.replace(/\/\/.*$/, ""))) hits.push(`${file}:${i + 1}`);
      });
    }
    expect(hits).toEqual([]);
  });
});
