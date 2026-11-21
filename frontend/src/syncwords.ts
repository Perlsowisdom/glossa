// The words for sync: counts, sizes, times and plain-language problems. Kept apart from the page so
// they can be tested.

export interface SyncDevice { id: string; name: string; self: boolean; files: number; bytes: number }
export interface SyncProgress {
  active: boolean; direction?: "upload" | "download"; name?: string;
  bytes_done: number; bytes_total: number; files_done: number; files_total: number;
}
export interface SyncBooks { mode: "all" | "opened"; files: number; bytes: number; on_device: number; remote_only: number; remote_bytes: number }
export interface SyncStatus {
  configured: boolean; folder?: string; device_id: string; device_name: string;
  unlocked: boolean; password_saved: boolean; last_sync?: number; pending_changes: number; conflicts: number;
  folder_bytes?: number; devices?: SyncDevice[]; folder_error?: string; books?: SyncBooks; progress?: SyncProgress;
  automatic: boolean; syncing: boolean; last_error?: string; last_error_code?: string;
}
export interface SyncSummary { changes: number; created: number; updated: number; deleted: number; purged: number; conflicts: number }
export interface SyncPreview {
  folder_has_data: boolean; new_folder?: boolean; devices: string[] | null;
  this_device: Record<string, number>; in_folder: Record<string, number>; after_merge: Record<string, number>;
  to_this_device: SyncSummary; to_folder: number;
}
export interface SyncConflict { id: string; object_type: string; object_id: string; field: string; change_id: string; device: string; text: string; note: string; created_at: number }

// The kinds of things a person recognises, with their words. Other tables (people, tags, settings)
// are bookkeeping and are not listed.
const KINDS: [string, string, string][] = [
  ["notes", "note", "notes"], ["highlights", "highlight", "highlights"], ["documents", "document or sermon", "documents and sermons"],
  ["lists", "list", "lists"], ["collections", "collection", "collections"], ["reading_plans", "reading plan", "reading plans"],
  ["items", "book", "books"],
];

// countsText lists what a count map holds: "3 notes, 1 highlight", or "nothing yet".
export function countsText(c: Record<string, number> | undefined | null): string {
  const parts = KINDS.filter(([k]) => (c?.[k] ?? 0) > 0).map(([k, one, many]) => `${c![k]} ${c![k] === 1 ? one : many}`);
  return parts.length ? parts.join(", ") : "nothing yet";
}

export function joinNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}

// previewSentences are the three lines of the merge preview.
export function previewSentences(p: SyncPreview): { here: string; folder: string; after: string } {
  const from = p.devices?.length ? `, from ${joinNames(p.devices)}` : "";
  return {
    here: `This device has ${countsText(p.this_device)}.`,
    folder: p.folder_has_data ? `The folder has ${countsText(p.in_folder)}${from}.` : "The folder is new and has nothing in it yet.",
    after: `After syncing, this device and the folder will both have ${countsText(p.after_merge)}.`,
  };
}

export function sizeText(bytes: number | undefined): string {
  const b = bytes ?? 0;
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(1)} GB`;
  if (b >= 1 << 20) return `${(b / (1 << 20)).toFixed(1)} MB`;
  if (b >= 1 << 10) return `${Math.round(b / 1024)} KB`;
  return `${b} bytes`;
}

// lastSyncText says when sync last finished.
export function lastSyncText(ms: number | undefined, now: Date = new Date()): string {
  if (!ms) return "Not synced yet";
  const mins = Math.max(0, Math.round((now.getTime() - ms) / 60000));
  if (mins < 1) return "Just now";
  if (mins < 60) return `${mins} minute${mins === 1 ? "" : "s"} ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours} hour${hours === 1 ? "" : "s"} ago`;
  return new Date(ms).toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" });
}

export function percent(p: Pick<SyncProgress, "bytes_done" | "bytes_total">): number {
  if (p.bytes_total <= 0) return 0;
  return Math.max(0, Math.min(100, Math.floor((p.bytes_done / p.bytes_total) * 100)));
}

export function progressText(p: SyncProgress): string {
  const verb = p.direction === "upload" ? "Uploading" : "Downloading";
  return `${verb}… ${percent(p)}%`;
}

// pendingText says how many changes are waiting to be sent.
export function pendingText(n: number): string {
  return n === 0 ? "Nothing waiting" : `${n} change${n === 1 ? "" : "s"} waiting to be sent`;
}

export type IndicatorState = "off" | "locked" | "syncing" | "problem" | "ok" | "pending";

// indicator is the small status in the rail: the state, a short name and the full words.
export function indicator(s: SyncStatus | null, now: Date = new Date()): { state: IndicatorState; short: string; label: string } | null {
  if (!s?.configured) return null;
  if (!s.last_sync) return { state: "pending", short: "Finish sync", label: "Sync: finish setting up" };
  if (!s.unlocked) return { state: "locked", short: "Sync locked", label: "Sync: enter the password" };
  if (s.syncing) return { state: "syncing", short: "Syncing…", label: "Syncing…" };
  if (s.last_error) return { state: "problem", short: "Sync problem", label: `Sync problem: ${s.last_error}` };
  const when = lastSyncText(s.last_sync, now).toLowerCase();
  if (!s.automatic) return { state: "off", short: "Sync off", label: `Sync is off. Last synced: ${when}` };
  return { state: "ok", short: "Synced", label: `Synced: ${when}` };
}

// advice adds what to do next to a sync error. The message the core gives is already plain.
export function advice(code: string | undefined): string {
  switch (code) {
    case "wrong_password": return "Check the password. It must be the one you chose when sync was first set up.";
    case "folder_missing": return "Check that the folder is still there and that your sync service (OneDrive, Dropbox and so on) is running.";
    case "missing_file": return "Your sync service may still be copying files. Try again in a minute.";
    case "disk_full": return "Free some space on this computer or in the sync folder, then try again.";
    case "corrupt": return "Nothing was changed on this device. Try again once your sync service has finished copying.";
    case "newer_version": return "Update Glossa on this computer.";
    case "forgotten": return "Turn sync off here and set it up again to rejoin.";
    case "locked": return "Enter the sync password.";
    default: return "";
  }
}

// conflictLine says what a kept second version is. The core's note names the device whose edit
// was replaced ("Changed on <device> too").
export function conflictLine(c: SyncConflict): string {
  const what = ({ notes: "note", documents: "document or sermon" } as Record<string, string>)[c.object_type] ?? "text";
  const device = /^Changed on (.+) too$/.exec(c.note)?.[1] ?? "another device";
  return `A ${what} was changed on ${device} too. The replaced text is kept in its history.`;
}
