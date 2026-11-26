// The right rail: a slim column at the window's right edge on every page, with the one button that
// shows and hides the study tools (the right sidebar, views/sidebar.ts). The button never moves: the
// sidebar opens to its left. The left rail's settings (width, hide automatically) do not touch it.
// At phone width the left rail is a dock at the bottom and this column becomes the dock's last cell
// (style.css), so the button is still in one place.

import { h } from "../dom";
import { icon } from "../icons";
import { onPrefs, prefs } from "../state";

export function rightRail(toggleSidebar: () => void): HTMLElement {
  const btn = h("button", {
    type: "button", class: "rrail-toggle", "aria-controls": "rightbar", "aria-pressed": "false", "aria-expanded": "false",
    "aria-keyshortcuts": "Control+.", title: "Show or hide the study tools (Ctrl+.)",
  }, icon("panel"), h("span", { class: "rrail-label" }, "Study tools"));
  btn.addEventListener("click", () => toggleSidebar());
  const apply = () => {
    const open = String(prefs.sidebar.open);
    btn.setAttribute("aria-pressed", open);
    btn.setAttribute("aria-expanded", open);
  };
  onPrefs(apply);
  apply();
  return h("div", { class: "right-rail" }, btn);
}
