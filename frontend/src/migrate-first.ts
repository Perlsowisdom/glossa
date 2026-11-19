// Imported first by main.ts so the old Xegesis settings are carried over before any module reads one.
import { migrateStorage } from "./storagemigrate";

try {
  migrateStorage(localStorage);
} catch {
  /* no storage: nothing to carry over */
}
