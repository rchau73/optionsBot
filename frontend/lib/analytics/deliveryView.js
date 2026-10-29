// The Analytics page's Delivery (DORA) topic (#473): pure helpers over the
// delivery report cmd/dora writes (<envs>/history/delivery/dora.json, the
// delivery.Report shape of internal/delivery/delivery.go). Nothing here
// recomputes a DORA figure: the tiles show the report's own numbers and
// bands, so the page and the Monday post on E6 agree. Times are UTC, as the
// report's.

/** The environments of the switch, production first: stable opens by default (#473). */
export const DELIVERY_ENVS = ["stable", "mvp"];

/** The switch's words for each environment. */
export const ENV_LABELS = { stable: "stable · production", mvp: "mvp · test" };

/** The page's two topics, in tab order. */
export const TOPICS = [
  { key: "usage", label: "Usage" },
  { key: "delivery", label: "Delivery (DORA)" },
];

/** Days a build runs on mvp before it may be promoted (scripts/promote.sh: the newest mvp deploy 3 days old). */
export const SOAK_DAYS = 3;

/** Days the release train shows, ending at the report's time. */
export const TRAIN_DAYS = 8;

/** Weeks of bars under each tile, the current week last. */
export const TILE_WEEKS = 4;

/** The repository, for a PR's link. */
export const REPO_URL = "https://github.com/rchau73/optionsBot";

const HOUR_MS = 3600 * 1000;
const DAY_MS = 24 * HOUR_MS;
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

const ms = (iso) => (iso ? Date.parse(iso) : NaN);
const isNum = (v) => typeof v === "number" && Number.isFinite(v);

/** The page's URL for the Delivery topic; no env = stable. */
export function deliveryHref(env = null) {
  const q = new URLSearchParams({ topic: "delivery" });
  if (env) q.set("env", env);
  return `/admin/analytics?${q}`;
}

