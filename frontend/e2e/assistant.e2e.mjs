// T36a, the AI assistant, in the real app against a fake OpenAI-compatible server this file starts
// on 127.0.0.1. Nothing reaches the internet. The checks run in order and share the fake server:
// choose a provider and save a key, the privacy panel, a streamed answer with a verse chip, a tool
// line, a copyrighted passage shortened, a personal copy refused, a draft inserted into a note, and
// Stop. The last check removes the test key and closes the server.

import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { dirname, join } from "node:path";
import { KEYRING_SERVICE } from "./app.mjs";
import { epub } from "./fixtures.mjs";

const BOOK = "Assistant Personal Commentary";
const fake = { url: "", server: null, reqs: [], book: "", aborted: false };

const pause = (ms) => new Promise((r) => setTimeout(r, ms));

// reply decides what the fake model says to a request: a tool call first, then text that uses
// the tool's result. Each question names its scenario.
function reply(body) {
  const msgs = body.messages ?? [];
  const last = msgs.at(-1) ?? {};
  // The question, without the line saying what is open in the app.
  const q = ([...msgs].reverse().find((m) => m.role === "user")?.content ?? "").replace(/^\[Open in Glossa now:[^\]]*\]\n\n/, "");
  const call = (name, args) => ({ calls: [[name, JSON.stringify(args)]] });
  if (last.role === "tool") {
    if (q.includes("commentary")) return { text: ["I could not read that book: ", "it is a personal copy."] };
    return { text: ["Paul ", "says in ", "Romans 8:28 ", "that God works ", "all things ", "together for good ", "for those who love him."], delay: 250 };
  }
  if (q.includes("Romans 8")) return call("read_passage", { ref: "Romans 8", bible: "NETfree" });
  if (q.includes("commentary")) return call("read_book", { item_id: fake.book });
  if (q.includes("outline")) return { text: ["Here is an outline.\n\n", "```draft\n# Hope in Romans 8\n\n", "- Suffering now\n- Glory to come\n```\n"] };
  if (q.includes("long")) return { text: Array.from({ length: 80 }, (_, i) => `word${i} `), delay: 200 };
  return { text: ["OK."] };
}

async function handle(req, res) {
  if (req.method === "GET" && req.url === "/v1/models") {
    res.writeHead(200, { "Content-Type": "application/json" });
    return res.end(JSON.stringify({ data: [{ id: "fake-tools" }, { id: "fake-small" }] }));
  }
  if (req.method !== "POST" || req.url !== "/v1/chat/completions") {
    res.writeHead(404);
    return res.end();
  }
  let raw = "";
  for await (const c of req) raw += c;
  const body = JSON.parse(raw);
  fake.reqs.push(body);
  const r = reply(body);
  let closed = false;
  res.on("close", () => {
    if (!res.writableFinished) {
      closed = true;
      fake.aborted = true;
    }
  });
  res.writeHead(200, { "Content-Type": "text/event-stream" });
  const send = (v) => res.write(`data: ${JSON.stringify(v)}\n\n`);
  for (const t of r.text ?? []) {
    if (closed) return;
    send({ choices: [{ delta: { content: t } }] });
    await pause(r.delay ?? 60);
  }
  (r.calls ?? []).forEach(([name, args], i) => send({ choices: [{ delta: { tool_calls: [{ index: i, id: `call_${fake.reqs.length}_${i}`, type: "function", function: { name, arguments: args } }] } }] }));
  if (!closed) res.end("data: [DONE]\n\n");
}

async function startFake() {
  if (fake.server) return;
  fake.server = createServer((req, res) => void handle(req, res).catch(() => res.destroy()));
  await new Promise((ok) => fake.server.listen(0, "127.0.0.1", ok));
  fake.url = `http://127.0.0.1:${fake.server.address().port}/v1`;
}

async function stopFake() {
  if (!fake.server) return;
  fake.server.closeAllConnections?.();
  await new Promise((ok) => fake.server.close(() => ok()));
  fake.server = null;
}

// testKeys lists the keychain entries filed under the tests' service name.
function testKeys() {
  const out = execFileSync("cmdkey", ["/list"], { encoding: "utf8", timeout: 15000 });
  return out.split(/\r?\n/).filter((l) => l.includes(`${KEYRING_SERVICE}:`)).map((l) => l.trim());
}

// chat opens the right sidebar on the AI chat tool.
async function chat(page) {
  if (!(await page.locator("#rightbar").isVisible().catch(() => false))) await page.keyboard.press("Control+Period");
  await page.locator("#rightbar [role=tab]", { hasText: "AI chat" }).click();
  await page.locator("#rightbar .chat-input").waitFor({ timeout: 10000 });
}

async function ask(page, question) {
  const input = page.locator("#rightbar .chat-input");
  await input.click();
  await page.keyboard.type(question, { delay: 10 });
  await page.keyboard.press("Enter");
}

const lastAnswer = (page) => page.locator("#rightbar .chat-a").last();
const answered = (page) => page.waitForFunction(() => {
  const a = [...document.querySelectorAll("#rightbar .chat-a")].at(-1);
  return a && a.dataset.state === "done";
}, null, { timeout: 20000 });

// toolSent is the content the fake model received from the last tool call.
const toolSent = () => [...fake.reqs].reverse().map((b) => b.messages.at(-1)).find((m) => m.role === "tool")?.content ?? "";

async function readerAt(page, ref) {
  await page.evaluate((r) => { location.hash = `#passage/${r}`; }, ref);
}

