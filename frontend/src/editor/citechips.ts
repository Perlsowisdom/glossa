// Keeps the citation chips and the bibliography block up to date: after the text changes, asks the app
// for the short form of each citation it has not seen, and for the bibliography, and draws them. The
// text of the note itself (the Markdown) never holds these: they are worked out from the sources.

import { $getRoot, $nodesOfType, type LexicalEditor } from "lexical";
import { citeMarkdown, type CiteData } from "./citeformat";
import {
  BibliographyNode, CiteNode, fillBibliography, forgetCiteLabels, knownCiteLabel, rememberCiteLabels, type CiteHost,
} from "./citenodes";

const WAIT = 350;

export interface CiteChips {
  // refresh draws everything again; force asks for every short form again (the style changed).
  refresh(force?: boolean): void;
  destroy(): void;
}

export function registerCites(editor: LexicalEditor, host: CiteHost): CiteChips {
  let timer = 0;
  let seq = 0;
  let alive = true;
  let bibSig = "";

  const read = () => editor.getEditorState().read(() => {
    void $getRoot();
    const chips = $nodesOfType(CiteNode).map((n) => ({ key: n.getKey(), cite: n.getCite() }));
    const bibs = $nodesOfType(BibliographyNode).map((n) => n.getKey());
    return { chips, bibs };
  });

  async function run(force: boolean) {
    const n = ++seq;
    if (force) {
      forgetCiteLabels();
      bibSig = "";
    }
    const { chips, bibs } = read();
    const unknown = new Map<string, CiteData>();
    for (const c of chips) if (knownCiteLabel(c.cite) === undefined) unknown.set(citeMarkdown(c.cite), c.cite);
    if (unknown.size > 0) {
      const list = [...unknown.values()];
      const labels = await host.labels(list).catch(() => [] as string[]);
      if (!alive || n !== seq) return;
      rememberCiteLabels(list, labels);
    }
    for (const c of chips) {
      const text = editor.getElementByKey(c.key)?.querySelector(".cite-chip-text");
      const label = knownCiteLabel(c.cite);
      if (text && label !== undefined && text.textContent !== label) text.textContent = label;
    }
    if (bibs.length === 0) return;
    const cites = chips.map((c) => c.cite);
    const sig = cites.map(citeMarkdown).join("\n") + "|" + bibs.join(",");
    if (sig === bibSig) return;
    const bib = await host.bibliography(cites).catch(() => null);
    if (!alive || n !== seq || !bib) return;
    bibSig = sig;
    for (const key of bibs) {
      const el = editor.getElementByKey(key);
      if (el) fillBibliography(el, bib.title, bib.entries);
    }
  }

  const later = (force = false) => {
    clearTimeout(timer);
    timer = window.setTimeout(() => void run(force), force ? 0 : WAIT);
  };
  const off = editor.registerUpdateListener(({ dirtyElements, dirtyLeaves }) => {
    if (dirtyElements.size > 0 || dirtyLeaves.size > 0) later();
  });
  later();
  return {
    refresh: (force) => later(!!force),
    destroy() {
      alive = false;
      clearTimeout(timer);
      off();
    },
  };
}

