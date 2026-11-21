import { describe, expect, it } from "vitest";
import { externalURL, followsLink } from "./linkclick";

describe("externalURL", () => {
  it("opens Logos links in Logos directly", () => {
    expect(externalURL("https://ref.ly/logosres/LLS:KJV")).toBe("logosres:LLS:KJV");
    expect(externalURL("https://ref.ly/logosres/LLS%3ABOOK1?ref=x")).toBe("logosres:LLS:BOOK1");
  });
  it("keeps web and mail links", () => {
    expect(externalURL("https://example.com/a")).toBe("https://example.com/a");
    expect(externalURL("mailto:a@example.com")).toBe("mailto:a@example.com");
  });
  it("refuses anything else", () => {
    expect(externalURL("javascript:alert(1)")).toBe("");
    expect(externalURL("logosres:LLS:KJV")).toBe("");
    expect(externalURL("#note/1")).toBe("");
  });
});

const base = { button: 0, shift: false, alt: false, hadSelection: false, hasSelection: false };
describe("followsLink", () => {
  it("follows on a plain click", () => expect(followsLink(base)).toBe(true));
  it("places the caret when text is selected, or with Shift or Alt", () => {
    expect(followsLink({ ...base, hadSelection: true })).toBe(false);
    expect(followsLink({ ...base, hasSelection: true })).toBe(false);
    expect(followsLink({ ...base, shift: true })).toBe(false);
    expect(followsLink({ ...base, alt: true })).toBe(false);
    expect(followsLink({ ...base, button: 1 })).toBe(false);
  });
});
