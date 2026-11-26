// The words for updates: what the settings page says about the last check and the newest version.
// Kept apart from the page so they can be tested.

export type Channel = "stable" | "preview";

export interface UpdateInfo {
  enabled: boolean;
  channel: Channel;
  channel_chosen?: boolean;
  last_checked?: string;
  last_error?: string;
  skip_until?: string;
  latest?: string;
  notes?: string;
  newer?: boolean; // the newest version found is newer than this one
  current: string;
  key_set: boolean;
  manual?: boolean; // macOS: the update is downloaded and installed by hand
}

// updateAction is the button for a newer version: Windows installs it, macOS opens the download.
export function updateAction(manual?: boolean): string {
  return manual ? "Download" : "Install and restart";
}

// noticeTitle is the line at the top of the notice that a newer version is ready.
export function noticeTitle(version: string): string {
  return `Glossa ${version} is ready.`;
}

// noticeHelp says what the button will do.
export function noticeHelp(manual?: boolean): string {
  return manual
    ? "Download it, open the disk image and drag Glossa to Applications. Your library is not touched."
    : "It installs in a moment and Glossa opens again. Your library is not touched.";
}

// shortNotes trims a release's notes to a few plain lines for the notice: no headings or bullets
// marks, at most three lines and about 220 characters.
export function shortNotes(notes: string | undefined, max = 220): string {
  const lines = (notes ?? "")
    .split(/\r?\n/)
    .map((l) => l.replace(/^\s*(#{1,6}\s+|[-*+]\s+|>\s*)/, "").replace(/[*_`]/g, "").trim())
    .filter(Boolean)
    .slice(0, 3);
  const text = lines.join(" ");
  return text.length <= max ? text : `${text.slice(0, max - 1).trimEnd()}…`;
}

export const CHANNELS: [Channel, string][] = [["stable", "Stable"], ["preview", "Preview"]];

// channelHint says what the chosen channel offers.
export function channelHint(c: Channel): string {
  return c === "preview"
    ? "Preview: the newest version, including test versions that may have rough edges."
    : "Stable: only finished releases.";
}

// lastErrorText says, in plain words, that the last check did not work and why.
export function lastErrorText(e: string | undefined): string {
  return e ? `The last check did not work: ${e}.` : "";
}

// lastCheckedText says when the last check was, in plain words.
export function lastCheckedText(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return "Not checked yet.";
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "Not checked yet.";
  const mins = Math.max(0, Math.round((now.getTime() - t.getTime()) / 60000));
  if (mins < 1) return "Last checked just now.";
  if (mins < 60) return `Last checked ${mins} minute${mins === 1 ? "" : "s"} ago.`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `Last checked ${hours} hour${hours === 1 ? "" : "s"} ago.`;
  return `Last checked on ${t.toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" })}.`;
}

// versionLine says which version is running and whether a newer one is known.
export function versionLine(info: UpdateInfo, newer: boolean): string {
  if (newer && info.latest) return `Version ${info.latest} is available. You have ${info.current}.`;
  return `You have version ${info.current}.`;
}
