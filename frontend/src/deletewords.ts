// The words for deleting. Everything the bin holds (notes, sermons, folders, lists, reading plans, collections) is deleted at
// once and the notice offers Undo. "For good" is only said in the Bin, of what is really removed (ADR 0010).

// binNotice: `"Name" went to the bin.`; extra names what went with it ("and its 2 notes").
export function binNotice(name: string, extra = ""): string {
  return `"${name}"${extra ? ` ${extra}` : ""} went to the bin.`;
}

export const backNotice = (name: string) => `"${name}" is back.`;

// The notice goes away by itself after this long, so it does not follow you around the app.
export const NOTICE_MS = 15000;
