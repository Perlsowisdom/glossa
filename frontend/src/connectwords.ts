// The plain words of Settings → Connections: what each connection does, how it signs in, and what
// its state means. No command names or internal terms; the interface passes in what the commands
// returned (connect.list, connect.test) and shows what comes back.

export type Capability = "import" | "export" | "cite";

export interface ConnectionInfo {
  id: string;
  name: string;
  sign_in: "key" | "app" | "folder";
  capabilities: Capability[];
  sends: string;
  enabled: boolean;
  key_set: boolean;
  key?: { label: string; help: string; required: boolean };
}

export interface TestOutcome {
  ok: boolean;
  via?: string;
  message: string;
}

const DOES: Record<Capability, string> = {
  import: "bring in your sources",
  export: "send your work out",
  cite: "cite them in your writing",
};

function joinAnd(parts: string[]): string {
  if (parts.length <= 1) return parts.join("");
  return parts.slice(0, -1).join(", ") + " and " + parts[parts.length - 1];
}

// whatItDoes: "Zotero lets you bring in your sources and cite them in your writing."
export function whatItDoes(c: ConnectionInfo): string {
  if (c.id === "readwise") return "Readwise lets you bring in your highlights as notes, one for each book or article.";
  if (c.id === "any") return "Any lets you send notes and sermons to a space in the Any app, and bring its pages in as notes.";
  const parts = (["import", "export", "cite"] as Capability[]).filter((k) => c.capabilities.includes(k)).map((k) => DOES[k]);
  return parts.length ? `${c.name} lets you ${joinAnd(parts)}.` : `${c.name} is a link to another program.`;
}

// howItSignsIn: the one line about what the person needs for it.
export function howItSignsIn(c: ConnectionInfo): string {
  if (c.id === "readwise") return "Needs your Readwise token.";
  switch (c.sign_in) {
    case "app": return c.key ? `Works with the ${c.name} app on this computer. A key is only needed if the app is not there.` : `Works with the ${c.name} app on this computer.`;
    case "key": return `Needs a key from ${c.name}.`;
    case "folder": return `Uses a folder on this computer.`;
  }
}

export interface Status { text: string; kind: "" | "ok" | "warn" }

// statusOf: Off, Checking, Connected or Needs attention. test is undefined until a check has finished.
export function statusOf(c: ConnectionInfo, test: TestOutcome | undefined): Status {
  if (!c.enabled) return { text: "Off", kind: "" };
  if (!test) return { text: "Checking…", kind: "" };
  return test.ok ? { text: "Connected", kind: "ok" } : { text: "Needs attention", kind: "warn" };
}

// keyBox: whether the key field is shown straight away (a key is required) or one step away.
export function keyShownByDefault(c: ConnectionInfo): boolean {
  return !!c.key?.required;
}

export function keyState(c: ConnectionInfo): string {
  if (!c.key) return "";
  return c.key_set ? `A ${c.key.label} is saved on this computer.` : `No ${c.key.label} saved.`;
}

// switchLabel and switchHint: the privacy switch's words.
export function switchLabel(c: ConnectionInfo): string {
  return `Use ${c.name}`;
}

export function switchHint(c: ConnectionInfo): string {
  return `${c.sends} Off until you turn it on.`;
}

// failureText turns a failed command into one sentence; commands already speak plainly.
export function failureText(e: unknown): string {
  const m = (e as { info?: { message?: string } })?.info?.message ?? (e instanceof Error ? e.message : String(e));
  return m.replace(/^[a-z_]+: /, "");
}
