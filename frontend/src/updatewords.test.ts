import { describe, expect, it } from "vitest";
import { CHANNELS, NO_KEY_LINE, channelHint, installFailure, lastCheckedText, lastErrorText, noticeHelp, noticeTitle, shortNotes, updateAction, versionLine } from "./updatewords";

const now = new Date("2026-11-12T12:00:00Z");

describe("lastCheckedText", () => {
  it("says so when there was no check", () => {
    expect(lastCheckedText(undefined, now)).toBe("Not checked yet.");
    expect(lastCheckedText("nonsense", now)).toBe("Not checked yet.");
  });
  it("counts minutes and hours", () => {
    expect(lastCheckedText("2026-11-12T12:00:00Z", now)).toBe("Last checked just now.");
    expect(lastCheckedText("2026-11-12T11:59:00Z", now)).toBe("Last checked 1 minute ago.");
    expect(lastCheckedText("2026-11-12T11:15:00Z", now)).toBe("Last checked 45 minutes ago.");
    expect(lastCheckedText("2026-11-12T09:00:00Z", now)).toBe("Last checked 3 hours ago.");
  });
  it("gives a date after a day", () => {
    expect(lastCheckedText("2026-11-01T09:00:00Z", now)).toMatch(/^Last checked on .*2026\.$/);
  });
});

describe("versionLine", () => {
  const info = { enabled: true, channel: "stable" as const, current: "1.0.0", latest: "1.1.0", key_set: true };
  it("names a newer version", () => {
    expect(versionLine(info, true)).toBe("Version 1.1.0 is available. You have 1.0.0.");
  });
  it("otherwise just the current one", () => {
    expect(versionLine(info, false)).toBe("You have version 1.0.0.");
  });
});

describe("updateAction", () => {
  it("installs on Windows and downloads on macOS", () => {
    expect(updateAction(false)).toBe("Install and restart");
    expect(updateAction(undefined)).toBe("Install and restart");
    expect(updateAction(true)).toBe("Download");
  });
  it("only opens the download page while the build has no update key", () => {
    expect(updateAction(false, true)).toBe("Open the download page");
    expect(noticeHelp(false, true)).toContain("download page");
    expect(noticeHelp(false, true)).not.toContain("installs in a moment");
    expect(NO_KEY_LINE).toBe("This build does not have its update key yet, so it cannot install updates.");
  });
  it("says an install failure in a plain sentence, without codes", () => {
    expect(installFailure("update.install: the signature does not match, so it may not come from Glossa and nothing was downloaded")).toBe("Glossa could not install the update: the signature does not match, so it may not come from Glossa and nothing was downloaded.");
    expect(installFailure("could not start the installer: access denied.")).toBe("Glossa could not install the update: could not start the installer: access denied.");
  });
});

describe("the notice", () => {
  it("names the version and what the button does", () => {
    expect(noticeTitle("0.5.0-alpha.2")).toBe("Glossa 0.5.0-alpha.2 is ready.");
    expect(noticeTitle("0.5.0-alpha.2", true)).toBe("Glossa 0.5.0-alpha.2 is out.");
    expect(noticeHelp(false)).toContain("opens again");
    expect(noticeHelp(true)).toContain("disk image");
    expect(noticeHelp(false)).toContain("library is not touched");
  });
  it("shortens release notes to a few plain lines", () => {
    expect(shortNotes(undefined)).toBe("");
    expect(shortNotes("## What is new\n- A **bold** fix\n- `Code` too\n- Third\n- Fourth")).toBe("A bold fix\nCode too\nThird");
    expect(shortNotes("x".repeat(400)).length).toBe(220);
    expect(shortNotes("x".repeat(400)).endsWith("…")).toBe(true);
    expect(shortNotes("\r\nOne\r\n\r\nTwo\r\n")).toBe("One\nTwo");
  });
});

describe("channels", () => {
  it("offers Stable and Preview, in plain words", () => {
    expect(CHANNELS).toEqual([["stable", "Stable"], ["preview", "Preview"]]);
    expect(channelHint("stable")).toContain("finished releases");
    expect(channelHint("preview")).toContain("test versions");
  });
});

describe("lastErrorText", () => {
  it("says nothing when the last check worked", () => {
    expect(lastErrorText(undefined)).toBe("");
    expect(lastErrorText("")).toBe("");
  });
  it("says the check did not work, and why", () => {
    expect(lastErrorText("api.github.com answered 500 Internal Server Error")).toBe("The last check did not work: api.github.com answered 500 Internal Server Error.");
  });
});
