// Read-only access to the comparison reports cmd/compare writes (#302):
// <COMPARE_DIR>/<UTC time>/report.json. Nothing here writes.
import fs from "node:fs";
import path from "node:path";

export const LIST_LIMIT = 50;

// The folder name cmd/compare gives a report: compare.OutDir, 20060102T150405Z.
const ID = /^\d{8}T\d{6}Z$/;

/** COMPARE_DIR, default data/compare in the clone. */
export function compareDir(raw = process.env.COMPARE_DIR) {
  return raw || path.join(process.cwd(), "data", "compare");
}

export const validId = (id) => typeof id === "string" && ID.test(id);

/** A file name only, never a path (the report may hold the author's absolute paths). */
export const baseName = (p) => (typeof p === "string" ? p.split(/[\\/]/).filter(Boolean).pop() ?? "" : "");

/** The list row of one parsed report. */
export function summarize(id, report) {
  return {
    id,
    underlying: report.underlying ?? "",
    base: baseName(report.base),
    proposed: baseName(report.proposed),
    from: report.from ?? null,
    to: report.to ?? null,
    ran: {
      backtest: report.backtest?.ran === true,
      replay: report.replay?.ran === true,
      stress: report.stress?.ran === true,
    },
  };
}

/**
 * The reports of `dir`, newest first, at most `limit`. A folder that is not a
 * report id, or whose report.json is missing or unreadable, is skipped (and
 * named in `skipped`, ids only). A missing dir is an empty list.
 */
export function listReports(dir, limit = LIST_LIMIT) {
  let names;
  try {
    names = fs.readdirSync(dir);
  } catch {
    return { reports: [], skipped: [] };
  }
  const reports = [];
  const skipped = [];
  for (const id of names.filter(validId).sort().reverse()) {
    if (reports.length >= limit) break;
    try {
      reports.push(summarize(id, JSON.parse(fs.readFileSync(path.join(dir, id, "report.json"), "utf8"))));
    } catch {
      skipped.push(id);
    }
  }
  return { reports, skipped };
}

/** The stored text of one report: { text } | { missing: true } | { error: true }. Only a valid id reaches the disk. */
export function readReport(dir, id) {
  const file = path.join(dir, id, "report.json");
  let text;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch (e) {
    return e?.code === "ENOENT" || e?.code === "ENOTDIR" ? { missing: true } : { error: true };
  }
  try {
    JSON.parse(text);
  } catch {
    return { error: true };
  }
  return { text };
}
