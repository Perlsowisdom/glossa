// The words for deleting. Everything the bin holds (notes, sermons, folders, lists) is deleted at once and
// the notice offers Undo; a thing the bin cannot hold (a reading plan) is asked about first and says it is
// deleted for good (ADR 0010: "for good" is only ever said of what cannot be brought back).

// binNotice: `"Name" went to the bin.`; extra names what went with it ("and its 2 notes").
export function binNotice(name: string, extra = ""): string {
  return `"${name}"${extra ? ` ${extra}` : ""} went to the bin.`;
}

export const backNotice = (name: string) => `"${name}" is back.`;

// forGoodQuestion is the question before deleting something the bin does not hold.
export function forGoodQuestion(name: string, what: string): string {
  return `Delete "${name}" for good? ${what} are not kept in the bin, so it cannot be brought back.`;
}

export const forGoodNotice = (name: string) => `"${name}" was deleted for good.`;

// The notice goes away by itself after this long, so it does not follow you around the app.
export const NOTICE_MS = 15000;
