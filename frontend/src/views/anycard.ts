// The Any part of its card in Settings → Connections: pairing Glossa with the Any app (a screen that
// shows what was found and one button), Unpair, the space Glossa sends to, and, under Advanced, the
// address of the Any app on this computer. The switch, status and Test are the card's own.

import { run } from "../api";
import { AnySpaces } from "../anywords";
import { TestOutcome, failureText } from "../connectwords";
import { button, clear, field, h, select } from "../dom";

// AnyAddress is where Glossa looks for the Any app (any.address).
interface AnyAddress { address?: string; manual: boolean; usual: string[] }

// AnyStatus is what any.status found: where Any is and which spaces it has.
interface AnyStatus { found: boolean; address?: string; spaces: { id: string; name: string }[]; paired: boolean; state: string }

let advancedOpen = false; // Advanced stays open across redraws

// PAIR_WORDS is the one honest sentence beside the Pair button.
export const PAIR_WORDS = "Glossa will be able to read the pages you pick and send the notes and sermons you choose to the space you choose. Any doesn’t check yet which apps connect, so other programs on this computer can reach it too.";

// anySection returns the pairing, space and address controls for a card. test is the last check (its
// auth is paired, unpaired, different or refused; empty when Any was not found; the check itself is
// undefined while it is still running); changed asks the card to check again; say shows a message.
export function anySection(test: TestOutcome | undefined, changed: () => Promise<void>, say: (t: string, kind?: string) => void): HTMLElement {
  const el = h("div", { class: "any-card" });
  const auth = test?.auth;
  const advanced = addressBox(!!test && !test.ok && !auth, changed, say);
  if (auth === "paired") {
    el.append(h("p", { class: "hint" }, "Glossa is paired with Any."), spaceChooser(say),
      h("div", { class: "row wrap" }, button("Unpair", async () => {
        try { await run("any.unpair"); say("Glossa is no longer paired with Any.", "ok"); } catch (e) { say(failureText(e), "error"); }
        await changed();
      }, { class: "danger" })), advanced);
    return el;
  }
  if (auth === "unpaired" || auth === "different") {
    el.append(pairScreen(changed, say), advanced);
    return el;
  }
  el.append(advanced); // Any was not found, refused Glossa, or the check is still running
  return el;
}

// pairScreen shows what was found and the Pair button.
function pairScreen(changed: () => Promise<void>, say: (t: string, kind?: string) => void): HTMLElement {
  const box = h("div", { class: "any-pair" });
  const found = h("p", { class: "any-found" }, "Looking at what Any has…");
  const pair = button("Pair with Any", async () => {
    pair.disabled = true;
    try { await run("any.pair"); say("Glossa is paired with Any.", "ok"); } catch (e) { say(failureText(e), "error"); }
    pair.disabled = false;
    await changed();
  }, { class: "primary" });
  box.append(found, h("p", { class: "hint" }, PAIR_WORDS), h("div", { class: "row wrap" }, pair));
  void (async () => {
    try {
      const s = await run<AnyStatus>("any.status");
      const where = (s.address ?? "").replace(/^https?:\/\//, "");
      const names = s.spaces.map((x) => x.name || "(unnamed)").join(", ");
      found.textContent = `Glossa found Any at ${where}. It has ${s.spaces.length === 1 ? "1 space" : `${s.spaces.length} spaces`}${names ? `: ${names}.` : "."}`;
    } catch {
      found.textContent = "";
    }
  })();
  return box;
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
        : `Glossa looks for Any at ${usual}${r.address && !notFound ? `, and found it at ${r.address.replace(/^http:\/\//, "")} last time` : ""}. If your Any app uses another address on this computer, enter it here.`),
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
