// The app's own yes-or-no question, used wherever a native confirm() box used to be. A dialog with a title,
// a plain sentence, Cancel (focused first) and the answer named plainly. Escape and Cancel answer no.

import { button, h } from "./dom";
import { closePopover } from "./popover";

// ask resolves true for the named answer. danger paints it red (something that cannot be undone); a plain
// question (turn something off, merge) uses the normal primary button.
export function ask(title: string, text: string, yes: string, danger = true): Promise<boolean> {
  return new Promise((resolve) => {
    const no = button("Cancel", () => done(false));
    const ok = button(yes, () => done(true), { class: danger ? "danger-solid" : "primary" });
    const tid = "confirm-" + Math.random().toString(36).slice(2);
    const dlg = h("dialog", { class: "confirm", "aria-labelledby": tid },
      h("h2", { id: tid }, title), h("p", {}, text), h("div", { class: "row confirm-actions" }, no, ok));
    dlg.addEventListener("cancel", (e) => { e.preventDefault(); done(false); });
    document.body.append(dlg);
    closePopover();
    dlg.showModal();
    no.focus();
    function done(v: boolean) {
      dlg.close();
      dlg.remove();
      resolve(v);
    }
  });
}