/** "29 Oct 00:49" (UTC), or "—". */
export function utcText(t) {
  const d = new Date(typeof t === "number" ? t : ms(t));
  if (Number.isNaN(d.getTime())) return "—";
  const hh = String(d.getUTCHours()).padStart(2, "0");
  const mm = String(d.getUTCMinutes()).padStart(2, "0");
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${hh}:${mm}`;
}

/** Hours as cmd/dora prints them: "4 min", "5.2 h", "3.1 d"; "—" without a number. */
export function durationText(hours) {
  if (!isNum(hours)) return "—";
  if (hours < 1) return `${Math.round(hours * 60)} min`;
  if (hours < 48) return `${hours.toFixed(1)} h`;
  return `${(hours / 24).toFixed(1)} d`;
}

/** Hours in the headline's words: "minutes", "about 6 hours", "about 5 days". */
export function roughText(hours) {
  if (hours < 1) return "minutes";
  if (hours < 48) return `about ${Math.round(hours)} ${Math.round(hours) === 1 ? "hour" : "hours"}`;
  return `about ${Math.round(hours / 24)} days`;
}

function median(values) {
  if (!values.length) return null;
  const v = [...values].sort((a, b) => a - b);
  const mid = Math.floor(v.length / 2);
  return v.length % 2 ? v[mid] : (v[mid - 1] + v[mid]) / 2;
}

const hoursBetween = (from, to) => (ms(to) - ms(from)) / HOUR_MS;

/**
 * The one headline line: how long a merged PR takes to go live on mvp and on
 * stable, as the median over every PR of the report that did.
 */
export function headline(report) {
  const prs = report?.prs ?? [];
  const mvp = median(prs.filter((p) => p.mvp_live).map((p) => hoursBetween(p.merged, p.mvp_live)));
  const stable = median(prs.filter((p) => p.stable_live).map((p) => hoursBetween(p.merged, p.stable_live)));
  const first = mvp === null ? "No change has a recorded mvp deploy yet." : `Every change reaches mvp in ${roughText(mvp)}.`;
  const second =
    stable === null
      ? `Nothing has reached stable through a release yet: a release needs the ${SOAK_DAYS}-day soak on mvp.`
      : `Stable waits ${roughText(stable)} for the ${SOAK_DAYS}-day soak and a release.`;
  return `${first} ${second}`;
}

/** A band of the report as the tile's chip: ELITE, HIGH, MEDIUM, LOW; null without one. */
const bandChip = (band) => (band ? band.toUpperCase() : null);

/** A week's bar: { week, value (for the height, null = none), text, recorded }. Weeks not recorded say "n/r", never 0. */
function bar(w, env, pick) {
  const e = w[env];
  if (!e?.recorded) return { week: w.week, value: null, text: "n/r", recorded: false };
  const { value, text } = pick(e);
  return { week: w.week, value: isNum(value) ? value : null, text, recorded: true };
}

const FREQUENCY_COUNTS = {
  mvp: "Healthy mvp deploys a day this week (deploys.tsv); a rolled-back deploy is not one.",
  stable: "New releases a week: a tag promoted to stable for the first time; a rollback is not one.",
};
const LEAD_COUNTS = {
  mvp: "Median time from a PR's merge to the first healthy mvp deploy that carries it, for the PRs live this week.",
  stable: "Median time from a PR's merge to the first release that carries it, for the PRs released this week.",
};
const FAILURE_COUNTS =
  "Deploys that failed (a rollback, a deploy-failed issue, a revert or a regression bug) of all attempts this week; each failure counts once.";
const RESTORE_COUNTS = "Median time from a failure to the healthy deploy (or fix) that ended it, for the failures of this week.";

/**
 * The four tiles of one environment, for the latest week of the report (the
 * current one, in progress), each { value, chip, context, counts, bars }.
 * chip: the report's band, NO FAILURE (no failed deploy to measure) or NOT
 * YET (too few samples, or not recorded yet). bars: TILE_WEEKS weeks, oldest
 * first. null when the report has no week.
 */
export function deliveryTiles(report, env) {
  const weeks = report?.weeks ?? [];
  const w = weeks.at(-1);
  if (!w) return null;
  const last = weeks.slice(-TILE_WEEKS);
  const e = w[env] ?? {};
  const rec = Boolean(e.recorded);
  const what = env === "mvp" ? "deploys" : "releases";
  const notRecorded = (extra) => ({ value: "—", chip: "NOT YET", context: e.note || "not recorded yet", ...extra });

  const rate = env === "mvp" ? e.per_day : e.per_week;
  const frequency = {
    ...(rec
      ? {
          value: isNum(rate) ? `${rate.toFixed(2)} ${env === "mvp" ? "a day" : "a week"}` : `${e.deploys} ${what}`,
          chip: bandChip(e.frequency_band) ?? "NOT YET",
          context: `${e.deploys} ${e.deploys === 1 ? what.slice(0, -1) : what} this week so far`,
        }
      : notRecorded()),
    counts: FREQUENCY_COUNTS[env],
    bars: last.map((x) => bar(x, env, (v) => ({ value: v.deploys, text: String(v.deploys) }))),
  };

  const lt = e.merge_to_live ?? {};
  const lead = {
    ...(rec
      ? {
          value: durationText(lt.median_hours),
          chip: bandChip(lt.band) ?? "NOT YET",
          context: isNum(lt.median_hours) ? `p90 ${durationText(lt.p90_hours)} · ${lt.n} ${lt.n === 1 ? "PR" : "PRs"}` : lt.note || "no PR live this week",
        }
      : notRecorded()),
    counts: LEAD_COUNTS[env],
    bars: last.map((x) => bar(x, env, (v) => ({ value: v.merge_to_live?.median_hours, text: durationText(v.merge_to_live?.median_hours) }))),
  };

  const noFailure = rec && e.attempts > 0 && e.failed === 0;
  const failure = {
    ...(rec
      ? {
          value: isNum(e.failure_rate_pct) ? `${Math.round(e.failure_rate_pct)} %` : "—",
          chip: bandChip(e.failure_band) ?? (noFailure ? "NO FAILURE" : "NOT YET"),
          context: e.attempts > 0 ? `${e.failed} of ${e.attempts} ${what} failed` : e.failure_note || `no ${what} this week`,
        }
      : notRecorded()),
    counts: FAILURE_COUNTS,
    bars: last.map((x) =>
      bar(x, env, (v) => ({ value: v.failure_rate_pct, text: isNum(v.failure_rate_pct) ? `${Math.round(v.failure_rate_pct)} %` : "—" })),
    ),
  };

  const rs = e.restore ?? {};
  const restoreContext = () => {
    if (e.failed === 0) return "nothing to restore this week";
    const restored = isNum(rs.median_hours) ? `${rs.n} restored, p90 ${durationText(rs.p90_hours)}` : rs.note || "none restored yet";
    return e.unrestored > 0 ? `${restored}; ${e.unrestored} not restored yet` : restored;
  };
  const restore = {
    ...(rec
      ? {
          value: durationText(rs.median_hours),
          chip: bandChip(rs.band) ?? (e.failed === 0 ? "NO FAILURE" : "NOT YET"),
          context: restoreContext(),
        }
      : notRecorded()),
    counts: RESTORE_COUNTS,
    bars: last.map((x) => bar(x, env, (v) => ({ value: v.restore?.median_hours, text: durationText(v.restore?.median_hours) }))),
  };

  return { week: w.week, frequency, lead, failure, restore };
}

/** The report's failures by id. */
const failureIndex = (report) => new Map((report?.failures ?? []).map((f) => [f.id, f]));

const sameCommit = (a, b) => Boolean(a && b && (a.startsWith(b) || b.startsWith(a)));

/**
 * Whether the PR's change was rolled back: a revert of it, or an mvp
 * rollback / failed deploy of its merge commit (the "[E6] mvp deploy failed:
 * <sha>" issue names it).
 */
export function rolledBack(pr, report) {
  const byId = failureIndex(report);
  if ((pr.failures ?? []).some((id) => byId.get(id)?.kind === "revert")) return true;
  return (report?.failures ?? []).some((f) => (f.kind === "rollback" || f.kind === "deploy_failed") && sameCommit(f.commit, pr.commit));
}

/** The result chip of a PR: ROLLED BACK, ON STABLE, LIVE ON MVP or NOT LIVE. */
export function resultOf(pr, report) {
  if (rolledBack(pr, report)) return "ROLLED BACK";
  if (pr.stable_live) return "ON STABLE";
  if (pr.mvp_live) return "LIVE ON MVP";
  return "NOT LIVE";
}

/** When the PR's soak on mvp ends (ms), or null when it is not live on mvp. */
export const soakEndOf = (pr) => (pr.mvp_live ? ms(pr.mvp_live) + SOAK_DAYS * DAY_MS : null);

/** "29 Oct 00:49 · +4 min" (live on mvp and minutes after the merge), or the report's note. */
export function mvpText(pr) {
  if (!pr.mvp_live) return pr.mvp_note || "—";
  return `${utcText(pr.mvp_live)} · +${durationText(hoursBetween(pr.merged, pr.mvp_live))}`;
}

/** "28 Oct 11:43 (v0.3.0)", or "waiting · soak ends 1 Nov 00:49" while on mvp, or the report's note. */
export function stableText(pr, now) {
  if (pr.stable_live) return `${utcText(pr.stable_live)}${pr.release ? ` (${pr.release})` : ""}`;
  const end = soakEndOf(pr);
  if (end === null) return pr.stable_note || "—";
  return end > now ? `waiting · soak ends ${utcText(end)}` : "waiting · soak over, waits for the next release";
}

/** The report's time (ms): "now" for every relative figure, so the page agrees with the file. */
export const reportNow = (report) => ms(report?.generated);

/** Every merged PR as a row of What went live. */
export function prRows(report) {
  const now = reportNow(report);
  return (report?.prs ?? []).map((pr) => ({
    pr,
    number: pr.number,
    title: pr.title || pr.branch || "",
    epic: pr.epic || "",
    merged: ms(pr.merged),
    mergedText: utcText(pr.merged),
    mvp: pr.mvp_live ? ms(pr.mvp_live) : null,
    mvpText: mvpText(pr),
    stable: pr.stable_live ? ms(pr.stable_live) : null,
    stableText: stableText(pr, now),
    result: resultOf(pr, report),
  }));
}

/** The table's filters. */
export const ROW_FILTERS = [
  { key: "all", label: "All" },
  { key: "mvp", label: "On mvp only" },
  { key: "stable", label: "On stable" },
];

/**
 * The rows a filter and a search keep. Search (case-insensitive) matches the
 * PR's number ("458" or "#458"), its epic, its title, or a date of it as shown
 * ("29 Oct") or as an ISO day ("2026-10-29").
 */
export function filterRows(rows, filter = "all", query = "") {
  const q = query.trim().toLowerCase();
  return rows.filter((r) => {
    if (filter === "mvp" && !(r.mvp !== null && r.stable === null)) return false;
    if (filter === "stable" && r.stable === null) return false;
    if (!q) return true;
    const days = [r.merged, r.mvp, r.stable].filter((t) => t !== null).map((t) => new Date(t).toISOString().slice(0, 10));
    const hay = [`#${r.number}`, r.epic, r.title, r.mergedText, r.mvpText, r.stableText, ...days].join(" ").toLowerCase();
    return hay.includes(q);
  });
}

