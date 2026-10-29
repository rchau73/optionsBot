// Server-side only: the weekly usage reports cmd/usage writes (#295, #445),
// read for the admin Analytics page. Each environment's
// <clone>/data/monitor/reports is mounted read-only at
// <USAGE_REPORTS_DIR>/<env> (docker-compose.yml); the files are read as
// written, never recomputed.
import fs from "node:fs/promises";
import path from "node:path";
import { WEEK_RE } from "./usageView";

/** USAGE_REPORTS_DIR, default data/reports under the monitor's working directory. */
export function reportsDir(raw = process.env.USAGE_REPORTS_DIR) {
  return raw || path.join(process.cwd(), "data", "reports");
}

/** The weeks with a <week>.json report in dir, oldest first; none when the folder does not exist. */
export async function listWeeks(dir) {
  let names;
  try {
    names = await fs.readdir(dir);
  } catch (err) {
    if (err.code === "ENOENT") return [];
    throw err;
  }
  return names
    .filter((n) => n.endsWith(".json"))
    .map((n) => n.slice(0, -".json".length))
    .filter((w) => WEEK_RE.test(w))
    .sort();
}

/**
 * The report of `week` in dir → { report, writtenAt } (the file's time,
 * ISO), or null when there is none. The events file's path is cut to its
 * name: the host's folders are no business of the page.
 */
export async function readReport(dir, week) {
  if (!WEEK_RE.test(week)) return null;
  const file = path.join(dir, `${week}.json`);
  let raw, stat;
  try {
    [raw, stat] = await Promise.all([fs.readFile(file, "utf8"), fs.stat(file)]);
  } catch (err) {
    if (err.code === "ENOENT") return null;
    throw err;
  }
  const report = JSON.parse(raw);
  if (report?.source?.file) report.source.file = path.basename(report.source.file);
  return { report, writtenAt: stat.mtime.toISOString() };
}
