// The Any part of its card in Settings → Connections: signing in with the 4-digit code Any shows
// (only when Any asks for it), the space Glossa sends to, and, under Advanced, the address of the Any
// app on this computer. The switch, status and Test are the card's own.

import { run } from "../api";
import { AnySpaces } from "../anywords";
import { TestOutcome, failureText } from "../connectwords";
import { button, clear, field, h, select } from "../dom";

// AnyAddress is where Glossa looks for the Any app (any.address).
interface AnyAddress { address?: string; manual: boolean; usual: string[] }

let advancedOpen = false; // Advanced stays open across redraws

// anySection returns the sign-in, space and address controls for a card. paired is whether a key is
// saved; test is the last check (its auth is none_needed, ok, needed or stale; empty when Any was not
// found; the check itself is undefined while it is still running); changed asks the card to check again; say shows a message.
export function anySection(paired: boolean, test: TestOutcome | undefined, changed: () => Promise<void>, say: (t: string, kind?: string) => void): HTMLElement {
  const el = h("div", { class: "any-card" });
  const auth = test?.auth;
  const advanced = addressBox(!!test && !test.ok && !auth, changed, say);
  if (auth === "none_needed") {
    el.append(h("p", { class: "hint" }, "Any didn’t ask for a key, so there is nothing to sign in to."), spaceChooser(say), advanced);
    return el;
  }
  if (paired && auth !== "stale") {
    el.append(h("p", { class: "hint" }, "Glossa is signed in to Any."), spaceChooser(say),
      h("div", { class: "row wrap" }, button("Sign out of Any", async () => {
        try { await run("any.unpair"); say("Signed out of Any."); } catch (e) { say(failureText(e), "error"); }
        await changed();
      }, { class: "danger" })), advanced);
    return el;
  }
  if (!auth) { // Any was not found (or the check is still running): nothing to sign in to yet
    el.append(advanced);
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
  clear(el, h("p", { class: "hint" }, auth === "stale"
    ? "Any no longer accepts the key Glossa had. Sign in again: Glossa asks Any to show a 4-digit code, and you type it here."
    : "Glossa asks Any to show a 4-digit code. Open the Any app, then type the code here."), start, slot, advanced);

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
    const options = r.spaces.map((s) => [s.id, s.name] as [string, string]);
    if (!r.default_space_id) options.unshift(["", "Choose a space…"]);
    const pick = select(options, r.default_space_id ?? "", { id: "any-space" });
    pick.addEventListener("change", async () => {
      if (!pick.value) return;
      try { await run("any.set_space", { space_id: pick.value }); say("Saved.", "ok"); } catch (e) { say(failureText(e), "error"); }
    });
    clear(box, field("Send to this space", pick, r.spaces.length > 1 ? "New notes and sermons go here. Pages you send before stay where they are." : undefined));
  })();
  return box;
}

// addressBox is the Advanced part: the address of the Any app on this computer. It opens by itself
// when Any was not found.
function addressBox(notFound: boolean, changed: () => Promise<void>, say: (t: string, kind?: string) => void): HTMLElement {
  const d = h("details", { class: "more connection-more any-advanced" }, h("summary", {}, "Advanced")) as HTMLDetailsElement;
  d.open = advancedOpen || notFound;
  d.querySelector("summary")!.addEventListener("click", () => { advancedOpen = !d.open; }); // only the person's own toggling is remembered
  void (async () => {
    let r: AnyAddress;
    try { r = await run<AnyAddress>("any.address"); } catch { return; }
    const input = h("input", { type: "text", autocomplete: "off", spellcheck: "false", class: "any-address", placeholder: r.usual[0]?.replace(/^http:\/\//, "") ?? "127.0.0.1:31310" }) as HTMLInputElement;
    if (r.manual && r.address) input.value = r.address;
    const usual = r.usual.map((u) => u.replace(/^http:\/\//, "")).join(", then ");
    const form = h("form", { class: "any-address-form", "aria-label": "Address of Any" },
      h("p", { class: "hint" }, r.manual
        ? "Glossa looks for Any only at the address below."
        : `Glossa looks for Any at ${usual}${r.address ? `, and found it at ${r.address.replace(/^http:\/\//, "")} last time` : ""}. If your Any app uses another address on this computer, enter it here.`),
      h("div", { class: "row wrap end" }, field("Address of the Any app", input),
        h("button", { type: "submit", class: "primary" }, "Use this address"),
        r.manual ? button("Find it by itself", async () => {
          advancedOpen = true;
          try { await run("any.set_address", { address: "" }); say("Glossa will look for Any by itself.", "ok"); } catch (e) { say(failureText(e), "error"); }
          await changed();
        }) : null));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      advancedOpen = true; // the person is working here: it stays open when the card redraws
      if (!input.value.trim()) return say("Type the address of the Any app, such as 127.0.0.1:31310.");
      try {
        await run("any.set_address", { address: input.value });
        say("Saved.", "ok");
      } catch (err) {
        return say(failureText(err), "error");
      }
      await changed();
    });
    d.append(form);
  })();
  return d;
}
