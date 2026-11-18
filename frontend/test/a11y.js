// Accessibility checks (#310): axe-core (through jest-axe) on a rendered
// panel, and the comparison of its violations with the committed baseline
// `__tests__/a11y-baseline.json` (component → rule id → nodes).
import axeCore from "axe-core";
import { configureAxe } from "jest-axe";

export const UPDATE_COMMAND = "UPDATE_A11Y=1 npm test";

// Colour contrast needs real layout and colours, which jsdom has not:
// it stays with the responsive audit and the browser check.
const axe = configureAxe({ rules: { "color-contrast": { enabled: false } } });

/** Rule id → number of offending nodes of axe's results. */
function countViolations(results) {
  const counts = {};
  for (const v of results.violations) counts[v.id] = (counts[v.id] ?? 0) + v.nodes.length;
  return counts;
}

/** Rule id → number of offending nodes axe finds in `container`. */
export async function axeCounts(container) {
  return countViolations(await axe(container));
}

/**
 * The same for the whole rendered document, only `rules` run (#840): a page
 * check, so the rules that look at the page as a whole (one main landmark,
 * landmarks at the top level) run too, which a panel's container cannot reach.
 */
export async function pageAxeCounts(rules) {
  // jsdom has no layout, so no point is over any element: axe's "is a modal
  // open" probe (one main, one h1) otherwise throws and skips the rule.
  document.elementFromPoint ??= () => null;
  const results = await axeCore.run(document, { runOnly: { type: "rule", values: rules } });
  // A rule axe could not run is not a pass: fail rather than pin it as one.
  const errors = results.incomplete.filter((r) => r.nodes.some((n) => [...n.any, ...n.all, ...n.none].some((c) => c.id === "error-occurred")));
  if (errors.length) throw new Error(`axe could not run ${errors.map((r) => r.id).join(", ")} on this page`);
  return countViolations(results);
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
