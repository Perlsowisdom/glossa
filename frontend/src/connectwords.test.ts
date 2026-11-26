import { describe, expect, it } from "vitest";
import { ConnectionInfo, failureText, howItSignsIn, keyShownByDefault, keyState, statusOf, switchHint, switchLabel, whatItDoes } from "./connectwords";

const zotero: ConnectionInfo = {
  id: "zotero", name: "Zotero", sign_in: "app", capabilities: ["import", "cite"], enabled: false, key_set: false,
  sends: "Glossa asks the Zotero app on this computer for your sources.",
  key: { label: "Zotero key", help: "Make one at zotero.org.", required: false },
};

describe("whatItDoes", () => {
  it("says what the connection is for in plain words", () => {
    expect(whatItDoes(zotero)).toBe("Zotero lets you bring in your sources and cite them in your writing.");
    expect(whatItDoes({ ...zotero, capabilities: ["export"] })).toBe("Zotero lets you send your work out.");
    expect(whatItDoes({ ...zotero, capabilities: ["import", "export", "cite"] })).toBe("Zotero lets you bring in your sources, send your work out and cite them in your writing.");
    expect(whatItDoes({ ...zotero, capabilities: [] })).toBe("Zotero is a link to another program.");
  });
});

describe("howItSignsIn", () => {
  it("names what the person needs", () => {
    expect(howItSignsIn(zotero)).toBe("Works with the Zotero app on this computer. A key is only needed if the app is not there.");
    expect(howItSignsIn({ ...zotero, key: undefined })).toBe("Works with the Zotero app on this computer.");
    expect(howItSignsIn({ ...zotero, sign_in: "key" })).toBe("Needs a key from Zotero.");
    expect(howItSignsIn({ ...zotero, sign_in: "folder" })).toBe("Uses a folder on this computer.");
  });
});

describe("statusOf", () => {
  it("is Off until turned on, then follows the check", () => {
    expect(statusOf(zotero, undefined)).toEqual({ text: "Off", kind: "" });
    const on = { ...zotero, enabled: true };
    expect(statusOf(on, undefined).text).toBe("Checking…");
    expect(statusOf(on, { ok: true, message: "" })).toEqual({ text: "Connected", kind: "ok" });
    expect(statusOf(on, { ok: false, message: "" })).toEqual({ text: "Needs attention", kind: "warn" });
  });
});

describe("keys", () => {
  it("is one step away unless required", () => {
    expect(keyShownByDefault(zotero)).toBe(false);
    expect(keyShownByDefault({ ...zotero, key: { ...zotero.key!, required: true } })).toBe(true);
    expect(keyShownByDefault({ ...zotero, key: undefined })).toBe(false);
  });
  it("says whether one is saved, never what it is", () => {
    expect(keyState(zotero)).toBe("No Zotero key saved.");
    expect(keyState({ ...zotero, key_set: true })).toBe("A Zotero key is saved on this computer.");
    expect(keyState({ ...zotero, key: undefined })).toBe("");
  });
});

describe("the switch", () => {
  it("names the connection and says what is sent", () => {
    expect(switchLabel(zotero)).toBe("Use Zotero");
    expect(switchHint(zotero)).toBe("Glossa asks the Zotero app on this computer for your sources. Off until you turn it on.");
  });
});

describe("failureText", () => {
  it("shows the message without a code prefix", () => {
    expect(failureText({ info: { code: "off", message: "Zotero is turned off." } })).toBe("Zotero is turned off.");
    expect(failureText(new Error("unavailable: Zotero is not running."))).toBe("Zotero is not running.");
  });
});

describe("Readwise", () => {
  const readwise: ConnectionInfo = {
    id: "readwise", name: "Readwise", sign_in: "key", capabilities: ["import"], enabled: false, key_set: false,
    sends: "Glossa sends your Readwise token to readwise.io.", key: { label: "Readwise token", help: "Copy it from readwise.io/access_token.", required: true },
  };
  it("says what it does and what it needs in its own words", () => {
    expect(whatItDoes(readwise)).toBe("Readwise lets you bring in your highlights as notes, one for each book or article.");
    expect(howItSignsIn(readwise)).toBe("Needs your Readwise token.");
  });
  it("shows the token box straight away, and never the token", () => {
    expect(keyShownByDefault(readwise)).toBe(true);
    expect(keyState(readwise)).toBe("No Readwise token saved.");
    expect(switchLabel(readwise)).toBe("Use Readwise");
    expect(switchHint(readwise)).toBe("Glossa sends your Readwise token to readwise.io. Off until you turn it on.");
  });
});

describe("Obsidian", () => {
  const obsidian: ConnectionInfo = {
    id: "obsidian", name: "Obsidian", sign_in: "folder", capabilities: ["import", "export"], enabled: false, key_set: false,
    sends: "Glossa only reads and writes Markdown files in the Obsidian vault folder you choose; it never uses the network.",
  };
  it("says what it does and what it needs in its own words", () => {
    expect(whatItDoes(obsidian)).toBe("Obsidian lets you export notes and sermons as Markdown files into your vault, and bring the Markdown files in your vault in as notes.");
    expect(howItSignsIn(obsidian)).toBe("Needs the folder of your Obsidian vault on this computer.");
  });
  it("has no key box, and its privacy sentence says it never uses the network", () => {
    expect(keyShownByDefault(obsidian)).toBe(false);
    expect(keyState(obsidian)).toBe("");
    expect(switchHint(obsidian)).toContain("never uses the network. Off until you turn it on.");
  });
});
