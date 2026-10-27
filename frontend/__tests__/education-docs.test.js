import fs from "node:fs";
import path from "node:path";
import emptyStates, { PANELS, STATES } from "@/lib/emptyStates";
import glossary, { findTerm } from "@/lib/glossary";
import { DENY } from "@/test/emptyStateChecks";

const REPO = path.join(__dirname, "..", "..");
const read = (file) => fs.readFileSync(path.join(REPO, "docs/product/education", file), "utf8");

/** { panel: { tips, rows: [{ state, copy, field }] } } from the inventory's tables. */
function inventory(text) {
  const out = {};
  for (const block of text.split(/^### (?=monitor\.)/m).slice(1)) {
    const panel = block.split("\n")[0].trim();
    const tips = [...(block.match(/^Tips: (.+)$/m)?.[1] ?? "").matchAll(/`([^`]+)`/g)].map((m) => m[1]);
    const rows = [...block.matchAll(/^\| `(\w+)`[^|]*\| ([^|]+) \| ([^|]+) \|$/gm)].map((m) => ({ state: m[1], copy: m[2].trim(), field: m[3].trim() }));
    out[panel] = { tips, rows };
  }
  return out;
}

const sentences = (t) => t.split(/(?<=[.!?])\s+/).filter(Boolean);

describe("empty-state inventory (#250)", () => {
  const inv = inventory(read("empty-states.md"));

  // Scenario: every panel has at least no data yet, unavailable, empty-because-normal, each with its field
  test("every panel of the catalogue has loading, empty, stale and error rows, each naming a field", () => {
    expect(Object.keys(inv).sort()).toEqual(Object.keys(PANELS).sort());
    const bad = [];
    for (const [panel, { rows }] of Object.entries(inv)) {
      expect([panel, rows.map((r) => r.state)]).toEqual([panel, [...STATES]]);
      for (const r of rows) if (!r.field) bad.push(`${panel}.${r.state}: no field`);
    }
    expect(bad).toEqual([]);
  });

  // Scenario: a copy line has at most two sentences, no promise
  test("each copy is the catalogue's sentence, at most two sentences, with no forecast or advice word", () => {
    const bad = [];
    for (const [panel, { rows }] of Object.entries(inv)) {
      for (const r of rows) {
        if (r.copy !== emptyStates[panel][r.state]) bad.push(`${panel}.${r.state}: differs from empty-states.json`);
        if (sentences(r.copy).length > 2) bad.push(`${panel}.${r.state}: over two sentences`);
        for (const w of DENY) if (r.copy.toLowerCase().includes(w)) bad.push(`${panel}.${r.state}: ${w}`);
      }
    }
    expect(bad).toEqual([]);
  });

  test("each tip a panel names is a glossary key or label", () => {
    const missing = Object.entries(inv).flatMap(([panel, { tips }]) => tips.filter((t) => !findTerm(t)).map((t) => `${panel}: ${t}`));
    expect(missing).toEqual([]);
  });

  // Scenario: the bot is unreachable
  test("an unreachable bot is never worded as holding nothing", () => {
    const nothing = /^(no|nothing) /i;
    const bad = Object.entries(inv).flatMap(([panel, { rows }]) => rows.filter((r) => r.state === "error" && nothing.test(r.copy)).map((r) => panel));
    expect(bad).toEqual([]);
    expect(inv["monitor.header"].rows.find((r) => r.state === "error").copy).toMatch(/not answering/i);
    expect(inv["monitor.panel.others"].rows.find((r) => r.state === "error").copy).toMatch(/not the same as none/i);
  });
});

describe("comprehension test protocol (#338)", () => {
  const text = read("comprehension-test.md");
  const tasks = text
    .split(/^### (?=T\d)/m)
    .slice(1)
    .map((b) => ({
      id: b.split(" ")[0],
      prompt: b.match(/^- \*\*Prompt:\*\* (.+)$/m)?.[1],
      expected: b.match(/^- \*\*Expected answer:\*\* (.+)$/m)?.[1],
      keys: [...(b.match(/^- \*\*Glossary keys:\*\* (.+)$/m)?.[1] ?? "").matchAll(/`([^`]+)`/g)].map((m) => m[1]),
      limit: b.match(/^- \*\*Time limit:\*\* (\d+) min$/m)?.[1],
    }));

  // Scenario: A facilitator can run a round without help
  test("it lists participants, tasks, scoring, time limits and the pass bar", () => {
    for (const heading of ["## 1. Who takes part", "## 2. Tasks", "## 3. Scoring", "## 5. Pass bar"]) expect(text).toContain(heading);
    expect(text).toMatch(/\*\*5 people per round/);
    expect(text).toMatch(/\| \*\*0\*\* \|[\s\S]*\| \*\*1\*\* \|[\s\S]*\| \*\*2\*\* \|/);
    expect(text).toMatch(/80 %/);
    expect(text).toMatch(/median of 5 minutes or less/);
    expect(text).toMatch(/Assumption, to confirm after the first round/);
    expect(text).toMatch(/needs legal review/);
    expect(tasks.map((t) => t.id)).toEqual(["T1", "T2", "T3", "T4", "T5", "T6"]);
    for (const t of tasks) expect([t.id, Boolean(t.prompt), Boolean(t.expected), Boolean(t.limit)]).toEqual([t.id, true, true, true]);
  });

  // Scenario: Expected answers match the content
  test("every task names glossary keys that exist, and its expected answer reuses the entry's words", () => {
    for (const t of tasks) {
      expect([t.id, t.keys.length > 0]).toEqual([t.id, true]);
      for (const k of t.keys) expect([t.id, k, Boolean(findTerm(k))]).toEqual([t.id, k, true]);
    }
    // T2 and T6 quote the entries' own sentences.
    expect(tasks[1].expected).toContain("a profit on paper is not yet income");
    expect(glossary["unrealized p&l"].risk_income).toMatch(/profit on paper is not yet income/);
    expect(tasks[4].expected).toContain("Up means fewer stop-outs but bigger losses each");
    expect(glossary.stop_loss_multiplier.risk_income).toMatch(/Up: fewer stop-outs, bigger losses each/);
  });

  // Scenario: Privacy
  test("results are stored only under data/product/ and no name or contact appears", () => {
    expect(text).toMatch(/only\*\* under `data\/product\/comprehension\/<round>\/`/);
    expect(text).not.toMatch(/[\w.+-]+@[\w-]+\.\w+/);
    expect(text).not.toMatch(/\+?\d[\d ()-]{8,}\d/);
    expect(text).toMatch(/No name, email, phone number or employer/);
  });
});
