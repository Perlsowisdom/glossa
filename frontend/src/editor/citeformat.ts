// How a citation is written in the Markdown a note or sermon is saved as. It is Pandoc-like, in square
// brackets, and is the same form internal/cite (Go) reads and writes:
//
//   [@lib:01HX...]                       the source alone
//   [@lib:01HX..., p. 12]                with a locator (p., pp., ch., sec., v., para., fig., n., vol., col., l., art.)
//   [see @lib:01HX..., p. 12, for more]  with words before and after
//   [-@lib:01HX...]                      without the author
//   [@bible:kjv, Rom 8:28-30]            a Bible version: the passage is the locator
//
// In the words before and after, a backslash, [ and ] are escaped with a backslash (and @ in the
// words before). A [...] followed by ( is a link and is never a citation. (x5c is the backslash below.)
// Kept apart from the editor so it is tested on its own (citeformat.test.ts).

export interface CiteData {
  key: string; // lib:<book>, bible:<version>, zotero:<key> or manual:<id>
  label?: string; // what the locator is (page, chapter ...); not used for a Bible
  locator?: string; // a page, chapter or section number; for a Bible the passage
  prefix?: string;
  suffix?: string;
  suppress?: boolean; // leave out the author
}

export const CITE_RE = /^\[(?:((?:[^\[\]\x5c@]|\x5c.)+?) )?(-?)@((?:lib|bible|zotero|manual):[A-Za-z0-9_.:-]*[A-Za-z0-9_])(?:, ((?:[^\[\]\x5c]|\x5c.)*))?\]/;
const LABEL_RE = /^(p|pp|ch|chap|sec|v|vv|para|fig|n|nn|vol|col|l|ll|art|§|¶)\.? (.+)$/;

const TOKEN_LABEL: Record<string, string> = { p: "page", pp: "page", ch: "chapter", chap: "chapter", sec: "section", "§": "section",
  v: "verse", vv: "verse", para: "paragraph", "¶": "paragraph", fig: "figure", n: "note", nn: "note", vol: "volume", col: "column",
  l: "line", ll: "line", art: "article" };
const LABEL_TOKEN: Record<string, string> = { page: "p.", chapter: "ch.", section: "sec.", verse: "v.", paragraph: "para.", figure: "fig.",
  note: "n.", volume: "vol.", column: "col.", line: "l.", article: "art." };

// The locator labels a picker offers, in order, as [value, words].
export const CITE_LABELS: [string, string][] = [["page", "Page"], ["chapter", "Chapter"], ["section", "Section"], ["verse", "Verse"],
  ["paragraph", "Paragraph"], ["figure", "Figure"], ["note", "Note"], ["volume", "Volume"], ["column", "Column"], ["line", "Line"], ["article", "Article"]];

const BS = "\x5c";

export function isBibleKey(key: string): boolean {
  return key.startsWith("bible:");
}

function unescape(s: string): string {
  if (!s.includes(BS)) return s;
  let out = "";
  for (let i = 0; i < s.length; i++) {
    if (s[i] === BS && i + 1 < s.length) i++;
    out += s[i];
  }
  return out;
}

function escape(s: string, at: boolean): string {
  let out = "";
  for (const ch of s) out += ch === BS || ch === "[" || ch === "]" || (at && ch === "@") ? BS + ch : ch;
  return out;
}

// parseCite reads a citation at the start of s; len is how many characters it took.
export function parseCite(s: string): { cite: CiteData; len: number } | null {
  if (s[0] !== "[") return null;
  const m = CITE_RE.exec(s);
  if (!m || s[m[0].length] === "(") return null;
  const cite: CiteData = { key: m[3] };
  if (m[1]) cite.prefix = unescape(m[1]);
  if (m[2] === "-") cite.suppress = true;
  const tail = unescape(m[4] ?? "");
  if (tail) {
    const at = tail.indexOf(", ");
    const first = at < 0 ? tail : tail.slice(0, at);
    const rest = at < 0 ? "" : tail.slice(at + 2);
    const lm = LABEL_RE.exec(first);
    if (isBibleKey(cite.key)) {
      cite.locator = first;
      if (rest) cite.suffix = rest;
    } else if (lm) {
      cite.label = TOKEN_LABEL[lm[1]];
      cite.locator = lm[2];
      if (rest) cite.suffix = rest;
    } else cite.suffix = tail;
  }
  return { cite, len: m[0].length };
}

// citeMarkdown writes a citation as parseCite reads it.
export function citeMarkdown(c: CiteData): string {
  let out = "[";
  if (c.prefix) out += escape(c.prefix, true) + " ";
  if (c.suppress) out += "-";
  out += "@" + c.key;
  const loc = (c.locator ?? "").replace(/\s+/g, " ").trim().replace(/, /g, ",");
  if (loc && isBibleKey(c.key)) out += ", " + escape(loc, false);
  else if (loc) {
    let tok = LABEL_TOKEN[c.label ?? ""] ?? "p.";
    if (tok === "p." && /[-–,&]/.test(loc)) tok = "pp.";
    out += `, ${tok} ${escape(loc, false)}`;
  }
  if (c.suffix) out += ", " + escape(c.suffix, false);
  return out + "]";
}

// The marker line where a document's bibliography goes.
export const BIBLIOGRAPHY_MARKER = "<!-- bibliography -->";

// sameCite compares two citations as they are saved.
export function sameCite(a: CiteData, b: CiteData): boolean {
  return citeMarkdown(a) === citeMarkdown(b);
}
