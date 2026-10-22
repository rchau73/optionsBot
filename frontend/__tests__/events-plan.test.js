/**
 * @jest-environment node
 */
import fs from "node:fs";
import path from "node:path";
import { EVENTS, PROPERTIES } from "@/lib/analytics/plan";

// lib/analytics/plan.js is the collector's copy of the event plan's tables
// (docs/product/analytics/event-plan.md §2 and §4); this keeps the two equal,
// so a change of the plan cannot leave the collector behind or ahead.
const doc = fs.readFileSync(path.join(process.cwd(), "..", "docs", "product", "analytics", "event-plan.md"), "utf8");

/** The body rows of the first table under "## <n>.", as arrays of cells. */
function table(section) {
  const start = doc.indexOf(`\n## ${section}.`);
  const rest = doc.slice(start + 1);
  const end = rest.indexOf("\n## ", 3);
  const lines = (end < 0 ? rest : rest.slice(0, end)).split("\n").filter((l) => l.startsWith("|"));
  return lines.slice(2).map((l) => l.replace(/^\||\|$/g, "").split("|").map((c) => c.trim()));
}
const names = (cell) => [...cell.matchAll(/`([^`]+)`/g)].map((m) => m[1]);

test("the collector's events and their properties are the plan's §2", () => {
  const plan = Object.fromEntries(table("2").map((r) => [names(r[0])[0], names(r[2])]));
  expect(EVENTS).toEqual(plan);
});

test("the collector's properties, types and enum values are the plan's §4", () => {
  const rows = table("4");
  expect(Object.keys(PROPERTIES).sort()).toEqual(rows.map((r) => names(r[0])[0]).sort());
  for (const r of rows) {
    const rule = PROPERTIES[names(r[0])[0]];
    expect(rule.type).toBe(r[1]);
    // The values are the cell's names outside its "(notes)".
    if (rule.type === "enum") expect(rule.values).toEqual(names(r[2].replace(/\([^)]*\)/g, "")));
  }
});
