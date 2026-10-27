// Accessibility checks (#310): axe-core (through jest-axe) on a rendered
// panel, and the comparison of its violations with the committed baseline
// `__tests__/a11y-baseline.json` (component → rule id → nodes).
import { configureAxe } from "jest-axe";

export const UPDATE_COMMAND = "UPDATE_A11Y=1 npm test";

// Colour contrast needs real layout and colours, which jsdom has not:
// it stays with the responsive audit and the browser check.
const axe = configureAxe({ rules: { "color-contrast": { enabled: false } } });

/** Rule id → number of offending nodes axe finds in `container`. */
export async function axeCounts(container) {
  const results = await axe(container);
  const counts = {};
  for (const v of results.violations) counts[v.id] = (counts[v.id] ?? 0) + v.nodes.length;
  return counts;
}

/**
 * What differs from the baseline, one sentence per rule, naming the
 * component and the rule: a new rule or a higher count is a regression;
 * a lower count asks for the baseline to be lowered.
 */
export function compareToBaseline(component, counts, baseline = {}) {
  const byRule = (o) => Object.entries(o).sort(([a], [b]) => a.localeCompare(b));
  const problems = [];
  for (const [rule, n] of byRule(counts)) {
    const was = baseline[rule] ?? 0;
    if (was === 0) problems.push(`${component}: new violation ${rule} (${n} node${n === 1 ? "" : "s"})`);
    else if (n > was) problems.push(`${component}: ${rule} rose from ${was} to ${n}`);
  }
  for (const [rule, was] of byRule(baseline)) {
    const n = counts[rule] ?? 0;
    if (n < was) problems.push(`${component}: ${rule} fell from ${was} to ${n} — lower the baseline with ${UPDATE_COMMAND}`);
  }
  return problems;
}
