// The notice after deleting something the bin holds: `"X" went to the bin. Undo`. It is the same for notes,
// sermons, folders and lists, and goes away by itself after a while (deletewords.ts).

import { Message } from "./api";
import { backNotice, binNotice, NOTICE_MS } from "./deletewords";
import { button, clear } from "./dom";

const timers = new WeakMap<Message, number>();

// binned shows the notice in msg. undo brings the thing back (it returns undefined when that failed).
export function binned(msg: Message, name: string, undo: () => Promise<unknown>, after: () => void, extra = ""): void {
  clear(msg.el, `${binNotice(name, extra)} `, button("Undo", async () => {
    if (await msg.attempt(undo) === undefined) return;
    msg.ok(backNotice(name));
    expire(msg);
    after();
  }, { class: "link" }));
  msg.el.className = "msg ok";
  msg.el.setAttribute("role", "status");
  expire(msg);
}

// expire clears what msg shows after a while, unless something newer has replaced it.
function expire(msg: Message) {
  clearTimeout(timers.get(msg));
  const text = msg.el.textContent;
  timers.set(msg, window.setTimeout(() => { if (msg.el.textContent === text) msg.clear(); }, NOTICE_MS));
}
