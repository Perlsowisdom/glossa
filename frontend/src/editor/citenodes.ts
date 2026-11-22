// Two things the editor holds for citations. A citation chip is a small piece inside the text that
// shows the citation's short form (for example "Moo, Romans, 12" or "Rom 8:28 (NRSV)"); the caret
// steps over it, Backspace removes it, and a click edits it. In the Markdown it is [@source, p. 12]
// (citeformat.ts). A bibliography block is where the document's bibliography goes: it shows the
// list of works cited so far, and exports fill the same place.

import { $applyNodeReplacement, $getNodeByKey, DecoratorNode, type LexicalEditor, type LexicalNode, type NodeKey, type SerializedLexicalNode, type Spread } from "lexical";
import { $caretAfterBlock } from "./nodes";
import { citeMarkdown, isBibleKey, type CiteData } from "./citeformat";

// What the app tells the editor about citations. The editor has no citing code of its own.
export interface CiteRun {
  text: string;
  italic?: boolean;
  bold?: boolean;
}

export interface CiteHost {
  // Insert → Citation and /cite, and a click on a chip: a picker of sources, with where in the source.
  // existing is the citation being edited, if any; done gets the new one, or null to remove it.
  pick(anchor: HTMLElement, existing: CiteData | null, done: (c: CiteData | null) => void): void;
  // The short form of each citation, in the document's style.
  labels(cites: CiteData[]): Promise<string[]>;
  // The bibliography of these citations, in the document's style.
  bibliography(cites: CiteData[]): Promise<{ title: string; entries: CiteRun[][] }>;
}

// Short forms already asked for, so a chip drawn again (undo, scrolling a long note) shows at once.
const known = new Map<string, string>();

export function rememberCiteLabels(cites: CiteData[], labels: string[]): void {
  cites.forEach((c, i) => labels[i] !== undefined && known.set(citeMarkdown(c), labels[i]));
}

export function forgetCiteLabels(): void {
  known.clear();
}

export function knownCiteLabel(c: CiteData): string | undefined {
  return known.get(citeMarkdown(c));
}

// stand-in is what a chip says until its short form arrives (or when there is no app to ask).
export function citeStandIn(c: CiteData): string {
  if (isBibleKey(c.key)) return c.locator ? `${c.locator} (${c.key.slice(6).toUpperCase()})` : c.key.slice(6).toUpperCase();
  return c.locator ? `source, ${c.locator}` : "source";
}

type SerializedCite = Spread<{ cite: CiteData }, SerializedLexicalNode>;

export class CiteNode extends DecoratorNode<null> {
  __cite: CiteData;

  static getType(): string {
    return "xg-cite";
  }
  static clone(n: CiteNode): CiteNode {
    return new CiteNode(n.__cite, n.__key);
  }
  static importJSON(j: SerializedCite): CiteNode {
    return $createCiteNode(j.cite);
  }
  constructor(cite: CiteData = { key: "" }, key?: NodeKey) {
    super(key);
    this.__cite = cite;
  }
  exportJSON(): SerializedCite {
    return { type: "xg-cite", version: 1, cite: this.getCite() };
  }
  getCite(): CiteData {
    return this.getLatest().__cite;
  }
  setCite(c: CiteData): this {
    const w = this.getWritable();
    w.__cite = c;
    return w;
  }
  getTextContent(): string {
    return knownCiteLabel(this.getCite()) ?? citeStandIn(this.getCite());
  }
  createDOM(): HTMLElement {
    const el = document.createElement("span");
    el.className = "cite-chip";
    el.contentEditable = "false";
    el.dataset.nodeKey = this.__key;
    el.setAttribute("role", "button");
    el.title = "Change this citation";
    const mark = document.createElement("span");
    mark.className = "cite-chip-mark";
    mark.setAttribute("aria-hidden", "true");
    mark.textContent = "❡";
    const text = document.createElement("span");
    text.className = "cite-chip-text";
    text.textContent = knownCiteLabel(this.__cite) ?? citeStandIn(this.__cite);
    el.append(mark, text);
    return el;
  }
  updateDOM(prev: CiteNode, dom: HTMLElement): boolean {
    if (citeMarkdown(prev.__cite) !== citeMarkdown(this.__cite)) {
      const t = dom.querySelector(".cite-chip-text");
      if (t) t.textContent = knownCiteLabel(this.__cite) ?? citeStandIn(this.__cite);
    }
    return false;
  }
  isInline(): true {
    return true;
  }
  decorate(): null {
    return null;
  }
}