/** The sortable columns: key → the row's value. A row without the value sorts last either way. */
export const SORT_KEYS = { number: (r) => r.number, merged: (r) => r.merged, mvp: (r) => r.mvp, stable: (r) => r.stable };

/** Rows sorted by a column, "desc" (newest first, the default) or "asc". */
export function sortRows(rows, key = "merged", dir = "desc") {
  const get = SORT_KEYS[key] ?? SORT_KEYS.merged;
  const sign = dir === "asc" ? 1 : -1;
  return [...rows].sort((a, b) => {
    const va = get(a);
    const vb = get(b);
    if (va === null && vb === null) return b.number - a.number;
    if (va === null) return 1;
    if (vb === null) return -1;
    return sign * (va - vb) || b.number - a.number;
  });
}

/**
 * Where a PR's time went, as the parts of one bar: first commit → merge,
 * merge → mvp, mvp → stable (or → the report's time while it waits), each
 * { key, label, hours, text, waiting }. A part without both ends is left out.
 */
export function timeParts(pr, now) {
  const parts = [];
  const add = (key, label, from, to, waiting = false) => {
    if (!from || !Number.isFinite(to)) return;
    const hours = Math.max(0, (to - ms(from)) / HOUR_MS);
    parts.push({ key, label, hours, text: durationText(hours), waiting });
  };
  add("review", "First commit → merge", pr.first_commit, ms(pr.merged));
  add("mvp", "Merge → live on mvp", pr.merged, ms(pr.mvp_live));
  if (pr.stable_live) add("stable", "mvp → live on stable", pr.mvp_live, ms(pr.stable_live));
  else add("stable", "On mvp, waiting for stable", pr.mvp_live, now, true);
  return parts;
}

