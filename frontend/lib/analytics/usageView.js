// The admin Analytics page (#445): pure helpers over the weekly usage report
// that cmd/usage writes (data/monitor/reports/<week>.json, the usage.Report
// shape of internal/usage/report.go). Nothing here recomputes a figure: it
// only picks, labels and formats what the report already holds, so the page,
// the E13 post and the drop alert always agree.

/** The environments whose reports the page reads. */
export const REPORT_ENVS = ["stable", "mvp"];

/** An ISO week label as cmd/usage names its files ("2026-W44"). */
export const WEEK_RE = /^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$/;

/** Under this many active people the week is a small sample: usage.DropMinBase (internal/usage/drops.go), the drop check's base rule. */
export const SMALL_SAMPLE = 10;

/** Under this many answers no NPS is given: usage.NPSMinAnswers. */
export const NPS_MIN_ANSWERS = 10;

/** Q4's target: the four answers seen within 5 minutes of the first session (E3). */
export const FIRST_ANSWERS_TARGET_MS = 5 * 60 * 1000;

/**
 * ?env=&week= of the page or its route → { ok, env, week } (week null = the
 * latest report) or { ok: false, error }. env defaults to this monitor's own
 * deployment (MONITOR_DEPLOYMENT) when it is one of REPORT_ENVS, else stable.
 */
export function parseAnalyticsQuery(params, ownEnv = null) {
  const get = (k) => (typeof params?.get === "function" ? params.get(k) : params?.[k]) ?? null;
  const env = get("env") || (REPORT_ENVS.includes(ownEnv) ? ownEnv : REPORT_ENVS[0]);
  if (!REPORT_ENVS.includes(env)) return { ok: false, error: `env: one of ${REPORT_ENVS.join(", ")}` };
  const week = get("week") || null;
  if (week !== null && !WEEK_RE.test(week)) return { ok: false, error: "week: an ISO week such as 2026-W44" };
  return { ok: true, env, week };
}

/** The ISO week label ("2026-W44") of a date, in UTC. */
export function isoWeekOf(date) {
  const d = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()));
  const day = d.getUTCDay() || 7; // Monday 1 … Sunday 7
  d.setUTCDate(d.getUTCDate() + 4 - day); // the week's Thursday names its year
  const year = d.getUTCFullYear();
  const week = Math.ceil(((d - Date.UTC(year, 0, 1)) / 86400000 + 1) / 7);
  return `${year}-W${String(week).padStart(2, "0")}`;
}

/** The page's URL for an environment and a week (null = the latest). */
export function analyticsHref(env, week = null) {
  const q = new URLSearchParams({ env });
  if (week) q.set("week", week);
  return `/admin/analytics?${q}`;
}

/** The weeks before and after `week` among the reported ones (sorted oldest first) → { older, newer }, null at either end. */
export function neighbourWeeks(weeks = [], week = null) {
  const i = weeks.indexOf(week);
  if (i < 0) return { older: weeks.at(-1) ?? null, newer: null };
  return { older: weeks[i - 1] ?? null, newer: weeks[i + 1] ?? null };
}

/** True while the week's active people are under SMALL_SAMPLE (a week with no events is not a sample at all). */
export function isSmallSample(report) {
  const users = report?.active?.week?.users;
  return typeof users === "number" && users < SMALL_SAMPLE;
}

const METRICS = {
  active_users: "Active people",
  help_used: "Help used (tips + videos)",
  first_answers: "First answers found",
  manual_close: "Manual-close funnel",
  rebalance: "Rebalance funnel",
};

/** A drop-check metric in plain words. */
export const metricLabel = (m) => METRICS[m] ?? m;

const round = (v) => Math.round(v);
const signed = (v) => `${v > 0 ? "+" : v < 0 ? "−" : ""}${Math.abs(round(v))}`;

/** One drop of the report's `drops` in one line, as the drop issue says it. */
export function dropText(d) {
  if (d.kind === "funnel") {
    return `${metricLabel(d.metric)}: ${round(d.current)} % of ${d.starts} starts, was ${round(d.base)} % of ${d.base_starts} (${signed(d.change_pts)} pts)`;
  }
  return `${metricLabel(d.metric)}: ${round(d.current)}, mean of the weeks before ${d.base.toFixed(1)} (${signed(d.change_pct)} %)`;
}

/** The screens of the week with no view, in the plan's order; "login" is never sent, so it is left out. */
export function neverOpenedScreens(report) {
  const screens = report?.panels?.screens ?? {};
  return Object.keys(screens).filter((s) => s !== "login" && screens[s] === 0);
}

/**
 * "Needs your attention", most urgent first: each drop (DROP), the flagged
 * assistant answers (FLAG), never-opened screens and panels (NEVER). Empty
 * when nothing needs a look. Each item { tag, text }.
 */
export function attentionItems(report) {
  if (!report || !report.events_in_week) return [];
  const items = (report.drops?.drops ?? []).map((d) => ({ tag: "DROP", text: dropText(d) }));
  const flagged = report.assistant?.flagged ?? 0;
  if (flagged > 0) {
    items.push({
      tag: "FLAG",
      text: `${flagged} assistant ${flagged === 1 ? "answer" : "answers"} flagged: see the weekly assistant review, data/monitor/assistant_reviews/${report.week ?? "<week>"}.md`,
    });
  }
  const screens = neverOpenedScreens(report);
  if (screens.length) items.push({ tag: "NEVER", text: `Screens never opened: ${screens.join(", ")}` });
  const panels = report.panels?.never_opened ?? [];
  if (panels.length) items.push({ tag: "NEVER", text: `Panels never opened: ${panels.join(", ")}` });
  return items;
}

/** A share in % ("42 %"), or "—" when the report has none (nothing to divide by). */
export const pctText = (v) => (typeof v === "number" ? `${round(v)} %` : "—");

/** Milliseconds as minutes ("2.5 min"), or "—". */
export const minutesText = (ms) => (typeof ms === "number" ? `${(ms / 60000).toFixed(1)} min` : "—");

/** The sum of a count map (e.g. tips opened per tip). */
export const total = (m) => Object.values(m ?? {}).reduce((a, b) => a + b, 0);

/** A count map as [key, count] rows, most first, ties by key. */
export function countRows(m) {
  return Object.entries(m ?? {}).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}
