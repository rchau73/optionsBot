// The strategy-change lab's read-only API (#539, epic E9): the comparison runs
// cmd/compare wrote (<COMPARE_DIR>/<UTC time>/report.json, read through
// lib/compare.js) with the setting changed and the headline numbers, the
// label and the risk beside every return. Nothing here writes, and nothing
// here applies a setting: a change takes effect only after a manager confirms.
import fs from "node:fs";
import path from "node:path";
import { LIST_LIMIT, listReports, readReport } from "@/lib/compare";

/**
 * The L10 gate (docs/legal/preliminary-answers.md, question L10): the AI
 * analysis beside a report (analysis.json, cmd/analyze) is shown only when
 * this is true. A plain constant, no setting and no env var: opening it is a
 * code change, and __tests__/lab-route.test.js pins it to false until
 * counsel's L10 answer is recorded in that file.
 */
export const L10_ANALYSIS_GATE_OPEN = false;

/** What `analysis` says while the gate is closed. */
export const ANALYSIS_WITHHELD = "withheld pending legal review";

/** The label every lab result carries. */
export const LAB_LABEL = "backtested, not a forecast";

const pct = (part, whole) => (Number.isFinite(part) && Number.isFinite(whole) && whole > 0 ? (part / whole) * 100 : null);
const num = (v) => (Number.isFinite(v) ? v : null);

/** A folder time (20261012T093000Z) as an ISO time. */
export function runTime(id) {
  const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/.exec(id);
  return m ? `${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:${m[6]}Z` : null;
}

/**
 * One config's headline in percentages only (amounts stay in the report):
 * the return with its risk beside it — worst week, drawdown and fees as % of
 * the run's starting equity. A run that did not happen or failed is null.
 */
function backtestFigures(run, equityUSD) {
  if (!run || run.error) return null;
  return {
    return_pct: num(run.return_pct),
    worst_week_pct: num(run.worst_week_pct),
    max_drawdown_pct: num(run.max_drawdown_pct),
    fees_pct: pct(run.fees_usd, equityUSD),
  };
}

function replayFigures(run, equity) {
  if (!run || run.error) return null;
  return {
    return_pct: num(run.return_pct),
    worst_week_pct: num(run.worst_week_pct),
    max_drawdown_pct: pct(run.max_drawdown, equity),
    fees_pct: pct(run.fees, equity),
  };
}

/** The base and proposed headline of one source, or null when it did not run. */
function source(section, figures, equity) {
  if (section?.ran !== true) return null;
  return { base: figures(section.base, equity), proposed: figures(section.proposed, equity) };
}

/** How often the proposal won per window (-windows), or null without them. */
function windowWins(windows) {
  const s = windows?.summary;
  if (!s) return null;
  const one = (x) => (x ? { run: x.run ?? 0, proposed_wins: x.proposed_wins ?? 0, base_wins: x.base_wins ?? 0 } : null);
  return { backtest: one(s.backtest), replay: one(s.replay) };
}

/** The headline of one parsed report: what changed and the numbers that decide it. */
export function headline(report) {
  return {
    changed: (report.diffs ?? []).map((d) => ({ key: d.key, base: d.base, proposed: d.proposed })),
    backtest: source(report.backtest, backtestFigures, report.backtest?.equity_usd),
    replay: source(report.replay, replayFigures, report.replay?.equity),
    stress_ran: report.stress?.ran === true,
    windows: windowWins(report.windows),
    cliffs: Array.isArray(report.neighbours?.cliffs) ? report.neighbours.cliffs.length : null,
  };
}

/** The list row of one run: its id and time, the label and the headline. */
export function runRow(id, report) {
  return { id, time: runTime(id), underlying: report.underlying ?? "", label: LAB_LABEL, ...headline(report) };
}

/** The runs of `dir`, newest first: { runs, skipped } (lib/compare's limit and skips). */
export function listRuns(dir) {
  const { reports, skipped } = listReports(dir, LIST_LIMIT, runRow);
  return { runs: reports, skipped };
}

/**
 * The analysis of a run: the parsed analysis.json only while the L10 gate is
 * open, else the notice — and then the file is never read.
 */
export function analysisOf(dir, id, gateOpen = L10_ANALYSIS_GATE_OPEN) {
  if (!gateOpen) return ANALYSIS_WITHHELD;
  try {
    return JSON.parse(fs.readFileSync(path.join(dir, id, "analysis.json"), "utf8"));
  } catch {
    return null;
  }
}

/** One run: { run } | { missing } | { error }. Only a valid id reaches the disk (the route checks it). */
export function readRun(dir, id, gateOpen = L10_ANALYSIS_GATE_OPEN) {
  const r = readReport(dir, id);
  if (!r.text) return r;
  const report = JSON.parse(r.text);
  return { run: { ...runRow(id, report), analysis: analysisOf(dir, id, gateOpen), report } };
}
