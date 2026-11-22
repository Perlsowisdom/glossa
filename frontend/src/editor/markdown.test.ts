import { describe, expect, it } from "vitest";
import { parseMarkdown, parsePasted, splitBlocks, joinBlocks, toMarkdown } from "./markdown";

const round = (md: string) => toMarkdown(parseMarkdown(md));

describe("blocks follow the canonical blank-line rules", () => {
  const cases: [string, string[]][] = [
    ["", []],
    ["a", ["a"]],
    ["a\n\nb", ["a", "b"]],
    ["a\nb", ["a\nb"]],
    ["a\n\n\nb", ["a", "", "b"]],
    ["a\n\n\n\nb", ["a", "", "", "b"]],
    ["\na", ["", "a"]],
    ["\n\na", ["", "", "a"]],
    ["a\n", ["a", ""]],
    ["\n", ["", ""]],
    ["\n\n", ["", "", ""]],
  ];
  for (const [md, blocks] of cases) {
    it(JSON.stringify(md), () => {
      expect(splitBlocks(md)).toEqual(blocks);
      expect(joinBlocks(blocks)).toBe(md);
    });
  }
});

describe("round trip", () => {
  const same = [
    "",
    "Plain text.",
    "Two paragraphs.\n\nSecond one.",
    "Line one\nline two in the same paragraph",
    "One\n\n\nafter an empty paragraph",
    "One\n\n\n\n\ntwo empty paragraphs",
    "\nstarts with an empty paragraph",
    "ends with one\n",
    "# Title\n\n## Sub\n\nText",
    "- a\n- b\n- c",
    "1. a\n2. b\n3. c",
    "> quoted\n> more",
    "Some **bold**, *italic*, `code` and [a link](https://example.com/x).",
    "***both*** and **bold with *italic* inside**",
    "[**bold link**](u)",
    "Verse: John 3:16 (ESV) – “love”",
    // Hand-written text the editor does not recognise stays exactly as it is.
    "* star list\n* second",
    "1. one\n3. three",
    "#nospace",
    "2 * 3 * 4",
    "snake_case_name and __dunder__",
    "- item\ncontinued",
    "```\ncode\n```",
    "<b>html</b>",
    "Trailing space ",
  ];
  for (const md of same) {
    it(JSON.stringify(md), () => expect(round(md)).toBe(md));
  }

  it("keeps empty paragraphs between blocks and at the edges", () => {
    const blocks = parseMarkdown("\n\nA\n\n\n\nB\n\n\n");
    expect(blocks.filter((b) => b.type === "paragraph" && b.spans.length === 0)).toHaveLength(7);
    expect(round("\n\nA\n\n\n\nB\n\n\n")).toBe("\n\nA\n\n\n\nB\n\n\n");
  });
});

describe("reading", () => {
  it("finds formatting", () => {
    const [b] = parseMarkdown("a **b** *c*");
    expect(b).toEqual({ type: "paragraph", spans: [
      { text: "a " }, { text: "b", bold: true }, { text: " " }, { text: "c", italic: true }] });
  });
  it("finds headings, lists and quotes", () => {
    expect(parseMarkdown("## H")[0]).toMatchObject({ type: "heading", level: 2 });
    expect(parseMarkdown("- a\n- b")[0]).toMatchObject({ type: "list", ordered: false });
    expect(parseMarkdown("1. a\n2. b")[0]).toMatchObject({ type: "list", ordered: true });
    expect(parseMarkdown("> a")[0]).toMatchObject({ type: "quote" });
  });
});

