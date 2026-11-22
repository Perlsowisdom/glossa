// Settings → Connections: the programs and services Glossa can link to (Zotero first). Each has a
// switch that is off until the person turns it on, with one plain sentence beside it saying exactly
// what is sent and where. When on, a check says whether it works; a key, if one is needed or useful,
// is one step away and goes to the system keychain (never shown back).

import { run } from "../api";
import {
  ConnectionInfo, TestOutcome, failureText, howItSignsIn, keyShownByDefault, keyState, statusOf, switchHint, switchLabel, whatItDoes,
} from "../connectwords";
import { toggleSwitch } from "../controls";
import { badge, button, clear, field, h } from "../dom";
import { anySection } from "./anycard";

export function connectionsSection(): { el: HTMLElement; show: () => void; focus: () => void } {
  const list = h("div", { class: "connections" });
  const note = h("p", { class: "msg", "aria-live": "polite" });
  const el = h("div", {}, list, note);
  const tests = new Map<string, TestOutcome>();
  const cards = new Map<string, HTMLElement>();
  const openMore = new Set<string>(); // "More options" stays open across redraws

  const say = (text: string, kind = "") => { note.className = `msg ${kind}`.trim(); note.textContent = text; };

  async function show() {
    let infos: ConnectionInfo[];
    try {
      infos = await run<ConnectionInfo[]>("connect.list");
    } catch (e) {
      return say(failureText(e), "error");
    }
    cards.clear();
    clear(list, infos.length ? infos.map((c) => { const card = h("section", { class: "settings-card connection", "data-connection": c.id }); cards.set(c.id, card); draw(card, c); return card; })
      : h("p", { class: "hint" }, "There are no connections yet."));
    for (const c of infos) if (c.enabled && !tests.has(c.id)) void check(c.id);
  }

  async function reload(id: string) {
    const infos = await run<ConnectionInfo[]>("connect.list");
    const c = infos.find((x) => x.id === id);
    const card = cards.get(id);
    if (c && card) draw(card, c);
  }

  async function check(id: string) {
    tests.delete(id);
    await reload(id);
    try {
      const r = await run<TestOutcome>("connect.test", { id });
      tests.set(id, r);
    } catch (e) {
      tests.set(id, { ok: false, message: failureText(e) });
    }
    await reload(id);
  }

  const FOCUS = { switch: "input.switch", test: "button[data-test]", key: "input[type=password]" };
  function focusedPart(card: HTMLElement): keyof typeof FOCUS | undefined {
    const a = document.activeElement;
    return (Object.keys(FOCUS) as (keyof typeof FOCUS)[]).find((k) => a && card.contains(a) && a.matches(FOCUS[k]));
  }

  function draw(card: HTMLElement, c: ConnectionInfo) {
    const had = focusedPart(card);
    const test = tests.get(c.id);
    const st = statusOf(c, test);
    const parts: (Node | null)[] = [
      h("div", { class: "connection-head" }, h("h3", {}, c.name), badge(st.text, st.kind)),
      h("p", { class: "hint connection-intro" }, `${whatItDoes(c)} ${howItSignsIn(c)}`),
      toggleSwitch({
        label: switchLabel(c), checked: c.enabled, hint: switchHint(c),
        onChange: async (on) => {
          try {
            await run(on ? "connect.enable" : "connect.disable", { id: c.id });
          } catch (e) {
            say(failureText(e), "error");
          }
          tests.delete(c.id);
          if (on) await check(c.id);
          else await reload(c.id);
        },
      }),
    ];
    if (c.enabled) parts.push(detail(c, test));
    clear(card, parts);
    if (had) card.querySelector<HTMLElement>(FOCUS[had])?.focus();
  }

  function detail(c: ConnectionInfo, test: TestOutcome | undefined): HTMLElement {
    const testBtn = button("Test connection", async () => {
      testBtn.disabled = true;
      testBtn.textContent = "Testing…";
      await check(c.id);
    });
    testBtn.dataset.test = c.id;
    const body = h("div", { class: "connection-detail" },
      h("p", { class: `connection-status${test && !test.ok ? " bad" : ""}`, role: "status", "aria-live": "polite" },
        test ? test.message : "Checking…"),
      c.id === "any" ? anySection(c.key_set, () => check(c.id), say) : null,
      h("div", { class: "row wrap" }, testBtn));
    if (c.key) {
      const keyForm = keyBox(c);
      body.append(keyShownByDefault(c) ? keyForm
        : moreOptions(c.id, keyForm));
    }
    return body;
  }

  function moreOptions(id: string, inner: HTMLElement): HTMLElement {
    const d = h("details", { class: "more connection-more" }, h("summary", {}, "More options"), inner);
    d.open = openMore.has(id);
    d.addEventListener("toggle", () => { if (d.open) openMore.add(id); else openMore.delete(id); });
    return d;
  }

  function keyBox(c: ConnectionInfo): HTMLElement {
    const key = h("input", { type: "password", autocomplete: "off", spellcheck: "false", placeholder: "Paste your key" });
    const form = h("form", { class: "connection-key", "aria-label": c.key!.label },
      h("p", { class: "hint" }, `${c.key!.help} ${keyState(c)} It is kept in your system keychain and never shown again.`),
      field(c.key!.label, key),
      h("div", { class: "row wrap" },
        h("button", { type: "submit", class: "primary" }, "Save key"),
        c.key_set ? button("Remove key", async () => {
          if (!confirm(`Remove the ${c.key!.label} from this computer?`)) return;
          try { await run("connect.clear_key", { id: c.id }); say("Key removed."); } catch (e) { say(failureText(e), "error"); }
          await check(c.id);
        }, { class: "danger" }) : null));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const k = key.value.trim();
      key.value = "";
      if (!k) return say("Paste a key first.");
      try {
        await run("connect.set_key", { id: c.id, key: k });
        say("Key saved to the keychain.", "ok");
      } catch (err) {
        return say(failureText(err), "error");
      }
      await check(c.id);
    });
    return form;
  }

  return { el, show, focus: () => el.querySelector<HTMLElement>("input.switch")?.focus() };
}
