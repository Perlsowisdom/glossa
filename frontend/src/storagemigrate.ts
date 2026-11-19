// The app was called Xegesis, and its saved interface settings were kept in localStorage under
// "xegesis." keys. They are now "glossa." keys; migrateStorage copies each old value to its new key
// once (never over a value already saved under the new key) and removes the old key. It runs before
// anything reads a setting (migrate-first.ts, the first import of main.ts).

const OLD = "xegesis.";
const NEW = "glossa.";
const DONE = "glossa.storageMigrated";

export type KeyStore = Pick<Storage, "getItem" | "setItem" | "removeItem" | "key" | "length">;

/** Returns how many settings were carried over. Safe to call again: it does nothing after the first run. */
export function migrateStorage(store: KeyStore): number {
  try {
    if (store.getItem(DONE) === "1") return 0;
    const old: string[] = [];
    for (let i = 0; i < store.length; i++) {
      const k = store.key(i);
      if (k && k.startsWith(OLD)) old.push(k);
    }
    let moved = 0;
    for (const k of old) {
      const target = NEW + k.slice(OLD.length);
      const v = store.getItem(k);
      if (v !== null && store.getItem(target) === null) {
        store.setItem(target, v);
        moved++;
      }
      store.removeItem(k);
    }
    store.setItem(DONE, "1");
    return moved;
  } catch {
    return 0; // storage is blocked or full: the app starts with default settings
  }
}