describe("underline, dividers and verse quotations", () => {
  it("round-trips them", () => {
    for (const md of [
      "Some <u>underlined</u> and **<u>bold underlined</u>** text",
      "Above\n\n---\n\nBelow",
      "> **28** And we know that all things work together for good.\n> — [Romans 8:28](#passage/Rom.8.28) (KJV). Public domain.",
      "> **1** In the beginning.\n>\n> — [Genesis 1:1](#passage/Gen.1.1) (WEB)",
    ]) expect(round(md)).toBe(md);
  });
  it("reads a credit line as a verse quotation", () => {
    const [b] = parseMarkdown("> **28** text\n> — [Rom 8:28](#passage/Rom.8.28) (KJV)");
    expect(b).toEqual({ type: "versequote", lines: ["**28** text", "— [Rom 8:28](#passage/Rom.8.28) (KJV)"] });
  });
  it("keeps a plain quote a quote", () => {
    expect(parseMarkdown("> just quoted\n> — said someone")[0]).toMatchObject({ type: "versequote" });
    expect(parseMarkdown("> just quoted\n> more")[0]).toMatchObject({ type: "quote" });
  });
  it("reads underline", () => {
    expect(parseMarkdown("<u>a</u>")[0]).toEqual({ type: "paragraph", spans: [{ text: "a", underline: true }] });
    expect(round("<u>unclosed")).toBe("<u>unclosed");
  });
});

describe("check lists", () => {
  it("round-trip with their ticks", () => {
    expect(round("- [ ] read\n- [x] pray")).toBe("- [ ] read\n- [x] pray");
    expect(parseMarkdown("- [x] done")[0]).toEqual({ type: "list", ordered: false, items: [[{ text: "done" }]], checked: [true] });
  });
  it("leave a mixed list a plain list", () => {
    expect(parseMarkdown("- [ ] a\n- b")[0]).not.toHaveProperty("checked");
  });
});

describe("pasted Markdown", () => {
  it("reads each line on its own", () => {
    const bs = parsePasted("# Title\nSome **bold** and _italic_.\n* one\n+ two\n1) first\n2) second\n> quoted\n> more\n***\n- [x] done\n- [ ] not yet");
    expect(bs.map((b) => b.type)).toEqual(["heading", "paragraph", "list", "list", "quote", "divider", "list"]);
    expect(bs[1]).toEqual({ type: "paragraph", spans: [{ text: "Some " }, { text: "bold", bold: true }, { text: " and " }, { text: "italic", italic: true }, { text: "." }] });
    expect(bs[2]).toMatchObject({ ordered: false, items: [[{ text: "one" }], [{ text: "two" }]] });
    expect(bs[3]).toMatchObject({ ordered: true });
    expect(bs[4]).toEqual({ type: "quote", spans: [{ text: "quoted" }, { text: "", br: true }, { text: "more" }] });
    expect(bs[6]).toMatchObject({ checked: [true, false] });
  });
  it("reads __double underscores__ as underline, not bold", () => {
    expect(parsePasted("an __underlined__ word")[0]).toEqual({ type: "paragraph",
      spans: [{ text: "an " }, { text: "underlined", underline: true }, { text: " word" }] });
    expect(toMarkdown(parsePasted("an __underlined__ word"))).toBe("an <u>underlined</u> word");
  });
  it("leaves underscores inside words alone", () => {
    expect(parsePasted("snake_case_name")).toEqual([{ type: "paragraph", spans: [{ text: "snake_case_name" }] }]);
  });
  it("skips blank lines and keeps plain lines as paragraphs", () => {
    expect(parsePasted("a\r\n\r\nb").map((b) => b.type)).toEqual(["paragraph", "paragraph"]);
  });
});