export function $createCiteNode(c: CiteData): CiteNode {
  return $applyNodeReplacement(new CiteNode(c));
}

export function $isCiteNode(n: LexicalNode | null | undefined): n is CiteNode {
  return n instanceof CiteNode;
}

export class BibliographyNode extends DecoratorNode<null> {
  static getType(): string {
    return "xg-bibliography";
  }
  static clone(n: BibliographyNode): BibliographyNode {
    return new BibliographyNode(n.__key);
  }
  static importJSON(): BibliographyNode {
    return $createBibliographyNode();
  }
  exportJSON(): SerializedLexicalNode {
    return { type: "xg-bibliography", version: 1 };
  }
  createDOM(_config: unknown, editor: LexicalEditor): HTMLElement {
    const el = document.createElement("section");
    el.className = "cite-bib";
    el.contentEditable = "false";
    el.setAttribute("aria-label", "Bibliography");
    const title = document.createElement("h4");
    title.className = "cite-bib-title";
    title.textContent = "Bibliography";
    const list = document.createElement("div");
    list.className = "cite-bib-list";
    const empty = document.createElement("p");
    empty.className = "cite-bib-empty";
    empty.textContent = "The works you cite will be listed here.";
    list.append(empty);
    const key = this.__key;
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "vq-remove";
    remove.title = "Remove the bibliography";
    remove.setAttribute("aria-label", "Remove the bibliography");
    remove.textContent = "×";
    remove.addEventListener("mousedown", (e) => e.preventDefault());
    remove.addEventListener("click", () => editor.update(() => $getNodeByKey(key)?.remove()));
    el.addEventListener("mousedown", (e) => {
      if ((e.target as Element).closest("button") || e.button !== 0) return;
      e.preventDefault();
      editor.update(() => $caretAfterBlock(key));
      editor.focus();
    });
    el.append(title, list, remove);
    return el;
  }
  updateDOM(): false {
    return false;
  }
  isInline(): false {
    return false;
  }
  decorate(): null {
    return null;
  }
}

export function $createBibliographyNode(): BibliographyNode {
  return $applyNodeReplacement(new BibliographyNode());
}

export function $isBibliographyNode(n: LexicalNode | null | undefined): n is BibliographyNode {
  return n instanceof BibliographyNode;
}

// fillBibliography draws the entries into a bibliography block's element.
export function fillBibliography(el: HTMLElement, title: string, entries: CiteRun[][]): void {
  const t = el.querySelector(".cite-bib-title");
  if (t) t.textContent = title;
  const list = el.querySelector(".cite-bib-list");
  if (!list) return;
  if (entries.length === 0) {
    const p = document.createElement("p");
    p.className = "cite-bib-empty";
    p.textContent = "The works you cite will be listed here.";
    list.replaceChildren(p);
    return;
  }
  list.replaceChildren(...entries.map((runs) => {
    const p = document.createElement("p");
    p.className = "cite-bib-entry";
    for (const r of runs) {
      let n: Node = document.createTextNode(r.text);
      if (r.italic) { const i = document.createElement("i"); i.append(n); n = i; }
      if (r.bold) { const b = document.createElement("b"); b.append(n); n = b; }
      p.append(n);
    }
    return p;
  }));
}
