// The Any part of its card in Settings → Connections: signing in with the 4-digit code Any shows,
// and the space Glossa sends to. The switch, status and Test are the card's own.

import { run } from "../api";
import { AnySpaces } from "../anywords";
import { failureText } from "../connectwords";
import { button, clear, field, h, select } from "../dom";

// anySection returns the pairing and space controls for a card. paired is whether a key is saved;
// changed asks the card to check again after signing in or out; say shows a short message.
export function anySection(paired: boolean, changed: () => Promise<void>, say: (t: string, kind?: string) => void): HTMLElement {
  const el = h("div", { class: "any-card" });
  if (paired) {
    el.append(h("p", { class: "hint" }, "Glossa is signed in to Any."), spaceChooser(say),
      h("div", { class: "row wrap" }, button("Sign out of Any", async () => {
        try { await run("any.unpair"); say("Signed out of Any."); } catch (e) { say(failureText(e), "error"); }
        await changed();
      }, { class: "danger" })));
    return el;
  }
  const start = button("Sign in to Any", async () => {
    start.disabled = true;
    try {
      const r = await run<{ challenge_id: string }>("any.pair_start");
      codeForm(r.challenge_id);
    } catch (e) {
      say(failureText(e), "error");
    }
    start.disabled = false;
  });
  const slot = h("div", {});
  clear(el, h("p", { class: "hint" }, "Glossa asks Any to show a 4-digit code. Open the Any app, then type the code here."), start, slot);

  function codeForm(challenge: string) {
    const code = h("input", { type: "text", inputmode: "numeric", maxlength: "4", autocomplete: "off", pattern: "[0-9]{4}", placeholder: "4 digits", class: "any-code" }) as HTMLInputElement;
    const form = h("form", { class: "any-code-form", "aria-label": "Code from Any" },
      h("p", { class: "hint" }, "Any is showing a 4-digit code. Type it here."),
      h("div", { class: "row wrap end" }, field("Code from Any", code),
        h("button", { type: "submit", class: "primary" }, "Finish"),
        button("Cancel", () => clear(slot))));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const value = code.value.trim();
      code.value = "";
      if (!/^[0-9]{4}$/.test(value)) return say("The code is 4 digits.", "error");
      try {
        await run("any.pair_finish", { challenge_id: challenge, code: value });
        say("Signed in to Any.", "ok");
      } catch (err) {
        return say(failureText(err), "error");
      }
      await changed();
    });
    clear(slot, form);
    code.focus();
  }
  return el;
}

function spaceChooser(say: (t: string, kind?: string) => void): HTMLElement {
  const box = h("div", { class: "any-space" });
  void (async () => {
    let r: AnySpaces;
    try {
      r = await run<AnySpaces>("any.spaces");
    } catch {
      return; // Any is not running: the card's status already says so
    }
    if (!r.spaces.length) return;
    const pick = select(r.spaces.map((s) => [s.id, s.name] as [string, string]), r.default_space_id ?? r.spaces[0].id, { "aria-label": "Space to send to", id: "any-space" });
    pick.addEventListener("change", async () => {
      try { await run("any.set_space", { space_id: pick.value }); say("Saved.", "ok"); } catch (e) { say(failureText(e), "error"); }
    });
    clear(box, field("Send to this space", pick, r.spaces.length > 1 ? "New notes and sermons go here. Pages you send before stay where they are." : undefined));
  })();
  return box;
}