describe("tables", () => {
  const md = "| Name | Ref |\n| --- | --- |\n| **Grace** | [Romans 8:28](#passage/Rom.8.28) |\n| a \\| b | line<br>two |";
  it("reads a GitHub-style table with formats, chips, pipes and line breaks", () => {
    const [b] = parseMarkdown(md);
    expect(b.type).toBe("table");
    if (b.type !== "table") return;
    expect(b.header).toBe(true);
    expect(b.rows).toHaveLength(3);
    expect(b.rows[1][0]).toEqual([{ text: "Grace", bold: true }]);
    expect(b.rows[1][1]).toEqual([{ text: "Romans 8:28", link: "#passage/Rom.8.28" }]);
    expect(b.rows[2][0]).toEqual([{ text: "a | b" }]);
    expect(b.rows[2][1]).toEqual([{ text: "line" }, { text: "", br: true }, { text: "two" }]);
  });
  it("round-trips, with column widths and no header row", () => {
    expect(toMarkdown(parseMarkdown(md))).toBe(md);
    const sized = "<!-- table widths=120,64 header=no -->\n| a | b |\n| --- | --- |\n| c |  |";
    const [b] = parseMarkdown(sized);
    expect(b).toMatchObject({ type: "table", header: false, widths: [120, 64] });
    expect(toMarkdown(parseMarkdown(sized))).toBe(sized);
    expect(toMarkdown(parseMarkdown("text\n\n" + sized + "\n\nafter"))).toBe("text\n\n" + sized + "\n\nafter");
  });
  it("leaves things that only look like tables as text", () => {
    for (const s of ["| a | b |", "| a | b |\n| - | - |", "| a | b |\n| --- | --- |\n| c |", "a | b\n--- | ---"]) {
      expect(parseMarkdown(s)[0].type).toBe("paragraph");
      expect(toMarkdown(parseMarkdown(s))).toBe(s);
    }
  });
});

describe("citations and the bibliography", () => {
  const md = "Grace is plain [@lib:01HXABC, p. 12]. Also **bold [see @zotero:ABCD2345, ch. 3, for more]** and a [link](#passage/Rom.8.28).\n\n"
    + "- a list item [-@manual:01HX]\n- [@bible:kjv, John 3:16]\n\n<!-- bibliography -->";
  it("reads citations as their own pieces of a line", () => {
    const [p] = parseMarkdown("One [@lib:AAA, p. 3] two.");
    expect(p).toEqual({ type: "paragraph", spans: [
      { text: "One " }, { text: "", cite: { key: "lib:AAA", label: "page", locator: "3" } }, { text: " two." }] });
  });
  it("keeps the marks round a citation", () => {
    const [p] = parseMarkdown("**a [@lib:AAA] b**");
    expect(p).toMatchObject({ type: "paragraph" });
    const spans = (p as { spans: { text: string; bold?: boolean; cite?: unknown }[] }).spans;
    expect(spans.map((s) => [s.text, !!s.bold, !!s.cite])).toEqual([["a ", true, false], ["", true, true], [" b", true, false]]);
  });
  it("round-trips exactly, and reads the marker as a block", () => {
    expect(round(md)).toBe(md);
    expect(parseMarkdown(md).map((b) => b.type)).toEqual(["paragraph", "list", "bibliography"]);
  });
  it("leaves links, brackets and other text as they were", () => {
    for (const s of ["a [link](http://x) and [@nokind:abc] and [plain] and email me@example.org", "[text](#passage/Rom.8.28)",
      "[see @lib:ABC](http://x)", "<!-- not bibliography -->", "<!-- bibliography --> extra"]) {
      expect(round(s)).toBe(s);
      expect(JSON.stringify(parseMarkdown(s))).not.toContain('"cite"');
    }
  });
  it("round-trips a citation with every part, and escapes brackets in the words", () => {
    const back = "\x5c";
    const s = `[see also @zotero:ABCD2345, v. 5, for ${back}[more${back}], and ${back}${back} more]`;
    const [p] = parseMarkdown(s);
    const c = (p as { spans: { cite?: { suffix?: string; prefix?: string; label?: string } }[] }).spans[0].cite!;
    expect(c).toMatchObject({ prefix: "see also", label: "verse", suffix: `for [more], and ${back} more` });
    expect(round(s)).toBe(s);
  });
  it("is the same form the Go side reads (internal/cite)", () => {
    // The same strings are checked in internal/cite/goldens_test.go.
    expect(round("[@lib:A, pp. 1,3]")).toBe("[@lib:A, pp. 1,3]");
    expect(round("[@bible:kjv, Rom 8:28-30]")).toBe("[@bible:kjv, Rom 8:28-30]");
    expect(round("[-@lib:A, p. 4]")).toBe("[-@lib:A, p. 4]");
  });
});