/** The Activity window of a PR: 1 h before to 24 h after live on mvp (ISO), or null. */
export function activityWindow(pr) {
  if (!pr.mvp_live) return null;
  const t = ms(pr.mvp_live);
  return { from: new Date(t - HOUR_MS).toISOString(), to: new Date(t + DAY_MS).toISOString() };
}

/** The Activity page around a PR's mvp deploy. The Activity page does not read from/to yet: the link's text gives the window. */
export function activityHref(window, bot = "btc") {
  const q = new URLSearchParams({ bot, from: window.from, to: window.to });
  return `/activity?${q}`;
}

/** The PR on GitHub. */
export const prHref = (number) => `${REPO_URL}/pull/${number}`;

/** A time's place on the train (0–100 %), or null outside the window. */
function place(t, from, to) {
  if (!Number.isFinite(t) || t < from || t > to) return null;
  return ((t - from) / (to - from)) * 100;
}

/**
 * The release train over the TRAIN_DAYS days before the report's time:
 * stable's releases (a mark each), mvp's deploys (a tick each), the day
 * starts (axis labels) and, for an open PR, its dot on each lane — filled
 * where it went live, hollow at the right edge while it waits ({ x, filled,
 * at }); a live time before the window is pinned to the left edge.
 */
export function releaseTrain(report, pr = null) {
  const to = reportNow(report);
  const from = to - TRAIN_DAYS * DAY_MS;
  const releases = (report?.releases ?? [])
    .map((r) => ({ tag: r.tag, at: r.at, failed: Boolean(r.failed), x: place(ms(r.at), from, to) }))
    .filter((r) => r.x !== null);
  const deploys = (report?.mvp_deploys ?? []).map((d) => ({ at: d.at, x: place(ms(d.at), from, to) })).filter((d) => d.x !== null);
  const days = [];
  for (let t = Math.ceil(from / DAY_MS) * DAY_MS; t <= to; t += DAY_MS) days.push({ x: place(t, from, to), text: utcText(t).slice(0, -6) });
  const dot = (iso) => {
    if (!iso) return { x: 100, filled: false, at: null };
    const t = ms(iso);
    return { x: t < from ? 0 : (place(t, from, to) ?? 100), filled: true, at: iso };
  };
  return {
    from,
    to,
    releases,
    deploys,
    days,
    open: pr ? { number: pr.number, mvp: dot(pr.mvp_live), stable: dot(pr.stable_live) } : null,
  };
}

/** The one line under the train: where the open PR went live. */
export function trainLine(pr, now) {
  if (!pr) return "Open a PR in What went live to place it on the lanes.";
  if (!pr.mvp_live) return `#${pr.number} is not live on mvp: ${pr.mvp_note || "no deploy carries it yet"}.`;
  const mvp = `#${pr.number} went live on mvp ${utcText(pr.mvp_live)}`;
  if (pr.stable_live) return `${mvp} and on stable ${utcText(pr.stable_live)}${pr.release ? ` (${pr.release})` : ""}.`;
  return `${mvp}; stable: ${stableText(pr, now)}.`;
}

/** The summary dora.sh posts on E6: the latest week that has ended by the report's time → { week, text }, or null. */
export function weeklySummary(report) {
  const now = reportNow(report);
  const ended = (report?.weeks ?? []).filter((w) => ms(w.to) <= now && w.summary);
  const w = ended.at(-1);
  return w ? { week: w.week, text: w.summary } : null;
}