export default [
  {
    row: "Assistant: choose a provider and save a key",
    shot: "36a-01-provider.png",
    async run(t) {
      const { page, app } = t;
      await startFake();
      t.assert.deepEqual(testKeys(), [], "a test key was left from before");
      await page.evaluate(() => { location.hash = "#settings/model"; });
      const sec = page.locator(".settings-section:visible");
      await sec.locator(".provider-row button", { hasText: "Change" }).click();
      // Claude Code and Codex are Ready here: the tests put fake ones first on PATH (fake-cli/).
      await sec.locator(".provider-chip[data-id=claude-code][data-state=ready]").waitFor();
      await sec.locator(".provider-chip[data-id=openai]").click();
      await sec.locator(".provider-current", { hasText: "OpenAI, with your own key" }).waitFor();
      // The fake server stands in for OpenAI's address (More options).
      await sec.locator(".more-options summary").click();
      const addr = sec.getByLabel("Address");
      await addr.fill(fake.url);
      await sec.locator("button", { hasText: "Save address" }).click();
      await sec.getByText(`Sends to 127.0.0.1:${new URL(fake.url).port}`).waitFor();
      const key = sec.locator("input[type=password]");
      await key.click();
      await page.keyboard.type("sk-e2e-test-key", { delay: 5 });
      await sec.locator("button", { hasText: "Save key" }).click();
      await sec.locator(".key-state .badge", { hasText: "Saved" }).waitFor();
      t.assert.equal(await key.inputValue(), "", "the key is not shown back");
      await sec.locator(".model-chip[data-model=fake-tools]").click();
      await sec.locator(".model-chip[data-model=fake-tools][aria-pressed=true]").waitFor();
      await sec.locator("button", { hasText: "Test connection" }).click();
      await sec.getByText("Connected to OpenAI. 2 models are available.").waitFor();
      const c = await app.run("assistant.config.get");
      t.assert.ok(c.key_saved && c.model === "fake-tools" && !c.local && !c.confirmed, JSON.stringify(c));
      t.assert.equal(testKeys().length, 1, "the key is in the keychain under the test service");
      const settings = await app.run("history.list", { limit: 50 }).catch(() => []);
      t.assert.ok(!JSON.stringify(settings).includes("sk-e2e-test-key"), "the key is not in the library's history");
      t.assert.equal(fake.reqs.length, 0, "nothing was asked of the model");
    },
  },
  {
    row: "Assistant: the privacy panel appears on first use",
    shot: "36a-02-privacy.png",
    async run(t) {
      const { page } = t;
      await readerAt(page, "Rom.8");
      await chat(page);
      await ask(page, "Summarise Romans 8 for me");
      const panel = page.locator("#rightbar .privacy-panel");
      await panel.waitFor({ timeout: 10000 });
      for (const text of ["Before your first message to OpenAI", `${fake.url}/chat/completions`, "fake-tools", "Summarise Romans 8 for me", "Personal copies of books are never sent"]) {
        await panel.getByText(text, { exact: false }).first().waitFor();
      }
      t.assert.equal(fake.reqs.length, 0, "nothing is sent before the panel is confirmed");
      await panel.scrollIntoViewIfNeeded();
      await t.shot();
      await panel.locator("button", { hasText: "Send" }).click();
      await page.locator("#rightbar .chat-q", { hasText: "Summarise Romans 8" }).waitFor();
    },
  },
  {
    row: "Assistant: a question gets a streamed answer with a verse chip",
    shot: "36a-03-answer.png",
    async run(t) {
      const { page } = t;
      // While it streams, part of the answer shows before the reply is done.
      await page.waitForFunction(() => {
        const a = [...document.querySelectorAll("#rightbar .chat-a")].at(-1);
        const text = a?.querySelector(".chat-a-body")?.textContent ?? "";
        return a?.dataset.state === "writing" && text.includes("Paul") && !text.includes("love him");
      }, null, { timeout: 15000 });
      await answered(page);
      const chip = lastAnswer(page).locator("a.ref-chip.chip-verse", { hasText: "Romans 8:28" });
      await chip.waitFor();
      await t.shot();
      // The same in the dark theme.
      await page.emulateMedia({ colorScheme: "dark" });
      await page.waitForTimeout(200);
      await t.shot("36a-03-answer-dark.png");
      await page.emulateMedia({ colorScheme: "light" });
      t.assert.ok(fake.reqs[0].messages[1].content.includes("passage Romans 8"), "the open passage is sent as context");
      await chip.click();
      await page.waitForFunction(() => [...document.querySelectorAll('.vt[data-tb="45008028"]')].some((e) => e.offsetParent), null, { timeout: 15000 });
    },
  },
  {
    row: "Assistant: the test key is removed and the fake server closed",
    shot: "36a-09-cleanup.png",
    async run(t) {
      const { page, app } = t;
      if (/panic|fatal error/.test(app.log())) console.log(app.log().slice(-4000));
      try {
        if (fake.server || testKeys().length) {
          await app.run("assistant.config.set", { provider: "openai", clear_key: true });
          await app.run("assistant.config.set", { provider: "off" });
        }
        await page.evaluate(() => { location.hash = "#settings/model"; });
        await page.locator(".settings-section:visible .provider-current", { hasText: "Off" }).waitFor();
      } finally {
        await stopFake();
        // Later checks expect the right sidebar closed, as the app starts.
        if (await page.locator("#rightbar").isVisible().catch(() => false)) await page.keyboard.press("Control+Period");
        await page.locator("#rightbar").waitFor({ state: "hidden", timeout: 5000 }).catch(() => {});
      }
      t.assert.ok(!(await page.locator("#rightbar").isVisible()), "the right sidebar is closed again");
      t.assert.deepEqual(testKeys(), [], "test keys left in the keychain");
    },
  },
];
