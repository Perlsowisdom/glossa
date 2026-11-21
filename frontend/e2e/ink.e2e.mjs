import { existsSync, mkdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { inflateSync } from "node:zlib";

// T37 stage 1: ink blocks in notes and sermons, drawn with the real mouse (and synthetic pen events).

const fresh = async (t, body = "") => {
  await t.openNote(body);
  await t.focusEnd();
};

// stroke draws a line through points (fractions of the page width, and of 150 px down) with the mouse.
export async function stroke(t, pts, nth = 0) {
  const box = await t.page.locator(`${t.root} .ink-page`).nth(nth).boundingBox();
  const at = ([fx, fy]) => [box.x + fx * box.width, box.y + fy * 150];
  const [x0, y0] = at(pts[0]);
  await t.page.mouse.move(x0, y0);
  await t.page.mouse.down();
  for (const p of pts.slice(1)) {
    const [x, y] = at(p);
    await t.page.mouse.move(x, y, { steps: 6 });
  }
  await t.page.mouse.up();
}

export const wave = (y0, n = 8) => Array.from({ length: n }, (_, i) => [0.1 + (0.8 * i) / (n - 1), y0 + (i % 2 ? 0.35 : 0)]);

// the pages of strokes in the saved Markdown
export async function savedInk(t) {
  await t.page.waitForTimeout(500); // the editor has handed the strokes over
  const md = await t.saved();
  return [...md.matchAll(/```ink\n(.*)\n```/g)].map((m) => JSON.parse(m[1]));
}

export async function addInk(t) {
  await t.press("Enter");
  await t.type("/hand");
  await t.press("Enter");
  await t.page.waitForSelector(`${t.root} .ink-block`);
}

const shared = {};

const tool = (t, label) => t.page.click(`${t.root} .ink-bar [aria-label="${label}"]`);
const count = (t) => t.page.locator(`${t.root} .ink-page path`).count();

// pen draws with synthetic pen pointer events (pressure, tilt, the eraser end), since the harness has no pen.
export async function pen(t, pts, o = {}) {
  await t.page.evaluate(({ root, pts, o }) => {
    const svg = document.querySelector(`${root} .ink-page`);
    const r = svg.getBoundingClientRect();
    const ev = (type, [fx, fy, pr], buttons) => new PointerEvent(type, {
      bubbles: true, cancelable: true, pointerId: o.id ?? 7, pointerType: o.type ?? "pen", isPrimary: true, pressure: pr, tiltX: 20, tiltY: 10,
      button: type === "pointerup" ? 0 : (o.button ?? 0), buttons: type === "pointerup" ? 0 : buttons, clientX: r.left + fx * r.width, clientY: r.top + fy * 150,
    });
    svg.dispatchEvent(ev("pointerdown", pts[0], o.buttons ?? 1));
    for (const p of pts.slice(1)) svg.dispatchEvent(ev("pointermove", p, o.buttons ?? 1));
    svg.dispatchEvent(ev("pointerup", pts[pts.length - 1], 0));
  }, { root: t.root, pts, o });
}

export const line = (y, from = 0.1, to = 0.9, n = 10) => Array.from({ length: n }, (_, i) => [from + ((to - from) * i) / (n - 1), y]);

const first = [
  {
    row: "/handwriting puts an ink block in a note, and a mouse draws strokes in it",
    shot: "ink-01-draw.png",
    async run(t) {
      await fresh(t);
      await t.type("Notes on Romans 8");
      await addInk(t);
      t.assert.equal((await t.page.locator(`${t.root} .ink-bar button`).count()) > 8, true, "the tool strip");
      await stroke(t, wave(0.2));
      await stroke(t, wave(0.9));
      t.assert.equal(await t.page.locator(`${t.root} .ink-page path`).count(), 2);
      const docs = await savedInk(t);
      t.assert.equal(docs.length, 1);
      t.assert.equal(docs[0].s.length, 2);
      t.assert.equal(docs[0].s[0].t, "pen");
      t.assert.ok(docs[0].s[0].p.length > 5);
      t.assert.ok((await t.blocks()).at(-1).tag === "p", "a line to write on after the ink");
    },
  },
  {
    row: "Ink is saved with the note, survives a restart, and comes back drawn",
    shot: "ink-02-restart.png",
    timeout: 180000,
    async run(t) {
      await fresh(t, "Intro line");
      await t.focusEnd();
      await addInk(t);
      await stroke(t, wave(0.2));
      const saved = await t.saved();
      t.assert.ok(saved.startsWith('Intro line\n\n```ink\n{"v":2,"h":'), saved.slice(0, 60));
      const id = t.id;
      shared.note = { id, body: saved };
      await t.app.restart();
      const page = t.app.page;
      t.assert.equal((await t.app.run("note.get", { id })).body, saved, "the same Markdown after a restart");
      await page.evaluate((id) => { location.hash = `#notes/note/${id}`; }, id);
      await page.locator(".note-page .ink-page path").first().waitFor({ timeout: 15000 });
      t.assert.equal(await page.locator(".note-page .ink-page path").count(), 1);
      await t.app.shot("ink-02-restart.png");
      t.shotTaken = true;
    },
  },
  {
    row: "The eraser removes a whole stroke; undo and redo step through changes",
    shot: "ink-05-erase-undo.png",
    async run(t) {
      await fresh(t);
      await addInk(t);
      await tool(t, "Pen");
      await stroke(t, line(0.15));
      await stroke(t, line(0.5));
      await stroke(t, line(0.85));
      t.assert.equal(await count(t), 3);
      await tool(t, "Eraser (removes a whole stroke)");
      await stroke(t, [[0.5, 0.4], [0.5, 0.6]]); // across the middle stroke only
      t.assert.equal(await count(t), 2, "one whole stroke gone");
      t.assert.equal((await savedInk(t))[0].s.length, 2);
      await tool(t, "Undo last stroke");
      t.assert.equal(await count(t), 3, "undo brings it back");
      await tool(t, "Redo");
      t.assert.equal(await count(t), 2, "redo removes it again");
      await tool(t, "Undo last stroke");
      await tool(t, "Undo last stroke");
      t.assert.equal(await count(t), 2, "undo also takes back the last drawn stroke");
      await t.page.locator(`${t.root} .ink-page`).focus();
      await t.press("Control+z");
      t.assert.equal(await count(t), 1, "Ctrl+Z with the page focused");
      await t.press("Control+y");
      t.assert.equal(await count(t), 2, "Ctrl+Y");
      t.assert.equal((await savedInk(t))[0].s.length, 2, "saved as shown");
      await t.shot();
    },
  },
];

const more = [
];

export default [...first, ...more];
